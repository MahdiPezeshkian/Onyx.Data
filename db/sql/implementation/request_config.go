package implementation

// RequestRequestConfig is the default query configuration: a page-size cap of 50 and
// allow-listed fields for id, status, version, and createdAt, with createdAt sorted
// descending by default. Callers may pass it to NewSpec, or supply their own
// RequestConfig when a query needs different fields or limits.
var RequestRequestConfig = &RequestConfig{
	MaxPageSize: 50,

	Fields: map[string]FieldConfig{
		"id": {
			APIName:     "id",
			ColumnName:  "id",
			Type:        FieldNumber,
			AllowFilter: true,
			AllowSort:   true,
			AllowedOps:  []string{"=", ">", "<"},
		},
		"status": {
			APIName:    "status",
			ColumnName: "status",

			Type:        FieldNumber,
			AllowFilter: true,
			AllowSort:   true,
			AllowedOps:  []string{"=", "!="},
		},
		"version": {
			APIName:     "version",
			ColumnName:  "version",
			Type:        FieldString,
			AllowFilter: true,
			AllowSort:   true,
			AllowedOps:  []string{"=", "like"},
		},
		"createdAt": {
			APIName:     "createdAt",
			ColumnName:  "created_at",
			Type:        FieldTime,
			AllowFilter: true,
			AllowSort:   true,
			AllowedOps:  []string{">=", "<="},
		},
	},

	DefaultSorts: []Sort{
		{Field: "createdAt", Direction: "DESC"},
	},
}
