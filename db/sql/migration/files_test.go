package migration

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

//go:embed testdata/migrations/*.sql
var embeddedMigrations embed.FS

func pair(version, name, up, down string) fstest.MapFS {
	return fstest.MapFS{
		version + "_" + name + ".up.sql":   {Data: []byte(up)},
		version + "_" + name + ".down.sql": {Data: []byte(down)},
	}
}

func testSource() fstest.MapFS {
	source := pair("001", "first", "UP 1", "DOWN 1")
	for file, content := range pair("002", "second", "UP 2", "DOWN 2") {
		source[file] = content
	}
	return source
}

func requireError(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("error = %v; want substring %q", err, contains)
	}
}

func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadMigrations(t *testing.T) {
	tests := []struct {
		name   string
		source fstest.MapFS
		want   string
	}{
		{"valid", testSource(), ""},
		{"empty source", fstest.MapFS{}, ""},
		{"invalid filename", fstest.MapFS{"bad.sql": {Data: []byte("SELECT 1")}}, "invalid migration filename"},
		{"invalid direction", fstest.MapFS{"001_first.side.sql": {}}, "invalid migration filename"},
		{"zero version", pair("000", "first", "UP", "DOWN"), "version must be greater than zero"},
		{"overflow", pair("999999999999999999999999999999", "first", "UP", "DOWN"), "parse migration version"},
		{"blank name", pair("001", " ", "UP", "DOWN"), "invalid migration name"},
		{"missing up", fstest.MapFS{"001_first.down.sql": {Data: []byte("DOWN")}}, "missing up file"},
		{"missing down", fstest.MapFS{"001_first.up.sql": {Data: []byte("UP")}}, "missing down file"},
		{"empty up", pair("001", "first", "", "DOWN"), "empty up SQL"},
		{"blank down", pair("001", "first", "UP", " \n\t"), "empty down SQL"},
		{"conflicting names", fstest.MapFS{"001_first.up.sql": {Data: []byte("UP")}, "001_other.down.sql": {Data: []byte("DOWN")}}, "conflicting names"},
		{"duplicate versions", fstest.MapFS{"001_first.up.sql": {}, "001_other.up.sql": {}}, "duplicate migration version 1 direction up"},
		{"duplicate direction", fstest.MapFS{"001_first.up.sql": {}, "1_first.up.sql": {}}, "duplicate migration version 1 direction up"},
		{"duplicate nested files", fstest.MapFS{"a/001_first.down.sql": {}, "b/001_first.down.sql": {}}, "duplicate migration version 1 direction down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := NewMigrator(nil, tt.source).loadMigrations()
			if tt.want != "" {
				requireError(t, err, tt.want)
				return
			}
			requireOK(t, err)
			for i := 1; i < len(items); i++ {
				if items[i-1].Version >= items[i].Version {
					t.Fatal("migrations are not sorted")
				}
			}
		})
	}
}

func TestFilesystemSources(t *testing.T) {
	sub, err := fs.Sub(embeddedMigrations, "testdata/migrations")
	requireOK(t, err)
	for name, m := range map[string]*Migrator{
		"embed root": NewMigrator(nil, embeddedMigrations),
		"embed sub":  NewMigrator(nil, sub),
		"directory":  NewMigratorFromDir(nil, "testdata/migrations"),
	} {
		t.Run(name, func(t *testing.T) {
			items, err := m.loadMigrations()
			requireOK(t, err)
			if len(items) != 1 || items[0].Version != 1 || items[0].Name != "example" {
				t.Fatalf("unexpected migrations: %+v", items)
			}
		})
	}
	_, err = NewMigrator(nil, nil).loadMigrations()
	requireError(t, err, "source must not be nil")
	_, err = NewMigratorFromDir(nil, "testdata/does-not-exist").loadMigrations()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("error = %v", err)
	}
}

func TestChecksumProtectsBothFiles(t *testing.T) {
	item := Migration{UpSQL: "a", DownSQL: "bc"}
	original := checksum(item)
	if len(original) != 64 || checksum(item) != original {
		t.Fatal("checksum is not stable SHA-256")
	}
	for _, changed := range []Migration{{UpSQL: "ab", DownSQL: "c"}, {UpSQL: "a\n", DownSQL: "bc"}, {UpSQL: "a", DownSQL: "bd"}} {
		if checksum(changed) == original {
			t.Fatal("modified file not detected")
		}
	}
}

func TestValidationWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	m := NewMigrator(nil, testSource())
	requireError(t, m.Up(ctx), "database must not be nil")
	for _, steps := range []int{0, -1} {
		requireError(t, m.UpSteps(ctx, steps), "greater than zero")
		requireError(t, m.Down(ctx, steps), "greater than zero")
	}
	requireError(t, m.Force(ctx, -1), "must not be negative")
	requireError(t, m.Force(ctx, 99), "migration file is missing")
	_, _, err := m.Version(nil)
	requireError(t, err, "context must not be nil")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.Up(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	for _, table := range []string{"", " ", "schema_migrations; DROP TABLE users;", "public.schema_migrations", "a\"b", strings.Repeat("a", 64)} {
		_, _, err := NewMigrator(nil, nil, WithMigrationsTable(table)).Version(ctx)
		requireError(t, err, "unsafe migration table identifier")
	}
	for _, table := range []string{"schema_migrations", "my_schema_migrations", "MixedCase", "_m1", strings.Repeat("a", 63)} {
		if !tablePattern.MatchString(table) {
			t.Fatalf("valid table rejected: %q", table)
		}
	}
}
