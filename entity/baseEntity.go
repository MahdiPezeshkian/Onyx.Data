// Package entity contains the base entity shape and domain event contract used by
// Onyx.Data models.
package entity

import (
	"time"
)

// BaseEntity is embedded by concrete models. T is the identifier type.
//
// Delete marks a soft delete by setting DeletedAt and IsDeleted; HardDeleted and
// Events are persistence-excluded (gorm:"-") and are carried only in memory for the
// current unit of work.
type BaseEntity[T any] struct {
	Id          T
	UpdatedAt   time.Time
	CreatedAt   time.Time
	DeletedAt   time.Time
	IsDeleted   bool    // soft-delete flag
	HardDeleted bool    `gorm:"-"` // hard-delete flag, excluded from persistence
	Events      []Event `gorm:"-:all"`
}

// Initial returns a new BaseEntity with the given id and both CreatedAt and UpdatedAt
// set to the current UTC time.
func Initial[T any](id T) BaseEntity[T] {
	now := time.Now().UTC()

	ent := BaseEntity[T]{
		Id:        id,
		CreatedAt: now,
		UpdatedAt: now,
	}

	return ent
}

// AddEvent appends an event to the entity's in-memory event list.
func (b *BaseEntity[T]) AddEvent(event Event) {
	b.Events = append(b.Events, event)
}

// ClearEvents removes all in-memory events from the entity.
func (b *BaseEntity[T]) ClearEvents() {
	b.Events = []Event{}
}

// Delete marks the entity as soft deleted by setting DeletedAt and IsDeleted, and
// refreshes UpdatedAt. The row is removed or updated according to the unit of work's
// persistence rules, not by this call.
func (b *BaseEntity[T]) Delete() {
	b.DeletedAt = time.Now().UTC()
	b.IsDeleted = true
	b.Update()
}

// HardDelete flags the entity for a physical DELETE by setting HardDeleted, and
// refreshes UpdatedAt. The delete is performed when the unit of work is committed.
func (b *BaseEntity[T]) HardDelete() {
	b.HardDeleted = true
	b.Update()
}

// Update refreshes UpdatedAt to the current UTC time.
func (b *BaseEntity[T]) Update() {
	b.UpdatedAt = time.Now().UTC()
}
