package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type entryRevisionSourcesDocument struct {
	Data struct {
		Attributes struct {
			Title        *string                    `json:"title"`
			FieldSources map[string]fieldSourceBody `json:"field_sources"`
		} `json:"attributes"`
	} `json:"data"`
}

type fieldSourceBody struct {
	Source    string  `json:"source"`
	Accession *string `json:"accession"`
	Release   *string `json:"release"`
}

func Test_should_keep_depositor_title_and_update_other_fields_when_entry_is_synced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an archive entry, imported before sources existed, whose title a depositor then
	// changed through the API and a reviewer approved.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
	_, err := database.Conn.ExecContext(t.Context(),
		`update data_sync_jobs set scheduled_at = now() + interval '1 day' where entry_id = $1`, fixture.EntryID)
	require.NoError(t, err)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	t.Setenv("DYNAMIC_PDB_RCSB_DATA_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_FILES_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_WWW_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_CDN_URL", rcsb.URL())
	var depositorRevision entryRevisionSourcesDocument
	// This backend takes the depositor's change; it stops when this subtest ends.
	t.Run("depositor edit", func(t *testing.T) {
		backend := startBackend(t, root, backendBinaryPath, s3.URL())
		revisionID := createEntryRevisionThroughAPI(t, backend.URL, auth.AccessToken, fixture.EntryID,
			map[string]any{"entry": map[string]any{"title": "Depositor title"}})
		revisionURL := fmt.Sprintf("%s/v1/entries/%s/revisions/%s", backend.URL, fixture.EntryID, revisionID)
		patchJSON(t, revisionURL, auth.AccessToken, map[string]any{"state": "active"}, http.StatusOK)
		depositorRevision = getJSON[entryRevisionSourcesDocument](t, revisionURL, auth.AccessToken)
	})

	// when: the backend syncs due jobs as it starts.
	_, err = database.Conn.ExecContext(t.Context(),
		`update data_sync_jobs set scheduled_at = now() where entry_id = $1`, fixture.EntryID)
	require.NoError(t, err)
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	var syncedRevisionID uuid.UUID
	require.Eventually(t, func() bool {
		err := database.Conn.QueryRowContext(t.Context(),
			`select revision.id
			 from entry_revisions revision join users on users.id = revision.created_by
			 where revision.entry_id = $1 and revision.state = 'active' and users.source = 'system'`,
			fixture.EntryID,
		).Scan(&syncedRevisionID)
		return err == nil
	}, 20*time.Second, 100*time.Millisecond)
	entry := getJSON[entryDocumentResponse](t, backend.URL+"/v1/entries/"+fixture.EntryID, auth.AccessToken).Entry()
	syncedRevision := getJSON[entryRevisionSourcesDocument](t,
		fmt.Sprintf("%s/v1/entries/%s/revisions/%s", backend.URL, fixture.EntryID, syncedRevisionID),
		auth.AccessToken)

	// then
	assert.Equal(t, map[string]fieldSourceBody{"/title": {Source: "depositor"}},
		depositorRevision.Data.Attributes.FieldSources)
	require.NotNil(t, entry.Title)
	assert.Equal(t, "Depositor title", *entry.Title)
	assert.Equal(t, "X-ray crystallography", entry.Metadata["method"])
	assert.Equal(t, 1.4, entry.Metadata["resolution"])
	sources := syncedRevision.Data.Attributes.FieldSources
	assert.Equal(t, fieldSourceBody{Source: "depositor"}, sources["/title"])
	assert.Equal(t, "rcsb", sources["/metadata/method"].Source)
	require.NotNil(t, sources["/metadata/method"].Accession)
	assert.Equal(t, pdbID, *sources["/metadata/method"].Accession)
	require.NotNil(t, sources["/metadata/method"].Release)
	assert.Equal(t, "1.1", *sources["/metadata/method"].Release)
	var changeSummary string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select change_summary from entry_revisions where id = $1`, syncedRevisionID,
	).Scan(&changeSummary))
	assert.Equal(t, "RCSB version 1.1 (2026-08-12): Refinement description", changeSummary)
}

func Test_should_leave_model_unchanged_when_synced_model_is_not_deposited(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: a depositor's model that names a PDB entry and has a due sync job
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncModel(t, database, auth.UserID, pdbID, "Single Conformer")
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	t.Setenv("DYNAMIC_PDB_RCSB_DATA_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_FILES_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_WWW_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_CDN_URL", rcsb.URL())

	// when
	startBackend(t, root, backendBinaryPath, s3.URL())
	require.Eventually(t, func() bool {
		var scheduledAt time.Time
		err := database.Conn.QueryRowContext(t.Context(),
			`select scheduled_at from data_sync_jobs where model_id = $1`, fixture.ModelID,
		).Scan(&scheduledAt)
		return err == nil && scheduledAt.After(time.Now().Add(6*24*time.Hour))
	}, 20*time.Second, 100*time.Millisecond)

	// then
	var activeRevisionID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select id from model_revisions where model_id = $1 and state = 'active'`, fixture.ModelID,
	).Scan(&activeRevisionID))
	assert.Equal(t, fixture.RevisionID, activeRevisionID)
	assert.Equal(t, 1, countModelRevisions(t, database, fixture.ModelID))
	assert.NotContains(t, rcsb.paths(), "/rest/v1/core/entry/"+pdbID)
}

func createEntryRevisionThroughAPI(
	t *testing.T,
	backendURL string,
	token string,
	entryID string,
	body map[string]any,
) string {
	t.Helper()
	response := postJSON(t, backendURL+"/v1/entries/"+entryID+"/revisions", token, body)
	defer func() {
		assert.NoError(t, response.Body.Close())
	}()
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(responseBody))
	var document struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(responseBody, &document))
	require.NotEmpty(t, document.Data.ID)
	return document.Data.ID
}
