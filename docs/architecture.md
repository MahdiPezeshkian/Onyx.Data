# Architecture

Onyx.Data layers a repository pattern and a Unit of Work over
[GORM](https://gorm.io). This page describes how the pieces fit together. Every
statement here reflects behavior implemented in this repository.

## Packages

| Package | Responsibility |
| --- | --- |
| `db` | Unit of Work, `RepositoryContext`, and repository constructors. |
| `db/sql/contract` | `Command`, `Event`, and `Specification` interfaces. |
| `db/sql/implementation` | GORM repositories, specifications, and request config. |
| `db/sql/migration` | File-based SQL migration runner (PostgreSQL). |
| `db/tracking` | Change tracker and snapshot cache. |
| `entity` | `BaseEntity[T]` and the domain `Event` interface. |
| `genericlist` | LINQ-style generic slice wrapper. |

## RepositoryContext

Repositories are always built from a `db.RepositoryContext`, which carries three
things:

- `db` — the root GORM handle (used by read repositories),
- `tx` — the active transaction (used by write repositories and commands),
- `changeTracker` — non-nil only for a Unit of Work started with `Begin`.

You normally obtain a context from a Unit of Work rather than constructing one
yourself.

## Repository kinds

- `db.NewReadRepository[T](ctx)` builds `implementation.GenericRepository[T]` over
  the root handle with no change tracker. Use it for queries outside a
  transaction.
- `db.NewWriteRepository[T](ctx)` builds the same repository over the transaction
  and records every loaded entity with the change tracker.
- `db.NewCommandRepository(ctx)` builds `implementation.CommandBaseRepository` for
  validated raw `UPDATE` statements.

`GenericRepository[T]` embeds `GenericSpecBaseRepository[T]`, so specification
queries are available on both read and write repositories.

## Unit of Work lifecycle

```
NewUnitOfWork(gormDB, readFactory, writeFactory)   // no transaction yet
        │
        ├── reads  ──► root handle
        └── Begin() ─► new UnitOfWork + transaction + fresh change tracker
                            │
                            ├── Commit(ctx, roots...)   // persist changes, then commit
                            ├── Rollback()              // abort and dispose
                            ├── EnsureRollback()        // safe deferred no-op after Commit
                            └── Dispose()               // release the transaction
```

Key points:

- The value returned by `NewUnitOfWork` is not transactional. Writes are
  unavailable until `Begin` is called.
- `Begin` does not mutate the receiver; it returns a **new** Unit of Work, so one
  base value can start several transactions.
- `Begin` builds a fresh change tracker, so snapshots are per transaction.
- `Commit` disposes the transaction on success. `EnsureRollback` is therefore safe
  to use with `defer` even after a successful commit.

## Change tracking

When a write repository loads an entity, the tracker stores a JSON snapshot keyed
by the entity's type and `Id`. On `Commit`, the tracker walks the change graphs
reachable from the supplied roots, diffs them against the snapshots, and reports
each entity as added or updated. `CreatedAt` and `UpdatedAt` are ignored when
diffing.

Because snapshots are keyed by type and id, entities loaded outside the change
tracker (for example, through a read repository) are not part of the diff unless
they are passed as a root.

An entity that disappears from the graph is **not** reported as removed: the
tracker builds its "before" map only from the keys present in the current graph.
Remove rows explicitly with `Delete`/`HardDelete` or
`GenericRepository.Delete`.

## Specifications

`implementation.NewSpec[T](req, cfg, preloads...)` produces a
`FilterSpecification` from:

- a `Request` — pagination (`Pagination`), sorting (`Sorts`), and a filter tree
  (`FilterGroup` with nested groups and an `AND`/`OR` operator),
- a `RequestConfig` — the field allow-list, page-size cap, default sorts, and an
  optional mandatory filter.

The specification returns association paths from `Preloads()` and builds the query
in `Apply(*gorm.DB)`. Its `RawQuery()` returns an empty query, so the repository
always uses `Apply`. Filters and sorts on fields that are not allow-listed are
skipped; a filter build error leaves the query unfiltered rather than failing.

## Command repository

`CommandBaseRepository.ApplyUpdateCommand` executes a `contract.Command`'s raw SQL
with bound parameters. The statement is validated to be a single `UPDATE` that
contains a `WHERE` clause; other statements, and statements containing forbidden
keywords, are rejected.

## Migrations

See [migrations.md](./migrations.md) for the migration runner's behavior.
