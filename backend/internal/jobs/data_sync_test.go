package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
)

func Test_should_create_data_sync_job_when_database_passed(t *testing.T) {
	// given
	database := &db.DB{}

	// when
	job, err := NewDataSyncJob(database, nil)

	// then
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, database, job.database)
	assert.NotNil(t, job.logger)
}

func Test_should_reject_data_sync_job_when_database_missing(t *testing.T) {
	// when
	job, err := NewDataSyncJob(nil, nil)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "database is nil")
	assert.Nil(t, job)
}

func Test_should_schedule_next_data_sync_between_seven_and_fourteen_days(t *testing.T) {
	// given
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)

	// when
	scheduledAt := nextDataSyncScheduledAt(now)

	// then
	assert.GreaterOrEqual(t, scheduledAt, now.Add(dataSyncMinimumInterval))
	assert.Less(t, scheduledAt, now.Add(dataSyncMinimumInterval+dataSyncScheduleJitter))
}
