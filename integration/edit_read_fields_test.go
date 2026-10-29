package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The edit forms read these fields from the public GETs to decide what is read-only.
type editEntryDocument struct {
	Data struct {
		Attributes struct {
			Title                    *string                    `json:"title"`
			FieldSources             map[string]fieldSourceBody `json:"field_sources"`
			DepositorPolymerEntities []map[string]any           `json:"depositor_polymer_entities"`
		} `json:"attributes"`
	} `json:"data"`
}

type editModelDocument struct {
	Data struct {
		Attributes struct {
			Metadata     map[string]any             `json:"metadata"`
			ResidueData  *[]map[string]any          `json:"residue_data"`
			FieldSources map[string]fieldSourceBody `json:"field_sources"`
		} `json:"attributes"`
	} `json:"data"`
}

func Test_should_return_field_sources_and_typed_entities_when_entry_is_read(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an entry stored before sources were recorded.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	reviewer := seedUserAndIssueToken(t, database)
	entryID := seedActiveEntry(t, database, auth.UserID)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	entryURL := backend.URL + "/v1/entries/" + entryID
	beforeEdit := getRawAttributes(t, entryURL, auth.AccessToken)

	// when: a depositor changes the title and types a construct, and a reviewer approves.
	revisionID := createEntryRevisionThroughAPI(t, backend.URL, auth.AccessToken, entryID, map[string]any{
		"entry": map[string]any{
			"title":            "Depositor title",
			"polymer_entities": []map[string]any{{"label_entity_id": "1", "construct": "residues 1-164"}},
		},
	})
	patchJSON(t, fmt.Sprintf("%s/revisions/%s", entryURL, revisionID), reviewer.AccessToken,
		map[string]any{"state": "active"}, http.StatusOK)
	afterEdit := getJSON[editEntryDocument](t, entryURL, auth.AccessToken).Data.Attributes

	// then
	assert.Contains(t, beforeEdit, "field_sources", "a revision without sources returns null, not nothing")
	assert.Nil(t, beforeEdit["field_sources"])
	assert.Equal(t, []any{}, beforeEdit["depositor_polymer_entities"])
	require.NotNil(t, afterEdit.Title)
	assert.Equal(t, "Depositor title", *afterEdit.Title)
	assert.Equal(t, map[string]fieldSourceBody{"/title": {Source: "depositor"}}, afterEdit.FieldSources)
	assert.Equal(t, []map[string]any{{"label_entity_id": "1", "construct": "residues 1-164"}},
		afterEdit.DepositorPolymerEntities)
}

func Test_should_keep_residue_data_when_model_revision_metadata_leaves_it_out(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: a model with residue data, whose GET returns residue data beside metadata.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	reviewer := seedUserAndIssueToken(t, database)
	entryID, modelID := seedActiveModelWithResidueData(t, database, auth.UserID)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	modelURL := backend.URL + "/v1/entries/" + entryID + "/models/" + modelID
	beforeEdit := getRawAttributes(t, modelURL, auth.AccessToken)
	listed := getJSON[struct {
		Data []struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}](t, backend.URL+"/v1/entries/"+entryID+"/models", auth.AccessToken)

	// when: a depositor sends the metadata the GET returned, with one value changed.
	metadata, ok := beforeEdit["metadata"].(map[string]any)
	require.True(t, ok)
	metadata["details"] = "Edited description"
	revisionID := createModelRevisionThroughAPI(t, modelURL, auth.AccessToken,
		map[string]any{"model": map[string]any{"metadata": metadata}})
	patchJSON(t, modelURL+"/revisions/"+revisionID, reviewer.AccessToken, map[string]any{"state": "active"}, http.StatusOK)
	afterEdit := getJSON[editModelDocument](t, modelURL, auth.AccessToken).Data.Attributes

	// then
	assert.Contains(t, beforeEdit, "field_sources", "the model detail returns null sources, not nothing")
	assert.Nil(t, beforeEdit["field_sources"])
	require.Len(t, listed.Data, 1)
	assert.NotContains(t, listed.Data[0].Attributes, "field_sources", "model lists must not carry field_sources")
	assert.Equal(t, "Edited description", afterEdit.Metadata["details"])
	require.NotNil(t, afterEdit.ResidueData)
	assert.Len(t, *afterEdit.ResidueData, 2)
	assert.Equal(t, map[string]fieldSourceBody{"/metadata/details": {Source: "depositor"}}, afterEdit.FieldSources)
}

// getRawAttributes reads a document's attributes as a map, so a test can tell a null field
// from a missing one.
func getRawAttributes(t *testing.T, url string, token string) map[string]any {
	t.Helper()
	document := getJSON[struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}](t, url, token)
	return document.Data.Attributes
}

func createModelRevisionThroughAPI(t *testing.T, modelURL string, token string, body map[string]any) string {
	t.Helper()
	response := postJSON(t, modelURL+"/revisions", token, body)
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
