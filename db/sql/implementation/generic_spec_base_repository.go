package implementation

import (
	"context"
	. "github.com/MahdiPezeshkian/Onyx.Data/db/sql/contract"
	"github.com/MahdiPezeshkian/Onyx.Data/db/tracking"

	"gorm.io/gorm"
)

// GenericSpecBaseRepository is the shared base for repositories that support
// specification-based queries over entities of type T. It is embedded by
// GenericRepository.
//
// When changeTracker is non-nil, every entity loaded through the specification methods
// is recorded with the tracker so a later unit-of-work Commit can detect changes.
type GenericSpecBaseRepository[T any] struct {
	tx            *gorm.DB
	changeTracker *tracking.ChangeTracker
}

// NewGenericSpecBaseRepository builds a specification-based repository over tx. Pass a
// non-nil ct to enable change tracking of loaded entities.
func NewGenericSpecBaseRepository[T any](tx *gorm.DB, ct *tracking.ChangeTracker) *GenericSpecBaseRepository[T] {
	return &GenericSpecBaseRepository[T]{tx: tx, changeTracker: ct}
}

// GetBySpec returns the first entity matching spec. Preloads are applied first; a
// non-empty RawQuery is executed directly, otherwise spec.Apply builds the query. The
// returned pointer is always non-nil; when the query fails it may point at a
// zero-valued entity, so callers must check the error before using the result. The
// loaded entity is recorded with the change tracker when one is configured.
func (r *GenericSpecBaseRepository[T]) GetBySpec(ctx context.Context, spec Specification[T]) (*T, error) {
	var result T

	tx := r.tx

	for _, p := range spec.Preloads() {
		tx = tx.Preload(p)
	}

	if query, args := spec.RawQuery(); len(query) > 0 {
		err := tx.Raw(query, args...).Scan(&result).Error
		return &result, err
	}

	tx = spec.Apply(tx)

	err := tx.WithContext(ctx).Find(&result).Error
	if err != nil {
		return nil, err
	}

	if r.changeTracker != nil {
		if err := r.changeTracker.Before(&result); err != nil {
			return nil, err
		}
	}

	return &result, err
}

// GetListBySpec returns every entity matching spec. Preloads are applied first; a
// non-empty RawQuery is executed directly, otherwise spec.Apply builds the query. It
// returns an error and a nil slice when the query fails, and records the loaded
// entities with the change tracker when one is configured.
func (r *GenericSpecBaseRepository[T]) GetListBySpec(ctx context.Context, spec Specification[T]) ([]T, error) {
	var result []T

	tx := r.tx

	for _, p := range spec.Preloads() {
		tx = tx.Preload(p)
	}

	if query, args := spec.RawQuery(); len(query) > 0 {
		err := tx.Raw(query, args...).Scan(&result).Error
		return result, err
	}

	tx = spec.Apply(tx)

	err := tx.WithContext(ctx).Find(&result).Error
	if err != nil {
		return nil, err
	}

	if r.changeTracker != nil {
		for i := range result {
			if err := r.changeTracker.Before(&result[i]); err != nil {
				return nil, err
			}
		}
	}

	return result, err
}

// GetPagedBySpec executes a paginated query based on the given Specification and returns the result set,
// the number of items in the current page, and the total number of matching records.
//
// Usage notes:
// - pageNember is zero-based (0 means first page). Negative values are normalized to 0.
// - pageSize defines the number of items per page; values <= 0 default to 10.
// - spec controls filtering, preloads, and query behavior:
//   - Preloads() are applied before executing the query.
//   - If spec.RawQuery() is provided, it will be executed directly with pagination.
//   - Otherwise, spec.Apply(tx) is used to build the query via GORM.
//   - cachedTotalCount is optional; if provided, the method skips the COUNT query and uses this value
//     as totalCount (useful for performance when the total count is already known).
//
// Returns:
// - result: slice of entities for the requested page
// - count: number of entities in the current page
// - totalCount: total number of records matching the specification (ignoring pagination)
// - err: any error occurred during query execution
func (r *GenericSpecBaseRepository[T]) GetPagedBySpec(ctx context.Context, spec Specification[T], pageNember, pageSize int, cachedTotalCount ...int) (result []T, count int, totalCount int64, err error) {

	if pageNember < 0 {
		pageNember = 0
	}
	if pageSize <= 0 {
		pageSize = 10
	}

	tx := r.tx

	for _, p := range spec.Preloads() {
		tx = tx.Preload(p)
	}

	if query, args := spec.RawQuery(); len(query) > 0 {
		err = tx.Raw(query, args...).Limit(pageSize).Offset(pageSize * pageNember).Scan(&result).Error
		if err != nil {
			return
		}

		count = len(result)

		if len(cachedTotalCount) != 0 {
			totalCount = int64(cachedTotalCount[0])
			return
		}

		err = tx.Raw(query, args...).Count(&totalCount).Error
		if err != nil {
			return
		}
		return
	}

	tx = spec.Apply(tx)

	err = tx.WithContext(ctx).Limit(pageSize).Offset(pageSize * pageNember).Find(&result).Error
	if err != nil {
		return
	}

	count = len(result)

	if len(cachedTotalCount) != 0 {
		totalCount = int64(cachedTotalCount[0])
		return
	}

	err = tx.WithContext(ctx).Find(new(T)).Count(&totalCount).Error
	if err != nil {
		return
	}

	return
}
