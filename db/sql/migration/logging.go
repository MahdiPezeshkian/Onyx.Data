package migration

import (
	"context"
	"time"
)

// EventKind identifies a migration lifecycle event.
type EventKind string

const (
	EventStarted          EventKind = "migration_started"
	EventApplied          EventKind = "migration_applied"
	EventRolledBack       EventKind = "migration_rolled_back"
	EventFailed           EventKind = "migration_failed"
	EventSkipped          EventKind = "migration_skipped"
	EventLockAcquired     EventKind = "migration_lock_acquired"
	EventLockReleased     EventKind = "migration_lock_released"
	EventDirtyDetected    EventKind = "dirty_migration_detected"
	EventChecksumMismatch EventKind = "checksum_mismatch"
)

// Event contains safe lifecycle metadata. SQL and raw database errors are omitted
// because they may contain sensitive values. Operations return their errors.
type Event struct {
	Kind      EventKind
	Version   int
	Name      string
	Direction Direction
	Duration  time.Duration
}

// Logger receives synchronous events. Implementations must be safe for concurrent
// use, must not panic, and must not call back into migration operations.
type Logger interface{ LogMigration(context.Context, Event) }

// LoggerFunc adapts a function into a migration Logger.
type LoggerFunc func(context.Context, Event)

// LogMigration implements Logger.
func (f LoggerFunc) LogMigration(ctx context.Context, event Event) { f(ctx, event) }

// WithLogger enables optional structured lifecycle logging. A nil logger disables it.
// The underlying GORM SQL logger is silenced for migration operations.
func WithLogger(logger Logger) Option { return func(m *Migrator) { m.logger = logger } }

func (m *Migrator) log(ctx context.Context, event Event) {
	if m.logger != nil {
		m.logger.LogMigration(ctx, event)
	}
}
