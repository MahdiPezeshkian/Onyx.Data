package tracking

import "testing"

// tracked is a minimal entity shape the tracker can key by its Id field.
type tracked struct {
	Id          int
	Name        string
	UpdatedAt   string
	IsDeleted   bool
	HardDeleted bool
}

// container is the change-graph root: the tracker walks struct fields, so tracked
// entities are reached through a slice field rather than passed directly.
type container struct {
	Items []tracked
}

func statusesOf(changes []Change) map[int]int {
	out := make(map[int]int, len(changes))
	for _, c := range changes {
		out[c.Status]++
	}
	return out
}

func TestTrackDetectsAddedAndUpdated(t *testing.T) {
	tracker := NewChangeTracker()

	kept := &tracked{Id: 1, Name: "kept"}
	other := &tracked{Id: 2, Name: "other"}
	if err := tracker.Before(&container{Items: []tracked{*kept, *other}}); err != nil {
		t.Fatalf("Before: %v", err)
	}

	kept.Name = "changed"
	added := &tracked{Id: 3, Name: "added"}

	// "other" (Id 2) is dropped from the graph here.
	changes, err := tracker.Track(&container{Items: []tracked{*kept, *added}})
	if err != nil {
		t.Fatalf("Track: %v", err)
	}

	got := statusesOf(changes)
	if got[ChangeStatus.Updated] != 1 {
		t.Fatalf("expected 1 updated change, got %d (changes: %+v)", got[ChangeStatus.Updated], changes)
	}
	if got[ChangeStatus.Added] != 1 {
		t.Fatalf("expected 1 added change, got %d (changes: %+v)", got[ChangeStatus.Added], changes)
	}
}

// TestTrackDoesNotReportGraphRemovals documents a current limitation: Track builds
// its "before" map only from the keys present in the new graph, so an entity that
// disappears from the graph is not reported as Removed. See the README "Current
// limitations" section.
func TestTrackDoesNotReportGraphRemovals(t *testing.T) {
	tracker := NewChangeTracker()

	if err := tracker.Before(&container{Items: []tracked{{Id: 1}, {Id: 2}}}); err != nil {
		t.Fatalf("Before: %v", err)
	}

	changes, err := tracker.Track(&container{Items: []tracked{{Id: 1}}})
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	for _, c := range changes {
		if c.Status == ChangeStatus.Removed {
			t.Fatalf("unexpected Removed change: %+v", c)
		}
	}
}

func TestTrackIgnoresTimestampOnlyChanges(t *testing.T) {
	tracker := NewChangeTracker()

	item := &tracked{Id: 1, Name: "same", UpdatedAt: "t0"}
	if err := tracker.Before(item); err != nil {
		t.Fatalf("Before: %v", err)
	}

	item.UpdatedAt = "t1"
	changes, err := tracker.Track(item)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes for an UpdatedAt-only diff, got %+v", changes)
	}
}

func TestTrackWithoutSnapshotReportsAdded(t *testing.T) {
	tracker := NewChangeTracker()

	changes, err := tracker.Track(&tracked{Id: 7, Name: "fresh"})
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if len(changes) != 1 || changes[0].Status != ChangeStatus.Added {
		t.Fatalf("expected a single Added change, got %+v", changes)
	}
}

func TestIsSoftDeletedAndHardDeleted(t *testing.T) {
	if !IsSoftDeleted(&tracked{IsDeleted: true}) {
		t.Fatal("IsSoftDeleted should be true")
	}
	if IsSoftDeleted(&tracked{}) {
		t.Fatal("IsSoftDeleted should be false")
	}
	if IsSoftDeleted(42) {
		t.Fatal("IsSoftDeleted should be false for non-structs")
	}

	if !IsHardDeleted(&tracked{HardDeleted: true}) {
		t.Fatal("IsHardDeleted should be true")
	}
	if IsHardDeleted(&tracked{}) {
		t.Fatal("IsHardDeleted should be false")
	}
	if IsHardDeleted(nil) {
		t.Fatal("IsHardDeleted should be false for nil")
	}
}
