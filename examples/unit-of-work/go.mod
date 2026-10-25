// This example is a standalone module so it can depend on a database driver
// without adding that dependency to the Onyx.Data library itself. The replace
// directive points at the local checkout, so the example always builds against
// the source in this repository.
module example.com/onyx-data-unit-of-work

go 1.27.0

require (
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.2
	github.com/MahdiPezeshkian/Onyx.Data v0.0.0
)

require (
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/mattn/go-sqlite3 v1.14.22 // indirect
	golang.org/x/text v0.41.0 // indirect
)

// Build against the library in this repository instead of a published version.
// Remove this directive to build against a released version from the module proxy.
replace github.com/MahdiPezeshkian/Onyx.Data => ../../
