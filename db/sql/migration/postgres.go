package migration

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Other dialects must define and test locking AND transactional-DDL semantics.
type migrationDialect interface {
	withLock(context.Context, *gorm.DB, func(*gorm.DB) error, func()) error
	ensureTable(*gorm.DB, string) error
}

type postgresDialect struct{}

func dialectFor(db *gorm.DB) (migrationDialect, error) {
	if db.Dialector.Name() != "postgres" {
		return nil, fmt.Errorf("unsupported migration dialect %q: only PostgreSQL is supported", db.Dialector.Name())
	}
	return postgresDialect{}, nil
}

// A fixed database-wide key also serializes different history tables/search
// paths and table creation. No table OID or process-random hash is involved.
const migrationLockKey int64 = 0x4f6e79784d696772

func (postgresDialect) withLock(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error, released func()) (err error) {
	pool, err := db.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer conn.Close()
	// Pin all GORM work to the same session. database/sql is used only for
	// connection ownership/disposal; queries and transactions remain in GORM.
	session := db.Session(&gorm.Session{NewDB: true, Context: ctx, Logger: logger.Discard, SkipDefaultTransaction: true})
	session.Statement.ConnPool = conn
	session.Config.ConnPool = conn
	session.Config.PrepareStmt = false
	// Never pool a potentially locked physical connection after an ambiguous
	// acquisition result or a failed unlock.
	discard := func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }
	if err := session.Exec("SELECT pg_advisory_lock(?)", migrationLockKey).Error; err != nil {
		discard()
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var unlocked bool
		unlockErr := session.WithContext(cleanupCtx).Raw("SELECT pg_advisory_unlock(?)", migrationLockKey).Scan(&unlocked).Error
		if unlockErr == nil && !unlocked {
			unlockErr = errors.New("migration advisory lock was not held")
		}
		if unlockErr != nil {
			discard()
			err = errors.Join(err, fmt.Errorf("release migration lock (connection discarded): %w", unlockErr))
		} else {
			released()
		}
	}()
	return fn(session)
}

func (postgresDialect) ensureTable(db *gorm.DB, table string) error {
	// table has passed strict ASCII identifier validation; quote to retain case.
	quoted := `"` + table + `"`
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS ` + quoted + ` (
			version BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			checksum TEXT,
			dirty BOOLEAN NOT NULL DEFAULT FALSE
		)`).Error; err != nil {
			return err
		}
		return tx.Exec(`ALTER TABLE ` + quoted + `
			ADD COLUMN IF NOT EXISTS checksum TEXT,
			ADD COLUMN IF NOT EXISTS dirty BOOLEAN NOT NULL DEFAULT FALSE`).Error
	})
}
