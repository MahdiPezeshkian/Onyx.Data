package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// MigrationState describes the relationship between source and migration history.
type MigrationState string

const (
	StateApplied      MigrationState = "applied"
	StatePending      MigrationState = "pending"
	StateDirty        MigrationState = "dirty"
	StateMissingFile  MigrationState = "missing_file"
	StateModified     MigrationState = "modified"
	StateNameMismatch MigrationState = "name_mismatch"
	StateUnverified   MigrationState = "unverified"
)

// MigrationStatus describes one source/history version. Name is the source name
// when present. Detail contains all detected inconsistencies; State prioritizes
// dirty, missing files, name mismatch, checksum mismatch, then unverified history.
type MigrationStatus struct {
	Version         int
	Name            string
	StoredName      string
	State           MigrationState
	Dirty           bool
	StoredChecksum  string
	CurrentChecksum string
	AppliedAt       time.Time
	Detail          string
}

// Status reports source and database versions in ascending order, including
// inconsistent history instead of failing on it. Ambiguous/invalid filenames are
// errors. Like Version, it locks and creates/upgrades the metadata table as needed.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	items, err := m.readMigrations(true)
	if err != nil {
		return nil, err
	}
	var result []MigrationStatus
	err = m.withState(ctx, func(_ *gorm.DB, rows []migrationRow) error {
		result = migrationStatuses(items, rows)
		return nil
	})
	return result, err
}

func migrationStatuses(items []Migration, rows []migrationRow) []MigrationStatus {
	byVersion := indexMigrations(items)
	history := make(map[int]migrationRow, len(rows))
	versions := make(map[int]bool, len(items)+len(rows))
	for _, item := range items {
		versions[item.Version] = true
	}
	for _, row := range rows {
		versions[row.Version] = true
		history[row.Version] = row
	}
	result := make([]MigrationStatus, 0, len(versions))
	for version := range versions {
		item, exists := byVersion[version]
		row, applied := history[version]
		status := MigrationStatus{Version: version, Name: item.Name, StoredName: row.Name,
			Dirty: row.Dirty, StoredChecksum: row.Checksum, AppliedAt: row.AppliedAt, State: StatePending}
		if applied {
			status.State = StateApplied
		}
		if !exists {
			status.Name = row.Name
		} else {
			status.CurrentChecksum = checksum(item)
		}
		var problems []string
		add := func(state MigrationState, detail string) {
			if len(problems) == 0 {
				status.State = state
			}
			problems = append(problems, detail)
		}
		if row.Dirty {
			add(StateDirty, fmt.Sprintf("database migration is dirty at version %d (%s); manual intervention is required; inspect the schema before using Force", version, row.Name))
		}
		if !exists {
			add(StateMissingFile, fmt.Sprintf("migration version %d is applied but migration files are missing", version))
		} else {
			if strings.TrimSpace(item.UpSQL) == "" {
				add(StateMissingFile, fmt.Sprintf("migration %03d_%s has missing or empty up SQL", version, item.Name))
			}
			if strings.TrimSpace(item.DownSQL) == "" {
				add(StateMissingFile, fmt.Sprintf("migration %03d_%s has missing or empty down SQL", version, item.Name))
			}
			if applied && row.Name != item.Name {
				add(StateNameMismatch, fmt.Sprintf("migration version %d name mismatch: stored %q, current %q", version, row.Name, item.Name))
			}
			if applied && row.Checksum != "" && row.Checksum != status.CurrentChecksum {
				add(StateModified, fmt.Sprintf("migration %03d_%s has been modified after it was applied; stored checksum: %s; current checksum: %s", version, item.Name, row.Checksum, status.CurrentChecksum))
			}
			if applied && row.Checksum == "" {
				add(StateUnverified, fmt.Sprintf("migration %03d_%s has no recorded checksum; review historical files and call Baseline explicitly", version, item.Name))
			}
		}
		status.Detail = strings.Join(problems, "; ")
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result
}

func (m *Migrator) validateHistory(ctx context.Context, items []Migration, rows []migrationRow, allowUnverified bool) error {
	statuses := migrationStatuses(items, rows)
	// Report dirty state first even if an earlier version has a different problem.
	for _, status := range statuses {
		if status.Dirty {
			m.log(ctx, Event{Kind: EventDirtyDetected, Version: status.Version, Name: status.Name})
			return fmt.Errorf("%s", status.Detail)
		}
	}
	for _, status := range statuses {
		switch status.State {
		case StateApplied, StatePending:
			continue
		case StateUnverified:
			if allowUnverified {
				continue
			}
		case StateModified:
			m.log(ctx, Event{Kind: EventChecksumMismatch, Version: status.Version, Name: status.Name})
		}
		return fmt.Errorf("%s", status.Detail)
	}
	return nil
}
