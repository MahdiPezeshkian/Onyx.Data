package contract

import "gorm.io/gorm"

// Specification describes a query over entities of type T in a form that repository
// implementations can execute. Implementations decide how the three methods combine:
// Preloads and Apply are used for GORM-built queries, while a non-empty RawQuery
// result is executed directly.
type Specification[T any] interface {
	// Apply adds the specification's conditions to db and returns the resulting query.
	Apply(*gorm.DB) *gorm.DB
	// RawQuery returns a raw SQL statement and its arguments. Returning an empty
	// query means the caller should fall back to Apply.
	RawQuery() (string, []any)
	// Preloads returns the association paths to preload before the query runs.
	Preloads() []string
}
