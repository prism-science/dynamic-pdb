package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type entryRevisionCompoundDocument struct {
	Data struct {
		Attributes struct {
			Ligands           []string                             `json:"ligands"`
			CompoundSources   map[string]map[string]map[string]any `json:"compound_sources"`
			GlycanDescriptors []map[string]any                     `json:"glycan_descriptors"`
			FieldSources      map[string]fieldSourceBody           `json:"field_sources"`
		} `json:"attributes"`
	} `json:"data"`
}

const derivedCompoundRecord = `{
	"ligands": {
		"NAG": {"rule": "branched", "list_version": "2026.10.0"},
		"SO4": {"rule": "list", "list_version": "2026.10.0", "review_reason": "often from the buffer"}
	},
	"cofactors": {"ZN": {"rule": "metalc", "list_version": "2026.10.0"}},
	"excluded": {"GOL": {"rule": "list", "list_version": "2026.10.0"}}
}`

const derivedGlycans = `[{"entity_id": "2", "compound_ids": ["NAG"], "descriptors": [{"descriptor": "WURCS=2.0/1,1,0/[a2122h-1b_1-5_2*NCC/3=O]/1/", "type": "WURCS"}]}]`

func Test_should_keep_compound_record_in_step_when_person_edits_entry_lists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an entry whose lists were derived from its mmCIF, with their decision record.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	// The schema has this branch's migrations, which the shared local database may not have.
	database, databaseEnv := setupMigratedSchema(t, root)
	auth := seedUserAndIssueToken(t, database)
	entryID := seedActiveEntry(t, database, auth.UserID)
	_, err := database.Conn.ExecContext(t.Context(),
		`update entry_revisions
		 set metadata = '{"ligands":["NAG","SO4"],"cofactors":["ZN"]}',
		     compound_sources = $2, glycan_descriptors = $3
		 where entry_id = $1`,
		entryID, derivedCompoundRecord, derivedGlycans)
	require.NoError(t, err)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), databaseEnv)

	// when: a person adds LIG to the ligands and removes SO4.
	revisionID := createEntryRevisionThroughAPI(t, backend.URL, auth.AccessToken, entryID,
		map[string]any{"entry": map[string]any{"ligands": []string{"NAG", "LIG"}}})
	revision := getJSON[entryRevisionCompoundDocument](t,
		fmt.Sprintf("%s/v1/entries/%s/revisions/%s", backend.URL, entryID, revisionID), auth.AccessToken).Data.Attributes

	// then
	assert.Equal(t, []string{"NAG", "LIG"}, revision.Ligands)
	assert.Equal(t, map[string]map[string]map[string]any{
		"ligands": {
			"NAG": {"rule": "branched", "list_version": "2026.10.0"},
			"LIG": {"rule": "depositor"},
		},
		"cofactors": {"ZN": {"rule": "metalc", "list_version": "2026.10.0"}},
		"excluded":  {"GOL": {"rule": "list", "list_version": "2026.10.0"}},
	}, revision.CompoundSources)
	require.Len(t, revision.GlycanDescriptors, 1)
	assert.Equal(t, "2", revision.GlycanDescriptors[0]["entity_id"])
	assert.Equal(t, map[string]fieldSourceBody{"/metadata/ligands": {Source: "depositor"}}, revision.FieldSources)
}

type createdRevisionResponse struct {
	status int
	body   []byte
	err    error
}

func Test_should_wait_for_the_entry_lock_and_copy_the_backfilled_lists_when_person_creates_revision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an entry whose lists the archive re-derivation job is filling in place, in a
	// transaction that holds the entry's lock
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database, databaseEnv := setupMigratedSchema(t, root)
	auth := seedUserAndIssueToken(t, database)
	entryID := seedActiveEntry(t, database, auth.UserID)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), databaseEnv)
	backfill, err := database.Conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	_, err = backfill.ExecContext(t.Context(), `select id from entries where id = $1 for update`, entryID)
	require.NoError(t, err)

	// when: a person creates a revision while the job holds the lock, and the job then commits
	created := make(chan createdRevisionResponse, 1)
	go func() {
		created <- postRevision(backend.URL+"/v1/entries/"+entryID+"/revisions", auth.AccessToken,
			map[string]any{"entry": map[string]any{"title": "A person's title"}})
	}()
	select {
	case response := <-created:
		require.Failf(t, "revision created while the entry was locked", "status %d: %s", response.status, response.body)
	case <-time.After(500 * time.Millisecond):
	}
	_, err = backfill.ExecContext(t.Context(),
		`update entry_revisions
		 set metadata = metadata || '{"ligands":["NAG","SO4"],"cofactors":["ZN"]}',
		     compound_sources = $2, glycan_descriptors = $3
		 where entry_id = $1 and state = 'active'`,
		entryID, derivedCompoundRecord, derivedGlycans)
	require.NoError(t, err)
	require.NoError(t, backfill.Commit())
	var response createdRevisionResponse
	select {
	case response = <-created:
	case <-time.After(20 * time.Second):
		require.FailNow(t, "the revision was not created after the lock was released")
	}

	// then: the new revision is built from the active revision as the job left it
	require.NoError(t, response.err)
	require.Equal(t, http.StatusCreated, response.status, string(response.body))
	var document struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.body, &document))
	revision := getJSON[entryRevisionCompoundDocument](t,
		fmt.Sprintf("%s/v1/entries/%s/revisions/%s", backend.URL, entryID, document.Data.ID), auth.AccessToken).Data.Attributes
	assert.Equal(t, []string{"NAG", "SO4"}, revision.Ligands)
	assert.Contains(t, revision.CompoundSources["ligands"], "SO4")
	assert.Len(t, revision.GlycanDescriptors, 1)
}

// postRevision posts body to url and returns the response, without failing the test, so it can
// run in its own goroutine.
func postRevision(url string, token string, body any) createdRevisionResponse {
	payload, err := json.Marshal(body)
	if err != nil {
		return createdRevisionResponse{err: fmt.Errorf("encode body: %w", err)}
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return createdRevisionResponse{err: fmt.Errorf("create request: %w", err)}
	}
	request.Header.Set("Accept", jsonAPIMediaType)
	request.Header.Set("Content-Type", jsonAPIMediaType)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return createdRevisionResponse{err: fmt.Errorf("send request: %w", err)}
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return createdRevisionResponse{err: fmt.Errorf("read response: %w", err)}
	}
	return createdRevisionResponse{status: response.StatusCode, body: responseBody}
}
