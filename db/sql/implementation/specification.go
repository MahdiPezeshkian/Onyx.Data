package implementation

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

/* ============================
   REQUEST MODELS
============================ */

// Pagination is the requested page and page size for a query.
type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

// Sort orders results by Field in Direction ("ASC" or "DESC").
type Sort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

// Filter is a single field comparison: Field, operator Op, and the value to compare.
type Filter struct {
	Field string      `json:"field"`
	Op    string      `json:"op"`
	Value interface{} `json:"value"`
}

// FilterGroup combines Filters and nested Groups with Operator ("AND" or "OR").
// Any operator other than "OR" is treated as "AND".
type FilterGroup struct {
	Operator string        `json:"operator"`
	Filters  []Filter      `json:"filters"`
	Groups   []FilterGroup `json:"groups"`
}

// Request is the query request accepted by NewSpec: pagination, sorting, and the
// user-supplied filter tree.
type Request struct {
	Pagination Pagination  `json:"pagination"`
	Sorts      []Sort      `json:"sorts"`
	Filter     FilterGroup `json:"filter"`
}

/* ============================
   SECURITY CONFIG
============================ */

// FieldType classifies an allow-listed field. It is descriptive metadata; filtering
// and sorting are gated by FieldConfig permissions rather than by this type.
type FieldType int

const (
	// FieldString marks a string field.
	FieldString FieldType = iota
	// FieldNumber marks a numeric field.
	FieldNumber
	// FieldBool marks a boolean field.
	FieldBool
	// FieldTime marks a timestamp field.
	FieldTime
)

// FieldConfig describes one request field: the name exposed to clients, the database
// column it maps to, and whether filtering and sorting are permitted. AllowedOps lists
// the operators accepted for filtering on this field.
type FieldConfig struct {
	APIName     string
	ColumnName  string
	Type        FieldType
	AllowFilter bool
	AllowSort   bool
	AllowedOps  []string
}

// RequestConfig is the allow-list that constrains a query. Fields maps request field
// names to their configuration; filters, sorts, or sort fields outside this map are
// rejected or ignored. MaxPageSize caps the page size when positive. DefaultSorts
// applies when the request supplies no sorts. MandatoryFilter is ANDed with the
// user-supplied filter so callers can enforce access-scoping conditions.
type RequestConfig struct {
	Fields          map[string]FieldConfig
	MaxPageSize     int
	DefaultSorts    []Sort
	MandatoryFilter *FilterGroup
}

/* ============================
   HELPER FUNCS
============================ */

func normalizeOp(op string) string { return strings.ToLower(strings.TrimSpace(op)) }

func mapOp(op string) (string, error) {
	switch normalizeOp(op) {
	case "=":
		return "=", nil
	case "!=":
		return "!=", nil
	case ">":
		return ">", nil
	case "<":
		return "<", nil
	case ">=", "gte":
		return ">=", nil
	case "<=", "lte":
		return "<=", nil
	case "like":
		return "LIKE", nil
	default:
		return "", fmt.Errorf("invalid operator: %s", op)
	}
}

func hasFilters(g FilterGroup) bool {
	if len(g.Filters) > 0 {
		return true
	}
	for _, gr := range g.Groups {
		if hasFilters(gr) {
			return true
		}
	}
	return false
}

func mergeWithMandatory(user FilterGroup, mandatory *FilterGroup) FilterGroup {
	if mandatory == nil || !hasFilters(*mandatory) {
		return user
	}
	if !hasFilters(user) {
		return *mandatory
	}
	return FilterGroup{
		Operator: "AND",
		Groups:   []FilterGroup{*mandatory, user},
	}
}

/* ============================
   SPECIFICATION IMPLEMENTATION
============================ */

// FilterSpecification implements Specification for the allow-listed request model. It
// never returns a raw query; filtering, sorting, and pagination are all applied through
// Apply.
type FilterSpecification[T any] struct {
	req      Request
	cfg      *RequestConfig
	preloads []string
}

// NewSpec builds a specification for req, validated against cfg. cfg may be nil, in
// which case Apply leaves the query untouched. preloads are the association paths
// returned by Preloads.
func NewSpec[T any](req Request, cfg *RequestConfig, preloads ...string) *FilterSpecification[T] {
	return &FilterSpecification[T]{
		req:      req,
		cfg:      cfg,
		preloads: preloads,
	}
}

// Preloads returns the association paths configured for this specification.
func (s *FilterSpecification[T]) Preloads() []string {
	return s.preloads
}

// RawQuery always returns an empty query and nil arguments; FilterSpecification builds
// its query through Apply instead.
func (s *FilterSpecification[T]) RawQuery() (string, []any) {
	return "", nil
}

// Apply merges the mandatory filter with the request filter, then applies filtering,
// sorting, and pagination to db. Page defaults to 1 and page size to 10, capped by
// MaxPageSize when it is positive. Sort fields that are not allow-listed are skipped,
// and a filter build error leaves the query unfiltered rather than returning an error.
func (s *FilterSpecification[T]) Apply(db *gorm.DB) *gorm.DB {
	cfg := s.cfg
	if cfg == nil {
		return db
	}

	/* ----- 1) Pagination ----- */
	page := s.req.Pagination.Page
	if page < 1 {
		page = 1
	}

	pageSize := s.req.Pagination.PageSize
	if pageSize <= 0 {
		pageSize = 10
	}
	if cfg.MaxPageSize > 0 && pageSize > cfg.MaxPageSize {
		pageSize = cfg.MaxPageSize
	}

	offset := (page - 1) * pageSize

	/* ----- 2) Filters ----- */
	finalFilter := mergeWithMandatory(s.req.Filter, cfg.MandatoryFilter)

	if hasFilters(finalFilter) {
		sql, args, err := s.buildGroup(finalFilter)
		if err == nil && sql != "" {
			db = db.Where(sql, args...)
		}
	}

	/* ----- 3) Sorting ----- */
	if len(s.req.Sorts) == 0 && len(cfg.DefaultSorts) > 0 {
		for _, srt := range cfg.DefaultSorts {
			if f, ok := cfg.Fields[srt.Field]; ok && f.AllowSort {
				dir := strings.ToUpper(srt.Direction)
				if dir != "DESC" {
					dir = "ASC"
				}
				db = db.Order(fmt.Sprintf("%s %s", f.ColumnName, dir))
			}
		}
	} else {
		for _, srt := range s.req.Sorts {
			if f, ok := cfg.Fields[srt.Field]; ok && f.AllowSort {
				dir := strings.ToUpper(srt.Direction)
				if dir != "DESC" {
					dir = "ASC"
				}
				db = db.Order(fmt.Sprintf("%s %s", f.ColumnName, dir))
			}
		}
	}

	/* ----- 4) Pagination ----- */
	db = db.Limit(pageSize).Offset(offset)

	return db
}

/* ============================
   BUILD SQL FILTERS
============================ */

func (s *FilterSpecification[T]) buildGroup(g FilterGroup) (string, []any, error) {
	var parts []string
	var args []any

	op := strings.ToUpper(strings.TrimSpace(g.Operator))
	if op != "OR" {
		op = "AND"
	}

	/* filters */
	for _, f := range g.Filters {
		fc, ok := s.cfg.Fields[f.Field]
		if !ok || !fc.AllowFilter {
			return "", nil, fmt.Errorf("filter not allowed on field: %s", f.Field)
		}

		if !contains(fc.AllowedOps, normalizeOp(f.Op)) {
			return "", nil, fmt.Errorf("operator %s not allowed for %s", f.Op, f.Field)
		}

		sqlOp, err := mapOp(f.Op)
		if err != nil {
			return "", nil, err
		}

		parts = append(parts, fmt.Sprintf("%s %s ?", fc.ColumnName, sqlOp))
		args = append(args, f.Value)
	}

	/* nested groups */
	for _, gr := range g.Groups {
		sql, a, err := s.buildGroup(gr)
		if err != nil {
			return "", nil, err
		}
		if sql != "" {
			parts = append(parts, "("+sql+")")
			args = append(args, a...)
		}
	}

	if len(parts) == 0 {
		return "", nil, nil
	}

	return strings.Join(parts, " "+op+" "), args, nil
}

func contains(list []string, x string) bool {
	for _, v := range list {
		if normalizeOp(v) == normalizeOp(x) {
			return true
		}
	}
	return false
}
