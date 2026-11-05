package integration

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
			"method": "CryoEM",
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

	t.Run("model create", func(t *testing.T) {
		// given: one entry per request, so each model is its entry's first. The ids are the
		// golden file's placeholders; withFreshIDs gives each body its own.
		formEntryID := seedActiveEntry(t, database, auth.UserID)
		apiEntryID := seedActiveEntry(t, database, auth.UserID)
		const (
			modelFileID = "00000000-0000-4000-8000-000000000101"
			dataFileID  = "00000000-0000-4000-8000-000000000102"
			runID       = "00000000-0000-4000-8000-000000000103"
		)
		formBody := withFreshIDs(t, golden["model_create"])
		apiBody := withFreshIDs(t, map[string]any{"model": map[string]any{
			"title":           "Room-temperature multiconformer model",
			"idempotency_key": strings.Repeat("a", 64),
			"metadata": map[string]any{
				"purpose":               "Refinement",
				"model_type":            "Multiconformer",
				"details":               "Refined at room temperature.",
				"authors":               []string{"A. Author", "B. Author"},
				"affiliation":           "Example Lab",
				"atom_count":            1234,
				"modeled_residues":      164,
				"unique_protein_chains": 1,
			},
			"primary_artifact_id": modelFileID,
			"artifacts": []any{
				map[string]any{
					"id": modelFileID, "name": "model.cif", "level": "L2", "type": "model",
					"uri": "s3://parity/model.cif", "sha256": strings.Repeat("a", 64), "format": "mmcif", "size_bytes": 2048,
				},
				map[string]any{
					"id": dataFileID, "name": "data.mtz", "level": "L0", "type": "structure_factors",
					"uri": "s3://parity/data.mtz", "sha256": strings.Repeat("b", 64), "format": "mtz", "size_bytes": 4096,
				},
			},
			"runs": []any{map[string]any{
				"id": runID, "name": "phenix.refine", "software_name": "phenix.refine", "software_version": "1.21",
				"command": "phenix.refine model.cif data.mtz", "parameters": map[string]any{"cycles": 5},
				"artifacts": []any{
					map[string]any{"artifact_id": dataFileID, "direction": "input"},
					map[string]any{"artifact_id": modelFileID, "direction": "output"},
				},
			}},
			"metrics": []any{
				map[string]any{"id": uuid.NewString(), "key": "r_work", "value": 0.196, "run_id": runID},
				map[string]any{"id": uuid.NewString(), "key": "r_free", "value": 0.231, "run_id": runID},
			},
			"value_sources": map[string]any{
				"/metadata/atom_count":            map[string]any{"artifact_id": modelFileID},
				"/metadata/modeled_residues":      map[string]any{"artifact_id": modelFileID},
				"/metadata/unique_protein_chains": map[string]any{"artifact_id": modelFileID},
			},
		}})

		// when
		formRevisionID := createModelThroughAPI(t, backend.URL, auth.AccessToken, formEntryID, formBody)
		apiRevisionID := createModelThroughAPI(t, backend.URL, auth.AccessToken, apiEntryID, apiBody)
		formRecord := storedModelGraph(t, database, formRevisionID)

		// then
		assert.Equal(t, storedModelGraph(t, database, apiRevisionID), formRecord)
		sources := nestedMap(t, nestedMap(t, formRecord, "revision"), "field_sources")
		assert.Equal(t, "file", nestedMap(t, sources, "/metadata/atom_count")["source"],
			"a count read from the model file keeps the file as its source")
	})
}

// withFreshIDs copies body with every UUID in it replaced by a fresh one, the same fresh
// one wherever the old one appears, so links between the request's parts still hold. A
// body posted twice would otherwise collide with the ids already stored.
func withFreshIDs(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	fresh := map[string]string{}
	var walk func(value any) any
	walk = func(value any) any {
		switch typed := value.(type) {
		case map[string]any:
			result := make(map[string]any, len(typed))
			for key, item := range typed {
				result[key] = walk(item)
			}
			return result
		case []any:
			result := make([]any, len(typed))
			for index, item := range typed {
				result[index] = walk(item)
			}
			return result
		case string:
			if _, err := uuid.Parse(typed); err != nil {
				return typed
			}
			if _, seen := fresh[typed]; !seen {
				fresh[typed] = uuid.NewString()
			}
			return fresh[typed]
		default:
			return value
		}
	}
	// Plain JSON values (maps, lists, strings, float64), the shape the golden file decodes
	// to, so hand-written and golden bodies are walked the same way.
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	freshBody, ok := walk(decodeJSONValue(t, encoded)).(map[string]any)
	require.True(t, ok)
	return freshBody
}

func createModelThroughAPI(t *testing.T, backendURL, token, entryID string, body any) string {
	t.Helper()
	response := postJSON(t, backendURL+"/v1/entries/"+entryID+"/models", token, body)
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

// storedModelGraph reads what a model create stored: the revision, its artifacts, runs, run
// links and metrics. Ids are replaced by the name of what they point to (artifact:model.cif,
// run:phenix.refine), and ids of the revision itself, of its model and timestamps are left
// out, so two creates with the same content compare equal.
func storedModelGraph(t *testing.T, database integrationDB, revisionID string) map[string]any {
	t.Helper()
	var encoded []byte
	require.NoError(t, database.Conn.QueryRowContext(t.Context(), `
		select jsonb_build_object(
			'revision', (select to_jsonb(revision) - 'id' - 'model_id' - 'created_by' - 'created_at' - 'updated_at'
				from model_revisions revision where revision.id = $1),
			'artifacts', (select coalesce(jsonb_agg(to_jsonb(artifact) - 'created_by' - 'created_at' order by artifact.name), '[]')
				from artifacts artifact
				join model_revision_artifacts link on link.artifact_id = artifact.id
				where link.model_revision_id = $1),
			'runs', (select coalesce(jsonb_agg(to_jsonb(run) - 'created_by' - 'created_at' - 'updated_at' order by run.name), '[]')
				from runs run
				join model_revision_runs link on link.run_id = run.id
				where link.model_revision_id = $1),
			'run_artifacts', (select coalesce(jsonb_agg(to_jsonb(run_artifact) order by run_artifact.direction), '[]')
				from run_artifacts run_artifact
				join model_revision_runs link on link.run_id = run_artifact.run_id
				where link.model_revision_id = $1),
			'metrics', (select coalesce(jsonb_agg(to_jsonb(metric) - 'id' - 'created_at' order by metric.key), '[]')
				from metrics metric
				join model_revision_metrics link on link.metric_id = metric.id
				where link.model_revision_id = $1)
		)`, revisionID,
	).Scan(&encoded))
	record, ok := decodeJSONValue(t, encoded).(map[string]any)
	require.True(t, ok)
	names := map[string]string{}
	for _, part := range []struct{ key, prefix string }{{"artifacts", "artifact:"}, {"runs", "run:"}} {
		for _, item := range nestedList(t, record, part.key) {
			object := item.(map[string]any)
			names[object["id"].(string)] = part.prefix + object["name"].(string)
		}
	}
	named := replaceStrings(record, names)
	return withoutKey(named, "retrieved_at").(map[string]any)
}

// replaceStrings copies value with every string that is a key of replacements replaced.
func replaceStrings(value any, replacements map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = replaceStrings(item, replacements)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = replaceStrings(item, replacements)
		}
		return result
	case string:
		if replacement, found := replacements[typed]; found {
			return replacement
		}
		return typed
	default:
		return value
	}
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
