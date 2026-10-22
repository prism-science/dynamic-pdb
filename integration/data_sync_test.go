package integration

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_update_entry_when_data_sync_job_is_scheduled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	t.Setenv("DYNAMIC_PDB_RCSB_DATA_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_FILES_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_WWW_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_CDN_URL", rcsb.URL())
	backend := startBackend(t, root, backendBinaryPath, s3.URL())

	// when
	var activeRevisionID uuid.UUID
	require.Eventually(t, func() bool {
		err := database.Conn.QueryRowContext(
			t.Context(),
			`select id from entry_revisions where entry_id = $1 and state = 'active'`,
			fixture.EntryID,
		).Scan(&activeRevisionID)
		return err == nil && activeRevisionID != fixture.RevisionID
	}, 15*time.Second, 100*time.Millisecond)
	entry := getJSON[entryDocumentResponse](
		t,
		backend.URL+"/v1/entries/"+fixture.EntryID,
		auth.AccessToken,
	).Entry()
	artifacts := getJSON[artifactListResponse](
		t,
		backend.URL+"/v1/entries/"+fixture.EntryID+"/artifacts",
		auth.AccessToken,
	)

	// then
	require.NotNil(t, entry.Title)
	assert.Equal(t, "example structure", *entry.Title)
	assert.Equal(t, "X-ray crystallography", entry.Metadata["method"])
	assert.NotContains(t, entry.Metadata, "organism")
	assert.Equal(t, 1.4, entry.Metadata["resolution"])
	assert.Equal(t, "P 21 21 21", entry.Metadata["space_group"])
	require.Len(t, entry.ProteinSequences, 1)
	assert.Equal(t, "ACDE", entry.ProteinSequences[0].Sequence)
	assertArtifactNames(t, artifacts.Items, []string{fixture.ArtifactName})

	var parentRevisionID uuid.UUID
	var revisionCreatedBy uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select parent_revision_id, created_by from entry_revisions where id = $1`,
		activeRevisionID,
	).Scan(&parentRevisionID, &revisionCreatedBy))
	assert.Equal(t, fixture.RevisionID, parentRevisionID)

	var systemUserID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select id from users where source = 'system' and external_ref = 'dynamic-pdb'`,
	).Scan(&systemUserID))
	assert.Equal(t, systemUserID, revisionCreatedBy)

	var oldRevisionState string
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select state from entry_revisions where id = $1`,
		fixture.RevisionID,
	).Scan(&oldRevisionState))
	assert.Equal(t, "archived", oldRevisionState)

	var oldArtifactLinks int
	var newArtifactLinks int
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select count(*) from entry_revision_artifacts where entry_revision_id = $1`,
		fixture.RevisionID,
	).Scan(&oldArtifactLinks))
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select count(*) from entry_revision_artifacts where entry_revision_id = $1`,
		activeRevisionID,
	).Scan(&newArtifactLinks))
	assert.Equal(t, 1, oldArtifactLinks)
	assert.Equal(t, 1, newArtifactLinks)

	var sequenceRevisionID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select entry_revision_id from protein_sequences where id = $1`,
		fixture.SequenceID,
	).Scan(&sequenceRevisionID))
	assert.Equal(t, activeRevisionID, sequenceRevisionID)

	var indexedRows int
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select count(*)
		 from entry_search_index
		 where entry_id = $1 and model_type = 'entry_revision' and model_id = $2`,
		fixture.EntryID,
		activeRevisionID.String(),
	).Scan(&indexedRows))
	assert.Equal(t, 1, indexedRows)

	var scheduledAt time.Time
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select scheduled_at from data_sync_jobs where entry_id = $1 and model_id is null`,
		fixture.EntryID,
	).Scan(&scheduledAt))
	assert.WithinRange(t, scheduledAt, time.Now().Add(6*24*time.Hour), time.Now().Add(15*24*time.Hour))

	paths := rcsb.paths()
	assert.Contains(t, paths, "/rest/v1/core/entry/"+pdbID)
	assert.Contains(t, paths, "/rest/v1/core/polymer_entity/"+pdbID+"/1")
}

func Test_should_update_model_when_data_sync_job_is_scheduled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncModel(t, database, auth.UserID, pdbID)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	t.Setenv("DYNAMIC_PDB_RCSB_DATA_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_FILES_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_WWW_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_CDN_URL", rcsb.URL())
	backend := startBackend(t, root, backendBinaryPath, s3.URL())

	// when
	var activeRevisionID uuid.UUID
	require.Eventually(t, func() bool {
		err := database.Conn.QueryRowContext(
			t.Context(),
			`select id from model_revisions where model_id = $1 and state = 'active'`,
			fixture.ModelID,
		).Scan(&activeRevisionID)
		return err == nil && activeRevisionID != fixture.RevisionID
	}, 15*time.Second, 100*time.Millisecond)
	models := getJSON[modelListResponse](
		t,
		backend.URL+"/v1/entries/"+fixture.EntryID+"/models",
		auth.AccessToken,
	)
	artifacts := getJSON[artifactListResponse](
		t,
		backend.URL+"/v1/entries/"+fixture.EntryID+"/models/"+fixture.ModelID+"/artifacts",
		auth.AccessToken,
	)

	// then
	require.Len(t, models.Items, 1)
	model := models.Items[0]
	assert.Equal(t, fixture.ModelID, model.ID)
	assert.Equal(t, "Deposited model details", model.Metadata["details"])
	assert.Equal(t, []any{"Nelson, R.", "Sawaya, M.R."}, model.Metadata["authors"])
	assert.Equal(t, "Howard Hughes Medical Institute, UCLA, USA.", model.Metadata["affiliation"])
	assert.Equal(t, 1383.0, model.Metadata["atom_count"])
	assert.Equal(t, 164.0, model.Metadata["modeled_residues"])
	assert.Equal(t, 1.0, model.Metadata["unique_protein_chains"])
	assert.InDelta(t, 0.18, model.Metadata["unmodeled_fraction"], 0.000001)
	assert.Equal(t, []any{"ATP"}, model.Metadata["ligands"])
	assert.Equal(t, "Refinement", model.Metadata["purpose"])
	assert.Equal(t, "Single Conformer", model.Metadata["model_type"])
	externalRefs := model.Metadata["external_refs"].(map[string]any)
	assert.Equal(t, pdbID, externalRefs["pdb"])
	assertMetric(t, model.Metrics, "r_free", 0.21)
	assertMetric(t, model.Metrics, "r_work", 0.18)
	assertMetric(t, model.Metrics, "clashscore", 4.8)
	assertMetric(t, model.Metrics, "ramachandran_outliers", 0.13)
	assertMetric(t, model.Metrics, "molprobity_score", 1.42)
	assertMetric(t, model.Metrics, "rscc", 0.91)
	assertMetric(t, model.Metrics, "custom_score", 12)
	assertArtifactNames(t, artifacts.Items, []string{fixture.ArtifactName})
	require.Len(t, artifacts.Runs, 1)
	assert.Equal(t, "existing refinement", artifacts.Runs[0].Name)

	var parentRevisionID uuid.UUID
	var revisionCreatedBy uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select parent_revision_id, created_by from model_revisions where id = $1`,
		activeRevisionID,
	).Scan(&parentRevisionID, &revisionCreatedBy))
	assert.Equal(t, fixture.RevisionID, parentRevisionID)

	var systemUserID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select id from users where source = 'system' and external_ref = 'dynamic-pdb'`,
	).Scan(&systemUserID))
	assert.Equal(t, systemUserID, revisionCreatedBy)

	var oldRevisionState string
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select state from model_revisions where id = $1`,
		fixture.RevisionID,
	).Scan(&oldRevisionState))
	assert.Equal(t, "archived", oldRevisionState)

	var oldIndexedRows int
	var newIndexedRows int
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select count(*) from entry_search_index where entry_id = $1 and model_id = $2`,
		fixture.EntryID,
		fixture.RevisionID.String(),
	).Scan(&oldIndexedRows))
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select count(*) from entry_search_index where entry_id = $1 and model_id = $2`,
		fixture.EntryID,
		activeRevisionID.String(),
	).Scan(&newIndexedRows))
	assert.Zero(t, oldIndexedRows)
	assert.Equal(t, 1, newIndexedRows)

	var scheduledAt time.Time
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select scheduled_at from data_sync_jobs where entry_id = $1 and model_id = $2`,
		fixture.EntryID,
		fixture.ModelID,
	).Scan(&scheduledAt))
	assert.WithinRange(t, scheduledAt, time.Now().Add(6*24*time.Hour), time.Now().Add(15*24*time.Hour))
	assert.Contains(t, rcsb.paths(), "/rest/v1/core/entry/"+pdbID)
}

type scheduledDataSyncEntry struct {
	EntryID      string
	RevisionID   uuid.UUID
	SequenceID   uuid.UUID
	ArtifactName string
}

func seedScheduledDataSyncEntry(
	t *testing.T,
	database integrationDB,
	createdBy string,
	pdbID string,
) scheduledDataSyncEntry {
	t.Helper()
	entryID := "entry-" + uuid.NewString()
	revisionID := uuid.New()
	artifactID := uuid.New()
	sequenceID := uuid.New()
	artifactName := strings.ToLower(pdbID) + ".fasta"
	now := time.Now().UTC()
	entryMetadata := fmt.Sprintf(`{"external_refs":{"pdb":%q}}`, pdbID)
	artifactMetadata := `{"records":[{"header":"protein","sequence":"ACDE"}]}`

	transaction, err := database.Conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back data sync fixture: %v", rollbackErr)
		}
	}()

	_, err = transaction.ExecContext(
		t.Context(),
		`insert into entries(id, state, created_by, created_at) values ($1, 'active', $2, $3)`,
		entryID,
		createdBy,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into entry_revisions(
		   id, entry_id, revision_number, state, entry_state, published_at, title,
		   metadata, created_by, created_at, updated_at
		 ) values ($1, $2, 1, 'active', 'active', $3, $4, $5::jsonb, $6, $3, $3)`,
		revisionID,
		entryID,
		now,
		"outdated title",
		entryMetadata,
		createdBy,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into artifacts(id, name, level, type, format, metadata, created_by, created_at)
		 values ($1, $2, 'L0', 'fasta', 'fasta', $3::jsonb, $4, $5)`,
		artifactID,
		artifactName,
		artifactMetadata,
		createdBy,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into entry_revision_artifacts(entry_revision_id, artifact_id) values ($1, $2)`,
		revisionID,
		artifactID,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into protein_sequences(
		   id, entry_revision_id, source_artifact_id, record_index, header, sequence, processing_state, created_at
		 ) values ($1, $2, $3, 0, 'protein', 'ACDE', 'pending', $4)`,
		sequenceID,
		revisionID,
		artifactID,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into data_sync_jobs(entry_id, model_id, scheduled_at) values ($1, null, $2)`,
		entryID,
		now.Add(-time.Hour),
	)
	require.NoError(t, err)
	require.NoError(t, transaction.Commit())

	return scheduledDataSyncEntry{
		EntryID:      entryID,
		RevisionID:   revisionID,
		SequenceID:   sequenceID,
		ArtifactName: artifactName,
	}
}

type scheduledDataSyncModel struct {
	EntryID      string
	ModelID      string
	RevisionID   uuid.UUID
	ArtifactName string
}

func seedScheduledDataSyncModel(
	t *testing.T,
	database integrationDB,
	createdBy string,
	pdbID string,
) scheduledDataSyncModel {
	t.Helper()
	entryID := "entry-" + uuid.NewString()
	entryRevisionID := uuid.New()
	modelID := entryID + "_m_001"
	modelRevisionID := uuid.New()
	artifactID := uuid.New()
	runID := uuid.New()
	rFreeMetricID := uuid.New()
	molProbityMetricID := uuid.New()
	rsccMetricID := uuid.New()
	customMetricID := uuid.New()
	artifactName := strings.ToLower(pdbID) + ".cif"
	now := time.Now().UTC()
	modelMetadata := fmt.Sprintf(
		`{"external_refs":{"pdb":%q},"purpose":"Refinement","model_type":"Single Conformer","atom_count":1}`,
		pdbID,
	)

	transaction, err := database.Conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back model data sync fixture: %v", rollbackErr)
		}
	}()

	_, err = transaction.ExecContext(
		t.Context(),
		`insert into entries(id, state, created_by, created_at) values ($1, 'active', $2, $3)`,
		entryID,
		createdBy,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into entry_revisions(
		   id, entry_id, revision_number, state, entry_state, published_at, title,
		   metadata, created_by, created_at, updated_at
		 ) values ($1, $2, 1, 'active', 'active', $3, 'model sync entry', '{}'::jsonb, $4, $3, $3)`,
		entryRevisionID,
		entryID,
		now,
		createdBy,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into artifacts(id, name, level, type, format, metadata, created_by, created_at)
		 values ($1, $2, 'L2', 'model', 'cif', '{}'::jsonb, $3, $4)`,
		artifactID,
		artifactName,
		createdBy,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into models(id, entry_id, state, created_by, created_at)
		 values ($1, $2, 'active', $3, $4)`,
		modelID,
		entryID,
		createdBy,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into model_revisions(
		   id, model_id, primary_artifact_id, revision_number, state, model_state, published_at,
		   title, metadata, created_by, created_at, updated_at
		 ) values ($1, $2, $3, 1, 'active', 'active', $4, 'RCSB model', $5::jsonb, $6, $4, $4)`,
		modelRevisionID,
		modelID,
		artifactID,
		now,
		modelMetadata,
		createdBy,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into model_revision_artifacts(model_revision_id, artifact_id) values ($1, $2)`,
		modelRevisionID,
		artifactID,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into runs(id, name, parameters, metadata, created_by, created_at, updated_at)
		 values ($1, 'existing refinement', '{}'::jsonb, '{}'::jsonb, $2, $3, $3)`,
		runID,
		createdBy,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into model_revision_runs(model_revision_id, run_id) values ($1, $2)`,
		modelRevisionID,
		runID,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into metrics(id, key, value, created_at)
		 values ($1, 'r_free', 0.5, $5),
		        ($2, 'molprobity_score', 1.42, $5),
		        ($3, 'rscc', 0.91, $5),
		        ($4, 'custom_score', 12, $5)`,
		rFreeMetricID,
		molProbityMetricID,
		rsccMetricID,
		customMetricID,
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into model_revision_metrics(model_revision_id, metric_id)
		 values ($1, $2), ($1, $3), ($1, $4), ($1, $5)`,
		modelRevisionID,
		rFreeMetricID,
		molProbityMetricID,
		rsccMetricID,
		customMetricID,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into entry_search_index(entry_id, model_type, model_id, updated_at, search_text, search_tsv)
		 values ($1, 'model_revision', $2, $3, 'old model', to_tsvector('simple', 'old model'))`,
		entryID,
		modelRevisionID.String(),
		now,
	)
	require.NoError(t, err)
	_, err = transaction.ExecContext(
		t.Context(),
		`insert into data_sync_jobs(entry_id, model_id, scheduled_at) values ($1, $2, $3)`,
		entryID,
		modelID,
		now.Add(-time.Hour),
	)
	require.NoError(t, err)
	require.NoError(t, transaction.Commit())

	return scheduledDataSyncModel{
		EntryID:      entryID,
		ModelID:      modelID,
		RevisionID:   modelRevisionID,
		ArtifactName: artifactName,
	}
}
