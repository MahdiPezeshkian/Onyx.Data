# Onyx.Data

Onyx.Data is a small persistence toolkit for Go applications that use
[GORM](https://gorm.io). It layers a repository pattern and a Unit of Work on top
of GORM so that application code can express reads and writes against typed
models, and persist a graph of changes inside a single transaction.

It also ships a lightweight specification-based query model (filtering, sorting,
pagination, and allow-listing), a change tracker, a raw SQL command runner, a
LINQ-style generic list, and a file-based SQL migration runner.

> **Project status: early, work in progress.** The API is not yet stable. There are
> no tagged releases, and the module has not been published to a module proxy.
> Treat this as pre-release software and expect breaking changes.
>
> Active development happens on the **`develop`** branch. The `main` branch only
> holds this project's introduction. See the
> [`develop` branch](https://github.com/MahdiPezeshkian/Onyx.Data/tree/develop)
> for the source.

## Key features

All of the following are implemented in this repository:

- **Generic repositories** — `GenericRepository[T]` with `GetByID`, `GetByIDWithPreload`,
  `GetAll`, `GetAllAsList`, and `Delete` (`db/sql/implementation`).
- **Read/write separation** — read repositories run against the root GORM handle;
  write repositories run against the Unit of Work transaction and record loaded
  entities for change tracking (`db.NewReadRepository`, `db.NewWriteRepository`).
- **Unit of Work** — `Begin`, `Commit`, `Rollback`, `EnsureRollback`, and `Dispose`
  over a single transaction, with repositories rebuilt per transaction
  (`db.UnitOfWork`).
- **Change tracking** — entities loaded through a write repository are snapshotted
  and diffed on commit, producing inserts, updates, and deletes (`db/tracking`).
- **Specifications** — build filtered, sorted, and paginated queries against an
  allow-list of fields, with mandatory (access-scoping) filters
  (`implementation.NewSpec`, `implementation.RequestConfig`).
- **Command repository** — execute validated, parameterized raw `UPDATE`
  statements (`implementation.CommandBaseRepository`).
- **Entity base type** — `entity.BaseEntity[T]` with timestamps, soft-delete and
  hard-delete flags, and an in-memory domain event list.
- **Generic list** — a small LINQ-style `genericlist.List[T, S]` for querying
  slices with `Where`, `Select`, `First`, `OrderBy`, and similar operators.
- **SQL migrations** — paired `.up.sql`/`.down.sql` files with checksums, a
  serialized advisory lock, and `Up`/`Down`/`Status`/`Baseline`/`Force`
  operations. PostgreSQL is currently the only supported dialect
  (`db/sql/migration`).

## Requirements

- **Go 1.27 or newer** (the module declares `go 1.27.0`).
- **GORM v1.31.2** (declared in `go.mod`).
- A GORM driver for the database you target. The library itself does not depend on
  any driver; you bring your own (for example `gorm.io/driver/postgres`). The
  migration package currently supports **PostgreSQL only**.

## Installation

No tagged releases exist yet, so install a specific commit rather than a version:

```sh
go get github.com/MahdiPezeshkian/Onyx.Data@main
```

Because `main` intentionally contains only this README, fetch the code from
`develop` while the project is pre-release:

```sh
go get github.com/MahdiPezeshkian/Onyx.Data@develop
```

Once a tagged release is published, prefer the tagged version
(`go get github.com/MahdiPezeshkian/Onyx.Data@vX.Y.Z`). Until then, pin a
pseudo-version produced by `go get` so your builds are reproducible.

## Minimal usage

Onyx.Data builds repositories from a `db.RepositoryContext`, which the Unit of
Work supplies. A typical integration defines small read and write repository
collections and passes factory functions to `db.NewUnitOfWork`:

```go
package example

import (
	"context"

	"github.com/MahdiPezeshkian/Onyx.Data/db"
	"github.com/MahdiPezeshkian/Onyx.Data/db/sql/implementation"
	"github.com/MahdiPezeshkian/Onyx.Data/entity"

	"gorm.io/gorm"
)

type Product struct {
	entity.BaseEntity[int]
	Name  string
	Price int64
}

type ReadRepos struct {
	Products *implementation.GenericRepository[Product]
}

type WriteRepos struct {
	Products *implementation.GenericRepository[Product]
}

func NewReadRepos(ctx db.RepositoryContext) ReadRepos {
	return ReadRepos{Products: db.NewReadRepository[Product](ctx)}
}

func NewWriteRepos(ctx db.RepositoryContext) WriteRepos {
	return WriteRepos{Products: db.NewWriteRepository[Product](ctx)}
}

func Example(ctx context.Context, gormDB *gorm.DB) error {
	uow := db.NewUnitOfWork(gormDB, NewReadRepos, NewWriteRepos)

	// Reads run against the root handle and are not tracked.
	if _, err := uow.ReadRepositories.Products.GetByID(ctx, 1); err != nil {
		return err
	}

	// Writes require Begin; Commit persists the tracked changes.
	tx := uow.Begin()
	defer tx.EnsureRollback()

	product := &Product{BaseEntity: entity.Initial(1), Name: "Keyboard", Price: 9900}
	return tx.Commit(ctx, product)
}
```

A complete, runnable version of this pattern (with a fictional `Product`,
`Customer`, and `Order` model, soft and hard deletes, a raw command update, a
specification query, and a rollback) lives in
[`examples/unit-of-work`](./examples/unit-of-work).

## Architecture

### Repositories

`implementation.GenericRepository[T]` is created with an explicit `*gorm.DB`:
either the root handle (read repositories) or a transaction (write repositories).
It embeds `GenericSpecBaseRepository[T]`, so both id-based and
specification-based queries are available:

| Method | Purpose |
| --- | --- |
| `GetByID(ctx, id)` | Load one entity by id. |
| `GetByIDWithPreload(ctx, id, preloads...)` | Load one entity and preload associations. |
| `GetAll(ctx)` / `GetAllAsList(ctx)` | Load all entities as a slice or a `genericlist.List`. |
| `Delete(ctx, id)` | Delete an entity by id. |
| `GetBySpec(ctx, spec)` / `GetListBySpec(ctx, spec)` | Run a specification query. |
| `GetPagedBySpec(ctx, spec, page, pageSize, cachedTotalCount...)` | Run a paginated specification query. |

Read repositories pass a nil change tracker and never record snapshots. Write
repositories record every loaded entity so that `Commit` can detect changes.

### Unit of Work

`db.UnitOfWork[R, W]` is parameterized by your read (`R`) and write (`W`)
repository collection types and constructed with two factory functions:

```go
uow := db.NewUnitOfWork(gormDB, NewReadRepos, NewWriteRepos)
```

A Unit of Work created this way is **not transactional**: reads use the root
handle and writes are unavailable until you call `Begin`. `Begin` returns a
**new** Unit of Work bound to a fresh transaction and a fresh change tracker; the
receiver is left unchanged, so one base Unit of Work can start several
transactions. Finish every begun value with `Commit`, `Rollback`, or a deferred
`EnsureRollback` (`EnsureRollback` is a no-op after `Commit` disposes the
transaction).

### Change tracking and commit

`Commit(ctx, roots...)` walks the change graphs reachable from each root, diffs
them against the snapshots taken when the entities were loaded, and then, in one
transaction:

- inserts entities reported as added (skipping any that were hard-deleted),
- updates entities reported as changed, or deletes them if they were hard-deleted,
- deletes entities reported as removed (see the note below on graph removals).

`CreatedAt` and `UpdatedAt` are ignored when diffing. Any failure rolls the whole
transaction back, so a commit is all-or-nothing.

> **Note on graph removals.** The change tracker builds its "before" map from the
> entities present in the current graph, so an entity that simply disappears from
> the graph is not reported as removed. Use `Delete`/`HardDelete` (or
> `GenericRepository.Delete`) to remove rows; the `Removed` status is currently
> not produced by the tracker.

### Soft and hard deletes

`entity.BaseEntity[T]` carries both an `IsDeleted` flag and a `HardDeleted` flag.
`Delete()` sets a soft-delete marker (`IsDeleted`, `DeletedAt`) and persists as an
`UPDATE`; `HardDelete()` marks the entity for a physical `DELETE` on commit.

Note the model's `DeletedAt` field is a plain `time.Time`, so GORM does **not**
apply automatic soft-delete filtering to queries. If you want GORM's built-in
soft-delete behavior, use `gorm.DeletedAt` on your own model instead.

### Specifications

`implementation.NewSpec[T](req, cfg, preloads...)` builds a
`FilterSpecification` from a `Request` (pagination, sorts, and a filter tree) and
a `RequestConfig` allow-list. `RequestConfig` maps request field names to
database columns and controls whether each field may be filtered or sorted and
which operators are permitted. A `MaxPageSize` cap and default sorts are also
enforced, and a `MandatoryFilter` can be ANDed in to scope results. The ready-made
default is `implementation.RequestRequestConfig`.

### Command repository

`implementation.CommandBaseRepository` executes a `contract.Command`'s raw
`UPDATE` statement. Values are passed as bound parameters, and the query is
validated to be a single `UPDATE` with a `WHERE` clause before it runs.

### Migrations

`migration.NewMigrator(db, fsys)` (or `NewMigratorFromDir`) discovers paired
`<version>_<name>.up.sql` / `.down.sql` files and manages a history table. It
supports `Up`, `UpSteps`, `Down`, `DownAll`, `Version`, `Status`, `Baseline`, and
`Force`, records a checksum of both files per migration, and serializes runs with
a database advisory lock. **PostgreSQL is the only supported dialect.**

## Configuration and database prerequisites

- The library does not open connections; pass a configured `*gorm.DB`. The
  migration package requires a **root** GORM handle (not an existing
  transaction) and is not compatible with `DryRun`.
- Migrations require PostgreSQL. Supplying an unsupported dialect returns an
  error rather than silently degrading.
- The migration history table defaults to `schema_migrations`; override it with
  `migration.WithMigrationsTable` (an unqualified ASCII identifier of at most 63
  bytes).
- For transactionally correct soft deletes, keep `entity.BaseEntity`'s
  `DeletedAt` semantics in mind (see above) or define your own model fields.

## Testing

Run the full check suite from the repository root:

```sh
gofmt -l .          # should print nothing
go vet ./...
go build ./...
go test ./...
```

The `db/sql/migration` package has tests that run against a scripted in-process
stub driver, so they need no external database. To run the example end to end:

```sh
cd examples/unit-of-work
go run .
```

## Project structure

```
db/                         Unit of Work, RepositoryContext, and repository constructors
db/sql/contract/            Command, Event, and Specification interfaces
db/sql/implementation/      GORM repositories, specifications, and request config
db/sql/migration/           File-based SQL migration runner
db/tracking/                Change tracker and snapshot cache
entity/                     BaseEntity and the domain Event interface
genericlist/                LINQ-style generic slice wrapper
examples/unit-of-work/      Runnable integration example (standalone module)
```

## Contributing

Contributions are welcome. Active development is on `develop`; please target that
branch. See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup, checks,
and commit conventions.

## Maturity and limitations

Onyx.Data is early, pre-release software. Known limitations, verified against the
current source:

- **No stable API and no releases.** The module has no tagged versions and has not
  been published to a module proxy; pin a commit when you depend on it.
- **No `LICENSE` file.** Licensing terms are unspecified (see below).
- **Graph removals are not tracked.** `Commit` does not detect entities that
  disappear from the graph; delete rows explicitly (see the note above).
- **PostgreSQL-only migrations.** The migration runner rejects other dialects.
- **Soft-delete filtering is opt-in.** `entity.BaseEntity` uses a plain
  `time.Time` for `DeletedAt`, so GORM does not automatically exclude
  soft-deleted rows; use `gorm.DeletedAt` on your own model if you want that.
- **Repository reads require a preloaded layout.** `GetByID` and
  `GetByIDWithPreload` filter on `id = ?`, so models must have an `id` column.
- **The command validator is textual.** Forbidden SQL keywords are matched with
  surrounding spaces, so it is a guardrail rather than a full SQL parser.
- **Only the migration package has broad test coverage.** Other packages have
  focused unit tests over pure helpers and entity behavior; database-backed
  behavior is exercised mainly through the example.

## License

A license has not been chosen yet, so no `LICENSE` file is included and the
project's licensing terms are currently unspecified. Do not assume any permission
to use, distribute, or modify the code until a license is added. See
[CONTRIBUTING.md](./CONTRIBUTING.md) for details.
