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

func Test_should_delete_exact_data_sync_job_when_job_selected(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entryRevision := createDBTestEntryRevision(t, "deleted-data-sync-entry", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "deleted-data-sync-model", now)
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
	err := testDB.DataSyncJobs.Delete(ctx, entryJob)

	// then
	require.NoError(t, err)
	next, err := testDB.DataSyncJobs.GetNextScheduled(ctx, now)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.NotNil(t, next.ModelID)
	assert.Equal(t, modelRevision.ModelID, *next.ModelID)

	// when
	err = testDB.DataSyncJobs.Delete(ctx, modelJob)

	// then
	require.NoError(t, err)
	next, err = testDB.DataSyncJobs.GetNextScheduled(ctx, now)
	require.NoError(t, err)
	assert.Nil(t, next)
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
