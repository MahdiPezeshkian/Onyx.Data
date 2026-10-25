// Package migration runs paired SQL migrations through GORM and an fs.FS source.
// PostgreSQL is currently the only supported database.
package migration

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultMigrationsTable = "schema_migrations"

var tablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// Direction identifies the SQL file being executed.
type Direction string

const (
	DirectionUp   Direction = "up"
	DirectionDown Direction = "down"
)

// Migrator manages migration history. Configure it before concurrent use.
// Its DB must be a root GORM handle, not an existing transaction.
type Migrator struct {
	db              *gorm.DB
	source          fs.FS
	migrationsTable string
	logger          Logger
}

// Option configures a Migrator.
type Option func(*Migrator)

// NewMigrator creates a migrator. Files are discovered recursively within source.
// Configuration errors are returned by operations, preserving this constructor's API.
func NewMigrator(db *gorm.DB, source fs.FS, options ...Option) *Migrator {
	m := &Migrator{db: db, source: source, migrationsTable: defaultMigrationsTable}
	for _, option := range options {
		option(m)
	}
	return m
}

// NewMigratorFromDir uses an operating-system directory as the migration source.
func NewMigratorFromDir(db *gorm.DB, dir string, options ...Option) *Migrator {
	return NewMigrator(db, os.DirFS(dir), options...)
}

// WithMigrationsTable selects an unqualified ASCII identifier of at most 63 bytes.
// Names are quoted and case-sensitive. Empty names and schema qualification are rejected.
func WithMigrationsTable(table string) Option {
	return func(m *Migrator) { m.migrationsTable = table }
}

// Up applies all pending migrations in ascending order, committing each separately.
func (m *Migrator) Up(ctx context.Context) error { return m.up(ctx, 0) }

// UpSteps applies at most steps pending migrations. Steps must be positive.
func (m *Migrator) UpSteps(ctx context.Context, steps int) error {
	if steps <= 0 {
		return errors.New("up migration steps must be greater than zero")
	}
	return m.up(ctx, steps)
}

func (m *Migrator) up(ctx context.Context, steps int) error {
	items, err := m.loadMigrations()
	if err != nil {
		return err
	}
	return m.withState(ctx, func(db *gorm.DB, rows []migrationRow) error {
		if err := m.validateHistory(ctx, items, rows, false); err != nil {
			return err
		}
		applied := make(map[int]bool, len(rows))
		for _, row := range rows {
			applied[row.Version] = true
		}
		count := 0
		for _, item := range items {
			if applied[item.Version] {
				m.log(ctx, Event{Kind: EventSkipped, Version: item.Version, Name: item.Name, Direction: DirectionUp})
				continue
			}
			if steps > 0 && count == steps {
				break
			}
			if err := m.execute(ctx, db, item, DirectionUp); err != nil {
				return err
			}
			count++
		}
		return nil
	})
}

// Down rolls back exactly steps applied migrations, newest first. Nonpositive
// steps or steps exceeding the applied count fail before any migration executes.
func (m *Migrator) Down(ctx context.Context, steps int) error {
	if steps <= 0 {
		return errors.New("down migration steps must be greater than zero")
	}
	return m.down(ctx, steps)
}

// DownAll rolls back every applied migration. An empty database is a no-op.
func (m *Migrator) DownAll(ctx context.Context) error { return m.down(ctx, 0) }

func (m *Migrator) down(ctx context.Context, steps int) error {
	items, err := m.loadMigrations()
	if err != nil {
		return err
	}
	return m.withState(ctx, func(db *gorm.DB, rows []migrationRow) error {
		if err := m.validateHistory(ctx, items, rows, false); err != nil {
			return err
		}
		if steps > len(rows) {
			return fmt.Errorf("cannot roll back %d migrations: only %d are applied", steps, len(rows))
		}
		if steps == 0 {
			steps = len(rows)
		}
		byVersion := indexMigrations(items)
		for i := len(rows) - 1; i >= len(rows)-steps; i-- {
			if err := m.execute(ctx, db, byVersion[rows[i].Version], DirectionDown); err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Migrator) execute(ctx context.Context, db *gorm.DB, item Migration, direction Direction) error {
	started := time.Now()
	event := Event{Kind: EventStarted, Version: item.Version, Name: item.Name, Direction: direction}
	m.log(ctx, event)
	// Autocommit BEFORE the migration transaction: rollback/cancellation cannot
	// erase the dirty marker. Migration SQL and the clean marker commit together.
	var err error
	if direction == DirectionUp {
		err = db.Table(m.migrationsTable).Create(&migrationRow{Version: item.Version, Name: item.Name,
			Checksum: checksum(item), Dirty: true, AppliedAt: time.Now().UTC()}).Error
	} else {
		err = db.Table(m.migrationsTable).Where("version = ?", item.Version).Update("dirty", true).Error
	}
	if err == nil {
		err = db.Transaction(func(tx *gorm.DB) error {
			sqlText := item.UpSQL
			if direction == DirectionDown {
				sqlText = item.DownSQL
			}
			if err := tx.Exec(sqlText).Error; err != nil {
				return err
			}
			if direction == DirectionDown {
				return tx.Table(m.migrationsTable).Where("version = ?", item.Version).Delete(&migrationRow{}).Error
			}
			return tx.Table(m.migrationsTable).Where("version = ?", item.Version).
				Updates(map[string]any{"dirty": false, "applied_at": time.Now().UTC()}).Error
		})
	}
	event.Duration = time.Since(started)
	if err != nil {
		event.Kind = EventFailed
		m.log(ctx, event)
		return fmt.Errorf("migration %03d_%s %s failed; inspect dirty state before recovery: %w", item.Version, item.Name, direction, err)
	}
	event.Kind = EventApplied
	if direction == DirectionDown {
		event.Kind = EventRolledBack
	}
	m.log(ctx, event)
	return nil
}

// Version returns the highest dirty version, if any, otherwise the highest
// applied version. An empty history returns (0, false, nil). It does not need source files.
func (m *Migrator) Version(ctx context.Context) (version int, dirty bool, err error) {
	err = m.withState(ctx, func(_ *gorm.DB, rows []migrationRow) error {
		for _, row := range rows {
			if row.Dirty {
				version, dirty = row.Version, true
			} else if !dirty {
				version = row.Version
			}
		}
		return nil
	})
	return
}

// Baseline records checksums only for legacy history rows without one. Review
// historical files first: their original contents cannot be verified retroactively.
// Dirty, missing, renamed, or already-checksummed modified migrations are rejected.
func (m *Migrator) Baseline(ctx context.Context) error {
	items, err := m.loadMigrations()
	if err != nil {
		return err
	}
	return m.withState(ctx, func(db *gorm.DB, rows []migrationRow) error {
		if err := m.validateHistory(ctx, items, rows, true); err != nil {
			return err
		}
		byVersion := indexMigrations(items)
		return db.Transaction(func(tx *gorm.DB) error {
			for _, row := range rows {
				if row.Checksum != "" {
					continue
				}
				if err := tx.Table(m.migrationsTable).Where("version = ?", row.Version).
					Update("checksum", checksum(byVersion[row.Version])).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// Force deliberately reconciles HISTORY ONLY: every source migration <= version
// is recorded clean with its current name/checksum, and all higher rows are removed.
// Version must exist in source, or be zero to clear history. No SQL files execute.
// Inspect/repair the actual schema first. This explicitly accepts changed files.
func (m *Migrator) Force(ctx context.Context, version int) error {
	if version < 0 {
		return errors.New("force version must not be negative")
	}
	items, err := m.loadMigrations()
	if err != nil {
		return err
	}
	if _, ok := indexMigrations(items)[version]; version != 0 && !ok {
		return fmt.Errorf("cannot force version %d: migration file is missing", version)
	}
	return m.withState(ctx, func(db *gorm.DB, _ []migrationRow) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Table(m.migrationsTable).Where("version > ?", version).Delete(&migrationRow{}).Error; err != nil {
				return err
			}
			for _, item := range items {
				if item.Version > version {
					break
				}
				row := migrationRow{Version: item.Version, Name: item.Name, Checksum: checksum(item), AppliedAt: time.Now().UTC()}
				if err := tx.Table(m.migrationsTable).Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "version"}},
					DoUpdates: clause.AssignmentColumns([]string{"name", "checksum", "dirty"}),
				}).Create(&row).Error; err != nil {
					return err
				}
			}
			return nil
		})
	})
}

type migrationRow struct {
	Version   int
	Name      string
	Checksum  string
	Dirty     bool
	AppliedAt time.Time
}

func (m *Migrator) withState(ctx context.Context, fn func(*gorm.DB, []migrationRow) error) error {
	if ctx == nil {
		return errors.New("migration context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !tablePattern.MatchString(m.migrationsTable) {
		return fmt.Errorf("unsafe migration table identifier %q: use an unqualified ASCII SQL identifier (1-63 bytes)", m.migrationsTable)
	}
	if m.db == nil || m.db.Dialector == nil {
		return errors.New("migration database must not be nil")
	}
	if m.db.Error != nil {
		return m.db.Error
	}
	if m.db.DryRun {
		return errors.New("migrations cannot use a DryRun database")
	}
	if _, ok := m.db.Statement.ConnPool.(gorm.TxCommitter); ok {
		return errors.New("migrations require a root GORM database, not an existing transaction")
	}
	dialect, err := dialectFor(m.db)
	if err != nil {
		return err
	}
	return dialect.withLock(ctx, m.db, func(db *gorm.DB) error {
		m.log(ctx, Event{Kind: EventLockAcquired})
		if err := dialect.ensureTable(db, m.migrationsTable); err != nil {
			return fmt.Errorf("prepare migration history: %w", err)
		}
		var rows []migrationRow
		if err := db.Table(m.migrationsTable).Order("version ASC").Find(&rows).Error; err != nil {
			return fmt.Errorf("read migration history: %w", err)
		}
		return fn(db, rows)
	}, func() { m.log(context.WithoutCancel(ctx), Event{Kind: EventLockReleased}) })
}
