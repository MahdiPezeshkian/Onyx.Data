package db

import (
	"context"
	"errors"

	// "github.com/MahdiPezeshkian/Onyx.Data/db/sql/contract"
	"github.com/MahdiPezeshkian/Onyx.Data/db/tracking"

	// "reflect"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UnitOfWork coordinates a set of read and write repositories over a single
// transaction. R is the caller's read repository aggregate type and W the write
// repository aggregate type; both are produced by the factories supplied to
// NewUnitOfWork.
//
// A UnitOfWork created by NewUnitOfWork has no transaction: reads use the root
// database handle and writes are unavailable until Begin has been called.
type UnitOfWork[R any, W any] struct {
	db            *gorm.DB
	tx            *gorm.DB
	changeTracker *tracking.ChangeTracker
	disposed      bool
	tracking      bool

	writeFactory func(RepositoryContext) W
	readFactory  func(RepositoryContext) R

	// WriteRepositories and ReadRepositories hold the aggregates built by the factories.
	WriteRepositories W
	ReadRepositories  R
}

// NewUnitOfWork creates a unit of work that is not yet transactional. The read and
// write factories may be nil; the corresponding repository aggregate is then left at
// its zero value. Call Begin to start a transaction before writing.
func NewUnitOfWork[R any, W any](
	db *gorm.DB,
	readFactory func(RepositoryContext) R,
	writeFactory func(RepositoryContext) W,
) *UnitOfWork[R, W] {
	uow := &UnitOfWork[R, W]{
		db:           db,
		tx:           db,
		readFactory:  readFactory,
		writeFactory: writeFactory,
	}
	uow.initRepositories()
	return uow
}

// Begin opens a new transaction and returns a unit of work bound to it, with a fresh
// change tracker enabled. The receiver is left unchanged, so the same base unit of
// work can start further transactions. Callers must Commit or Rollback the returned
// value.
func (u *UnitOfWork[R, W]) Begin() *UnitOfWork[R, W] {
	tx := u.db.Begin()
	ct := tracking.NewChangeTracker()

	nu := &UnitOfWork[R, W]{
		db:            u.db,
		tx:            tx,
		changeTracker: ct,
		tracking:      true,
		readFactory:   u.readFactory,
		writeFactory:  u.writeFactory,
	}

	nu.initRepositories()

	return nu
}

// RepositoryContext returns the context used to build repositories for this unit of
// work. changeTracker is nil until Begin has been called.
func (u *UnitOfWork[R, W]) RepositoryContext() RepositoryContext {
	return RepositoryContext{
		db:            u.db,
		tx:            u.tx,
		changeTracker: u.changeTracker,
	}
}

func (u *UnitOfWork[R, W]) initRepositories() {
	ctx := u.RepositoryContext()
	if u.readFactory != nil {
		u.ReadRepositories = u.readFactory(ctx)
	}
	if u.writeFactory != nil {
		u.WriteRepositories = u.writeFactory(ctx)
	}
}

// GetReadRepository returns a pointer to the read repository aggregate.
func (u *UnitOfWork[R, W]) GetReadRepository() *R {
	return &u.ReadRepositories
}

// GetWriteRepository returns a pointer to the write repository aggregate.
func (u *UnitOfWork[R, W]) GetWriteRepository() *W {
	return &u.WriteRepositories
}

// Commit tracks the change graphs reachable from roots, persists every detected
// change, then commits the transaction and disposes the unit of work.
//
// Entities are matched between the tracking snapshot and the current graph, producing
// Added, Updated, and Removed changes. Added entities that were hard-deleted are
// skipped, Updated entities that were hard-deleted are physically deleted, and other
// updates are persisted as saves. Any failure rolls the transaction back and returns
// the error. Commit requires a tracking unit of work created by Begin; otherwise it
// returns an error.
func (u *UnitOfWork[R, W]) Commit(ctx context.Context, roots ...any) (err error) {
	if !u.tracking || u.tx == nil || u.changeTracker == nil {
		return errors.New("can not save without transaction")
	}

	total := 0

	for _, root := range roots {
		chgs, err := u.changeTracker.Track(root)
		if err != nil {
			_ = u.Rollback()
			return err
		}
		total += len(chgs)

		for _, ch := range chgs {
			switch ch.Status {
			case tracking.ChangeStatus.Added:
				// A newly created entity that was hard-deleted must not be inserted.
				if tracking.IsHardDeleted(ch.Data) {
					continue
				}
				if err := u.tx.
					WithContext(ctx).
					Session(&gorm.Session{FullSaveAssociations: false}).
					Omit(clause.Associations).
					Create(ch.Data).Error; err != nil {
					_ = u.Rollback()
					return err
				}

			case tracking.ChangeStatus.Updated:
				// A hard-deleted entity is removed with a real DELETE.
				if tracking.IsHardDeleted(ch.Data) {
					if err := u.tx.WithContext(ctx).Delete(ch.Data).Error; err != nil {
						_ = u.Rollback()
						return err
					}
					continue
				}

				// A soft delete is still just an UPDATE, and every Updated change is
				// saved. The IsSoftDeleted check is kept for readability only.
				_ = tracking.IsSoftDeleted(ch.Data)

				if err := u.tx.
					WithContext(ctx).
					Session(&gorm.Session{FullSaveAssociations: false}).
					Omit(clause.Associations).
					Save(ch.Data).Error; err != nil {
					_ = u.Rollback()
					return err
				}

			case tracking.ChangeStatus.Removed:
				// An entity removed from the graph is deleted for real.
				if err := u.tx.WithContext(ctx).Delete(ch.Data).Error; err != nil {
					_ = u.Rollback()
					return err
				}
			}
		}

		// v := reflect.ValueOf(root)

		// eventsField := v.FieldByName("Events")
		// if eventsField.IsValid() && eventsField.CanInterface() {
		// 	events, ok := eventsField.Interface().([]contract.Event)
		// 	if ok && len(events) > 0 {
		// 		for _, event := range events {
		// 			_ = event
		// 			// asynq.Enqueue(event)
		// 		}

		// 		clearMethod := v.MethodByName("ClearEvents")
		// 		if clearMethod.IsValid() && clearMethod.CanInterface() {
		// 			clearMethod.Call(nil)
		// 		}
		// 	}
		// }
	}

	if err := u.tx.Commit().Error; err != nil {
		_ = u.Rollback()
		return err
	}

	u.Dispose()
	_ = total
	return nil
}

// Rollback aborts the transaction and disposes the unit of work. It is a no-op that
// returns nil when the unit of work has already been disposed or has no transaction.
func (u *UnitOfWork[R, W]) Rollback() error {
	if u.disposed || u.tx == nil {
		return nil
	}
	err := u.tx.Rollback().Error
	u.Dispose()
	return err
}

// EnsureRollback rolls back the transaction if it is still active.
// It is safe to call after Commit, because Commit disposes the transaction.
// Designed to be used with 'defer' to automatically rollback on any error.
func (u *UnitOfWork[R, W]) EnsureRollback() {
	_ = u.Rollback()
}

// Dispose releases the transaction and change tracker and marks the unit of work as
// disposed. Further Commit, Rollback, or EnsureRollback calls become no-ops. It does
// not roll back; use Rollback or EnsureRollback for that.
func (u *UnitOfWork[R, W]) Dispose() {
	if u.disposed {
		return
	}
	u.tx = nil
	u.changeTracker = nil
	u.disposed = true
}
