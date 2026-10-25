# Unit of Work example

This example shows how a Go application integrates **Onyx.Data**: it defines typed
read and write repository collections, builds them with the library's generic
repository constructors, wires them into a Unit of Work, and wraps that Unit of Work
to expose convenient repository fields.

It is a standalone module (`example.com/onyx-data-unit-of-work`) with neutral
fictional domain types (`Product`, `Customer`, `Order`). It does not depend on any
other application.

## Prerequisites

- Go 1.27 or newer.
- A C toolchain (`gcc` or `clang`), because the example uses
  `gorm.io/driver/sqlite`, which is backed by `github.com/mattn/go-sqlite3` and
  requires CGO.

No database server, credentials, or configuration are needed: the example uses an
in-memory SQLite database and creates its schema with `AutoMigrate`.

## Run it

```sh
cd examples/unit-of-work
go run .
```

The program prints one section per integration point and exits with
`example finished successfully`.

## Files

| File | Purpose |
| --- | --- |
| `models.go` | Fictional domain models that embed `entity.BaseEntity`. |
| `repositories.go` | Typed read/write repository collections and their factory functions. |
| `unitofwork.go` | A thin wrapper around `db.UnitOfWork`, plus an `InTransaction` helper. |
| `main.go` | The runnable walkthrough of each integration point. |

## The integration pattern

1. **Define your models.** Embed `entity.BaseEntity[T]` to get `Id`, timestamps, the
   soft-delete and hard-delete flags, and the event list:

   ```go
   type Product struct {
       entity.BaseEntity[int]
       Name  string
       Price int64
   }
   ```

   Use `entity.Initial(id)` to construct an entity with the id and timestamps set.
   `Delete()` marks a soft delete and `HardDelete()` marks a physical delete; both
   are persisted by `Commit`.

2. **Collect your repositories into two structs.** Read repositories are used for
   queries outside the transaction; write repositories belong to the transaction and
   take part in change tracking:

   ```go
   type ReadRepositorySet struct {
       ProductRepo *implementation.GenericRepository[Product]
   }

   type WriteRepositorySet struct {
       ProductRepo       *implementation.GenericRepository[Product]
       CommandRepository *implementation.CommandBaseRepository
   }
   ```

3. **Build them with the library constructors.** Each factory receives a
   `db.RepositoryContext` from the Unit of Work:

   ```go
   func NewReadRepositorySet(ctx db.RepositoryContext) ReadRepositorySet {
       return ReadRepositorySet{ProductRepo: db.NewReadRepository[Product](ctx)}
   }

   func NewWriteRepositorySet(ctx db.RepositoryContext) WriteRepositorySet {
       return WriteRepositorySet{
           ProductRepo:       db.NewWriteRepository[Product](ctx),
           CommandRepository: db.NewCommandRepository(ctx),
       }
   }
   ```

4. **Create the Unit of Work** from a `*gorm.DB` and the two factories:

   ```go
   uow := db.NewUnitOfWork(gormDB, NewReadRepositorySet, NewWriteRepositorySet)
   ```

   A Unit of Work created this way is **not** transactional: reads run against the
   root handle, and writes are unavailable until you call `Begin`.

5. **Wrap it to expose typed access.** Because `db.UnitOfWork` is generic, a small
   application wrapper removes the type parameters from call sites:

   ```go
   type appUnitOfWork = db.UnitOfWork[ReadRepositorySet, WriteRepositorySet]

   type UnitOfWork struct {
       *appUnitOfWork
   }

   func (u *UnitOfWork) Reads() ReadRepositorySet   { return u.ReadRepositories }
   func (u *UnitOfWork) Writes() WriteRepositorySet { return u.WriteRepositories }
   ```

   `ReadRepositories` and `WriteRepositories` are the library's aggregate fields; the
   accessor methods just give them application-friendly names.

   Embedding the alias promotes `Commit`, `Rollback`, `EnsureRollback`, and `Dispose`
   to the wrapper. `Begin` is overridden so it returns a `*UnitOfWork` rather than the
   generic library type.

## Transaction lifecycle

`Begin` returns a **new** Unit of Work bound to a fresh transaction and a fresh
change tracker; the receiver is left unchanged, so one base Unit of Work can start
several transactions. Always finish a begun value with `Commit`, `Rollback`, or a
deferred `EnsureRollback`:

```go
tx := uow.Begin()
defer tx.EnsureRollback() // no-op after a successful Commit

product, err := tx.Writes().ProductRepo.GetByID(ctx, 1)
if err != nil {
    return err
}
product.Price = 12900

return tx.Commit(ctx, product)
```

`EnsureRollback` is safe to call after `Commit`, because `Commit` disposes the
transaction. The `InTransaction` helper in `unitofwork.go` packages this pattern; it
is application-level sugar, not part of the library.

## What `Commit` does

`Commit(ctx, roots...)` walks the change graphs reachable from each root, diffs them
against the snapshots taken when entities were loaded through a write repository, and
then, in one transaction:

- **inserts** entities reported as added (skipping ones that were hard-deleted),
- **updates** entities reported as changed, or **deletes** them if they were
  hard-deleted,
- **deletes** entities that were removed from the graph.

Loaded timestamps (`CreatedAt`, `UpdatedAt`) are ignored when diffing. Any failure
rolls the whole transaction back, so a partially applied commit never occurs.

Note that repository reads record snapshots only when the repository was built with a
change tracker — that is, write repositories. Read repositories never track changes.
