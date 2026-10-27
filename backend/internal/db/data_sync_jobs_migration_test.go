package db_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// joblessArchiveEntriesSeed adds an active archive entry without a job, one that already has a
// job, and a deleted archive entry without one.
const joblessArchiveEntriesSeed = `
insert into entries(id, state, created_by) values
    ('e_jobless', 'active', '00000000-0000-0000-0000-0000000000a1'),
    ('e_with_job', 'active', '00000000-0000-0000-0000-0000000000a1'),
    ('e_deleted', 'deleted', '00000000-0000-0000-0000-0000000000a1');
insert into entry_revisions(id, entry_id, state, title, metadata, created_by) values
    ('00000000-0000-0000-0000-0000000002a0', 'e_jobless', 'active', 'jobless',
     '{"external_refs":{"pdb":"4YYU"}}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-0000000002a1', 'e_with_job', 'active', 'with job',
     '{"external_refs":{"pdb":"2ABC"}}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-0000000002a2', 'e_deleted', 'active', 'deleted',
     '{"external_refs":{"pdb":"3ABC"}}', '00000000-0000-0000-0000-0000000000a1');
insert into data_sync_jobs(entry_id, model_id, scheduled_at) values ('e_with_job', null, '2030-01-01T00:00:00Z');
`

func Test_should_give_active_archive_entries_without_a_job_a_due_job_when_versions_are_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixtureWithExtraSeed(t, joblessArchiveEntriesSeed)

	// when
	var jobless time.Time
	require.NoError(t, fixture.database.Get(&jobless,
		`select scheduled_at from data_sync_jobs where entry_id = 'e_jobless' and model_id is null`))
	var withJob time.Time
	require.NoError(t, fixture.database.Get(&withJob,
		`select scheduled_at from data_sync_jobs where entry_id = 'e_with_job' and model_id is null`))
	var deletedJobs int
	require.NoError(t, fixture.database.Get(&deletedJobs,
		`select count(*) from data_sync_jobs where entry_id = 'e_deleted'`))

	// then
	assert.WithinDuration(t, time.Now(), jobless, time.Minute)
	assert.Equal(t, 2030, withJob.Year())
	assert.Zero(t, deletedJobs)
}

func Test_should_reject_half_recorded_version_or_stop_when_jobs_are_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixtureWithExtraSeed(t, joblessArchiveEntriesSeed)

	// when
	_, versionErr := fixture.database.Exec(
		`update data_sync_jobs set archive_revision_ordinal = 3 where entry_id = 'e_with_job'`)
	_, stopErr := fixture.database.Exec(
		`update data_sync_jobs set stopped_at = now() where entry_id = 'e_with_job'`)

	// then
	require.Error(t, versionErr)
	assert.Contains(t, versionErr.Error(), "data_sync_jobs_archive_version_check")
	require.Error(t, stopErr)
	assert.Contains(t, stopErr.Error(), "data_sync_jobs_stop_check")
}

func Test_should_validate_both_job_checks_when_jobs_are_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixtureWithExtraSeed(t, joblessArchiveEntriesSeed)

	// when
	var validated []string
	require.NoError(t, fixture.database.Select(&validated,
		`select conname from pg_constraint
		 where conrelid = 'data_sync_jobs'::regclass and contype = 'c' and convalidated
		 order by conname`))

	// then
	assert.Equal(t, []string{"data_sync_jobs_archive_version_check", "data_sync_jobs_stop_check"}, validated)
}
