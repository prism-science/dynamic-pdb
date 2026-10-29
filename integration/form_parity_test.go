package integration

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The website's golden form requests (website/test/form-parity.test.cjs writes them). Each
// is posted next to a hand-written API body with the same content; both must store the same
// records, apart from ids and timestamps.
const formParityGoldenPath = "website/test/fixtures/form-parity-requests.json"

// The PDB ID the golden create request names; the test serves an unused one in its place.
const formParityGoldenPDBID = "9PAR"

// The base model the golden model revision was built from (BASE_MODEL in the website test).
const formParityBaseModelMetadata = `{
	"purpose": "Refinement",
	"model_type": "Multiconformer",
	"details": "Base details",
	"authors": ["A. Author"],
	"ligands": [],
	"cofactors": [],
	"extensions": {"lab": {"notebook": "page 12"}}
}`

func Test_should_store_the_same_records_when_form_and_api_send_the_same_content(t *testing.T) {
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
	reviewer := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	pdbID := unusedPDBID(t, database)
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	useRCSBStub(t, rcsb)
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	golden := readFormParityGolden(t, root)

	t.Run("entry create, autofilled from the PDB entry", func(t *testing.T) {
		// given
		formBody := golden["entry_create"]
		entry := nestedMap(t, formBody, "entry")
		require.Equal(t, map[string]any{"pdb": formParityGoldenPDBID}, entry["external_refs"])
		entry["external_refs"] = map[string]any{"pdb": pdbID}
		apiBody := map[string]any{"entry": map[string]any{
			"title":         "example structure",
			"external_refs": map[string]any{"pdb": pdbID},
			"details":       "Room-temperature structure.",
			"method":        "X-ray crystallography",
			"resolution":    1.4,
			"space_group":   "P 21 21 21",
			"crystallography": map[string]any{"crystals": []any{map[string]any{
				"id":           "1",
				"growth":       map[string]any{"ph": 7.5, "temperature_kelvin": 293},
				"diffractions": []any{map[string]any{"id": "1", "temperature_kelvin": 100}},
			}}},
			"ligands":   []string{"ATP"},
			"cofactors": []string{},
			"polymer_entities": []any{map[string]any{
				"label_entity_id": "1", "construct": "residues 1-164", "mutations": []string{"C54T"},
			}},
		}}

		// when
		_, formRevisionID := createEntryThroughAPI(t, backend.URL, auth.AccessToken, formBody)
		_, apiRevisionID := createEntryThroughAPI(t, backend.URL, auth.AccessToken, apiBody)
		formRecord := storedEntryRevision(t, database, formRevisionID)

		// then
		assert.Equal(t, storedEntryRevision(t, database, apiRevisionID), formRecord)
		sources := nestedMap(t, formRecord, "field_sources")
		assert.Equal(t, "rcsb", nestedMap(t, sources, "/title")["source"], "the autofilled title stays the archive's")
		assert.Equal(t, "depositor", nestedMap(t, sources, "/metadata/details")["source"])
	})

	t.Run("entry revision", func(t *testing.T) {
		// given: two published entries made from the same base.
		baseBody := map[string]any{"entry": map[string]any{
			"title":      "Parity base entry",
			"method":     "X-ray crystallography",
			"resolution": 2,
			"crystallography": map[string]any{"crystals": []any{map[string]any{
				"id":           "1",
				"growth":       map[string]any{"ph": 7},
				"diffractions": []any{map[string]any{"id": "1", "temperature_kelvin": 100}},
			}}},
			"ligands":   []string{"ATP"},
			"cofactors": []string{},
		}}
		formEntryID := createPublishedEntry(t, backend.URL, auth.AccessToken, reviewer.AccessToken, baseBody)
		apiEntryID := createPublishedEntry(t, backend.URL, auth.AccessToken, reviewer.AccessToken, baseBody)
		apiBody := map[string]any{"entry": map[string]any{
			"method": "Neutron diffraction",
			"crystallography": map[string]any{"crystals": []any{
				map[string]any{
					"id":           "1",
					"growth":       map[string]any{"ph": 7.5},
					"diffractions": []any{map[string]any{"id": "1", "temperature_kelvin": 100}},
				},
				map[string]any{"id": "2", "growth": map[string]any{"temperature_kelvin": 293}},
			}},
		}}

		// when
		formRevisionID := createEntryRevisionThroughAPI(t, backend.URL, auth.AccessToken, formEntryID,
			golden["entry_revision"])
		apiRevisionID := createEntryRevisionThroughAPI(t, backend.URL, auth.AccessToken, apiEntryID, apiBody)

		// then
		assert.Equal(t, storedEntryRevision(t, database, apiRevisionID),
			storedEntryRevision(t, database, formRevisionID))
	})

	t.Run("model revision", func(t *testing.T) {
		// given: two published models with the same metadata.
		entryID := seedActiveEntry(t, database, auth.UserID)
		formModelID := entryID + "_m_001"
		apiModelID := entryID + "_m_002"
		now := time.Now().UTC()
		seedActiveModel(t, database, entryID, formModelID, auth.UserID, now, formParityBaseModelMetadata)
		seedActiveModel(t, database, entryID, apiModelID, auth.UserID, now, formParityBaseModelMetadata)
		formBody := golden["model_revision"]
		for _, metric := range nestedList(t, nestedMap(t, formBody, "model"), "metrics") {
			metric.(map[string]any)["id"] = uuid.NewString()
		}
		apiBody := map[string]any{"model": map[string]any{
			"metadata": map[string]any{
				"purpose":    "Refinement",
				"model_type": "Multiconformer",
				"details":    "Edited details",
				"authors":    []string{"A. Author", "B. Author"},
				"ligands":    []string{},
				"cofactors":  []string{},
				"extensions": map[string]any{"lab": map[string]any{"notebook": "page 12"}},
			},
			"metrics": []any{map[string]any{
				"id": uuid.NewString(), "key": "r_free", "value": 0.231,
				"software_name": "phenix.refine", "software_version": "1.21",
			}},
		}}
		modelsURL := backend.URL + "/v1/entries/" + entryID + "/models/"

		// when
		formRevisionID := createModelRevisionThroughAPI(t, modelsURL+formModelID, auth.AccessToken, formBody)
		apiRevisionID := createModelRevisionThroughAPI(t, modelsURL+apiModelID, auth.AccessToken, apiBody)

		// then
		assert.Equal(t, storedModelRevision(t, database, apiRevisionID),
			storedModelRevision(t, database, formRevisionID))
	})
}

func readFormParityGolden(t *testing.T, root string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, formParityGoldenPath))
	require.NoError(t, err)
	var golden map[string]map[string]any
	require.NoError(t, json.Unmarshal(data, &golden))
	return golden
}

func createEntryThroughAPI(t *testing.T, backendURL, token string, body any) (string, string) {
	t.Helper()
	response := postJSON(t, backendURL+"/v1/entries", token, body)
	defer func() {
		assert.NoError(t, response.Body.Close())
	}()
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(responseBody))
	var document struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				EntryID string `json:"entry_id"`
			} `json:"attributes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(responseBody, &document))
	return document.Data.Attributes.EntryID, document.Data.ID
}

// createPublishedEntry creates an entry and approves its first revision as another user: a
// reviewer cannot approve their own revisions.
func createPublishedEntry(t *testing.T, backendURL, token, reviewerToken string, body any) string {
	t.Helper()
	entryID, revisionID := createEntryThroughAPI(t, backendURL, token, body)
	patchJSON(t, backendURL+"/v1/entries/"+entryID+"/revisions/"+revisionID, reviewerToken,
		map[string]any{"state": "active"}, http.StatusOK)
	return entryID
}

// storedEntryRevision reads what an entry revision stored, without ids and timestamps.
func storedEntryRevision(t *testing.T, database integrationDB, revisionID string) map[string]any {
	t.Helper()
	var title, thumbnail, changeSummary sql.NullString
	var state string
	var metadata, sources []byte
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select title, thumbnail_image_url, change_summary, state, metadata,
		        coalesce(field_sources, 'null'::jsonb)
		 from entry_revisions where id = $1`, revisionID,
	).Scan(&title, &thumbnail, &changeSummary, &state, &metadata, &sources))
	return map[string]any{
		"title":               title,
		"thumbnail_image_url": thumbnail,
		"change_summary":      changeSummary,
		"state":               state,
		"metadata":            decodeJSONValue(t, metadata),
		"field_sources":       withoutKey(decodeJSONValue(t, sources), "retrieved_at"),
	}
}

// storedModelRevision reads what a model revision stored, without ids and timestamps.
func storedModelRevision(t *testing.T, database integrationDB, revisionID string) map[string]any {
	t.Helper()
	var title, changeSummary sql.NullString
	var state string
	var metadata, sources []byte
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select title, change_summary, state, metadata, coalesce(field_sources, 'null'::jsonb)
		 from model_revisions where id = $1`, revisionID,
	).Scan(&title, &changeSummary, &state, &metadata, &sources))
	rows, err := database.Conn.QueryContext(t.Context(),
		`select metric.key, metric.value, metric.software_name, metric.software_version, metric.run_id
		 from metrics metric
		 join model_revision_metrics link on link.metric_id = metric.id
		 where link.model_revision_id = $1`, revisionID)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rows.Close())
	}()
	var metrics []string
	for rows.Next() {
		var key string
		var value float64
		var softwareName, softwareVersion, runID sql.NullString
		require.NoError(t, rows.Scan(&key, &value, &softwareName, &softwareVersion, &runID))
		encoded, err := json.Marshal([]any{key, value, softwareName, softwareVersion, runID})
		require.NoError(t, err)
		metrics = append(metrics, string(encoded))
	}
	require.NoError(t, rows.Err())
	sort.Strings(metrics)
	return map[string]any{
		"title":          title,
		"change_summary": changeSummary,
		"state":          state,
		"metadata":       decodeJSONValue(t, metadata),
		"field_sources":  withoutKey(decodeJSONValue(t, sources), "retrieved_at"),
		"metrics":        metrics,
	}
}

func decodeJSONValue(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	require.NoError(t, json.Unmarshal(data, &value))
	return value
}

// withoutKey drops key from every object in value, at any depth.
func withoutKey(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for name, item := range typed {
			if name != key {
				result[name] = withoutKey(item, key)
			}
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = withoutKey(item, key)
		}
		return result
	default:
		return value
	}
}

func nestedMap(t *testing.T, value any, key string) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	require.True(t, ok, "not an object: %v", value)
	nested, ok := object[key].(map[string]any)
	require.True(t, ok, "%s is not an object: %v", key, object[key])
	return nested
}

func nestedList(t *testing.T, object map[string]any, key string) []any {
	t.Helper()
	list, ok := object[key].([]any)
	require.True(t, ok, "%s is not a list: %v", key, object[key])
	return list
}
