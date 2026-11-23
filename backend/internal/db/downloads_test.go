package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

const testMaxArchiveBytes = int64(10_000_000_000)

// downloadsTestDB is a schema of its own with saved selections to ask archives of.
type downloadsTestDB struct {
	*modelSearchTestDB
}

func newDownloadsTestDB(t *testing.T) *downloadsTestDB {
	t.Helper()
	return &downloadsTestDB{modelSearchTestDB: newModelSearchTestDB(t)}
}

// selection saves a selection of one new model and returns its ID.
func (f *downloadsTestDB) selection(t *testing.T) uuid.UUID {
	t.Helper()
	entry := f.entry(t, map[string]any{"method": "X-ray crystallography"})
	model := f.model(t, entry, map[string]any{"model_type": "Deposited"})
	saved, err := f.database.Selections.Freeze(context.Background(), freezeRequest([]string{model.id}, nil, nil))
	require.NoError(t, err)
	return saved.ID
}

func (f *downloadsTestDB) create(
	t *testing.T,
	selectionID uuid.UUID,
	choice models.DownloadFileChoice,
	now time.Time,
) (*models.DownloadJob, bool, error) {
	t.Helper()
	return f.database.CreateDownload(context.Background(), db.DownloadCreate{
		ID: uuid.New(), SelectionID: selectionID, FileChoice: choice, MaxArchiveBytes: testMaxArchiveBytes,
		MaxUnfinished: 20, MaxUnfinishedPerSelection: 3, Now: now,
	})
}

// finish takes the job through claim, preparing and finish, ready until expiresAt.
func (f *downloadsTestDB) finish(t *testing.T, jobID uuid.UUID, now, expiresAt time.Time) {
	t.Helper()
	ctx := context.Background()
	claimed, err := f.database.Downloads.Claim(ctx, now, now.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, jobID, claimed.ID)
	require.NoError(t, f.database.Downloads.StartPreparing(ctx, jobID, claimed.Attempts, 100))
	require.NoError(t, f.database.Downloads.Finish(ctx, jobID, claimed.Attempts, archiveResult(), now, expiresAt))
}

func archiveResult() db.DownloadResult {
	return db.DownloadResult{
		ArchiveKey: "downloads/a/archive.zip", ArchiveURL: "https://files.example.org/downloads/a/archive.zip",
		ArchiveBytes: 120, ArchiveSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ManifestKey: "downloads/a/manifest.tsv", ManifestURL: "https://files.example.org/downloads/a/manifest.tsv",
		Counts: models.DownloadCounts{Included: 3, Linked: 1, Unavailable: 0, ChangedAtSource: 2},
	}
}

func Test_should_return_the_same_archive_when_it_is_asked_for_again_while_unfinished_or_ready(t *testing.T) {
	// given a saved selection
	fixture := newDownloadsTestDB(t)
	selectionID := fixture.selection(t)
	now := time.Now().UTC()

	// when the same archive is asked for twice
	first, created, err := fixture.create(t, selectionID, models.DownloadFileChoiceAllFiles, now)
	require.NoError(t, err)
	require.True(t, created)
	again, createdAgain, err := fixture.create(t, selectionID, models.DownloadFileChoiceAllFiles, now)
	require.NoError(t, err)

	// then the second request gets the first job
	assert.False(t, createdAgain)
	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, models.DownloadStatusQueued, first.Status)
	assert.Equal(t, testMaxArchiveBytes, first.MaxArchiveBytes)
	assert.Zero(t, first.Attempts)

	// and another file choice is another job
	other, createdOther, err := fixture.create(t, selectionID, models.DownloadFileChoiceOriginalMMCIF, now)
	require.NoError(t, err)
	assert.True(t, createdOther)
	assert.NotEqual(t, first.ID, other.ID)

	// and a ready archive is returned until it expires
	fixture.finish(t, first.ID, now, now.Add(time.Hour))
	ready, createdReady, err := fixture.create(t, selectionID, models.DownloadFileChoiceAllFiles, now.Add(30*time.Minute))
	require.NoError(t, err)
	assert.False(t, createdReady)
	assert.Equal(t, first.ID, ready.ID)
	assert.Equal(t, models.DownloadStatusReady, ready.Status)
	fresh, createdFresh, err := fixture.create(t, selectionID, models.DownloadFileChoiceAllFiles, now.Add(2*time.Hour))
	require.NoError(t, err)
	assert.True(t, createdFresh, "an expired archive is built again")
	assert.NotEqual(t, first.ID, fresh.ID)
}

func Test_should_refuse_the_archive_when_its_selection_is_unknown(t *testing.T) {
	// given
	fixture := newDownloadsTestDB(t)

	// when
	_, _, err := fixture.create(t, uuid.New(), models.DownloadFileChoiceAllFiles, time.Now())

	// then
	require.ErrorIs(t, err, db.ErrDownloadSelectionNotFound)
}

func Test_should_refuse_the_archive_when_unfinished_archives_reach_a_cap(t *testing.T) {
	// given a selection with one unfinished archive
	fixture := newDownloadsTestDB(t)
	selectionID := fixture.selection(t)
	otherSelectionID := fixture.selection(t)
	ctx := context.Background()
	request := func(selection uuid.UUID, choice models.DownloadFileChoice, perSelection, total int) error {
		_, _, err := fixture.database.CreateDownload(ctx, db.DownloadCreate{
			ID: uuid.New(), SelectionID: selection, FileChoice: choice, MaxArchiveBytes: testMaxArchiveBytes,
			MaxUnfinished: total, MaxUnfinishedPerSelection: perSelection, Now: time.Now(),
		})
		return err
	}
	require.NoError(t, request(selectionID, models.DownloadFileChoiceAllFiles, 1, 20))

	// then a second unfinished archive of the same selection is refused
	require.ErrorIs(t, request(selectionID, models.DownloadFileChoiceOriginalMMCIF, 1, 20), db.ErrDownloadSelectionBusy)

	// and one of another selection is refused when the total is reached
	require.ErrorIs(t, request(otherSelectionID, models.DownloadFileChoiceAllFiles, 3, 1), db.ErrDownloadsBusy)

	// and allowed below both caps
	require.NoError(t, request(otherSelectionID, models.DownloadFileChoiceAllFiles, 3, 2))
}

func Test_should_hold_the_job_until_it_is_ready_when_a_worker_claims_it(t *testing.T) {
	// given a queued archive
	fixture := newDownloadsTestDB(t)
	ctx := context.Background()
	job, _, err := fixture.create(t, fixture.selection(t), models.DownloadFileChoiceAllFiles, time.Now())
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)

	// when a worker claims it
	claimed, err := fixture.database.Downloads.Claim(ctx, now, now.Add(10*time.Minute))
	require.NoError(t, err)

	// then it is planning, with a lease and one attempt, and no other claim gets it
	require.NotNil(t, claimed)
	assert.Equal(t, job.ID, claimed.ID)
	assert.Equal(t, models.DownloadStatusPlanning, claimed.Status)
	assert.Equal(t, 1, claimed.Attempts)
	assert.True(t, claimed.LeaseUntil.Equal(now.Add(10*time.Minute)))
	assert.True(t, claimed.StartedAt.Equal(now))
	nothing, err := fixture.database.Downloads.Claim(ctx, now, now.Add(10*time.Minute))
	require.NoError(t, err)
	assert.Nil(t, nothing)

	// and the worker extends its lease, prepares and finishes it
	require.NoError(t, fixture.database.Downloads.Heartbeat(ctx, job.ID, 1, now.Add(20*time.Minute)))
	require.NoError(t, fixture.database.Downloads.StartPreparing(ctx, job.ID, 1, 4096))
	require.NoError(t, fixture.database.Downloads.Finish(ctx, job.ID, 1, archiveResult(), now, now.Add(7*24*time.Hour)))

	ready, err := fixture.database.Downloads.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.DownloadStatusReady, ready.Status)
	assert.Nil(t, ready.LeaseUntil)
	assert.Equal(t, int64(4096), *ready.PlannedBytes)
	assert.Equal(t, "https://files.example.org/downloads/a/archive.zip", *ready.ArchiveURL)
	assert.Equal(t, &models.DownloadCounts{Included: 3, Linked: 1, ChangedAtSource: 2}, ready.Counts)
	assert.True(t, ready.ExpiresAt.Equal(now.Add(7*24*time.Hour)))

	// and the finished job no longer takes a heartbeat
	require.ErrorIs(t, fixture.database.Downloads.Heartbeat(ctx, job.ID, 1, now.Add(time.Hour)), db.ErrDownloadLeaseLost)
}

func Test_should_let_another_attempt_take_the_job_when_its_lease_ran_out(t *testing.T) {
	// given an archive claimed by a worker that stopped extending its lease
	fixture := newDownloadsTestDB(t)
	ctx := context.Background()
	job, _, err := fixture.create(t, fixture.selection(t), models.DownloadFileChoiceAllFiles, time.Now())
	require.NoError(t, err)
	now := time.Now().UTC()
	_, err = fixture.database.Downloads.Claim(ctx, now, now.Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, fixture.database.Downloads.StartPreparing(ctx, job.ID, 1, 10))

	// when the lease has run out
	later := now.Add(2 * time.Minute)
	reclaimed, err := fixture.database.Downloads.Claim(ctx, later, later.Add(time.Minute))
	require.NoError(t, err)

	// then a second attempt holds it, back in planning
	require.NotNil(t, reclaimed)
	assert.Equal(t, job.ID, reclaimed.ID)
	assert.Equal(t, 2, reclaimed.Attempts)
	assert.Equal(t, models.DownloadStatusPlanning, reclaimed.Status)
	assert.Nil(t, reclaimed.PlannedBytes)

	// and the first attempt can no longer write
	require.ErrorIs(t, fixture.database.Downloads.Heartbeat(ctx, job.ID, 1, later), db.ErrDownloadLeaseLost)
	require.ErrorIs(t, fixture.database.Downloads.Fail(ctx, job.ID, 1, "late", later), db.ErrDownloadLeaseLost)
}

func Test_should_record_each_ending_when_an_attempt_blocks_requeues_releases_or_fails(t *testing.T) {
	// given
	fixture := newDownloadsTestDB(t)
	ctx := context.Background()
	selectionID := fixture.selection(t)
	now := time.Now().UTC()
	claim := func(choice models.DownloadFileChoice) *models.DownloadJob {
		t.Helper()
		job, _, err := fixture.create(t, selectionID, choice, now)
		require.NoError(t, err)
		claimed, err := fixture.database.Downloads.Claim(ctx, now, now.Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, job.ID, claimed.ID)
		return claimed
	}

	// when an attempt blocks, it needs planned bytes over the limit
	blocked := claim(models.DownloadFileChoiceAllFiles)
	require.Error(t, fixture.database.Downloads.Block(ctx, blocked.ID, 1, testMaxArchiveBytes, "not over", now),
		"a blocked archive's planned bytes are over its limit")
	require.NoError(t, fixture.database.Downloads.Block(ctx, blocked.ID, 1, testMaxArchiveBytes+1, "over", now))

	// then
	got, err := fixture.database.Downloads.Get(ctx, blocked.ID)
	require.NoError(t, err)
	assert.Equal(t, models.DownloadStatusBlocked, got.Status)
	assert.Equal(t, "over", *got.BlockedReason)

	// and a failed attempt is requeued and still counts
	requeued := claim(models.DownloadFileChoiceOriginalMMCIF)
	require.NoError(t, fixture.database.Downloads.Requeue(ctx, requeued.ID, 1, "storage hiccup"))
	got, err = fixture.database.Downloads.Get(ctx, requeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.DownloadStatusQueued, got.Status)
	assert.Equal(t, 1, got.Attempts, "a failed attempt still counts")
	assert.Equal(t, "storage hiccup", *got.Error)

	// and a stopped worker's attempt is released without counting
	reclaimed, err := fixture.database.Downloads.Claim(ctx, now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, requeued.ID, reclaimed.ID)
	require.NoError(t, fixture.database.Downloads.Release(ctx, requeued.ID, 2))
	got, err = fixture.database.Downloads.Get(ctx, requeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.DownloadStatusQueued, got.Status)
	assert.Equal(t, 1, got.Attempts, "a stopped worker's attempt does not count")

	// and the last attempt fails the job
	reclaimed, err = fixture.database.Downloads.Claim(ctx, now, now.Add(time.Minute))
	require.NoError(t, err)
	require.NoError(t, fixture.database.Downloads.Fail(ctx, requeued.ID, reclaimed.Attempts, "gave up", now))
	got, err = fixture.database.Downloads.Get(ctx, requeued.ID)
	require.NoError(t, err)
	assert.Equal(t, models.DownloadStatusFailed, got.Status)
	assert.Equal(t, "gave up", *got.Error)
	assert.NotNil(t, got.FinishedAt)
}

func Test_should_refuse_the_row_when_a_ready_archive_lacks_its_files(t *testing.T) {
	// given
	fixture := newDownloadsTestDB(t)
	selectionID := fixture.selection(t)

	// when
	_, err := fixture.sql.Exec(`insert into download_jobs (id, selection_id, file_choice, status, max_archive_bytes, finished_at, expires_at)
	                            values ($1, $2, 'all_files', 'ready', 10, now(), now() + interval '7 days')`,
		uuid.New(), selectionID)

	// then
	require.ErrorContains(t, err, "download_jobs_status_fields_check")

	// and an unknown file choice is refused
	_, err = fixture.sql.Exec(`insert into download_jobs (id, selection_id, file_choice, max_archive_bytes)
	                           values ($1, $2, 'everything', 10)`, uuid.New(), selectionID)
	require.ErrorContains(t, err, "download_jobs_file_choice_check")
}

func Test_should_read_the_pinned_revisions_values_when_manifest_details_are_read(t *testing.T) {
	// given an entry with a PDB ID and a related dataset, and a Deposited model with authors
	fixture := newDownloadsTestDB(t)
	entry := fixture.entry(t, map[string]any{
		"method":        "X-ray crystallography",
		"external_refs": map[string]any{"pdb": "1ABC"},
		"related_datasets": []map[string]any{{"doi": "10.15785/SBGRID/1", "data_type": "diffraction image data",
			"repository": "SBGrid Data Bank"}},
	})
	deposited := fixture.model(t, entry, map[string]any{
		"model_type": "Deposited", "external_refs": map[string]any{"pdb": "1ABC"},
		"authors": []string{"Smith, R.D.", "Olson, J.S."}, "affiliation": "Rice University",
	})
	rerefined := fixture.model(t, entry, map[string]any{"model_type": "Ensemble", "origin": "archive",
		"external_refs": map[string]any{"pdb": "2DEF"}})
	saved, err := fixture.database.Selections.Freeze(context.Background(),
		freezeRequest([]string{deposited.id, rerefined.id}, nil, nil))
	require.NoError(t, err)

	// when
	details, err := fixture.database.Downloads.SelectionModelDetails(context.Background(), saved.ID, 1, 2)

	// then
	require.NoError(t, err)
	require.Len(t, details, 2)
	assert.Equal(t, models.DownloadSelectionModel{
		Ordinal: 1, ModelType: ptr("Deposited"), ModelPDBID: ptr("1ABC"), Authors: []string{"Smith, R.D.", "Olson, J.S."},
		Affiliation: ptr("Rice University"), EntryPDBID: ptr("1ABC"),
		RelatedDatasets: []models.RelatedDataset{{DOI: "10.15785/SBGRID/1", DataType: "diffraction image data",
			Repository: ptr("SBGrid Data Bank")}},
	}, details[1])
	assert.Equal(t, ptr("archive"), details[2].Origin)
	assert.Equal(t, ptr("2DEF"), details[2].ModelPDBID)
	assert.Nil(t, details[2].Authors)
}
