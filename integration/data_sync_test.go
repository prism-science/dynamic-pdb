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
	require.NotNil(t, entry.Description)
	assert.Equal(t, "example structure", *entry.Description)
	assert.Equal(t, "X-ray crystallography", entry.Metadata["method"])
	assert.Equal(t, "Homo sapiens", entry.Metadata["organism"])
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
		   id, entry_id, revision_number, state, entry_state, published_at, name, description,
		   metadata, created_by, created_at, updated_at
		 ) values ($1, $2, 1, 'active', 'active', $3, $4, $5, $6::jsonb, $7, $3, $3)`,
		revisionID,
		entryID,
		now,
		pdbID,
		"outdated description",
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
