package genericlist

import "testing"

func ints(values ...int) *List[int, []int] {
	return New[int](append([]int{}, values...))
}

func TestCountAnyAndEmpty(t *testing.T) {
	l := ints(1, 2, 3)
	if l.Count() != 3 {
		t.Fatalf("Count() = %d, want 3", l.Count())
	}
	if !l.Any() {
		t.Fatal("Any() = false, want true")
	}
	if l.IsEmpty() {
		t.Fatal("IsEmpty() = true, want false")
	}

	empty := ints()
	if empty.Count() != 0 || empty.Any() || !empty.IsEmpty() {
		t.Fatal("empty list accessors are inconsistent")
	}
	if !empty.All(func(int) bool { return false }) {
		t.Fatal("All() on an empty list should be true")
	}
}

func TestFirstLastAndDefaults(t *testing.T) {
	l := ints(10, 20, 30)
	if l.First() != 10 {
		t.Fatalf("First() = %d, want 10", l.First())
	}
	if l.Last() != 30 {
		t.Fatalf("Last() = %d, want 30", l.Last())
	}

	if v, ok := l.FirstOrDefault(); v != 10 || !ok {
		t.Fatalf("FirstOrDefault() = (%d, %v), want (10, true)", v, ok)
	}
	if v, ok := l.LastOrDefault(); v != 30 || !ok {
		t.Fatalf("LastOrDefault() = (%d, %v), want (30, true)", v, ok)
	}

	empty := ints()
	if empty.First() != 0 || empty.Last() != 0 {
		t.Fatal("empty list should return the zero value")
	}
	if v, ok := empty.FirstOrDefault(); v != 0 || ok {
		t.Fatalf("empty FirstOrDefault() = (%d, %v), want (0, false)", v, ok)
	}
	if v, ok := empty.LastOrDefault(); v != 0 || ok {
		t.Fatalf("empty LastOrDefault() = (%d, %v), want (0, false)", v, ok)
	}
}

func TestWhereSelectAndFind(t *testing.T) {
	l := ints(1, 2, 3, 4)

	even := l.Where(func(v int) bool { return v%2 == 0 })
	if got := even.ToSlice(); len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Fatalf("Where() = %v, want [2 4]", got)
	}

	doubled := Select(l, func(v int) int { return v * 2 })
	if got := doubled.ToSlice(); len(got) != 4 || got[3] != 8 {
		t.Fatalf("Select() = %v, want [2 4 6 8]", got)
	}

	if v, ok := l.Find(func(v int) bool { return v == 3 }); v != 3 || !ok {
		t.Fatalf("Find() = (%d, %v), want (3, true)", v, ok)
	}
	if v, ok := l.Find(func(v int) bool { return v == 99 }); v != 0 || ok {
		t.Fatalf("Find() miss = (%d, %v), want (0, false)", v, ok)
	}
	if !l.AnyBy(func(v int) bool { return v > 3 }) {
		t.Fatal("AnyBy() should find 4")
	}
	if !l.Contains(func(v int) bool { return v == 1 }) {
		t.Fatal("Contains() should find 1")
	}
}

func TestSkipTakeAndElementAt(t *testing.T) {
	l := ints(1, 2, 3, 4, 5)

	if got := l.Skip(2).ToSlice(); len(got) != 3 || got[0] != 3 {
		t.Fatalf("Skip(2) = %v, want [3 4 5]", got)
	}
	if got := l.Skip(5).ToSlice(); len(got) != 0 {
		t.Fatalf("Skip(5) = %v, want empty", got)
	}
	if got := l.Take(2).ToSlice(); len(got) != 2 || got[1] != 2 {
		t.Fatalf("Take(2) = %v, want [1 2]", got)
	}
	if got := l.Take(10).ToSlice(); len(got) != 5 {
		t.Fatalf("Take(10) = %v, want the whole list", got)
	}

	if l.ElementAt(2) != 3 {
		t.Fatalf("ElementAt(2) = %d, want 3", l.ElementAt(2))
	}
	if v, ok := l.ElementAtOrDefault(4); v != 5 || !ok {
		t.Fatalf("ElementAtOrDefault(4) = (%d, %v), want (5, true)", v, ok)
	}
	if v, ok := l.ElementAtOrDefault(-1); v != 0 || ok {
		t.Fatalf("ElementAtOrDefault(-1) = (%d, %v), want (0, false)", v, ok)
	}
}

func TestElementAtOutOfRangeCapturesPanic(t *testing.T) {
	l := ints(1, 2, 3)
	_ = l.ElementAt(10)
	if l.Err() == nil {
		t.Fatal("expected an error after an out-of-range ElementAt")
	}
	if l.ErrorMassge() == "" {
		t.Fatal("expected a non-empty error message")
	}
}

func TestDistinctGroupByAndOrderBy(t *testing.T) {
	l := ints(1, 2, 2, 3, 1)

	if got := Distinct(l).ToSlice(); len(got) != 3 {
		t.Fatalf("Distinct() = %v, want 3 unique values", got)
	}

	groups := GroupBy(l, func(v int) int { return v % 2 })
	if len(groups[0]) != 2 || len(groups[1]) != 3 {
		t.Fatalf("GroupBy() = %v, want 2 evens and 3 odds", groups)
	}

	ordered := OrderBy([]int{3, 1, 2}, func(a, b int) bool { return a < b })
	if ordered[0] != 1 || ordered[2] != 3 {
		t.Fatalf("OrderBy() = %v, want ascending order", ordered)
	}
}
