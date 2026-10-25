// Package implementation contains the GORM-backed repositories and the specification
// types used to build filtered, sorted, and paginated queries.
//
// Repositories are always created with an explicit *gorm.DB, which may be a root
// handle or an active transaction, so a repository performs no transaction management
// of its own.
package implementation

import (
	"context"
	"github.com/MahdiPezeshkian/Onyx.Data/db/tracking"
	genericlist "github.com/MahdiPezeshkian/Onyx.Data/genericlist"

	"gorm.io/gorm"
)

// GenericRepository is a repository for entities of type T. It embeds
// GenericSpecBaseRepository, so specification-based queries are available as well as
// the id-based methods defined here.
//
// When changeTracker is non-nil (write repositories), every loaded entity is recorded
// with the tracker so a later unit-of-work Commit can detect changes. Read
// repositories pass a nil tracker and load entities without tracking.
type GenericRepository[T any] struct {
	GenericSpecBaseRepository[T]
	// tx            *gorm.DB
	changeTracker *tracking.ChangeTracker
}

// NewRepository builds a repository over tx. Pass a non-nil ct to enable change
// tracking of loaded entities.
func NewRepository[T any](tx *gorm.DB, ct *tracking.ChangeTracker) *GenericRepository[T] {
	return &GenericRepository[T]{
		// tx:                        tx,
		changeTracker:             ct,
		GenericSpecBaseRepository: GenericSpecBaseRepository[T]{tx: tx, changeTracker: ct},
	}
}

// GetAllAsList returns all entities as a genericlist.List. Loaded entities are
// recorded with the change tracker when one is configured.
func (r *GenericRepository[T]) GetAllAsList(ctx context.Context) (genericlist.List[T, []T], error) {
	var entities []T

	if err := r.tx.WithContext(ctx).Find(&entities).Error; err != nil {
		return genericlist.List[T, []T]{}, err
	}

	if r.changeTracker != nil {
		for i := range entities {
			if err := r.changeTracker.Before(&entities[i]); err != nil {
				return genericlist.List[T, []T]{}, err
			}
		}
	}
	return *genericlist.New(entities), nil
}

// GetAll returns all entities. Loaded entities are recorded with the change tracker
// when one is configured.
func (r *GenericRepository[T]) GetAll(ctx context.Context) ([]T, error) {
	var entities []T

	if err := r.tx.WithContext(ctx).Find(&entities).Error; err != nil {
		return nil, err
	}

	if r.changeTracker != nil {
		for i := range entities {
			if err := r.changeTracker.Before(&entities[i]); err != nil {
				return nil, err
			}
		}
	}
	return entities, nil
}

// GetByID returns the entity with the given id, or an error if no row matches. The
// loaded entity is recorded with the change tracker when one is configured.
func (r *GenericRepository[T]) GetByID(ctx context.Context, id any) (*T, error) {
	var entity T

	if err := r.tx.WithContext(ctx).First(&entity, "id = ?", id).Error; err != nil {
		return nil, err
	}

	if r.changeTracker != nil {
		if err := r.changeTracker.Before(&entity); err != nil {
			return nil, err
		}
	}
	return &entity, nil
}

// Delete removes the entity with the given id. The delete is soft or hard according to
// the model's GORM configuration.
func (r *GenericRepository[T]) Delete(ctx context.Context, id any) error {
	var entity T
	return r.tx.WithContext(ctx).Delete(&entity, id).Error
}

// GetByIDWithPreload returns the entity with the given id after preloading the named
// association paths, or an error if no row matches. The loaded entity is recorded with
// the change tracker when one is configured.
func (r *GenericRepository[T]) GetByIDWithPreload(ctx context.Context, id any, preloads ...string) (*T, error) {
	var entity T

	tx := r.tx

	for _, p := range preloads {
		tx = tx.Preload(p)
	}

	if err := tx.WithContext(ctx).First(&entity, "id = ?", id).Error; err != nil {
		return nil, err
	}

	if r.changeTracker != nil {
		if err := r.changeTracker.Before(&entity); err != nil {
			return nil, err
		}
	}

	return &entity, nil
}
