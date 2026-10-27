package jobs

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
)

// dataSyncOutcome tells the runner what to store after a job ran.
type dataSyncOutcome struct {
	// archiveVersion is the archive version the job applied or recorded as its baseline; nil when
	// there is nothing new to record.
	archiveVersion *models.ArchiveVersion
	// stopReason, when not empty, stops the job for good.
	stopReason string
}

// archiveVersionOf returns the latest version of an RCSB entry.
func archiveVersionOf(mapping archive.EntryMapping) (models.ArchiveVersion, error) {
	if mapping.LatestRevisionOrdinal == nil || mapping.LatestVersion == nil {
		return models.ArchiveVersion{}, errors.New("RCSB entry has no revision history")
	}
	return models.ArchiveVersion{
		Ordinal:      *mapping.LatestRevisionOrdinal,
		Version:      *mapping.LatestVersion,
		RevisionDate: mapping.LatestRevisionDate,
	}, nil
}

// isNewerArchiveVersion reports whether latest comes after the version a job applied. A job with
// no applied version records latest as its baseline instead (decision D2).
func isNewerArchiveVersion(applied *models.ArchiveVersion, latest models.ArchiveVersion) bool {
	return applied != nil && latest.Ordinal > applied.Ordinal
}

// archiveChangeSummary describes a sync revision for its change_summary: the archive version it
// applies and the revision groups RCSB gave the versions since the applied one, for example
// "RCSB version 4.3 (2026-08-12): Data collection; Derived calculations".
func archiveChangeSummary(
	applied models.ArchiveVersion,
	latest models.ArchiveVersion,
	groups []archive.RevisionGroup,
) string {
	var names []string
	for _, group := range groups {
		if group.RevisionOrdinal <= applied.Ordinal || group.RevisionOrdinal > latest.Ordinal {
			continue
		}
		if !slices.Contains(names, group.Group) {
			names = append(names, group.Group)
		}
	}
	summary := "RCSB version " + latest.Version
	if latest.RevisionDate != nil {
		summary += " (" + latest.RevisionDate.UTC().Format(time.DateOnly) + ")"
	}
	if len(names) > 0 {
		summary += ": " + strings.Join(names, "; ")
	}
	return summary
}

// obsoleteArchiveStatus is the archive status of an entry RCSB obsoleted.
func obsoleteArchiveStatus(removed rcsb.RemovedEntry) (models.EntryArchiveStatus, error) {
	obsoletedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(removed.RemoveDate))
	if err != nil {
		return models.EntryArchiveStatus{}, fmt.Errorf("parse RCSB removal date %q: %w", removed.RemoveDate, err)
	}
	return models.EntryArchiveStatus{
		State:        models.EntryArchiveStateObsolete,
		ObsoletedAt:  &obsoletedAt,
		SupersededBy: normalizedUniqueStrings(removed.ReplacedBy),
	}, nil
}

// obsoleteChangeSummary describes the sync revision that marks an entry obsolete, for example
// "RCSB obsoleted 4YYU on 2024-01-17; superseded by 8R76".
func obsoleteChangeSummary(pdbID string, status models.EntryArchiveStatus) string {
	summary := "RCSB obsoleted " + pdbID
	if status.ObsoletedAt != nil {
		summary += " on " + status.ObsoletedAt.UTC().Format(time.DateOnly)
	}
	if len(status.SupersededBy) > 0 {
		summary += "; superseded by " + strings.Join(status.SupersededBy, ", ")
	}
	return summary
}
