package migration

// This test-only driver is a scripted, in-process stub. It has no network,
// filesystem database, DSN, or environment-variable access. Tests assert GORM's
// query/transaction ordering; they do not claim to emulate PostgreSQL semantics.
import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type sqlStep struct {
	kind    string
	prefix  string
	columns []string
	rows    [][]driver.Value
	err     error
	run     func(context.Context) error
}

type sqlCall struct {
	kind          string
	query         string
	args          []driver.NamedValue
	connection    int
	inTransaction bool
}

type sqlScript struct {
	mu         sync.Mutex
	steps      []sqlStep
	calls      []sqlCall
	unexpected []string
	gate       chan struct{}
	owner      *scriptConn
	nextID     int
	lockErr    error
	unlockErr  error
	closed     int
}

func (s *sqlScript) call(ctx context.Context, conn *scriptConn, kind, query string, args []driver.NamedValue) (sqlStep, error) {
	s.mu.Lock()
	s.calls = append(s.calls, sqlCall{kind, query, args, conn.id, conn.inTransaction})
	if len(s.steps) == 0 || s.steps[0].kind != kind || !strings.HasPrefix(query, s.steps[0].prefix) {
		msg := fmt.Sprintf("unexpected %s %q (remaining: %+v)", kind, query, s.steps)
		s.unexpected = append(s.unexpected, msg)
		s.mu.Unlock()
		return sqlStep{}, errors.New(msg)
	}
	if s.owner != conn {
		msg := fmt.Sprintf("%s executed without owning the migration lock on connection %d", kind, conn.id)
		s.unexpected = append(s.unexpected, msg)
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	s.mu.Unlock()
	if step.run != nil {
		return step, step.run(ctx)
	}
	return step, step.err
}

func (s *sqlScript) append(steps ...sqlStep) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, steps...)
}

func (s *sqlScript) snapshot() []sqlCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sqlCall(nil), s.calls...)
}

type scriptConnector struct{ script *sqlScript }

func (c scriptConnector) Connect(context.Context) (driver.Conn, error) {
	c.script.mu.Lock()
	defer c.script.mu.Unlock()
	c.script.nextID++
	return &scriptConn{script: c.script, id: c.script.nextID}, nil
}
func (c scriptConnector) Driver() driver.Driver { return scriptDriver{} }

type scriptDriver struct{}

func (scriptDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("mock only supports its in-process connector")
}

type scriptConn struct {
	script        *sqlScript
	id            int
	inTransaction bool
}

func (c *scriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *scriptConn) Close() error {
	s := c.script
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	if s.owner == c {
		s.owner = nil
		s.gate <- struct{}{}
	}
	return nil
}
func (c *scriptConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *scriptConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if _, err := c.script.call(ctx, c, "begin", "", nil); err != nil {
		return nil, err
	}
	c.inTransaction = true
	return scriptTx{c}, nil
}
func (c *scriptConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s := c.script
	if strings.HasPrefix(query, "SELECT pg_advisory_lock(") {
		if s.lockErr != nil {
			return nil, s.lockErr
		}
		if len(args) != 1 || args[0].Value != migrationLockKey {
			return nil, errors.New("unexpected advisory lock key")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.gate:
			s.mu.Lock()
			s.owner = c
			s.calls = append(s.calls, sqlCall{kind: "lock", connection: c.id})
			s.mu.Unlock()
			return driver.RowsAffected(1), nil
		}
	}
	_, err := s.call(ctx, c, "exec", query, args)
	return driver.RowsAffected(1), err
}
func (c *scriptConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.script
	if strings.HasPrefix(query, "SELECT pg_advisory_unlock(") {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.unlockErr != nil {
			return nil, s.unlockErr
		}
		if len(args) != 1 || args[0].Value != migrationLockKey {
			return nil, errors.New("unexpected advisory unlock key")
		}
		s.mu.Lock()
		owned := s.owner == c
		if owned {
			s.owner = nil
			s.gate <- struct{}{}
		}
		s.calls = append(s.calls, sqlCall{kind: "unlock", connection: c.id})
		s.mu.Unlock()
		return &scriptRows{columns: []string{"pg_advisory_unlock"}, values: [][]driver.Value{{owned}}}, nil
	}
	step, err := s.call(ctx, c, "query", query, args)
	if err != nil {
		return nil, err
	}
	return &scriptRows{columns: step.columns, values: step.rows}, nil
}

type scriptTx struct{ conn *scriptConn }

func (tx scriptTx) Commit() error {
	_, err := tx.conn.script.call(context.Background(), tx.conn, "commit", "", nil)
	tx.conn.inTransaction = false
	return err
}
func (tx scriptTx) Rollback() error {
	_, err := tx.conn.script.call(context.Background(), tx.conn, "rollback", "", nil)
	tx.conn.inTransaction = false
	return err
}

type scriptRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *scriptRows) Columns() []string { return r.columns }
func (r *scriptRows) Close() error      { return nil }
func (r *scriptRows) Next(dest []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

// A minimal test dialect registers GORM's standard callbacks and double-quoted
// identifiers. It is never exposed by production code as a supported database.
type scriptDialect struct {
	pool *sql.DB
	name string
}

func (d scriptDialect) Name() string { return d.name }
func (d scriptDialect) Initialize(db *gorm.DB) error {
	db.ConnPool = d.pool
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
	return nil
}
func (scriptDialect) Migrator(*gorm.DB) gorm.Migrator { return nil }
func (scriptDialect) DataTypeOf(*schema.Field) string { return "" }
func (scriptDialect) DefaultValueOf(*schema.Field) clause.Expression {
	return clause.Expr{SQL: "DEFAULT"}
}
func (scriptDialect) BindVarTo(w clause.Writer, _ *gorm.Statement, _ interface{}) {
	_ = w.WriteByte('?')
}
func (scriptDialect) QuoteTo(w clause.Writer, name string) {
	if name == "*" {
		_, _ = w.WriteString(name)
		return
	}
	_, _ = w.WriteString(`"` + strings.ReplaceAll(name, `"`, `""`) + `"`)
}
func (scriptDialect) Explain(query string, _ ...interface{}) string { return query }

func mockDB(t *testing.T) (*gorm.DB, *sqlScript) {
	t.Helper()
	s := &sqlScript{gate: make(chan struct{}, 1)}
	s.gate <- struct{}{}
	pool := sql.OpenDB(scriptConnector{s})
	pool.SetMaxOpenConns(4)
	db, err := gorm.Open(scriptDialect{pool: pool, name: "postgres"}, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	requireOK(t, err)
	t.Cleanup(func() {
		requireOK(t, pool.Close())
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.steps) != 0 {
			t.Errorf("unconsumed SQL expectations: %+v", s.steps)
		}
		for _, message := range s.unexpected {
			t.Error(message)
		}
	})
	return db, s
}

func expectState(s *sqlScript, rows ...migrationRow) {
	var values [][]driver.Value
	for _, row := range rows {
		values = append(values, []driver.Value{int64(row.Version), row.Name, row.Checksum, row.Dirty, row.AppliedAt})
	}
	s.append(sqlStep{kind: "begin"}, sqlStep{kind: "exec", prefix: `CREATE TABLE IF NOT EXISTS "schema_migrations"`},
		sqlStep{kind: "exec", prefix: `ALTER TABLE "schema_migrations"`}, sqlStep{kind: "commit"},
		sqlStep{kind: "query", prefix: `SELECT * FROM "schema_migrations" ORDER BY version ASC`,
			columns: []string{"version", "name", "checksum", "dirty", "applied_at"}, rows: values})
}

func expectUp(s *sqlScript, version int, fail error) {
	s.append(sqlStep{kind: "exec", prefix: `INSERT INTO "schema_migrations"`}, sqlStep{kind: "begin"},
		sqlStep{kind: "exec", prefix: fmt.Sprintf("UP %d", version), err: fail})
	if fail != nil {
		s.append(sqlStep{kind: "rollback"})
		return
	}
	s.append(sqlStep{kind: "exec", prefix: `UPDATE "schema_migrations" SET "applied_at"=?,"dirty"=? WHERE version = ?`}, sqlStep{kind: "commit"})
}

func expectDown(s *sqlScript, version int, fail error) {
	s.append(sqlStep{kind: "exec", prefix: `UPDATE "schema_migrations" SET "dirty"=? WHERE version = ?`},
		sqlStep{kind: "begin"}, sqlStep{kind: "exec", prefix: fmt.Sprintf("DOWN %d", version), err: fail})
	if fail != nil {
		s.append(sqlStep{kind: "rollback"})
		return
	}
	s.append(sqlStep{kind: "exec", prefix: `DELETE FROM "schema_migrations" WHERE version = ?`}, sqlStep{kind: "commit"})
}

func appliedRows(t *testing.T) []migrationRow {
	t.Helper()
	items, err := NewMigrator(nil, testSource()).loadMigrations()
	requireOK(t, err)
	rows := make([]migrationRow, 0, len(items))
	for _, item := range items {
		rows = append(rows, migrationRow{Version: item.Version, Name: item.Name, Checksum: checksum(item)})
	}
	return rows
}
