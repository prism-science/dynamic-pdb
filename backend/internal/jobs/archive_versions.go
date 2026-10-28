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

// obsoleteEntryUpdate is what the sync writes to an entry RCSB obsoleted.
type obsoleteEntryUpdate struct {
	status models.EntryArchiveStatus
	// title is the title of RCSB's removal record when the entry has none; nil keeps the
	// entry's title.
	title   *string
	summary string
}

// planObsoleteEntryUpdate returns what the sync writes to an entry RCSB obsoleted, and false when
// the entry already records all of it, so a rerun of the job adds no revision. The title of
// RCSB's removal record only fills a missing title, never replaces one: some entries were imported
// after RCSB had obsoleted them and got no title.
func planObsoleteEntryUpdate(
	pdbID string,
	current models.EntryRevision,
	removed rcsb.RemovedEntry,
) (obsoleteEntryUpdate, bool, error) {
	status, err := obsoleteArchiveStatus(removed)
	if err != nil {
		return obsoleteEntryUpdate{}, false, err
	}
	update := obsoleteEntryUpdate{status: status}
	if title := strings.TrimSpace(removed.Title); title != "" && !hasTitle(current) {
		update.title = &title
	}
	statusChanged := !sameArchiveStatus(current.Metadata.ArchiveStatus, status)
	switch {
	case statusChanged && update.title != nil:
		update.summary = obsoleteChangeSummary(pdbID, status) + "; title from RCSB's removal record"
	case statusChanged:
		update.summary = obsoleteChangeSummary(pdbID, status)
	case update.title != nil:
		update.summary = "Title from RCSB's removal record of " + pdbID
	default:
		return obsoleteEntryUpdate{}, false, nil
	}
	return update, true, nil
}

func hasTitle(revision models.EntryRevision) bool {
	return revision.Title != nil && strings.TrimSpace(*revision.Title) != ""
}

// sameArchiveStatus reports whether the entry already records status. Dates compare as instants:
// a status read back from the database is in UTC, one parsed from RCSB may carry a zone offset.
func sameArchiveStatus(recorded *models.EntryArchiveStatus, status models.EntryArchiveStatus) bool {
	if recorded == nil || recorded.State != status.State || !slices.Equal(recorded.SupersededBy, status.SupersededBy) {
		return false
	}
	if recorded.ObsoletedAt == nil || status.ObsoletedAt == nil {
		return recorded.ObsoletedAt == nil && status.ObsoletedAt == nil
	}
	return recorded.ObsoletedAt.Equal(*status.ObsoletedAt)
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
