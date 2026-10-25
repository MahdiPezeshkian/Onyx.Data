package entity

import "time"

// Event is a domain event attached to an entity. Implementations are collected by
// BaseEntity.AddEvent and clearable via BaseEntity.ClearEvents.
type Event interface {
	// EventName returns the stable name identifying the event type.
	EventName() string
	// OccurredAt returns the time the event took place.
	OccurredAt() time.Time
	// EventID returns the unique identifier of the event instance.
	EventID() string
}
