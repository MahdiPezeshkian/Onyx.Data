package main

import (
	"context"

	"github.com/MahdiPezeshkian/Onyx.Data/db"

	"gorm.io/gorm"
)

// appUnitOfWork is the library's Unit of Work parameterized with this
// application's repository collections.
type appUnitOfWork = db.UnitOfWork[ReadRepositorySet, WriteRepositorySet]

// UnitOfWork wraps the library Unit of Work so application code can reach the
// repositories through typed methods instead of the generic aggregate fields.
//
// Commit, Rollback, EnsureRollback, and Dispose are promoted from the embedded
// value; Begin is overridden below so it returns the wrapper type.
type UnitOfWork struct {
	*appUnitOfWork
}

// NewUnitOfWork builds a non-transactional Unit of Work over gormDB. Reads work
// immediately; writes require Begin.
func NewUnitOfWork(gormDB *gorm.DB) *UnitOfWork {
	return &UnitOfWork{appUnitOfWork: db.NewUnitOfWork(
		gormDB,
		NewReadRepositorySet,
		NewWriteRepositorySet,
	)}
}

// Begin starts a transaction and returns a Unit of Work bound to it. The returned
// value must be finished with Commit or Rollback (or a deferred EnsureRollback).
func (u *UnitOfWork) Begin() *UnitOfWork {
	return &UnitOfWork{appUnitOfWork: u.appUnitOfWork.Begin()}
}

// Reads returns the read-side repositories, which query the root database handle and
// do not track changes.
func (u *UnitOfWork) Reads() ReadRepositorySet {
	return u.ReadRepositories
}

// Writes returns the write-side repositories for the current transaction. Before
// Begin is called these are built over the root handle, so prefer Writes on a value
// returned by Begin.
func (u *UnitOfWork) Writes() WriteRepositorySet {
	return u.WriteRepositories
}

// InTransaction runs fn inside a fresh transaction and commits the change roots it
// returns. Any error from fn skips the commit, and the deferred EnsureRollback aborts
// the transaction. The deferred rollback is harmless on success because Commit
// disposes the transaction, making EnsureRollback a no-op.
//
// This helper is application-level sugar over Begin/Commit/EnsureRollback; the
// library itself does not provide it.
func (u *UnitOfWork) InTransaction(ctx context.Context, fn func(tx *UnitOfWork) ([]any, error)) error {
	tx := u.Begin()
	defer tx.EnsureRollback()

	roots, err := fn(tx)
	if err != nil {
		return err
	}
	return tx.Commit(ctx, roots...)
}
