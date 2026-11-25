package jobs

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

// syncRunFixture runs the data sync on a schema of its own. Its RCSB knows no entry and counts
// the requests it gets, so a test can tell whether a run asked RCSB at all.
type syncRunFixture struct {
	*rederiveFixture
	job          *DataSyncJob
	now          time.Time
	rcsbRequests *atomic.Int64
}

// syncJobRow is a data_sync_jobs row as a test reads it back.
type syncJobRow struct {
	ScheduledAt    time.Time      `db:"scheduled_at"`
	StopReason     sql.NullString `db:"stop_reason"`
	ArchiveVersion sql.NullString `db:"archive_version"`
}

func newSyncRunFixture(t *testing.T) *syncRunFixture {
	t.Helper()
	base := newRederiveFixture(t)
	requests := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	client := rcsb.NewClient(rcsb.WithBaseURLs(server.URL, server.URL, server.URL, server.URL))
	job, err := NewDataSyncJob(
		base.database, client, sifts.NewClient(), slog.New(slog.DiscardHandler), validation.ModeEnforce, DataSyncConfig{},
	)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	job.now = func() time.Time { return now }
	return &syncRunFixture{rederiveFixture: base, job: job, now: now, rcsbRequests: requests}
}

// createEntryInReview creates an entry the way a deposition does: its first revision is in review
// and the entry stays new until a reviewer approves it.
func (f *syncRunFixture) createEntryInReview(t *testing.T, pdbID string) models.EntryRevision {
	t.Helper()
	revision, err := f.database.Entries.Create(context.Background(), models.EntryRevision{
		ID:         uuid.New(),
		EntryID:    "entry-" + uuid.NewString(),
		State:      models.RevisionStateInReview,
		EntryState: models.EntryStateActive,
		Title:      new("deposited entry " + pdbID),
		Metadata:   models.EntryMetadata{ExternalRefs: map[models.EntrySource]string{models.EntrySourcePDB: pdbID}},
		CreatedBy:  f.userID,
		CreatedAt:  f.now,
		UpdatedAt:  f.now,
	})
	require.NoError(t, err)
	return *revision
}

// createModelInReview adds a model with one in-review revision to the entry and returns its ID.
func (f *syncRunFixture) createModelInReview(t *testing.T, entryID string) string {
	t.Helper()
	ctx := context.Background()
	modelID, err := f.database.Models.NextID(ctx, entryID)
	require.NoError(t, err)
	_, err = f.database.Models.Create(ctx, entryID, models.ModelRevision{
		ID:        uuid.New(),
		EntryID:   entryID,
		ModelID:   modelID,
		State:     models.RevisionStateInReview,
		Title:     new("model " + modelID),
		CreatedBy: f.userID,
		CreatedAt: f.now,
		UpdatedAt: f.now,
	})
	require.NoError(t, err)
	return modelID
}

func (f *syncRunFixture) scheduleDueJob(t *testing.T, entryID string, modelID *string) {
	t.Helper()
	require.NoError(t, f.database.DataSyncJobs.Schedule(context.Background(), models.DataSyncJob{
		EntryID:     entryID,
		ModelID:     modelID,
		ScheduledAt: f.now.Add(-time.Minute),
	}))
}

// runDueJobs runs every due job once.
func (f *syncRunFixture) runDueJobs(t *testing.T) {
	t.Helper()
	for {
		executed, err := f.job.executeNext(context.Background())
		require.NoError(t, err)
		if !executed {
			return
		}
	}
}

func (f *syncRunFixture) jobRow(t *testing.T, entryID string, modelID *string) syncJobRow {
	t.Helper()
	var row syncJobRow
	require.NoError(t, f.raw.Get(&row,
		`select scheduled_at, stop_reason, archive_version
		 from data_sync_jobs
		 where entry_id = $1 and model_id is not distinct from $2`,
		entryID, modelID,
	))
	return row
}

func (f *syncRunFixture) setEntryState(t *testing.T, entryID string, state models.EntryState) {
	t.Helper()
	_, err := f.raw.Exec(`update entries set state = $2 where id = $1`, entryID, string(state))
	require.NoError(t, err)
}

func Test_should_wait_at_normal_cadence_without_asking_rcsb_when_entry_is_still_in_review(t *testing.T) {
	// given
	fixture := newSyncRunFixture(t)
	entry := fixture.createEntryInReview(t, "1ABC")
	fixture.scheduleDueJob(t, entry.EntryID, nil)

	// when
	fixture.runDueJobs(t)

	// then
	row := fixture.jobRow(t, entry.EntryID, nil)
	assert.False(t, row.StopReason.Valid, "the job keeps waiting for the review")
	assert.False(t, row.ScheduledAt.Before(fixture.now.Add(dataSyncMinimumInterval)), "next run %s", row.ScheduledAt)
	assert.True(t, row.ScheduledAt.Before(fixture.now.Add(dataSyncMinimumInterval+dataSyncScheduleJitter)),
		"next run %s", row.ScheduledAt)
	assert.False(t, row.ArchiveVersion.Valid, "no baseline is recorded before approval")
	assert.Zero(t, fixture.rcsbRequests.Load())
	assert.Equal(t, 1, fixture.revisionCount(t, entry.EntryID))
}

func Test_should_ask_rcsb_when_waiting_entry_was_approved(t *testing.T) {
	// given: the job waited once, then a reviewer approved the entry
	fixture := newSyncRunFixture(t)
	entry := fixture.createEntryInReview(t, "1ABC")
	fixture.scheduleDueJob(t, entry.EntryID, nil)
	fixture.runDueJobs(t)
	_, err := fixture.database.Entries.ActivateRevision(context.Background(), entry.EntryID, entry.ID)
	require.NoError(t, err)
	fixture.scheduleDueJob(t, entry.EntryID, nil)

	// when
	fixture.runDueJobs(t)

	// then: the run reached RCSB, which knows no entry in this fixture
	assert.Positive(t, fixture.rcsbRequests.Load())
	row := fixture.jobRow(t, entry.EntryID, nil)
	assert.Equal(t, "RCSB does not know PDB ID 1ABC", row.StopReason.String)
}

func Test_should_stop_job_when_entry_was_rejected_before_any_approval(t *testing.T) {
	// given
	fixture := newSyncRunFixture(t)
	entry := fixture.createEntryInReview(t, "1ABC")
	_, err := fixture.database.Entries.RejectRevision(context.Background(), entry.EntryID, entry.ID)
	require.NoError(t, err)
	fixture.scheduleDueJob(t, entry.EntryID, nil)

	// when
	fixture.runDueJobs(t)

	// then
	row := fixture.jobRow(t, entry.EntryID, nil)
	assert.Equal(t, models.DataSyncStopReasonEntryNeverApproved, row.StopReason.String)
	assert.Zero(t, fixture.rcsbRequests.Load())
}

func Test_should_stop_entry_and_model_jobs_when_entry_was_deleted(t *testing.T) {
	// given
	fixture := newSyncRunFixture(t)
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	modelID := fixture.createModelInReview(t, entry.entryID)
	fixture.setEntryState(t, entry.entryID, models.EntryStateDeleted)
	fixture.scheduleDueJob(t, entry.entryID, nil)
	fixture.scheduleDueJob(t, entry.entryID, &modelID)

	// when
	fixture.runDueJobs(t)

	// then
	assert.Equal(t, models.DataSyncStopReasonEntryDeleted, fixture.jobRow(t, entry.entryID, nil).StopReason.String)
	assert.Equal(t, models.DataSyncStopReasonEntryDeleted, fixture.jobRow(t, entry.entryID, &modelID).StopReason.String)
	assert.Zero(t, fixture.rcsbRequests.Load())
}

func Test_should_stop_model_job_when_model_was_deleted(t *testing.T) {
	// given
	fixture := newSyncRunFixture(t)
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	modelID := fixture.createModelInReview(t, entry.entryID)
	_, err := fixture.raw.Exec(`update models set state = 'deleted' where id = $1`, modelID)
	require.NoError(t, err)
	fixture.scheduleDueJob(t, entry.entryID, &modelID)

	// when
	fixture.runDueJobs(t)

	// then
	assert.Equal(t, models.DataSyncStopReasonModelDeleted, fixture.jobRow(t, entry.entryID, &modelID).StopReason.String)
	assert.Zero(t, fixture.rcsbRequests.Load())
}

func Test_should_retry_within_the_hour_when_active_entry_or_model_has_no_active_revision(t *testing.T) {
	// given: an active entry whose only revision is in review, and a new model of an active entry
	// whose only revision is in review; neither should happen, so both runs fail
	fixture := newSyncRunFixture(t)
	inconsistent := fixture.createEntryInReview(t, "1ABC")
	fixture.setEntryState(t, inconsistent.EntryID, models.EntryStateActive)
	entry := fixture.seedArchiveEntry(t, "2XYZ", models.EntryMetadata{})
	modelID := fixture.createModelInReview(t, entry.entryID)
	fixture.scheduleDueJob(t, inconsistent.EntryID, nil)
	fixture.scheduleDueJob(t, entry.entryID, &modelID)

	// when
	fixture.runDueJobs(t)

	// then
	retryAt := fixture.now.Add(dataSyncRetryInterval)
	entryRow := fixture.jobRow(t, inconsistent.EntryID, nil)
	assert.False(t, entryRow.StopReason.Valid)
	assert.True(t, retryAt.Equal(entryRow.ScheduledAt), "entry job retry at %s", entryRow.ScheduledAt)
	modelRow := fixture.jobRow(t, entry.entryID, &modelID)
	assert.False(t, modelRow.StopReason.Valid)
	assert.True(t, retryAt.Equal(modelRow.ScheduledAt), "model job retry at %s", modelRow.ScheduledAt)
	assert.Zero(t, fixture.rcsbRequests.Load())
}
