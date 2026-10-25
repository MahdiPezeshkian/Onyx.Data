// Package db provides a unit-of-work layer over GORM together with the wiring that
// builds read, write, and command repositories from a shared context.
package db

import (
	"github.com/MahdiPezeshkian/Onyx.Data/db/sql/implementation"
	"github.com/MahdiPezeshkian/Onyx.Data/db/tracking"

	"gorm.io/gorm"
)

// RepositoryContext carries the database handles and change tracker used to build
// repositories. It is produced by UnitOfWork.RepositoryContext and is not meant to
// be constructed directly by callers.
//
// db is the root handle used by read repositories, tx is the active transaction used
// by write repositories and commands, and changeTracker is non-nil only for a unit of
// work that has been started with Begin.
type RepositoryContext struct {
	db            *gorm.DB
	tx            *gorm.DB
	changeTracker *tracking.ChangeTracker
}

// NewReadRepository builds a repository whose queries run against the root database
// handle in ctx, outside any active transaction. It does not attach a change tracker.
func NewReadRepository[T any](ctx RepositoryContext) *implementation.GenericRepository[T] {
	return implementation.NewRepository[T](ctx.db, nil)
}

// NewWriteRepository builds a repository whose writes run against the transaction in
// ctx and whose reads record loaded entities with the change tracker.
func NewWriteRepository[T any](ctx RepositoryContext) *implementation.GenericRepository[T] {
	return implementation.NewRepository[T](ctx.tx, ctx.changeTracker)
}

// NewCommandRepository builds a repository that executes raw update commands on the
// transaction in ctx.
func NewCommandRepository(ctx RepositoryContext) *implementation.CommandBaseRepository {
	return implementation.NewCommandRepository(ctx.tx)
}

// OperationType classifies a repository operation as a read or a write.
type OperationType int

const (
	// OperationRead marks an operation that only reads data.
	OperationRead OperationType = iota
	// OperationWrite marks an operation that modifies data.
	OperationWrite
)

// String returns "READ" for OperationRead and "WRITE" for OperationWrite. Any other
// value panics.
func (o OperationType) String() string {
	return [...]string{"READ", "WRITE"}[o]
}
