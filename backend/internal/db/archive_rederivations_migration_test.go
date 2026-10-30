package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fixturePDBRevision         = fixtureRevisionBase + "01"
	fixtureReferenceVersion    = "cofactors:2026.10.0+metals:2026.10.0+solvents:2026.10.0+covalent:2026.10.0+ccd-types:2026.10.0"
	insertArchiveRederivation  = `insert into archive_rederivations(entry_revision_id, entry_id, pdb_id, outcome, reference_version, before, after, error) values ($1, 'e_pdb', '1ABC', $2, $3, $4, $5, $6)`
	resolveEntryListsReviewRow = `update schema_migration_review set resolved_at = $1, resolved_by = $2 where object_type = 'entry' and object_id = 'e_pdb' and issue = 'ligands_cofactors_rederive_from_archive'`
)

func Test_should_leave_compound_record_unset_when_entry_revisions_are_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	var withRecord int
	require.NoError(t, fixture.database.Get(&withRecord,
		`select count(*) from entry_revisions where compound_sources is not null or glycan_descriptors is not null`))

	// then
	assert.Zero(t, withRecord)
}

func Test_should_keep_one_log_row_per_revision_when_archive_rederivation_is_logged(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	_, firstErr := fixture.database.Exec(insertArchiveRederivation,
		fixturePDBRevision, "derived", fixtureReferenceVersion, `{"metadata":{}}`, `{"metadata":{"ligands":[]}}`, nil)
	_, secondErr := fixture.database.Exec(insertArchiveRederivation,
		fixturePDBRevision, "unchanged", fixtureReferenceVersion, nil, nil, nil)

	// then
	require.NoError(t, firstErr)
	require.Error(t, secondErr)
	assert.Contains(t, secondErr.Error(), "archive_rederivations_pkey")
}

func Test_should_reject_inconsistent_log_rows_when_archive_rederivation_is_logged(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	rows := map[string]struct {
		outcome    string
		before     any
		after      any
		error      any
		constraint string
	}{
		"unknown outcome":            {outcome: "skipped", constraint: "archive_rederivations_outcome_check"},
		"failed without error":       {outcome: "failed", constraint: "archive_rederivations_error_check"},
		"error on a success":         {outcome: "unchanged", error: "boom", constraint: "archive_rederivations_error_check"},
		"before without after":       {outcome: "derived", before: `{}`, constraint: "archive_rederivations_change_check"},
		"derived without change":     {outcome: "derived", constraint: "archive_rederivations_change_check"},
		"change on an unchanged row": {outcome: "unchanged", before: `{}`, after: `{}`, constraint: "archive_rederivations_change_check"},
	}

	for name, row := range rows {
		// when
		_, err := fixture.database.Exec(insertArchiveRederivation,
			fixturePDBRevision, row.outcome, fixtureReferenceVersion, row.before, row.after, row.error)

		// then
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), row.constraint, name)
	}
}

func Test_should_accept_every_outcome_the_job_writes_when_archive_rederivation_is_logged(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	rows := map[string]struct {
		before any
		after  any
		error  any
	}{
		"derived":            {before: `{}`, after: `{"metadata":{"ligands":[]}}`},
		"unchanged":          {},
		"skipped_person_set": {before: `{}`, after: `{"polymer_entities":[]}`},
		"skipped_in_review":  {},
		"skipped_not_active": {},
		"failed":             {error: "download 1ABC: 404"},
	}

	for outcome, row := range rows {
		// when
		_, err := fixture.database.Exec(insertArchiveRederivation,
			fixturePDBRevision, outcome, fixtureReferenceVersion, row.before, row.after, row.error)
		_, cleanupErr := fixture.database.Exec(`delete from archive_rederivations`)

		// then
		require.NoError(t, err, outcome)
		require.NoError(t, cleanupErr, outcome)
	}
}

func Test_should_resolve_review_row_only_with_time_and_resolver_when_review_rows_are_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	_, timeOnlyErr := fixture.database.Exec(resolveEntryListsReviewRow, "2026-10-30T12:00:00Z", nil)
	_, resolverOnlyErr := fixture.database.Exec(resolveEntryListsReviewRow, nil, "archive-rederive-job")
	_, bothErr := fixture.database.Exec(resolveEntryListsReviewRow, "2026-10-30T12:00:00Z", "archive-rederive-job")

	// then
	require.Error(t, timeOnlyErr)
	assert.Contains(t, timeOnlyErr.Error(), "schema_migration_review_resolved_check")
	require.Error(t, resolverOnlyErr)
	assert.Contains(t, resolverOnlyErr.Error(), "schema_migration_review_resolved_check")
	require.NoError(t, bothErr)
}

func Test_should_validate_resolved_check_when_review_rows_are_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	var validated bool
	require.NoError(t, fixture.database.Get(&validated,
		`select convalidated from pg_constraint
		 where conrelid = 'schema_migration_review'::regclass and conname = 'schema_migration_review_resolved_check'`))

	// then
	assert.True(t, validated)
}
