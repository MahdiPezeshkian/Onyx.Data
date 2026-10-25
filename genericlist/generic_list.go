// Package genericlist provides a small LINQ-style, immutable-by-convention wrapper
// around slice values. Query operators return new lists; methods that can fail record
// a panic as an error retrievable through Err instead of propagating it.
package genericlist

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Enumerable[T any] interface {
	~[]T
}

// List wraps a slice of T. S is the concrete slice type stored, which must satisfy
// Enumerable[T]; operators that change the element type or shape return a
// *List[T, []T].
//
// Most accessors return the zero value of T when the list is empty, and methods
// declared with a recover guard (Last, First, FirstOrDefault, ElementAt) store any
// recovered panic in err rather than letting it escape.
type List[T any, S Enumerable[T]] struct {
	enumerable S
	err        error
}

// New wraps enumerable in a list.
func New[T any, S Enumerable[T]](enumerable S) *List[T, S] {
	return &List[T, S]{
		enumerable: enumerable,
	}
}

// Last returns the final element, or the zero value when the list is empty.
func (l *List[T, S]) Last() T {
	if !l.Any() {
		return l.zero()
	}

	defer l.handlePanic()
	return l.enumerable[len(l.enumerable)-1]
}

// Count returns the number of elements.
func (l *List[T, S]) Count() int {
	return len(l.enumerable)
}

// Any reports whether the list has at least one element.
func (l *List[T, S]) Any() bool {
	return len(l.enumerable) > 0
}

// IsEmpty reports whether the list has no elements.
func (l *List[T, S]) IsEmpty() bool {
	return len(l.enumerable) == 0
}

// First returns the first element, or the zero value when the list is empty.
func (l *List[T, S]) First() T {
	if !l.Any() {
		return l.zero()
	}

	defer l.handlePanic()
	return l.enumerable[0]
}

// FirstOrDefault returns the first element and true, or the zero value and false when
// the list is empty.
func (l *List[T, S]) FirstOrDefault() (T, bool) {
	if len(l.enumerable) == 0 {
		return l.zero(), false
	}

	defer l.handlePanic()
	return l.enumerable[0], true
}

// LastOrDefault returns the last element and true, or the zero value and false when
// the list is empty.
func (l *List[T, S]) LastOrDefault() (T, bool) {
	if len(l.enumerable) == 0 {
		return l.zero(), false
	}

	return l.enumerable[len(l.enumerable)-1], true
}

// Where returns a new list containing only the elements that satisfy predicate.
func (l *List[T, S]) Where(predicate func(T) bool) *List[T, []T] {
	result := make([]T, 0)

	for _, item := range l.enumerable {
		if predicate(item) {
			result = append(result, item)
		}
	}

	return New[T](result)
}

// AnyBy reports whether any element satisfies predicate.
func (l *List[T, S]) AnyBy(predicate func(T) bool) bool {
	for _, item := range l.enumerable {
		if predicate(item) {
			return true
		}
	}

	return false
}

// All reports whether every element satisfies predicate. It returns true for an empty
// list.
func (l *List[T, S]) All(predicate func(T) bool) bool {
	for _, item := range l.enumerable {
		if !predicate(item) {
			return false
		}
	}

	return true
}

// Contains reports whether any element satisfies predicate. It is equivalent to AnyBy.
func (l *List[T, S]) Contains(predicate func(T) bool) bool {
	for _, item := range l.enumerable {
		if predicate(item) {
			return true
		}
	}

	return false
}

// Find returns the first element satisfying predicate and true, or the zero value and
// false when no element matches.
func (l *List[T, S]) Find(predicate func(T) bool) (T, bool) {
	for _, item := range l.enumerable {
		if predicate(item) {
			return item, true
		}
	}

	return l.zero(), false
}

// Skip returns a new list without the first count elements. If count is at least the
// length, the result is empty.
func (l *List[T, S]) Skip(count int) *List[T, []T] {
	if count >= len(l.enumerable) {
		return New([]T{})
	}

	return &List[T, []T]{
		enumerable: l.enumerable[count:],
	}
}

// Take returns a new list with at most the first count elements.
func (l *List[T, S]) Take(count int) *List[T, []T] {
	if count >= len(l.enumerable) {
		return New([]T(l.enumerable))
	}

	return New([]T(l.enumerable[:count]))
}

// ElementAt returns the element at index, or the zero value when the list is empty. An
// out-of-range index panics, and the panic is captured by handlePanic into Err.
func (l *List[T, S]) ElementAt(index int) T {
	if len(l.enumerable) == 0 {
		return l.zero()
	}

	defer l.handlePanic()
	return l.enumerable[index]
}

// ElementAtOrDefault returns the element at index and true, or the zero value and false
// when index is out of range.
func (l *List[T, S]) ElementAtOrDefault(index int) (T, bool) {
	if index < 0 || index >= len(l.enumerable) {
		return l.zero(), false
	}

	return l.enumerable[index], true
}

// ToSlice returns the underlying elements as a []T.
func (l *List[T, S]) ToSlice() []T {
	return []T(l.enumerable)
}

// Distinct returns a new list with duplicate elements removed, keeping the first
// occurrence of each value.
func Distinct[T comparable](
	l *List[T, []T],
) *List[T, []T] {

	seen := make(map[T]struct{})
	result := make([]T, 0)

	for _, item := range l.enumerable {
		if _, ok := seen[item]; ok {
			continue
		}

		seen[item] = struct{}{}
		result = append(result, item)
	}

	return New(result)
}

// GroupBy groups elements into a map keyed by keySelector, preserving each group's
// original order.
func GroupBy[T any, S Enumerable[T], K comparable](
	l *List[T, S],
	keySelector func(T) K,
) map[K][]T {

	result := make(map[K][]T)

	for _, item := range l.enumerable {
		key := keySelector(item)
		result[key] = append(result[key], item)
	}

	return result
}

// OrderBy returns a new slice sorted by less, leaving items unchanged.
func OrderBy[T any](
	items []T,
	less func(a, b T) bool,
) []T {

	result := slices.Clone(items)

	slices.SortFunc(result, func(a, b T) int {
		if less(a, b) {
			return -1
		}

		if less(b, a) {
			return 1
		}

		return 0
	})

	return result
}

// Select projects each element through selector and returns a new list of the results.
func Select[T any, S Enumerable[T], R any](
	l *List[T, S],
	selector func(T) R,
) *List[R, []R] {

	result := make([]R, 0, len(l.enumerable))

	for _, item := range l.enumerable {
		result = append(result, selector(item))
	}

	return New[R](result)
}

// Err returns the error captured by the most recent recovered panic, or nil when no
// panic has occurred.
func (l *List[T, S]) Err() error {
	// defer func() {
	// 	l.err = nil
	// }()

	return l.err
}

// ErrorMassge returns the message of the captured error. It panics with a nil pointer
// dereference when no error has been captured.
func (l *List[T, S]) ErrorMassge() string {
	return l.err.Error()
}

func (l *List[T, S]) handlePanic() {
	if r := recover(); r != nil {
		var msg string

		switch v := r.(type) {
		case error:
			msg = v.Error()
		case string:
			msg = v
		default:
			msg = fmt.Sprintf("%v", v)
		}

		msg = strings.TrimPrefix(msg, "panic: ")
		msg = strings.TrimPrefix(msg, "runtime error: ")

		l.err = errors.New(msg)
	}
}

func (l *List[T, S]) zero() T {
	var zero T
	return zero
}
