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

// The entry values the archive lookup proposes, keyed as a create request carries them.
var archiveProposalEntryKeys = []string{"title", "details", "resolution", "method", "space_group", "crystallography"}

// The field-source pointer of each proposed value.
var archiveProposalPointers = map[string]string{
	"title":           "/title",
	"details":         "/metadata/details",
	"resolution":      "/metadata/resolution",
	"method":          "/metadata/method",
	"space_group":     "/metadata/space_group",
	"crystallography": "/metadata/crystallography",
}

func Test_should_mark_values_equal_to_the_archive_rcsb_when_entry_is_created(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: a backend whose RCSB serves one PDB entry.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	pdbID := unusedPDBID(t, database)
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	useRCSBStub(t, rcsb)
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	proposal := archiveEntryProposal(t, backend.URL, auth.AccessToken, pdbID)
	release, ok := proposal["latest_version"].(string)
	require.True(t, ok, "the stub entry has a version")

	// when: the form sends the lookup's values unchanged.
	revisionURL := createEntryFromProposal(t, backend.URL, auth.AccessToken, pdbID, proposal, nil)
	sources := getJSON[entryRevisionSourcesDocument](t, revisionURL, auth.AccessToken).Data.Attributes.FieldSources

	// then: each value the archive has is rcsb, so the weekly sync keeps it current.
	proposed := 0
	for _, key := range archiveProposalEntryKeys {
		if proposal[key] == nil {
			continue
		}
		proposed++
		source := sources[archiveProposalPointers[key]]
		assert.Equal(t, "rcsb", source.Source, key)
		require.NotNil(t, source.Accession, key)
		assert.Equal(t, pdbID, *source.Accession, key)
		require.NotNil(t, source.Release, key)
		assert.Equal(t, release, *source.Release, key)
	}
	assert.GreaterOrEqual(t, proposed, 3, "the stub proposes title, method and resolution at least")
}

func Test_should_mark_a_changed_value_depositor_and_the_rest_rcsb_when_entry_is_created(t *testing.T) {
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
	s3 := newS3Stub(t)
	defer s3.Close()
	pdbID := unusedPDBID(t, database)
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	useRCSBStub(t, rcsb)
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	proposal := archiveEntryProposal(t, backend.URL, auth.AccessToken, pdbID)

	// when: the request's title differs from the archive's.
	revisionURL := createEntryFromProposal(t, backend.URL, auth.AccessToken, pdbID, proposal,
		map[string]any{"title": "A title the depositor typed"})
	sources := getJSON[entryRevisionSourcesDocument](t, revisionURL, auth.AccessToken).Data.Attributes.FieldSources

	// then
	assert.Equal(t, fieldSourceBody{Source: "depositor"}, sources["/title"])
	assert.Equal(t, "rcsb", sources["/metadata/method"].Source)
	assert.Equal(t, "rcsb", sources["/metadata/resolution"].Source)
}

func Test_should_keep_every_value_depositor_when_archive_lookup_fails_at_create(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: RCSB knows another PDB entry, so the lookup for this one fails.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, unusedPDBID(t, database))
	defer rcsb.Close()
	useRCSBStub(t, rcsb)
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	unknownPDBID := unusedPDBID(t, database)

	// when: the form, unable to read the PDB entry, sends typed values.
	revisionURL := createEntryFromProposal(t, backend.URL, auth.AccessToken, unknownPDBID,
		map[string]any{"title": "example structure", "method": "X-ray crystallography", "resolution": 1.4}, nil)
	sources := getJSON[entryRevisionSourcesDocument](t, revisionURL, auth.AccessToken).Data.Attributes.FieldSources

	// then: every typed value is the depositor's.
	assert.Equal(t, map[string]fieldSourceBody{
		"/title":               {Source: "depositor"},
		"/metadata/method":     {Source: "depositor"},
		"/metadata/resolution": {Source: "depositor"},
	}, sources)
}

// unusedPDBID returns a test PDB ID no active entry uses yet: a second entry with the same
// PDB ID is refused, and test entries stay in the shared local database.
func unusedPDBID(t *testing.T, database integrationDB) string {
	t.Helper()
	for range 50 {
		pdbID := integrationPDBID()
		var used bool
		require.NoError(t, database.Conn.QueryRowContext(t.Context(),
			`select exists(
			   select 1 from entry_revisions
			   where state = 'active' and metadata->'external_refs'->>'pdb' = $1
			 )`, pdbID,
		).Scan(&used))
		if !used {
			return pdbID
		}
	}
	require.FailNow(t, "no unused test PDB ID after 50 tries")
	return ""
}

func useRCSBStub(t *testing.T, rcsb *rcsbStub) {
	t.Helper()
	t.Setenv("DYNAMIC_PDB_RCSB_DATA_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_FILES_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_WWW_URL", rcsb.URL())
	t.Setenv("DYNAMIC_PDB_RCSB_CDN_URL", rcsb.URL())
}

// archiveEntryProposal reads the entry values the lookup proposes, as the New entry form does.
func archiveEntryProposal(t *testing.T, backendURL, token, pdbID string) map[string]any {
	t.Helper()
	document := getJSON[struct {
		Data struct {
			Attributes struct {
				Entry map[string]any `json:"entry"`
			} `json:"attributes"`
		} `json:"data"`
	}](t, backendURL+"/v1/archive/pdb/"+pdbID, token)
	return document.Data.Attributes.Entry
}

// createEntryFromProposal creates an entry for pdbID with the proposed values, each copied
// unchanged unless overrides replaces it, and returns the URL of its first revision.
func createEntryFromProposal(
	t *testing.T,
	backendURL, token, pdbID string,
	proposal map[string]any,
	overrides map[string]any,
) string {
	t.Helper()
	entry := map[string]any{"external_refs": map[string]string{"pdb": pdbID}}
	for _, key := range archiveProposalEntryKeys {
		if value, ok := proposal[key]; ok && value != nil {
			entry[key] = value
		}
	}
	for key, value := range overrides {
		entry[key] = value
	}
	response := postJSON(t, backendURL+"/v1/entries", token, map[string]any{"entry": entry})
	defer func() {
		assert.NoError(t, response.Body.Close())
	}()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(body))
	var document struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				EntryID string `json:"entry_id"`
			} `json:"attributes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &document))
	return fmt.Sprintf("%s/v1/entries/%s/revisions/%s", backendURL, document.Data.Attributes.EntryID, document.Data.ID)
}
