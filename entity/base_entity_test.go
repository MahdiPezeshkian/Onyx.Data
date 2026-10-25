package entity

import (
	"testing"
	"time"
)

type testEvent struct {
	name string
	at   time.Time
}

func (e testEvent) EventName() string     { return e.name }
func (e testEvent) OccurredAt() time.Time { return e.at }
func (e testEvent) EventID() string       { return e.name + "-id" }

func TestInitialSetsIdAndTimestamps(t *testing.T) {
	before := time.Now().UTC()
	ent := Initial(42)
	after := time.Now().UTC()

	if ent.Id != 42 {
		t.Fatalf("Id = %d, want 42", ent.Id)
	}
	if ent.CreatedAt.Before(before) || ent.CreatedAt.After(after) {
		t.Fatalf("CreatedAt %v is outside the expected window", ent.CreatedAt)
	}
	if !ent.CreatedAt.Equal(ent.UpdatedAt) {
		t.Fatalf("CreatedAt (%v) and UpdatedAt (%v) should match after Initial", ent.CreatedAt, ent.UpdatedAt)
	}
}

func TestDeleteMarksSoftDelete(t *testing.T) {
	ent := Initial(1)
	ent.Delete()

	if !ent.IsDeleted {
		t.Fatal("Delete() should set IsDeleted")
	}
	if ent.DeletedAt.IsZero() {
		t.Fatal("Delete() should set DeletedAt")
	}
	if ent.HardDeleted {
		t.Fatal("Delete() should not set HardDeleted")
	}
}

func TestHardDeleteMarksHardDelete(t *testing.T) {
	ent := Initial(1)
	ent.HardDelete()

	if !ent.HardDeleted {
		t.Fatal("HardDelete() should set HardDeleted")
	}
	if ent.IsDeleted {
		t.Fatal("HardDelete() should not set IsDeleted")
	}
}

func TestUpdateRefreshesUpdatedAt(t *testing.T) {
	ent := Initial(1)
	original := ent.UpdatedAt

	time.Sleep(time.Millisecond)
	ent.Update()

	if !ent.UpdatedAt.After(original) {
		t.Fatalf("Update() did not advance UpdatedAt (%v -> %v)", original, ent.UpdatedAt)
	}
}

func TestAddAndClearEvents(t *testing.T) {
	ent := Initial(1)
	ent.AddEvent(testEvent{name: "created", at: time.Now().UTC()})
	ent.AddEvent(testEvent{name: "updated", at: time.Now().UTC()})

	if len(ent.Events) != 2 {
		t.Fatalf("AddEvent() left %d events, want 2", len(ent.Events))
	}
	if ent.Events[0].EventName() != "created" {
		t.Fatalf("first event name = %q, want %q", ent.Events[0].EventName(), "created")
	}

	ent.ClearEvents()
	if len(ent.Events) != 0 {
		t.Fatalf("ClearEvents() left %d events, want 0", len(ent.Events))
	}
}
