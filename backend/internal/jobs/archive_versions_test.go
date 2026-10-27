package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
)

func Test_should_read_latest_version_when_rcsb_entry_has_revision_history(t *testing.T) {
	// given
	revisionDate := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	mapping := archive.EntryMapping{
		LatestRevisionOrdinal: new(9),
		LatestVersion:         new("4.3"),
		LatestRevisionDate:    &revisionDate,
	}

	// when
	version, err := archiveVersionOf(mapping)

	// then
	require.NoError(t, err)
	assert.Equal(t, models.ArchiveVersion{Ordinal: 9, Version: "4.3", RevisionDate: &revisionDate}, version)
}

func Test_should_fail_when_rcsb_entry_has_no_revision_history(t *testing.T) {
	// given
	mapping := archive.EntryMapping{LatestVersion: new("4.3")}

	// when
	_, err := archiveVersionOf(mapping)

	// then
	require.ErrorContains(t, err, "RCSB entry has no revision history")
}

func Test_should_apply_only_newer_archive_versions_when_job_has_a_baseline(t *testing.T) {
	// given
	applied := &models.ArchiveVersion{Ordinal: 8, Version: "4.2"}

	// when
	newer := isNewerArchiveVersion(applied, models.ArchiveVersion{Ordinal: 9, Version: "4.3"})
	same := isNewerArchiveVersion(applied, models.ArchiveVersion{Ordinal: 8, Version: "4.2"})
	older := isNewerArchiveVersion(applied, models.ArchiveVersion{Ordinal: 7, Version: "4.1"})

	// then
	assert.True(t, newer)
	assert.False(t, same)
	assert.False(t, older)
}

func Test_should_list_each_revision_group_once_when_several_versions_are_applied(t *testing.T) {
	// given
	revisionDate := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	applied := models.ArchiveVersion{Ordinal: 7, Version: "4.1"}
	latest := models.ArchiveVersion{Ordinal: 9, Version: "4.3", RevisionDate: &revisionDate}
	groups := []archive.RevisionGroup{
		{Group: "Advisory", RevisionOrdinal: 7},
		{Group: "Data collection", RevisionOrdinal: 8},
		{Group: "Derived calculations", RevisionOrdinal: 9},
		{Group: "Data collection", RevisionOrdinal: 9},
	}

	// when
	summary := archiveChangeSummary(applied, latest, groups)

	// then
	assert.Equal(t, "RCSB version 4.3 (2026-08-12): Data collection; Derived calculations", summary)
}

func Test_should_name_only_the_version_when_rcsb_lists_no_groups_for_it(t *testing.T) {
	// given
	applied := models.ArchiveVersion{Ordinal: 1, Version: "1.0"}
	latest := models.ArchiveVersion{Ordinal: 2, Version: "1.1"}

	// when
	summary := archiveChangeSummary(applied, latest, nil)

	// then
	assert.Equal(t, "RCSB version 1.1", summary)
}

func Test_should_describe_obsolete_entry_with_date_and_successors_when_rcsb_removed_it(t *testing.T) {
	// given
	removed := rcsb.RemovedEntry{ReplacedBy: []string{"8R76"}, RemoveDate: "2024-01-17T00:00:00.000+00:00"}

	// when
	status, err := obsoleteArchiveStatus(removed)
	summary := obsoleteChangeSummary("4YYU", status)

	// then
	require.NoError(t, err)
	require.NotNil(t, status.ObsoletedAt)
	assert.Equal(t, models.EntryArchiveStateObsolete, status.State)
	assert.Equal(t, time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC), status.ObsoletedAt.UTC())
	assert.Equal(t, []string{"8R76"}, status.SupersededBy)
	assert.Equal(t, "RCSB obsoleted 4YYU on 2024-01-17; superseded by 8R76", summary)
}

func Test_should_describe_obsolete_entry_without_successor_when_rcsb_names_none(t *testing.T) {
	// given
	removed := rcsb.RemovedEntry{RemoveDate: "2018-03-07T00:00:00.000+00:00"}

	// when
	status, err := obsoleteArchiveStatus(removed)
	summary := obsoleteChangeSummary("5P9Q", status)

	// then
	require.NoError(t, err)
	assert.Empty(t, status.SupersededBy)
	assert.Equal(t, "RCSB obsoleted 5P9Q on 2018-03-07", summary)
}

func Test_should_fail_when_removal_date_is_malformed(t *testing.T) {
	// given
	removed := rcsb.RemovedEntry{RemoveDate: "yesterday"}

	// when
	_, err := obsoleteArchiveStatus(removed)

	// then
	require.ErrorContains(t, err, "parse RCSB removal date")
}
