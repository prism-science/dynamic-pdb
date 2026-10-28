package integration

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const coordinateRevisionsOnEnv = "DYNAMIC_PDB_DATA_SYNC_COORDINATE_REVISIONS=true"

func Test_should_add_deposited_model_revision_with_new_coordinate_file_when_rcsb_changes_atomic_model(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: the switch on, a copied coordinate file, and RCSB version 1.1 changing the atomic model
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	rcsb := newRCSBStub(t, pdbID)
	rcsb.revisionGroups = []string{"Atomic model", "Data collection"}
	defer rcsb.Close()
	setRCSBURLs(t, rcsb.URL())
	fixture := seedArchiveDepositedModel(t, database, auth.UserID, pdbID, rcsb.URL(), "verified")
	s3 := newS3Stub(t)
	defer s3.Close()

	// when
	startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), []string{coordinateRevisionsOnEnv})
	activeRevisionID := waitForNewActiveModelRevision(t, database, fixture.ModelID, fixture.RevisionID)

	// then
	var changeSummary string
	var primaryArtifactID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select change_summary, primary_artifact_id from model_revisions where id = $1`, activeRevisionID,
	).Scan(&changeSummary, &primaryArtifactID))
	assert.Equal(t, "RCSB version 1.1 (2026-08-12): Atomic model; Data collection", changeSummary)

	newLinks := modelRevisionArtifactIDs(t, database, activeRevisionID)
	require.Len(t, newLinks, 2)
	assert.Contains(t, newLinks, fixture.StructureFactorsArtifactID)
	assert.NotContains(t, newLinks, fixture.CoordinateArtifactID)
	var coordinateArtifactID uuid.UUID
	for _, artifactID := range newLinks {
		if artifactID != fixture.StructureFactorsArtifactID {
			coordinateArtifactID = artifactID
		}
	}
	assert.Equal(t, coordinateArtifactID, primaryArtifactID)

	var name, level, artifactType, format, uri, archiveVersion, checksumStatus string
	var sha256 sql.NullString
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select artifacts.name, artifacts.level, artifacts.type, artifacts.format, artifacts.uri, artifacts.sha256,
		        artifacts.metadata->>'archive_version', artifact_checksums.status
		 from artifacts
		 join artifact_checksums on artifact_checksums.artifact_id = artifacts.id
		 where artifacts.id = $1`, coordinateArtifactID,
	).Scan(&name, &level, &artifactType, &format, &uri, &sha256, &archiveVersion, &checksumStatus))
	assert.Equal(t, pdbID+".cif", name)
	assert.Equal(t, "L2", level)
	assert.Equal(t, "model", artifactType)
	assert.Equal(t, "cif", format)
	assert.Equal(t, fixture.CoordinateURI, uri)
	assert.False(t, sha256.Valid)
	assert.Equal(t, "1.1", archiveVersion)
	assert.Equal(t, "pending", checksumStatus)

	var oldState string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select state from model_revisions where id = $1`, fixture.RevisionID,
	).Scan(&oldState))
	assert.Equal(t, "archived", oldState)
	assert.ElementsMatch(t,
		[]uuid.UUID{fixture.CoordinateArtifactID, fixture.StructureFactorsArtifactID},
		modelRevisionArtifactIDs(t, database, fixture.RevisionID))
	assert.Equal(t, "1.1", modelJobArchiveVersion(t, database, fixture))
}

func Test_should_leave_coordinate_change_for_later_when_coordinate_revisions_are_switched_off(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: the switch off, a copied coordinate file, and RCSB version 1.1 changing the atomic model
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	rcsb := newRCSBStub(t, pdbID)
	rcsb.revisionGroups = []string{"Atomic model"}
	defer rcsb.Close()
	setRCSBURLs(t, rcsb.URL())
	fixture := seedArchiveDepositedModel(t, database, auth.UserID, pdbID, rcsb.URL(), "verified")
	s3 := newS3Stub(t)
	defer s3.Close()

	// when
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	activeRevisionID := waitForNewActiveModelRevision(t, database, fixture.ModelID, fixture.RevisionID)

	// then
	assertCoordinateChangeLeftForLater(t, database, backend, fixture, activeRevisionID, "coordinate revisions are switched off")
}

func Test_should_leave_coordinate_change_for_later_when_previous_coordinate_file_checksum_is_mismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: the switch on, but RCSB replaced the coordinate file before the checksum job copied it
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	rcsb := newRCSBStub(t, pdbID)
	rcsb.revisionGroups = []string{"Atomic model"}
	defer rcsb.Close()
	setRCSBURLs(t, rcsb.URL())
	fixture := seedArchiveDepositedModel(t, database, auth.UserID, pdbID, rcsb.URL(), "mismatch")
	s3 := newS3Stub(t)
	defer s3.Close()

	// when
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), []string{coordinateRevisionsOnEnv})
	activeRevisionID := waitForNewActiveModelRevision(t, database, fixture.ModelID, fixture.RevisionID)

	// then
	assertCoordinateChangeLeftForLater(t, database, backend, fixture, activeRevisionID,
		"RCSB replaced the previous coordinate file before the checksum job copied it (checksum mismatch)")
}

func Test_should_create_one_model_sync_job_when_archive_deposited_model_is_activated_twice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an archive Deposited model whose first revision waits for review, and no model job
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	setRCSBURLs(t, rcsb.URL())
	fixture := seedArchiveDepositedModel(t, database, auth.UserID, pdbID, rcsb.URL(), "pending")
	_, err := database.Conn.ExecContext(t.Context(),
		`delete from data_sync_jobs where model_id = $1`, fixture.ModelID)
	require.NoError(t, err)
	_, err = database.Conn.ExecContext(t.Context(),
		`update model_revisions set state = 'in_review', revision_number = null, published_at = null where id = $1`,
		fixture.RevisionID)
	require.NoError(t, err)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	modelRevisionsURL := backend.URL + "/v1/entries/" + fixture.EntryID + "/models/" + fixture.ModelID + "/revisions/"
	activatedAt := time.Now().UTC()

	// when
	patchJSON(t, modelRevisionsURL+fixture.RevisionID.String(), auth.AccessToken,
		map[string]any{"state": "active"}, http.StatusOK)
	firstJobs, firstScheduledAt := modelJobs(t, database, fixture)
	secondRevisionID := seedInReviewModelRevision(t, database, fixture, auth.UserID)
	patchJSON(t, modelRevisionsURL+secondRevisionID.String(), auth.AccessToken,
		map[string]any{"state": "active"}, http.StatusOK)
	secondJobs, secondScheduledAt := modelJobs(t, database, fixture)

	// then
	assert.Equal(t, 1, firstJobs)
	assert.WithinRange(t, firstScheduledAt, activatedAt.Add(7*24*time.Hour), time.Now().UTC().Add(14*24*time.Hour))
	assert.Equal(t, 1, secondJobs)
	assert.True(t, firstScheduledAt.Equal(secondScheduledAt))
}

func assertCoordinateChangeLeftForLater(
	t *testing.T,
	database integrationDB,
	backend backendProcess,
	fixture archiveDepositedModel,
	activeRevisionID uuid.UUID,
	reason string,
) {
	t.Helper()
	var changeSummary string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select change_summary from model_revisions where id = $1`, activeRevisionID,
	).Scan(&changeSummary))
	assert.Equal(t, "RCSB version 1.1 (2026-08-12): Atomic model (coordinate file not replaced yet)", changeSummary)
	assert.ElementsMatch(t,
		[]uuid.UUID{fixture.CoordinateArtifactID, fixture.StructureFactorsArtifactID},
		modelRevisionArtifactIDs(t, database, activeRevisionID))
	var coordinateArtifacts int
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select count(*) from artifacts where uri = $1`, fixture.CoordinateURI,
	).Scan(&coordinateArtifacts))
	assert.Equal(t, 1, coordinateArtifacts)
	assert.Equal(t, "1.0", modelJobArchiveVersion(t, database, fixture))
	require.Eventually(t, func() bool {
		return strings.Contains(backend.Logs(), "data sync left the RCSB coordinate change for a later run")
	}, 5*time.Second, 100*time.Millisecond)
	logs := backend.Logs()
	assert.Contains(t, logs, "level=WARN")
	assert.Contains(t, logs, "model_id="+fixture.ModelID)
	assert.Contains(t, logs, fmt.Sprintf("reason=%q", reason))
}

type archiveDepositedModel struct {
	EntryID                    string
	ModelID                    string
	RevisionID                 uuid.UUID
	CoordinateURI              string
	CoordinateArtifactID       uuid.UUID
	StructureFactorsArtifactID uuid.UUID
}

// seedArchiveDepositedModel stores an archive entry with its active Deposited model, which links
// RCSB's coordinate and structure-factor files at filesURL, and a due model job at version 1.0.
// The coordinate file's checksum row has checksumStatus; a verified file has our copy.
func seedArchiveDepositedModel(
	t *testing.T,
	database integrationDB,
	createdBy string,
	pdbID string,
	filesURL string,
	checksumStatus string,
) archiveDepositedModel {
	t.Helper()
	now := time.Now().UTC()
	fixture := archiveDepositedModel{
		EntryID:                    "entry-" + uuid.NewString(),
		RevisionID:                 uuid.New(),
		CoordinateURI:              filesURL + "/download/" + pdbID + ".cif",
		CoordinateArtifactID:       uuid.New(),
		StructureFactorsArtifactID: uuid.New(),
	}
	fixture.ModelID = fixture.EntryID + "_m_001"
	storedURI := sql.NullString{}
	if checksumStatus == "verified" {
		storedURI = sql.NullString{String: "s3://dynamic-pdb-test/copies/" + pdbID + ".cif", Valid: true}
	}
	modelMetadata := fmt.Sprintf(
		`{"external_refs":{"pdb":%q},"purpose":"Reference","model_type":"Deposited","atom_count":1}`, pdbID)

	transaction, err := database.Conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back archive model fixture: %v", rollbackErr)
		}
	}()
	statements := []struct {
		query string
		args  []any
	}{
		{`insert into entries(id, state, created_by, created_at) values ($1, 'active', $2, $3)`,
			[]any{fixture.EntryID, createdBy, now}},
		{`insert into entry_revisions(
		    id, entry_id, revision_number, state, entry_state, published_at, title, metadata, created_by, created_at, updated_at
		  ) values ($1, $2, 1, 'active', 'active', $3, 'archive entry', $4::jsonb, $5, $3, $3)`,
			[]any{uuid.New(), fixture.EntryID, now, fmt.Sprintf(`{"external_refs":{"pdb":%q}}`, pdbID), createdBy}},
		{`insert into artifacts(id, name, level, type, uri, sha256, format, size_bytes, metadata, created_by, created_at)
		  values ($1, $2, 'L2', 'model', $3, repeat('a', 64), 'cif', 100, '{}'::jsonb, $4, $5),
		         ($6, $7, 'L1', 'structure_factors', $8, repeat('b', 64), 'structure_factors_cif', 50, '{}'::jsonb, $4, $5)`,
			[]any{fixture.CoordinateArtifactID, pdbID + ".cif", fixture.CoordinateURI, createdBy, now,
				fixture.StructureFactorsArtifactID, pdbID + "-sf.cif", filesURL + "/download/" + pdbID + "-sf.cif"}},
		{`insert into artifact_checksums(artifact_id, status, sha256, size_bytes, stored_uri, checked_at)
		  values ($1, $2, repeat('c', 64), 100, $3, case when $2 = 'pending' then null else $4::timestamptz end)`,
			[]any{fixture.CoordinateArtifactID, checksumStatus, storedURI, now}},
		{`insert into models(id, entry_id, state, created_by, created_at) values ($1, $2, 'active', $3, $4)`,
			[]any{fixture.ModelID, fixture.EntryID, createdBy, now}},
		{`insert into model_revisions(
		    id, model_id, primary_artifact_id, revision_number, state, model_state, published_at,
		    title, metadata, created_by, created_at, updated_at
		  ) values ($1, $2, $3, 1, 'active', 'active', $4, 'Deposited model', $5::jsonb, $6, $4, $4)`,
			[]any{fixture.RevisionID, fixture.ModelID, fixture.CoordinateArtifactID, now, modelMetadata, createdBy}},
		{`insert into model_revision_artifacts(model_revision_id, artifact_id) values ($1, $2), ($1, $3)`,
			[]any{fixture.RevisionID, fixture.CoordinateArtifactID, fixture.StructureFactorsArtifactID}},
		{`insert into data_sync_jobs(entry_id, model_id, scheduled_at, archive_revision_ordinal, archive_version)
		  values ($1, $2, $3, 1, '1.0')`,
			[]any{fixture.EntryID, fixture.ModelID, now.Add(-time.Hour)}},
	}
	for _, statement := range statements {
		_, err := transaction.ExecContext(t.Context(), statement.query, statement.args...)
		require.NoError(t, err)
	}
	require.NoError(t, transaction.Commit())
	return fixture
}

// seedInReviewModelRevision stores a second revision of the fixture's model, waiting for review,
// that links the same files as the active one.
func seedInReviewModelRevision(t *testing.T, database integrationDB, fixture archiveDepositedModel, createdBy string) uuid.UUID {
	t.Helper()
	revisionID := uuid.New()
	_, err := database.Conn.ExecContext(t.Context(),
		`insert into model_revisions(
		   id, model_id, parent_revision_id, primary_artifact_id, state, model_state,
		   title, metadata, created_by, created_at, updated_at
		 )
		 select $1, model_id, id, primary_artifact_id, 'in_review', 'active', title, metadata, $2, now(), now()
		 from model_revisions where id = $3`,
		revisionID, createdBy, fixture.RevisionID)
	require.NoError(t, err)
	_, err = database.Conn.ExecContext(t.Context(),
		`insert into model_revision_artifacts(model_revision_id, artifact_id)
		 select $1, artifact_id from model_revision_artifacts where model_revision_id = $2`,
		revisionID, fixture.RevisionID)
	require.NoError(t, err)
	return revisionID
}

func modelRevisionArtifactIDs(t *testing.T, database integrationDB, revisionID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := database.Conn.QueryContext(t.Context(),
		`select artifact_id from model_revision_artifacts where model_revision_id = $1`, revisionID)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rows.Close())
	}()
	var artifactIDs []uuid.UUID
	for rows.Next() {
		var artifactID uuid.UUID
		require.NoError(t, rows.Scan(&artifactID))
		artifactIDs = append(artifactIDs, artifactID)
	}
	require.NoError(t, rows.Err())
	return artifactIDs
}

func modelJobArchiveVersion(t *testing.T, database integrationDB, fixture archiveDepositedModel) string {
	t.Helper()
	var version string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select archive_version from data_sync_jobs where entry_id = $1 and model_id = $2`,
		fixture.EntryID, fixture.ModelID,
	).Scan(&version))
	return version
}

func modelJobs(t *testing.T, database integrationDB, fixture archiveDepositedModel) (int, time.Time) {
	t.Helper()
	var jobs int
	var scheduledAt sql.NullTime
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select count(*), max(scheduled_at) from data_sync_jobs where model_id = $1`, fixture.ModelID,
	).Scan(&jobs, &scheduledAt))
	return jobs, scheduledAt.Time
}
