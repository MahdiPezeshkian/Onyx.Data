// Package contract declares the interfaces that repositories and specifications
// depend on: update commands, domain events, and query specifications.
package contract

// Command exposes a raw SQL UPDATE statement and its arguments. Repositories execute
// the returned query directly, so values must be passed as parameters in args rather
// than interpolated into the query string.
type Command interface {
	// RawUpdateQuery returns the UPDATE statement and the arguments bound to its
	// placeholders.
	RawUpdateQuery() (string, []any)
}
