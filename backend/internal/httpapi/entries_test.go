package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

func Test_should_default_artifact_type_to_other_when_type_is_missing(t *testing.T) {
	// given
	// when
	artifactType, err := artifactTypeFromRequest("")

	// then
	require.NoError(t, err)
	assert.Equal(t, domainmodels.ArtifactTypeOther, artifactType)
}

func Test_should_reject_duplicate_structure_factors_when_model_artifacts_are_validated(t *testing.T) {
	// given
	requests := []CreateArtifactRequest{
		{Id: uuid.New(), Type: ArtifactTypeStructureFactors},
		{Id: uuid.New(), Type: ArtifactTypeStructureFactors},
	}

	// when
	err := validateArtifactRequests(&requests, artifactAttachmentScopeModel)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_assembly_artifact_type(t *testing.T) {
	// given
	artifactType := ArtifactType("assembly")

	// when
	_, err := artifactTypeFromRequest(artifactType)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_model_artifact_type_when_entry_artifacts_are_validated(t *testing.T) {
	// given
	requests := []CreateArtifactRequest{{Id: uuid.New(), Type: ArtifactTypeModel}}

	// when
	err := validateArtifactRequests(&requests, artifactAttachmentScopeEntry)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_fasta_type_when_artifact_format_is_not_fasta(t *testing.T) {
	// given
	format := "cif"
	requests := []CreateArtifactRequest{
		{Id: uuid.New(), Type: ArtifactTypeFASTA, Format: &format},
	}

	// when
	err := validateArtifactRequests(&requests, artifactAttachmentScopeEntry)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

// schemaTestServer has no database, so a handler that got past validation would panic on its first query.
func schemaTestServer() *Server {
	return &Server{validationMode: validation.ModeEnforce}
}

func schemaTestRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	return request.WithContext(WithUser(request.Context(), &domainmodels.User{ID: uuid.New()}))
}

func schemaTestModel() map[string]any {
	artifactID := uuid.NewString()
	return map[string]any{
		"title": "model",
		"metadata": map[string]any{
			"external_refs": map[string]any{"pdb": "1ABC"},
			"purpose":       "Refinement",
			"model_type":    "Single Conformer",
		},
		"artifacts": []map[string]any{{
			"id": artifactID, "name": "model.cif", "level": "L1", "type": "model", "format": "cif",
			"sha256": strings.Repeat("a", 64), "size_bytes": 12,
		}},
		"runs": []map[string]any{{
			"id": uuid.NewString(), "name": "refine",
			"artifacts": []map[string]any{{"artifact_id": artifactID, "direction": "output"}},
		}},
		"metrics": []map[string]any{
			{"id": uuid.NewString(), "key": "r_free", "value": 0.21},
			{"id": uuid.NewString(), "key": "r_work", "value": 0.18},
		},
	}
}

func decodeErrorDocument(t *testing.T, recorder *httptest.ResponseRecorder) Error {
	t.Helper()
	var document Error
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&document))
	return document
}

func errorPointers(document Error) []string {
	pointers := make([]string, 0, len(document.Errors))
	for _, errorObject := range document.Errors {
		pointer := ""
		if errorObject.Source != nil && errorObject.Source.Pointer != nil {
			pointer = *errorObject.Source.Pointer
		}
		pointers = append(pointers, pointer)
	}
	return pointers
}

func Test_should_reject_metric_when_key_not_in_schema(t *testing.T) {
	// given
	model := schemaTestModel()
	model["metrics"].([]map[string]any)[1]["key"] = "r_factor"
	request := schemaTestRequest(t, http.MethodPost, "/v1/entries/dpdb_test/models", map[string]any{"model": model})
	recorder := httptest.NewRecorder()

	// when
	schemaTestServer().CreateModel(recorder, request, "dpdb_test")

	// then
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	document := decodeErrorDocument(t, recorder)
	require.Len(t, document.Errors, 1)
	assert.Equal(t, "422", document.Errors[0].Status)
	assert.Equal(t, string(validation.CodeNotInSchema), *document.Errors[0].Code)
	assert.Equal(t, []string{"/model/metrics/1/key"}, errorPointers(document))
}

func Test_should_reject_metric_when_value_missing(t *testing.T) {
	// given
	model := schemaTestModel()
	delete(model["metrics"].([]map[string]any)[0], "value")
	request := schemaTestRequest(t, http.MethodPost, "/v1/entries/dpdb_test/models", map[string]any{"model": model})
	recorder := httptest.NewRecorder()

	// when
	schemaTestServer().CreateModel(recorder, request, "dpdb_test")

	// then
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	document := decodeErrorDocument(t, recorder)
	assert.Equal(t, []string{"/model/metrics/0/value"}, errorPointers(document))
	assert.Equal(t, string(validation.CodeRequired), *document.Errors[0].Code)
}

func Test_should_return_every_violation_with_pointer_when_create_entry_is_invalid(t *testing.T) {
	// given
	model := schemaTestModel()
	model["metadata"].(map[string]any)["purpose"] = "Validation"
	model["artifacts"].([]map[string]any)[0]["format"] = "CIF"
	model["artifacts"].([]map[string]any)[0]["metadata"] = map[string]any{"__registry": true}
	request := schemaTestRequest(t, http.MethodPost, "/v1/entries", map[string]any{
		"entry":            map[string]any{"title": "entry", "resolution": -1.5},
		"model_operations": []map[string]any{{"op": "add", "data": model}},
	})
	recorder := httptest.NewRecorder()

	// when
	schemaTestServer().CreateEntry(recorder, request)

	// then
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	assert.Equal(t, []string{
		"/entry/method",
		"/entry/resolution",
		"/model_operations/0/data/metadata/purpose",
		"/model_operations/0/data/artifacts/0/format",
		"/model_operations/0/data/artifacts/0/metadata/__registry",
	}, errorPointers(decodeErrorDocument(t, recorder)))
}

func Test_should_reject_entry_revision_when_method_is_cleared(t *testing.T) {
	// given
	request := schemaTestRequest(t, http.MethodPost, "/v1/entries/dpdb_test/revisions", map[string]any{
		"entry": map[string]any{"method": nil},
	})
	recorder := httptest.NewRecorder()

	// when
	schemaTestServer().CreateEntryRevision(recorder, request, "dpdb_test")

	// then
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	assert.Equal(t, []string{"/entry/method"}, errorPointers(decodeErrorDocument(t, recorder)))
}

func Test_should_reject_model_revision_when_metric_key_is_duplicated(t *testing.T) {
	// given
	request := schemaTestRequest(t, http.MethodPost, "/v1/entries/dpdb_test/models/dpdb_test_m_001/revisions", map[string]any{
		"model": map[string]any{"metrics": []map[string]any{
			{"id": uuid.NewString(), "key": "rmsf", "value": 0.4},
			{"id": uuid.NewString(), "key": "rmsf", "value": 0.5},
		}},
	})
	recorder := httptest.NewRecorder()

	// when
	schemaTestServer().CreateModelRevision(recorder, request, "dpdb_test", "dpdb_test_m_001")

	// then
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	document := decodeErrorDocument(t, recorder)
	assert.Equal(t, []string{"/model/metrics/1/key"}, errorPointers(document))
	assert.Equal(t, string(validation.CodeDuplicate), *document.Errors[0].Code)
}

func Test_should_keep_bad_request_when_model_metadata_cannot_be_decoded(t *testing.T) {
	// given
	model := schemaTestModel()
	model["metadata"] = map[string]any{"purpose": 5}
	request := schemaTestRequest(t, http.MethodPost, "/v1/entries/dpdb_test/models", map[string]any{"model": model})
	recorder := httptest.NewRecorder()

	// when
	schemaTestServer().CreateModel(recorder, request, "dpdb_test")

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func Test_should_apply_default_limit_when_list_entries_limit_is_missing(t *testing.T) {
	// given
	// when
	_, page, err := entryFiltersFromParams(ListEntriesParams{})

	// then
	require.NoError(t, err)
	assert.Equal(t, defaultListLimit, page.Limit)
}

func Test_should_keep_explicit_limit_when_list_entries_limit_is_provided(t *testing.T) {
	// given
	limit := 7

	// when
	got, page, err := entryFiltersFromParams(ListEntriesParams{Limit: &limit})

	// then
	require.NoError(t, err)
	assert.Equal(t, limit, page.Limit)
	require.NotNil(t, got.Limit)
	assert.Equal(t, limit+1, *got.Limit, "lists fetch one row past the page to detect a next page")
}

func Test_should_cap_limit_when_list_entries_limit_exceeds_maximum(t *testing.T) {
	// given
	limit := 5000

	// when
	_, page, err := entryFiltersFromParams(ListEntriesParams{Limit: &limit})

	// then
	require.NoError(t, err)
	assert.Equal(t, maxListLimit, page.Limit)
}

func Test_should_truncate_search_query_when_list_entries_query_is_too_long(t *testing.T) {
	// given
	query := strings.Repeat("Ж", maxEntrySearchQueryLength+1)

	// when
	got, _, err := entryFiltersFromParams(ListEntriesParams{Query: &query})

	// then
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("Ж", maxEntrySearchQueryLength), got.Query)
}

func Test_should_bound_pdb_filters_when_list_entries_has_too_many_long_values(t *testing.T) {
	// given
	pdbIDs := make([]string, maxListFilterValues+1)
	for index := range pdbIDs {
		pdbIDs[index] = strings.Repeat("A", maxEntryPDBIDLength+1)
	}

	// when
	got, _, err := entryFiltersFromParams(ListEntriesParams{PdbId: &pdbIDs})

	// then
	require.NoError(t, err)
	require.Len(t, got.PDBIDs, maxListFilterValues)
	assert.Equal(t, strings.Repeat("A", maxEntryPDBIDLength), got.PDBIDs[0])
}

func Test_should_bound_levels_when_list_artifacts_has_too_many_values(t *testing.T) {
	// given
	levels := make([]ArtifactLevel, maxListFilterValues+1)
	for index := range levels {
		levels[index] = L0
	}

	// when
	got, _, err := artifactFiltersFromParams(ListArtifactsParams{Levels: &levels})

	// then
	require.NoError(t, err)
	require.Len(t, got.Levels, maxListFilterValues)
}

func Test_should_cap_limit_when_revision_pagination_limit_exceeds_maximum(t *testing.T) {
	// given
	limit := 5000

	// when
	page, err := listPageFromParams(&limit, nil, nil, listCursorKindEntryRevisions)

	// then
	require.NoError(t, err)
	assert.Equal(t, maxListLimit, page.Limit)
}

func Test_should_apply_default_limit_when_list_models_across_entries_limit_is_missing(t *testing.T) {
	// given
	// when
	_, page, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{})

	// then
	require.NoError(t, err)
	assert.Equal(t, defaultListLimit, page.Limit)
}

func Test_should_reject_negative_offset_when_list_models_across_entries_offset_is_provided(t *testing.T) {
	// given
	offset := -1

	// when
	_, _, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{Offset: &offset})

	// then
	require.Error(t, err)
}

func Test_should_apply_default_limit_when_list_artifacts_limit_is_missing(t *testing.T) {
	// given
	// when
	_, page, err := artifactFiltersFromParams(ListArtifactsParams{})

	// then
	require.NoError(t, err)
	assert.Equal(t, defaultListLimit, page.Limit)
}

func Test_should_apply_default_limit_when_list_model_artifacts_limit_is_missing(t *testing.T) {
	// given
	// when
	_, page, err := modelArtifactFiltersFromParams(ListModelArtifactsParams{})

	// then
	require.NoError(t, err)
	assert.Equal(t, defaultListLimit, page.Limit)
}

func Test_should_reject_state_field_when_entry_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"state": json.RawMessage(`"deleted"`)}

	// when
	err := validateRevisionFields(fields, entryRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_apply_flat_properties_when_entry_revision_is_created(t *testing.T) {
	// given
	details := "crystal structure"
	resolution := 1.8
	method := "X-ray crystallography"
	spaceGroup := "P 21 21 21"
	ph := 7.4
	crystals := []EntryCrystal{{
		Id: "1",
		Growth: &EntryCrystalGrowth{
			Ph: &ph,
		},
	}}
	change := EntryRevisionChange{
		ExternalRefs:    &map[string]string{"pdb": "1ABC"},
		Details:         &details,
		Resolution:      &resolution,
		Method:          &method,
		SpaceGroup:      &spaceGroup,
		Crystallography: &EntryCrystallography{Crystals: &crystals},
	}
	fields := map[string]json.RawMessage{
		"external_refs":   json.RawMessage(`{"pdb":"1ABC"}`),
		"details":         json.RawMessage(`"crystal structure"`),
		"resolution":      json.RawMessage(`1.8`),
		"method":          json.RawMessage(`"X-ray crystallography"`),
		"space_group":     json.RawMessage(`"P 21 21 21"`),
		"crystallography": json.RawMessage(`{"crystals":[{"id":"1","growth":{"ph":7.4}}]}`),
	}
	revision := domainmodels.EntryRevision{}

	// when
	err := applyEntryRevisionChange(&revision, change, fields)

	// then
	require.NoError(t, err)
	assert.Equal(t, "1ABC", revision.Metadata.ExternalRefs[domainmodels.EntrySourcePDB])
	require.NotNil(t, revision.Metadata.Details)
	assert.Equal(t, details, *revision.Metadata.Details)
	require.NotNil(t, revision.Metadata.Resolution)
	assert.Equal(t, resolution, *revision.Metadata.Resolution)
	require.NotNil(t, revision.Metadata.Method)
	assert.Equal(t, domainmodels.StructureMethodXRayCrystallography, *revision.Metadata.Method)
	require.NotNil(t, revision.Metadata.SpaceGroup)
	assert.Equal(t, spaceGroup, *revision.Metadata.SpaceGroup)
	require.NotNil(t, revision.Metadata.Crystallography)
	require.Len(t, revision.Metadata.Crystallography.Crystals, 1)
	assert.Equal(t, "1", revision.Metadata.Crystallography.Crystals[0].ID)
}

func Test_should_reject_metadata_field_when_entry_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"metadata": json.RawMessage(`{}`)}

	// when
	err := validateRevisionFields(fields, entryRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_state_field_when_model_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"state": json.RawMessage(`"deleted"`)}

	// when
	err := validateRevisionFields(fields, modelRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}
