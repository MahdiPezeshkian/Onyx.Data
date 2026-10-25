package migration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"gorm.io/gorm"
)

func TestUp(t *testing.T) {
	tests := []struct {
		name     string
		existing int
		steps    int
		apply    []int
	}{
		{"fresh database", 0, 0, []int{1, 2}},
		{"already applied", 2, 0, nil},
		{"partially pending", 1, 0, []int{2}},
		{"one step", 0, 1, []int{1}},
		{"two steps", 0, 2, []int{1, 2}},
		{"excess up steps applies available", 0, 5, []int{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, script := mockDB(t)
			expectState(script, appliedRows(t)[:tt.existing]...)
			for _, version := range tt.apply {
				expectUp(script, version, nil)
			}
			m := NewMigrator(db, testSource())
			if tt.steps == 0 {
				requireOK(t, m.Up(context.Background()))
			} else {
				requireOK(t, m.UpSteps(context.Background(), tt.steps))
			}
			assertMigrationTransactions(t, script, DirectionUp, len(tt.apply))
		})
	}
}

func assertMigrationTransactions(t *testing.T, script *sqlScript, direction Direction, count int) {
	t.Helper()
	markers, migrations := 0, 0
	for _, call := range script.snapshot() {
		if strings.HasPrefix(call.query, "UP ") || strings.HasPrefix(call.query, "DOWN ") {
			migrations++
			if !call.inTransaction {
				t.Errorf("migration SQL ran outside transaction: %+v", call)
			}
		}
		marker := strings.HasPrefix(call.query, `INSERT INTO "schema_migrations"`)
		if direction == DirectionDown {
			marker = strings.HasPrefix(call.query, `UPDATE "schema_migrations" SET "dirty"=`)
		}
		if marker {
			markers++
			if call.inTransaction {
				t.Error("dirty marker would roll back with migration")
			}
			foundDirty := false
			for _, arg := range call.args {
				if arg.Value == true {
					foundDirty = true
				}
			}
			if !foundDirty {
				t.Error("dirty marker did not set dirty=true")
			}
		}
		if strings.HasPrefix(call.query, `UPDATE "schema_migrations" SET "applied_at"=`) || strings.HasPrefix(call.query, `DELETE FROM "schema_migrations"`) {
			if !call.inTransaction {
				t.Error("final migration history change must be transactional")
			}
		}
	}
	if markers != count || migrations != count {
		t.Fatalf("markers=%d, migrations=%d; want %d", markers, migrations, count)
	}
}

func TestFailedUpStopsAndLeavesDirtyMarker(t *testing.T) {
	db, script := mockDB(t)
	expectedErr := errors.New("deliberate SQL failure")
	expectState(script)
	expectUp(script, 1, nil)
	expectUp(script, 2, expectedErr)
	m := NewMigrator(db, testSource())
	err := m.Up(context.Background())
	if !errors.Is(err, expectedErr) {
		t.Fatalf("error = %v", err)
	}
	assertMigrationTransactions(t, script, DirectionUp, 2)
	rows := appliedRows(t)
	rows[1].Dirty = true
	expectState(script, rows...)
	requireError(t, m.Up(context.Background()), "dirty at version 2")
	// Dirty history can be inspected without loading files.
	expectState(script, rows...)
	version, dirty, err := NewMigrator(db, nil).Version(context.Background())
	requireOK(t, err)
	if version != 2 || !dirty {
		t.Fatalf("version=%d, dirty=%v", version, dirty)
	}
}

func TestHistoryValidationBlocksUpAndDown(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]migrationRow)
		source fstest.MapFS
		want   string
	}{
		{"dirty", func(rows []migrationRow) { rows[0].Dirty = true }, testSource(), "manual intervention"},
		{"checksum", func(rows []migrationRow) { rows[0].Checksum = "changed" }, testSource(), "stored checksum: changed"},
		{"name", func(rows []migrationRow) { rows[0].Name = "old_name" }, testSource(), "name mismatch"},
		{"legacy", func(rows []migrationRow) { rows[0].Checksum = "" }, testSource(), "call Baseline explicitly"},
		{"missing files", func([]migrationRow) {}, pair("002", "second", "UP 2", "DOWN 2"), "migration files are missing"},
	}
	for _, tt := range tests {
		for _, direction := range []Direction{DirectionUp, DirectionDown} {
			t.Run(tt.name+"/"+string(direction), func(t *testing.T) {
				db, script := mockDB(t)
				rows := appliedRows(t)
				tt.mutate(rows)
				expectState(script, rows...)
				m := NewMigrator(db, tt.source)
				var err error
				if direction == DirectionUp {
					err = m.Up(context.Background())
				} else {
					err = m.Down(context.Background(), 1)
				}
				requireError(t, err, tt.want)
			})
		}
	}
}

func TestDown(t *testing.T) {
	tests := []struct {
		name            string
		existing, steps int
		all             bool
		rollback        []int
		wantErr         string
	}{
		{"one", 2, 1, false, []int{2}, ""},
		{"multiple", 2, 2, false, []int{2, 1}, ""},
		{"all", 2, 0, true, []int{2, 1}, ""},
		{"all empty", 0, 0, true, nil, ""},
		{"too many", 2, 100, false, nil, "only 2 are applied"},
		{"empty", 0, 1, false, nil, "only 0 are applied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, script := mockDB(t)
			expectState(script, appliedRows(t)[:tt.existing]...)
			for _, version := range tt.rollback {
				expectDown(script, version, nil)
			}
			m := NewMigrator(db, testSource())
			var err error
			if tt.all {
				err = m.DownAll(context.Background())
			} else {
				err = m.Down(context.Background(), tt.steps)
			}
			if tt.wantErr != "" {
				requireError(t, err, tt.wantErr)
			} else {
				requireOK(t, err)
			}
			assertMigrationTransactions(t, script, DirectionDown, len(tt.rollback))
		})
	}
}

func TestDownMissingSQLAndFailure(t *testing.T) {
	t.Run("missing down file", func(t *testing.T) {
		db, _ := mockDB(t)
		source := testSource()
		delete(source, "002_second.down.sql")
		requireError(t, NewMigrator(db, source).Down(context.Background(), 1), "missing down file")
	})
	t.Run("failed rollback", func(t *testing.T) {
		db, script := mockDB(t)
		expectState(script, appliedRows(t)...)
		failure := errors.New("rollback SQL failed")
		expectDown(script, 2, failure)
		m := NewMigrator(db, testSource())
		if err := m.DownAll(context.Background()); !errors.Is(err, failure) {
			t.Fatalf("error = %v", err)
		}
		assertMigrationTransactions(t, script, DirectionDown, 1)
		rows := appliedRows(t)
		rows[1].Dirty = true
		expectState(script, rows...)
		requireError(t, m.Down(context.Background(), 1), "dirty at version 2")
	})
}

func TestLegacyUpgradeAndBaseline(t *testing.T) {
	db, script := mockDB(t)
	rows := appliedRows(t)
	rows[0].Checksum = ""
	rows[0].AppliedAt = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	expectState(script, rows...)
	script.append(sqlStep{kind: "begin"}, sqlStep{kind: "exec", prefix: `UPDATE "schema_migrations" SET "checksum"=? WHERE version = ?`}, sqlStep{kind: "commit"})
	m := NewMigrator(db, testSource())
	requireOK(t, m.Baseline(context.Background()))
	updates := 0
	for _, call := range script.snapshot() {
		if strings.HasPrefix(call.query, "ALTER TABLE") {
			if !strings.Contains(call.query, "ADD COLUMN IF NOT EXISTS checksum TEXT") || !strings.Contains(call.query, "ADD COLUMN IF NOT EXISTS dirty BOOLEAN NOT NULL DEFAULT FALSE") {
				t.Fatalf("unsafe legacy upgrade: %s", call.query)
			}
		}
		if strings.HasPrefix(call.query, "UPDATE") {
			updates++
			if strings.Contains(call.query, "applied_at") || strings.Contains(call.query, "name") {
				t.Fatal("baseline changes historical metadata")
			}
			if call.args[0].Value != appliedRows(t)[0].Checksum || call.args[1].Value != int64(1) {
				t.Fatalf("wrong baseline arguments: %+v", call.args)
			}
		}
	}
	if updates != 1 {
		t.Fatalf("baseline updated %d rows, want 1", updates)
	}
	// Baseline must never bless a changed checksum that already exists.
	rows[1].Checksum = "incorrect"
	expectState(script, rows...)
	requireError(t, m.Baseline(context.Background()), "modified after it was applied")
}

func TestForceRecovery(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			db, script := mockDB(t)
			rows := appliedRows(t)
			rows[1].Dirty = true
			expectState(script, rows...)
			script.append(sqlStep{kind: "begin"}, sqlStep{kind: "exec", prefix: `DELETE FROM "schema_migrations" WHERE version > ?`})
			for i := 0; i < version; i++ {
				script.append(sqlStep{kind: "exec", prefix: `INSERT INTO "schema_migrations"`})
			}
			script.append(sqlStep{kind: "commit"})
			requireOK(t, NewMigrator(db, testSource()).Force(context.Background(), version))
			for _, call := range script.snapshot() {
				if strings.HasPrefix(call.query, "UP ") || strings.HasPrefix(call.query, "DOWN ") {
					t.Fatal("Force ran migration SQL")
				}
				if strings.HasPrefix(call.query, "INSERT") {
					if !call.inTransaction || !strings.Contains(call.query, "ON CONFLICT") {
						t.Fatal("Force is not transactional reconciliation")
					}
					if call.args[3].Value != false {
						t.Fatal("Force did not clear dirty state")
					}
				}
			}
		})
	}
}

func TestVersion(t *testing.T) {
	for _, tt := range []struct {
		name    string
		rows    []migrationRow
		version int
		dirty   bool
	}{
		{"empty", nil, 0, false},
		{"applied", appliedRows(t), 2, false},
		{"dirty lower version", []migrationRow{{Version: 1, Dirty: true}, {Version: 2}}, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, script := mockDB(t)
			expectState(script, tt.rows...)
			version, dirty, err := NewMigrator(db, nil).Version(context.Background())
			requireOK(t, err)
			if version != tt.version || dirty != tt.dirty {
				t.Fatalf("got (%d,%v), want (%d,%v)", version, dirty, tt.version, tt.dirty)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(fstest.MapFS, []migrationRow)
		state  MigrationState
	}{
		{"applied", func(fstest.MapFS, []migrationRow) {}, StateApplied},
		{"dirty", func(_ fstest.MapFS, rows []migrationRow) { rows[0].Dirty = true }, StateDirty},
		{"modified up", func(source fstest.MapFS, _ []migrationRow) { source["001_first.up.sql"].Data = []byte("changed") }, StateModified},
		{"modified down", func(source fstest.MapFS, _ []migrationRow) { source["001_first.down.sql"].Data = []byte("changed") }, StateModified},
		{"name mismatch", func(_ fstest.MapFS, rows []migrationRow) { rows[0].Name = "original" }, StateNameMismatch},
		{"unverified", func(_ fstest.MapFS, rows []migrationRow) { rows[0].Checksum = "" }, StateUnverified},
		{"missing pair", func(source fstest.MapFS, _ []migrationRow) {
			delete(source, "001_first.up.sql")
			delete(source, "001_first.down.sql")
		}, StateMissingFile},
		{"missing down", func(source fstest.MapFS, _ []migrationRow) { delete(source, "001_first.down.sql") }, StateMissingFile},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, script := mockDB(t)
			source, rows := testSource(), appliedRows(t)[:1]
			tt.change(source, rows)
			expectState(script, rows...)
			statuses, err := NewMigrator(db, source).Status(context.Background())
			requireOK(t, err)
			if len(statuses) != 2 || statuses[0].State != tt.state || statuses[1].State != StatePending {
				t.Fatalf("unexpected statuses: %+v", statuses)
			}
		})
	}
}

func TestLogging(t *testing.T) {
	db, script := mockDB(t)
	expectState(script, appliedRows(t)[:1]...)
	expectUp(script, 2, nil)
	var events []Event
	logger := LoggerFunc(func(_ context.Context, event Event) { events = append(events, event) })
	requireOK(t, NewMigrator(db, testSource(), WithLogger(logger)).Up(context.Background()))
	var kinds []EventKind
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	if !reflect.DeepEqual(kinds, []EventKind{EventLockAcquired, EventSkipped, EventStarted, EventApplied, EventLockReleased}) {
		t.Fatalf("events = %v", kinds)
	}
	if events[3].Version != 2 || events[3].Name != "second" || events[3].Direction != DirectionUp || events[3].Duration <= 0 {
		t.Fatalf("missing metadata: %+v", events[3])
	}
}

func TestRejectUnsupportedAndUnsafeHandles(t *testing.T) {
	db, _ := mockDB(t)
	pool, err := db.DB()
	requireOK(t, err)
	unsupported, err := gorm.Open(scriptDialect{pool: pool, name: "mysql"}, &gorm.Config{DisableAutomaticPing: true})
	requireOK(t, err)
	requireError(t, NewMigrator(unsupported, testSource()).Up(context.Background()), "unsupported migration dialect")
	requireError(t, NewMigrator(db.Session(&gorm.Session{DryRun: true}), testSource()).Up(context.Background()), "DryRun")
	// No real transaction is opened: this only presents the GORM TxCommitter interface.
	txDB := db.Session(&gorm.Session{Context: context.Background()})
	txDB.Statement.ConnPool = rejectingTransaction{ConnPool: db.Statement.ConnPool}
	requireError(t, NewMigrator(txDB, testSource()).Up(context.Background()), "not an existing transaction")
}

type rejectingTransaction struct{ gorm.ConnPool }

func (rejectingTransaction) Commit() error   { return nil }
func (rejectingTransaction) Rollback() error { return nil }
