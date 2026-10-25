# Migrations

The `db/sql/migration` package runs paired SQL migration files through GORM and an
`fs.FS` source. **PostgreSQL is currently the only supported dialect.** Supplying
any other dialect returns an error.

## Migration files

Files are discovered recursively in the source and must be named:

```
<positive-number>_<name>.<up|down>.sql
```

For example:

```
migrations/001_create_users.up.sql
migrations/001_create_users.down.sql
```

- The version must be a positive integer, and each `(version, direction)` pair may
  appear only once.
- A version's `up` and `down` files must share the same name.
- Non-SQL files (such as a `README`) are ignored; malformed `.sql` filenames are
  errors.
- `Up`/`Down` require both files and non-empty SQL for every migration; `Status`
  tolerates incomplete pairs so a missing file can be reported.

## Setup

```go
migrator := migration.NewMigrator(gormDB, os.DirFS("migrations"))
// or:
migrator = migration.NewMigratorFromDir(gormDB, "migrations")
```

Options:

- `migration.WithMigrationsTable("schema_migrations")` — choose the history table.
  It must be an unqualified ASCII identifier of at most 63 bytes.
- `migration.WithLogger(logger)` — receive structured lifecycle events. A nil
  logger disables logging. The GORM SQL logger is silenced during migrations.

The GORM handle must be a **root** handle, not an existing transaction, and must
not be a `DryRun` database; the package returns an error otherwise.

## Operations

| Method | Behavior |
| --- | --- |
| `Up(ctx)` | Apply all pending migrations in ascending order. |
| `UpSteps(ctx, n)` | Apply at most `n` pending migrations (`n` must be positive). |
| `Down(ctx, n)` | Roll back exactly `n` applied migrations, newest first. |
| `DownAll(ctx)` | Roll back every applied migration (no-op when none are applied). |
| `Version(ctx)` | Highest dirty version, otherwise highest applied version. |
| `Status(ctx)` | Per-version source/history comparison in ascending order. |
| `Baseline(ctx)` | Record checksums for legacy history rows that lack one. |
| `Force(ctx, version)` | Reconcile history only; executes no SQL. |

## Safety model

- **Advisory lock.** Each operation runs under a single database-wide
  `pg_advisory_lock`, so concurrent migrators serialize. The connection used for
  the lock is discarded rather than pooled if the lock cannot be released cleanly.
- **Dirty tracking.** Before a migration's SQL runs, its history row is marked
  dirty in a separate autocommitted statement, so a rollback or cancellation cannot
  erase the marker. The SQL and the clean marker then commit together. A failed
  migration is left dirty for manual inspection.
- **Checksums.** A checksum covers the exact bytes of both the `up` and `down`
  files, so a migration modified after being applied is detected.
- **History validation.** `Up`/`Down`/`Baseline` reject dirty history, missing
  files, name mismatches, modified checksums, and (unless baseline) unverified
  rows. Dirty state is always reported first.

`Baseline` records checksums only for rows that have none; review historical files
first, because their original contents cannot be verified retroactively.

`Force` deliberately reconciles **history only** and accepts changed files. Inspect
and repair the actual schema before using it.

## Testing migrations

The package's own tests use a scripted in-process stub driver and require no
database. To exercise this package against real PostgreSQL, point
`NewMigratorFromDir` at your migration directory and pass a configured GORM
PostgreSQL handle.
