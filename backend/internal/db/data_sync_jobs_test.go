package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_return_oldest_due_data_sync_job_when_jobs_scheduled(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entryRevision := createDBTestEntryRevision(t, "data-sync-entry", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "data-sync-model", now)
	entryJob := models.DataSyncJob{
		EntryID:     entryRevision.EntryID,
		ScheduledAt: now.Add(-2 * time.Hour),
	}
	modelJob := models.DataSyncJob{
		EntryID:     entryRevision.EntryID,
		ModelID:     &modelRevision.ModelID,
		ScheduledAt: now.Add(-time.Hour),
	}
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, entryJob))
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, modelJob))

	// when
	oldest, err := testDB.DataSyncJobs.GetNextScheduled(ctx, now)

	// then
	require.NoError(t, err)
	require.NotNil(t, oldest)
	assert.Equal(t, entryJob.EntryID, oldest.EntryID)
	assert.Nil(t, oldest.ModelID)
	assert.True(t, entryJob.ScheduledAt.Equal(oldest.ScheduledAt))

	// when
	entryJob.ScheduledAt = now.Add(time.Hour)
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, entryJob))
	next, err := testDB.DataSyncJobs.GetNextScheduled(ctx, now)

	// then
	require.NoError(t, err)
	require.NotNil(t, next)
	require.NotNil(t, next.ModelID)
	assert.Equal(t, modelJob.EntryID, next.EntryID)
	assert.Equal(t, *modelJob.ModelID, *next.ModelID)
	assert.True(t, modelJob.ScheduledAt.Equal(next.ScheduledAt))

	// when
	modelJob.ScheduledAt = now.Add(time.Hour)
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, modelJob))
	next, err = testDB.DataSyncJobs.GetNextScheduled(ctx, now)

	// then
	require.NoError(t, err)
	assert.Nil(t, next)
}

func Test_should_skip_stopped_data_sync_job_when_next_job_is_selected(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entryRevision := createDBTestEntryRevision(t, "stopped-data-sync-entry", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "stopped-data-sync-model", now)
	entryJob := models.DataSyncJob{
		EntryID:     entryRevision.EntryID,
		ScheduledAt: now.Add(-2 * time.Hour),
	}
	modelJob := models.DataSyncJob{
		EntryID:     entryRevision.EntryID,
		ModelID:     &modelRevision.ModelID,
		ScheduledAt: now.Add(-time.Hour),
	}
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, entryJob))
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, modelJob))

	// when
	err := testDB.DataSyncJobs.Stop(ctx, entryJob, "obsoleted at RCSB", now)

	// then
	require.NoError(t, err)
	next, err := testDB.DataSyncJobs.GetNextScheduled(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.NotNil(t, next.ModelID)
	assert.Equal(t, modelRevision.ModelID, *next.ModelID)

	// when
	err = testDB.DataSyncJobs.Stop(ctx, modelJob, "entry not found at RCSB", now)

	// then
	require.NoError(t, err)
	next, err = testDB.DataSyncJobs.GetNextScheduled(ctx, now)
	require.NoError(t, err)
	assert.Nil(t, next)
}

func Test_should_return_recorded_archive_version_when_job_is_selected_again(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entryRevision := createDBTestEntryRevision(t, "versioned-data-sync-entry", now)
	job := models.DataSyncJob{EntryID: entryRevision.EntryID, ScheduledAt: now.Add(-3 * time.Hour)}
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, job))
	before, err := testDB.DataSyncJobs.GetNextScheduled(ctx, now)
	require.NoError(t, err)
	revisionDate := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	version := models.ArchiveVersion{Ordinal: 9, Version: "4.3", RevisionDate: &revisionDate}

	// when
	err = testDB.DataSyncJobs.RecordArchiveVersion(ctx, job, version)
	require.NoError(t, err)
	after, getErr := testDB.DataSyncJobs.GetNextScheduled(ctx, now)

	// then
	require.NoError(t, getErr)
	require.NotNil(t, before)
	assert.Equal(t, job.EntryID, before.EntryID)
	assert.Nil(t, before.ArchiveVersion)
	require.NotNil(t, after)
	assert.Equal(t, job.EntryID, after.EntryID)
	require.NotNil(t, after.ArchiveVersion)
	assert.Equal(t, 9, after.ArchiveVersion.Ordinal)
	assert.Equal(t, "4.3", after.ArchiveVersion.Version)
	require.NotNil(t, after.ArchiveVersion.RevisionDate)
	assert.True(t, revisionDate.Equal(*after.ArchiveVersion.RevisionDate))
	require.NoError(t, testDB.DataSyncJobs.Stop(ctx, job, "test finished", now))
}

func Test_should_run_callback_when_advisory_lock_acquired(t *testing.T) {
	// given
	callbackCalled := false

	// when
	ran, err := testDB.RunLocked(context.Background(), "data-sync-test", func(context.Context) error {
		callbackCalled = true
		return nil
	})

	// then
	require.NoError(t, err)
	assert.True(t, ran)
	assert.True(t, callbackCalled)
}

func Test_should_create_model_data_sync_job_only_once_when_create_if_missing_runs_twice(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entryRevision := createDBTestEntryRevision(t, "create-if-missing-entry", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "create-if-missing-model", now)
	job := models.DataSyncJob{EntryID: entryRevision.EntryID, ModelID: &modelRevision.ModelID, ScheduledAt: now.Add(-time.Hour)}
	version := models.ArchiveVersion{Ordinal: 2, Version: "1.1"}

	// when
	created, createErr := testDB.DataSyncJobs.CreateIfMissing(ctx, job)
	require.NoError(t, testDB.DataSyncJobs.RecordArchiveVersion(ctx, job, version))
	again := job
	again.ScheduledAt = now.Add(-2 * time.Hour)
	createdAgain, againErr := testDB.DataSyncJobs.CreateIfMissing(ctx, again)
	next, nextErr := testDB.DataSyncJobs.GetNextScheduled(ctx, now)

	// then
	require.NoError(t, createErr)
	require.NoError(t, againErr)
	require.NoError(t, nextErr)
	assert.True(t, created)
	assert.False(t, createdAgain)
	require.NotNil(t, next)
	require.NotNil(t, next.ModelID)
	assert.Equal(t, modelRevision.ModelID, *next.ModelID)
	assert.True(t, job.ScheduledAt.Equal(next.ScheduledAt))
	assert.Equal(t, &version, next.ArchiveVersion)
	require.NoError(t, testDB.DataSyncJobs.Stop(ctx, job, "test finished", now))
}

func Test_should_leave_stopped_model_data_sync_job_stopped_when_create_if_missing_runs(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entryRevision := createDBTestEntryRevision(t, "stopped-create-if-missing-entry", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "stopped-create-if-missing-model", now)
	job := models.DataSyncJob{EntryID: entryRevision.EntryID, ModelID: &modelRevision.ModelID, ScheduledAt: now.Add(-time.Hour)}
	require.NoError(t, testDB.DataSyncJobs.Schedule(ctx, job))
	require.NoError(t, testDB.DataSyncJobs.Stop(ctx, job, "RCSB obsoleted 1ABC", now))

	// when
	created, err := testDB.DataSyncJobs.CreateIfMissing(ctx, job)
	next, nextErr := testDB.DataSyncJobs.GetNextScheduled(ctx, now)

	// then
	require.NoError(t, err)
	require.NoError(t, nextErr)
	assert.False(t, created)
	assert.Nil(t, next)
}
