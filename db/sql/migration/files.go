package migration

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var migrationFilePattern = regexp.MustCompile(`^([0-9]+)_(.+)\.(up|down)\.sql$`)

// Migration is a versioned pair of SQL files loaded from the source.
type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string
}

func (m *Migrator) loadMigrations() ([]Migration, error) { return m.readMigrations(false) }

// Status tolerates incomplete pairs so operators can inspect missing files.
// Malformed filenames and ambiguous versions are always errors.
func (m *Migrator) readMigrations(allowIncomplete bool) ([]Migration, error) {
	if m.source == nil {
		return nil, fmt.Errorf("migration source must not be nil")
	}
	byVersion := make(map[int]*Migration)
	seen := make(map[string]string)
	err := fs.WalkDir(m.source, ".", func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("read migration path %s: %w", file, walkErr)
		}
		if entry.IsDir() {
			return nil
		}
		name := path.Base(file)
		matches := migrationFilePattern.FindStringSubmatch(name)
		if len(matches) != 4 {
			if strings.EqualFold(path.Ext(name), ".sql") {
				return fmt.Errorf("invalid migration filename %q: expected <positive-number>_<name>.(up|down).sql", file)
			}
			return nil // README and other non-SQL assets are allowed.
		}
		version, err := strconv.Atoi(matches[1])
		if err != nil {
			return fmt.Errorf("parse migration version %q: %w", file, err)
		}
		if version <= 0 {
			return fmt.Errorf("migration %q: version must be greater than zero", file)
		}
		migrationName := matches[2]
		if strings.TrimSpace(migrationName) != migrationName || migrationName == "" {
			return fmt.Errorf("invalid migration name in %q: must not be blank or have surrounding whitespace", file)
		}
		key := fmt.Sprintf("%d:%s", version, matches[3])
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("duplicate migration version %d direction %s: %s and %s", version, matches[3], previous, file)
		}
		seen[key] = file
		item, ok := byVersion[version]
		if !ok {
			item = &Migration{Version: version, Name: migrationName}
			byVersion[version] = item
		}
		if item.Name != migrationName {
			return fmt.Errorf("migration version %d has conflicting names: %s and %s", version, item.Name, migrationName)
		}
		content, err := fs.ReadFile(m.source, file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}
		if matches[3] == string(DirectionUp) {
			item.UpSQL = string(content)
		} else {
			item.DownSQL = string(content)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	items := make([]Migration, 0, len(byVersion))
	for _, item := range byVersion {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version < items[j].Version })
	if !allowIncomplete {
		for _, item := range items {
			for _, direction := range []Direction{DirectionUp, DirectionDown} {
				key := fmt.Sprintf("%d:%s", item.Version, direction)
				if _, ok := seen[key]; !ok {
					return nil, fmt.Errorf("migration %03d_%s is missing %s file", item.Version, item.Name, direction)
				}
				sqlText := item.UpSQL
				if direction == DirectionDown {
					sqlText = item.DownSQL
				}
				if strings.TrimSpace(sqlText) == "" {
					return nil, fmt.Errorf("migration %03d_%s has empty %s SQL", item.Version, item.Name, direction)
				}
			}
		}
	}
	return items, nil
}

func indexMigrations(items []Migration) map[int]Migration {
	result := make(map[int]Migration, len(items))
	for _, item := range items {
		result[item.Version] = item
	}
	return result
}

// Hash exact bytes of BOTH files, framing each with its big-endian uint64 length.
// This protects rollback SQL as well as up SQL, with no concatenation ambiguity.
func checksum(item Migration) string {
	h := sha256.New()
	for _, sqlText := range []string{item.UpSQL, item.DownSQL} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(sqlText)))
		_, _ = h.Write(length[:])
		_, _ = h.Write([]byte(sqlText))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
