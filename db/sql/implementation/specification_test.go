package implementation

import "testing"

func TestMapOp(t *testing.T) {
	cases := map[string]string{
		"=":    "=",
		"!=":   "!=",
		">":    ">",
		"<":    "<",
		"gte":  ">=",
		">=":   ">=",
		"lte":  "<=",
		"<=":   "<=",
		"LIKE": "LIKE",
	}
	for input, want := range cases {
		got, err := mapOp(input)
		if err != nil {
			t.Fatalf("mapOp(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("mapOp(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := mapOp("drop"); err == nil {
		t.Fatal("mapOp should reject an unknown operator")
	}
}

func TestHasFilters(t *testing.T) {
	if hasFilters(FilterGroup{}) {
		t.Fatal("an empty group should have no filters")
	}
	if !hasFilters(FilterGroup{Filters: []Filter{{Field: "id"}}}) {
		t.Fatal("a group with a filter should report filters")
	}
	nested := FilterGroup{Groups: []FilterGroup{{Filters: []Filter{{Field: "id"}}}}}
	if !hasFilters(nested) {
		t.Fatal("a nested group with a filter should report filters")
	}
}

func TestMergeWithMandatory(t *testing.T) {
	user := FilterGroup{Filters: []Filter{{Field: "id"}}}
	mandatory := &FilterGroup{Filters: []Filter{{Field: "tenantId"}}}

	// No mandatory filter: the user filter is returned unchanged.
	if got := mergeWithMandatory(user, nil); len(got.Filters) != 1 || got.Filters[0].Field != "id" {
		t.Fatalf("mergeWithMandatory with nil mandatory = %+v", got)
	}

	// No user filter: the mandatory filter is returned.
	if got := mergeWithMandatory(FilterGroup{}, mandatory); len(got.Filters) != 1 || got.Filters[0].Field != "tenantId" {
		t.Fatalf("mergeWithMandatory with empty user = %+v", got)
	}

	// Both present: they are combined under an AND group.
	got := mergeWithMandatory(user, mandatory)
	if got.Operator != "AND" || len(got.Groups) != 2 {
		t.Fatalf("mergeWithMandatory should produce an AND group of two, got %+v", got)
	}
}

func TestValidateUpdateQuery(t *testing.T) {
	valid := []string{
		"UPDATE products SET price = ? WHERE id = ?",
		"update products set price = ? where id = ?",
	}
	for _, q := range valid {
		if err := validateUpdateQuery(q); err != nil {
			t.Fatalf("validateUpdateQuery(%q) returned error: %v", q, err)
		}
	}

	invalid := []string{
		"",                                  // empty
		"DELETE FROM products WHERE id = 1", // not an update
		"UPDATE products SET price = 1",     // missing WHERE
		// Forbidden keywords are matched with surrounding spaces, so this is
		// rejected.
		"UPDATE products SET price = 1 WHERE id in (select id from t) ; delete from products",
	}
	for _, q := range invalid {
		if err := validateUpdateQuery(q); err == nil {
			t.Fatalf("validateUpdateQuery(%q) should have failed", q)
		}
	}
}

func TestNewSpecPreloadsAndRawQuery(t *testing.T) {
	spec := NewSpec[struct{}](Request{}, RequestRequestConfig, "Customer", "Product")

	preloads := spec.Preloads()
	if len(preloads) != 2 || preloads[0] != "Customer" || preloads[1] != "Product" {
		t.Fatalf("Preloads() = %v, want [Customer Product]", preloads)
	}
	if q, args := spec.RawQuery(); q != "" || args != nil {
		t.Fatalf("RawQuery() = (%q, %v), want empty", q, args)
	}
}
