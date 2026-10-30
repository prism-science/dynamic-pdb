package integration

import (
	"fmt"
	"os"
	"testing"

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
