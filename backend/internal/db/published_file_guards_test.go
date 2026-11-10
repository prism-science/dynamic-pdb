package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

// requireFileGuard asserts that err is a V20261110170100 trigger refusing a change.
func requireFileGuard(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	var pqErr *pq.Error
	require.ErrorAs(t, err, &pqErr, msgAndArgs...)
	assert.Equal(t, pq.ErrorCode("23001"), pqErr.Code, msgAndArgs...)
}

// guardRevisions is one model revision and one entry revision in a state, each with one file.
type guardRevisions struct {
	modelRevisionID uuid.UUID
	entryRevisionID uuid.UUID
	runID           uuid.UUID
	modelFile       uuid.UUID
	entryFile       uuid.UUID
	runFile         uuid.UUID
}

// revisionsInState makes a model revision and an entry revision in state, with a file each (and
// a run with a file on the model revision), linked while they were pending.
func (f *modelSearchTestDB) revisionsInState(t *testing.T, state models.RevisionState) guardRevisions {
	t.Helper()
	revisions := guardRevisions{
		modelFile: f.file(t, "model.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("1")),
		entryFile: f.file(t, "entry.fasta", models.ArtifactLevelL0, models.ArtifactTypeFASTA, sha256Of("2")),
		runFile:   f.file(t, "run.log", models.ArtifactLevelL3, models.ArtifactTypeOther, sha256Of("3")),
	}
	if state == models.RevisionStateArchived {
		// Archived revisions were active first.
		entry := f.filedEntryInState(t, models.RevisionStateActive, map[string]any{}, revisions.entryFile)
		model := f.filedModelInState(t, models.RevisionStateActive, entry, map[string]any{"model_type": "Multiconformer"},
			revisions.modelFile, []uuid.UUID{revisions.modelFile}, testRun{outputs: []uuid.UUID{revisions.runFile}})
		f.exec(t, `update model_revisions set state = 'archived' where id = $1`, model.revisionID)
		f.exec(t, `update entry_revisions set state = 'archived' where id = $1`, entry.revisionID)
		return f.withRevisions(t, revisions, entry, model)
	}
	entry := f.filedEntryInState(t, state, map[string]any{}, revisions.entryFile)
	model := f.filedModelInState(t, state, entry, map[string]any{"model_type": "Multiconformer"}, revisions.modelFile,
		[]uuid.UUID{revisions.modelFile}, testRun{outputs: []uuid.UUID{revisions.runFile}})
	return f.withRevisions(t, revisions, entry, model)
}

func (f *modelSearchTestDB) withRevisions(
	t *testing.T,
	revisions guardRevisions,
	entry searchEntry,
	model searchModel,
) guardRevisions {
	t.Helper()
	revisions.modelRevisionID = model.revisionID
	revisions.entryRevisionID = entry.revisionID
	require.NoError(t, f.sql.Get(&revisions.runID,
		`select run_id from model_revision_runs where model_revision_id = $1`, model.revisionID))
	return revisions
}

func Test_should_refuse_to_change_its_recorded_file_when_an_artifact_is_updated_or_deleted(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	file := fixture.file(t, "1abc.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("1"))
	statements := map[string]string{
		"sha256":     `update artifacts set sha256 = repeat('2', 64) where id = $1`,
		"no sha256":  `update artifacts set sha256 = null where id = $1`,
		"uri":        `update artifacts set uri = 'https://elsewhere.example.org/1abc.cif' where id = $1`,
		"size_bytes": `update artifacts set size_bytes = 101 where id = $1`,
		"name":       `update artifacts set name = 'renamed.cif' where id = $1`,
		"level":      `update artifacts set level = 'L1' where id = $1`,
		"type":       `update artifacts set type = 'other' where id = $1`,
		"format":     `update artifacts set format = 'pdb' where id = $1`,
		"id":         `update artifacts set id = gen_random_uuid() where id = $1`,
		"delete":     `delete from artifacts where id = $1`,
	}

	for name, statement := range statements {
		// when
		_, err := fixture.sql.Exec(statement, file)

		// then
		requireFileGuard(t, err, name)
	}
	_, err := fixture.sql.Exec(`update artifacts set metadata = '{"note": "described"}' where id = $1`, file)
	assert.NoError(t, err, "metadata describes the file and stays editable")
}

func Test_should_refuse_new_files_and_runs_when_a_revision_is_published(t *testing.T) {
	for _, state := range []models.RevisionState{models.RevisionStateActive, models.RevisionStateArchived} {
		t.Run(string(state), func(t *testing.T) {
			// given
			fixture := newModelSearchTestDB(t)
			published := fixture.revisionsInState(t, state)
			newFile := fixture.file(t, "new.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("4"))
			newRunID := uuid.New()
			fixture.exec(t, `insert into runs (id, name, created_by) values ($1, 'late run', $2)`, newRunID, fixture.userID)
			ctx := context.Background()

			// when
			modelFileErr := fixture.database.Artifacts.AttachToModelRevision(ctx, published.modelRevisionID, newFile)
			entryFileErr := fixture.database.Artifacts.AttachToEntryRevision(ctx, published.entryRevisionID, newFile)
			runErr := fixture.database.Runs.AttachToModelRevision(ctx, published.modelRevisionID, newRunID)
			runFileErr := fixture.database.Runs.AttachArtifact(ctx, published.runID, newFile, models.RunArtifactDirectionInput)
			primaryErr := func() error {
				_, err := fixture.sql.Exec(`update model_revisions set primary_artifact_id = $2 where id = $1`,
					published.modelRevisionID, newFile)
				return err
			}()

			// then
			requireFileGuard(t, modelFileErr, "model revision file")
			requireFileGuard(t, entryFileErr, "entry revision file")
			requireFileGuard(t, runErr, "model revision run")
			requireFileGuard(t, runFileErr, "file of a run of the revision")
			requireFileGuard(t, primaryErr, "primary file")
		})
	}
}

func Test_should_accept_links_it_already_has_when_a_published_revision_is_linked_again(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	published := fixture.revisionsInState(t, models.RevisionStateActive)
	ctx := context.Background()

	// when: the app's attach calls insert with ON CONFLICT DO NOTHING
	modelFileErr := fixture.database.Artifacts.AttachToModelRevision(ctx, published.modelRevisionID, published.modelFile)
	entryFileErr := fixture.database.Artifacts.AttachToEntryRevision(ctx, published.entryRevisionID, published.entryFile)
	runErr := fixture.database.Runs.AttachToModelRevision(ctx, published.modelRevisionID, published.runID)
	runFileErr := fixture.database.Runs.AttachArtifact(ctx, published.runID, published.runFile, models.RunArtifactDirectionOutput)
	primaryErr := func() error {
		_, err := fixture.sql.Exec(`update model_revisions set primary_artifact_id = primary_artifact_id where id = $1`,
			published.modelRevisionID)
		return err
	}()

	// then nothing changes, so nothing is refused
	assert.NoError(t, modelFileErr)
	assert.NoError(t, entryFileErr)
	assert.NoError(t, runErr)
	assert.NoError(t, runFileErr)
	assert.NoError(t, primaryErr)
}

func Test_should_accept_new_files_and_runs_when_a_revision_is_a_draft(t *testing.T) {
	for _, state := range []models.RevisionState{
		models.RevisionStatePending, models.RevisionStateInReview, models.RevisionStateRejected,
	} {
		t.Run(string(state), func(t *testing.T) {
			// given
			fixture := newModelSearchTestDB(t)
			draft := fixture.revisionsInState(t, state)
			newFile := fixture.file(t, "new.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("4"))
			newRunID := uuid.New()
			fixture.exec(t, `insert into runs (id, name, created_by) values ($1, 'new run', $2)`, newRunID, fixture.userID)
			ctx := context.Background()

			// when
			modelFileErr := fixture.database.Artifacts.AttachToModelRevision(ctx, draft.modelRevisionID, newFile)
			entryFileErr := fixture.database.Artifacts.AttachToEntryRevision(ctx, draft.entryRevisionID, newFile)
			runErr := fixture.database.Runs.AttachToModelRevision(ctx, draft.modelRevisionID, newRunID)
			runFileErr := fixture.database.Runs.AttachArtifact(ctx, draft.runID, newFile, models.RunArtifactDirectionInput)
			primaryErr := func() error {
				_, err := fixture.sql.Exec(`update model_revisions set primary_artifact_id = $2 where id = $1`,
					draft.modelRevisionID, newFile)
				return err
			}()

			// then
			assert.NoError(t, modelFileErr)
			assert.NoError(t, entryFileErr)
			assert.NoError(t, runErr)
			assert.NoError(t, runFileErr)
			assert.NoError(t, primaryErr)
		})
	}
}

func Test_should_refuse_new_files_when_a_run_is_shared_with_a_published_revision(t *testing.T) {
	// given a run that a draft revision and a published revision both use, as a new revision
	// copies its parent's runs
	fixture := newModelSearchTestDB(t)
	published := fixture.revisionsInState(t, models.RevisionStateActive)
	draft := fixture.revisionsInState(t, models.RevisionStatePending)
	ctx := context.Background()
	require.NoError(t, fixture.database.Runs.CopyModelRevisionLinks(ctx, published.modelRevisionID, draft.modelRevisionID))
	newFile := fixture.file(t, "new.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("4"))

	// when
	err := fixture.database.Runs.AttachArtifact(ctx, published.runID, newFile, models.RunArtifactDirectionInput)

	// then
	requireFileGuard(t, err)
}

func Test_should_refuse_to_change_or_remove_a_link_when_file_links_are_written(t *testing.T) {
	// given links of a draft, which may still get new links but never lose or change one
	fixture := newModelSearchTestDB(t)
	draft := fixture.revisionsInState(t, models.RevisionStatePending)
	other := fixture.file(t, "other.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("5"))
	statements := map[string]struct {
		query string
		args  []any
	}{
		"update model file": {`update model_revision_artifacts set artifact_id = $2 where model_revision_id = $1`,
			[]any{draft.modelRevisionID, other}},
		"delete model file": {`delete from model_revision_artifacts where model_revision_id = $1`,
			[]any{draft.modelRevisionID}},
		"update entry file": {`update entry_revision_artifacts set artifact_id = $2 where entry_revision_id = $1`,
			[]any{draft.entryRevisionID, other}},
		"delete entry file": {`delete from entry_revision_artifacts where entry_revision_id = $1`,
			[]any{draft.entryRevisionID}},
		"delete run": {`delete from model_revision_runs where model_revision_id = $1`,
			[]any{draft.modelRevisionID}},
		"update run file": {`update run_artifacts set position = 1 where run_id = $1`, []any{draft.runID}},
		"delete run file": {`delete from run_artifacts where run_id = $1`, []any{draft.runID}},
	}

	for name, statement := range statements {
		// when
		_, err := fixture.sql.Exec(statement.query, statement.args...)

		// then
		requireFileGuard(t, err, name)
	}
}

func Test_should_only_move_from_active_to_archived_when_a_published_revision_changes_state(t *testing.T) {
	refused := map[string][2]models.RevisionState{
		"active to pending":    {models.RevisionStateActive, models.RevisionStatePending},
		"active to in_review":  {models.RevisionStateActive, models.RevisionStateInReview},
		"active to rejected":   {models.RevisionStateActive, models.RevisionStateRejected},
		"archived to active":   {models.RevisionStateArchived, models.RevisionStateActive},
		"archived to pending":  {models.RevisionStateArchived, models.RevisionStatePending},
		"archived to rejected": {models.RevisionStateArchived, models.RevisionStateRejected},
	}
	for name, change := range refused {
		t.Run(name, func(t *testing.T) {
			// given
			fixture := newModelSearchTestDB(t)
			published := fixture.revisionsInState(t, change[0])

			// when
			_, modelErr := fixture.sql.Exec(`update model_revisions set state = $2 where id = $1`,
				published.modelRevisionID, string(change[1]))
			_, entryErr := fixture.sql.Exec(`update entry_revisions set state = $2 where id = $1`,
				published.entryRevisionID, string(change[1]))

			// then
			requireFileGuard(t, modelErr, "model revision")
			requireFileGuard(t, entryErr, "entry revision")
		})
	}
	t.Run("active to archived", func(t *testing.T) {
		// given
		fixture := newModelSearchTestDB(t)
		published := fixture.revisionsInState(t, models.RevisionStateActive)

		// when
		_, modelErr := fixture.sql.Exec(`update model_revisions set state = 'archived' where id = $1`,
			published.modelRevisionID)
		_, entryErr := fixture.sql.Exec(`update entry_revisions set state = 'archived' where id = $1`,
			published.entryRevisionID)

		// then
		assert.NoError(t, modelErr)
		assert.NoError(t, entryErr)
	})
}

func Test_should_keep_a_recorded_checksum_result_when_it_is_changed_or_deleted(t *testing.T) {
	// given a file the checksum job verified
	fixture := newModelSearchTestDB(t)
	file := fixture.file(t, "sf.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, file, models.ChecksumStatusVerified, sha256Of("1"), time.Now().Add(-time.Hour))
	statements := map[string]string{
		"sha256":     `update artifact_checksums set sha256 = repeat('2', 64) where artifact_id = $1`,
		"size_bytes": `update artifact_checksums set size_bytes = 101 where artifact_id = $1`,
		"checked_at": `update artifact_checksums set checked_at = now() where artifact_id = $1`,
		"requeue": `update artifact_checksums set status = 'pending', sha256 = null, size_bytes = null,
			copy_status = null, checked_at = null where artifact_id = $1`,
		"delete": `delete from artifact_checksums where artifact_id = $1`,
	}

	for name, statement := range statements {
		// when
		_, err := fixture.sql.Exec(statement, file)

		// then
		requireFileGuard(t, err, name)
	}
	_, err := fixture.sql.Exec(`update artifact_checksums set copy_status = 'over_limit' where artifact_id = $1`, file)
	assert.NoError(t, err, "where the bytes are kept is not part of the result")
}

func Test_should_let_the_checksum_job_record_and_retry_when_no_result_is_recorded_yet(t *testing.T) {
	// given a pending file and a failed one
	fixture := newModelSearchTestDB(t)
	pending := fixture.file(t, "pending.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, pending, models.ChecksumStatusPending, nil, time.Time{})
	failed := fixture.file(t, "failed.cif", models.ArtifactLevelL1, models.ArtifactTypeStructureFactors, nil)
	fixture.checksum(t, failed, models.ChecksumStatusFailed, nil, time.Now().Add(-time.Hour))

	// when
	_, recordErr := fixture.sql.Exec(`update artifact_checksums
		set status = 'verified', sha256 = repeat('3', 64), size_bytes = 100, copy_status = 'own_storage', checked_at = now()
		where artifact_id = $1`, pending)
	_, retryErr := fixture.sql.Exec(`update artifact_checksums set status = 'pending', last_error = null, checked_at = null
		where artifact_id = $1`, failed)

	// then
	assert.NoError(t, recordErr)
	assert.NoError(t, retryErr)
}

// lockWaitProbe is how long a statement must stay blocked to count as waiting for a lock.
const lockWaitProbe = 300 * time.Millisecond

// unlinkedRun stores a run no revision uses yet.
func (f *modelSearchTestDB) unlinkedRun(t *testing.T) uuid.UUID {
	t.Helper()
	runID := uuid.New()
	f.exec(t, `insert into runs (id, name, created_by) values ($1, 'unlinked run', $2)`, runID, f.userID)
	return runID
}

// beginForTest starts a transaction on its own connection and rolls it back at the end of the
// test unless it was committed.
func (f *modelSearchTestDB) beginForTest(t *testing.T) *sqlx.Tx {
	t.Helper()
	tx, err := f.sql.Beginx()
	require.NoError(t, err)
	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back test transaction: %v", rollbackErr)
		}
	})
	return tx
}

// requireStillWaiting asserts that done has not delivered within lockWaitProbe.
func requireStillWaiting(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		require.FailNow(t, what+" did not wait for the other transaction", "finished with %v", err)
	case <-time.After(lockWaitProbe):
	}
}

// requireFinished returns what done delivers within five seconds.
func requireFinished(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		require.FailNow(t, what+" is still waiting after the other transaction committed")
		return nil
	}
}

func Test_should_publish_a_run_with_its_file_when_the_file_is_added_while_the_run_is_linked(t *testing.T) {
	// given a run no revision uses, a draft revision, and a transaction adding a file to the run
	fixture := newModelSearchTestDB(t)
	runID := fixture.unlinkedRun(t)
	draft := fixture.revisionsInState(t, models.RevisionStatePending)
	file := fixture.file(t, "late.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("6"))
	adding := fixture.beginForTest(t)
	_, err := adding.Exec(`insert into run_artifacts (run_id, artifact_id, direction) values ($1, $2, 'input')`, runID, file)
	require.NoError(t, err)

	// when another transaction links the run to the draft and publishes it
	publishing := fixture.beginForTest(t)
	done := make(chan error, 1)
	go func() {
		if _, err := publishing.Exec(`insert into model_revision_runs (model_revision_id, run_id) values ($1, $2)`,
			draft.modelRevisionID, runID); err != nil {
			done <- err
			return
		}
		if _, err := publishing.Exec(`update model_revisions set state = 'active' where id = $1`, draft.modelRevisionID); err != nil {
			done <- err
			return
		}
		done <- publishing.Commit()
	}()

	// then linking waits for the file, so the revision is published with the file already there
	requireStillWaiting(t, done, "linking the run")
	require.NoError(t, adding.Commit())
	require.NoError(t, requireFinished(t, done, "linking the run"))
	var files int
	require.NoError(t, fixture.sql.Get(&files, `select count(*) from run_artifacts where run_id = $1`, runID))
	assert.Equal(t, 1, files)
}

func Test_should_refuse_a_file_for_a_run_when_another_transaction_links_and_publishes_it_first(t *testing.T) {
	// given a run no revision uses, and a transaction that links it to a draft and publishes the
	// draft but has not committed
	fixture := newModelSearchTestDB(t)
	runID := fixture.unlinkedRun(t)
	draft := fixture.revisionsInState(t, models.RevisionStatePending)
	file := fixture.file(t, "late.cif", models.ArtifactLevelL2, models.ArtifactTypeModel, sha256Of("6"))
	publishing := fixture.beginForTest(t)
	_, err := publishing.Exec(`insert into model_revision_runs (model_revision_id, run_id) values ($1, $2)`,
		draft.modelRevisionID, runID)
	require.NoError(t, err)
	_, err = publishing.Exec(`update model_revisions set state = 'active' where id = $1`, draft.modelRevisionID)
	require.NoError(t, err)

	// when another transaction adds a file to the run
	done := make(chan error, 1)
	go func() {
		_, err := fixture.sql.Exec(`insert into run_artifacts (run_id, artifact_id, direction) values ($1, $2, 'input')`,
			runID, file)
		done <- err
	}()

	// then the file waits for the publication and is refused once it commits
	requireStillWaiting(t, done, "adding the file")
	require.NoError(t, publishing.Commit())
	requireFileGuard(t, requireFinished(t, done, "adding the file"))
}
