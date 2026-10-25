package contract

import "time"

// Event is a domain event that can be attached to an entity. It mirrors
// entity.Event; keep both definitions in sync when the contract changes.
type Event interface {
	// EventName returns the stable name identifying the event type.
	EventName() string
	// OccurredAt returns the time the event took place.
	OccurredAt() time.Time
	// EventID returns the unique identifier of the event instance.
	EventID() string
}
