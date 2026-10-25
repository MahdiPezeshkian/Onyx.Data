package main

import (
	"github.com/MahdiPezeshkian/Onyx.Data/db"
	"github.com/MahdiPezeshkian/Onyx.Data/db/sql/implementation"
)

// ReadRepositorySet collects the read-only repositories for the application. Read
// repositories run against the root database handle and do not participate in
// change tracking.
type ReadRepositorySet struct {
	ProductRepo  *implementation.GenericRepository[Product]
	CustomerRepo *implementation.GenericRepository[Customer]
	OrderRepo    *implementation.GenericRepository[Order]
}

// WriteRepositorySet collects the write-side repositories. Write repositories run
// against the Unit of Work transaction and record loaded entities with the change
// tracker, so Commit can detect what changed.
type WriteRepositorySet struct {
	ProductRepo  *implementation.GenericRepository[Product]
	CustomerRepo *implementation.GenericRepository[Customer]
	OrderRepo    *implementation.GenericRepository[Order]

	// CommandRepository runs raw, allow-listed UPDATE statements.
	CommandRepository *implementation.CommandBaseRepository
}

// NewReadRepositorySet builds the read repository collection from the context the
// Unit of Work passes to it.
func NewReadRepositorySet(ctx db.RepositoryContext) ReadRepositorySet {
	return ReadRepositorySet{
		ProductRepo:  db.NewReadRepository[Product](ctx),
		CustomerRepo: db.NewReadRepository[Customer](ctx),
		OrderRepo:    db.NewReadRepository[Order](ctx),
	}
}

// NewWriteRepositorySet builds the write repository collection, including the
// command repository for raw updates.
func NewWriteRepositorySet(ctx db.RepositoryContext) WriteRepositorySet {
	return WriteRepositorySet{
		ProductRepo:       db.NewWriteRepository[Product](ctx),
		CustomerRepo:      db.NewWriteRepository[Customer](ctx),
		OrderRepo:         db.NewWriteRepository[Order](ctx),
		CommandRepository: db.NewCommandRepository(ctx),
	}
}
