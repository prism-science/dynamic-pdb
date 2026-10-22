package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

func Test_should_report_unknown_model_metadata_key_at_its_own_pointer_when_key_is_not_a_field(t *testing.T) {
	// given
	data := CreateModelData{
		Title:    "model",
		Metadata: &map[string]interface{}{"purpose": "Refinement", "colour": "blue", "ligand": []any{"HEM"}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Equal(t, []validation.Pointer{"/model/metadata/colour", "/model/metadata/ligand"}, pointersOf(fieldErrors))
	assert.Equal(t, []validation.Code{validation.CodeUnknownField, validation.CodeUnknownField}, codesOf(fieldErrors))
}

func Test_should_accept_model_metadata_extensions_when_keys_are_not_reserved(t *testing.T) {
	// given
	data := CreateModelData{
		Title:    "model",
		Metadata: &map[string]interface{}{"extensions": map[string]any{"lab_note": "see ELN 42", "nested": map[string]any{"a": 1}}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_accept_compound_review_when_cli_uploads_it_under_model_extensions(t *testing.T) {
	// given
	data := CreateModelData{
		Title: "model",
		Metadata: &map[string]interface{}{
			"cofactors": []string{"ZN"},
			"ligands":   []string{"SO4"},
			"extensions": map[string]any{"compound_review": []any{
				map[string]any{"list": "cofactors", "id": "ZN", "reason": "metal ion kept for review: the file has no metalc connection rows"},
			}},
		},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_reserved_keys_when_model_artifact_and_run_metadata_use_double_underscore(t *testing.T) {
	// given
	data := CreateModelData{
		Title:    "model",
		Metadata: &map[string]interface{}{"__internal": true, "extensions": map[string]any{"__hidden": 1}},
		Artifacts: &[]CreateArtifactRequest{{
			Id: uuid.New(), Name: "a.cif", Level: "L0", Type: new(ArtifactTypeModel),
			Metadata: &map[string]interface{}{"__x": 1},
		}},
		Runs: &[]CreateRunRequest{{
			Id: uuid.New(), Name: "refine",
			Metadata: &map[string]interface{}{"__y": 1},
		}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.ElementsMatch(t, []validation.Pointer{
		"/model/metadata/__internal",
		"/model/metadata/extensions/__hidden",
		"/model/artifacts/0/metadata/__x",
		"/model/runs/0/metadata/__y",
	}, pointersOf(fieldErrors))
	for _, fieldError := range fieldErrors {
		assert.Equal(t, validation.CodeReservedKey, fieldError.Code)
	}
}

func Test_should_accept_artifact_metadata_when_it_holds_fasta_records_and_single_underscore_source(t *testing.T) {
	// given
	metadata := map[string]interface{}{
		"records":             []any{map[string]any{"header": "1abc A", "sequence": "ACDE"}},
		"_dynamic_pdb_source": map[string]any{"provider": "ext", "experiment_id": "e1", "path": "a.cif", "sha256": "ab"},
		"extensions":          map[string]any{"anything": true},
	}
	request := []CreateArtifactRequest{{Id: uuid.New(), Name: "a.fasta", Level: "L0", Type: new(ArtifactTypeFASTA), Metadata: &metadata}}

	// when
	fieldErrors := artifactRequestErrors(validation.Root.Field("artifacts"), &request)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_unknown_artifact_and_run_metadata_keys_when_keys_are_outside_the_schema(t *testing.T) {
	// given
	artifactMetadata := map[string]interface{}{"records": []any{map[string]any{"header": "h", "sequence": "A", "length": 1}}, "color": 1}
	runMetadata := map[string]interface{}{"notes": "x"}
	artifacts := []CreateArtifactRequest{{Id: uuid.New(), Name: "a.fasta", Level: "L0", Type: new(ArtifactTypeFASTA), Metadata: &artifactMetadata}}
	runs := []CreateRunRequest{{Id: uuid.New(), Name: "refine", Metadata: &runMetadata}}

	// when
	artifactErrors := artifactRequestErrors(validation.Root.Field("artifacts"), &artifacts)
	runErrors := runRequestErrors(validation.Root.Field("runs"), &runs)

	// then
	assert.Equal(t, []validation.Pointer{"/artifacts/0/metadata/color", "/artifacts/0/metadata/records/0/length"}, pointersOf(artifactErrors))
	assert.Equal(t, []validation.Pointer{"/runs/0/metadata/notes"}, pointersOf(runErrors))
}

func Test_should_keep_null_and_empty_lists_distinct_when_model_metadata_round_trips(t *testing.T) {
	// given
	unknown := map[string]interface{}{"ligands": nil}
	absent := map[string]interface{}{"ligands": []any{}, "cofactors": []any{}, "authors": []any{}}
	present := map[string]interface{}{"ligands": []any{"HEM"}}

	// when
	unknownResponse := modelMetadataRoundTripForTest(t, unknown)
	absentResponse := modelMetadataRoundTripForTest(t, absent)
	presentResponse := modelMetadataRoundTripForTest(t, present)

	// then
	assert.Contains(t, unknownResponse, "ligands")
	assert.Nil(t, unknownResponse["ligands"])
	assert.Nil(t, unknownResponse["cofactors"])
	assert.Equal(t, []any{}, absentResponse["ligands"])
	assert.Equal(t, []any{}, absentResponse["cofactors"])
	assert.Equal(t, []any{}, absentResponse["authors"])
	assert.Equal(t, []any{"HEM"}, presentResponse["ligands"])
}

func Test_should_keep_extensions_when_model_metadata_round_trips(t *testing.T) {
	// given
	metadata := map[string]interface{}{"purpose": "Reference", "extensions": map[string]any{"lab": map[string]any{"id": "x"}}}

	// when
	response := modelMetadataRoundTripForTest(t, metadata)

	// then
	assert.Equal(t, "Reference", response["purpose"])
	assert.Equal(t, map[string]any{"lab": map[string]any{"id": "x"}}, response["extensions"])
}

func Test_should_require_method_when_new_entry_request_has_none(t *testing.T) {
	// given
	request := CreateEntryRequest{Entry: CreateEntryData{Title: "Lysozyme"}}

	// when
	fieldErrors := createEntryRequestErrors(request)

	// then
	assert.Equal(t, []validation.Pointer{"/entry/method"}, pointersOf(fieldErrors))
	assert.Equal(t, []validation.Code{validation.CodeRequired}, codesOf(fieldErrors))
}

func Test_should_accept_free_text_method_when_new_entry_request_names_one(t *testing.T) {
	// given
	request := CreateEntryRequest{Entry: CreateEntryData{Title: "Lysozyme", Method: "Serial femtosecond crystallography"}}

	// when
	fieldErrors := createEntryRequestErrors(request)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_validate_depositor_polymer_entities_when_entry_request_carries_them(t *testing.T) {
	// given
	entities := DepositorPolymerEntities{{LabelEntityId: "1", Mutations: &[]string{"C54T", "bad"}}}
	request := CreateEntryRequest{Entry: CreateEntryData{Title: "Lysozyme", Method: "X-ray", PolymerEntities: &entities}}

	// when
	fieldErrors := createEntryRequestErrors(request)

	// then
	assert.Equal(t, []validation.Pointer{"/entry/polymer_entities/0/mutations/1"}, pointersOf(fieldErrors))
	assert.Equal(t, []validation.Code{validation.CodeInvalidFormat}, codesOf(fieldErrors))
}

func Test_should_validate_depositor_polymer_entities_when_entry_revision_change_carries_them(t *testing.T) {
	// given
	payload := createEntryRevisionPayloadForTest(t,
		`{"entry":{"change_summary":"fix construct","polymer_entities":[{"label_entity_id":"","mutations":["x"]}]}}`)

	// when
	fieldErrors := entryRevisionChangeErrors(payload)

	// then
	assert.Equal(t, []validation.Pointer{
		"/entry/polymer_entities/0/label_entity_id",
		"/entry/polymer_entities/0/mutations/0",
	}, pointersOf(fieldErrors))
}

func Test_should_allow_change_summary_when_revision_changes_name_it(t *testing.T) {
	// given
	entryChange := map[string]json.RawMessage{"change_summary": json.RawMessage(`"x"`), "polymer_entities": json.RawMessage(`[]`)}
	modelChange := map[string]json.RawMessage{"change_summary": json.RawMessage(`"x"`)}

	// when
	entryErr := validateRevisionFields(entryChange, entryRevisionFields)
	modelErr := validateRevisionFields(modelChange, modelRevisionFields)

	// then
	assert.NoError(t, entryErr)
	assert.NoError(t, modelErr)
}

func Test_should_reject_polymer_entities_null_when_entry_revision_change_sets_them_to_null(t *testing.T) {
	// given
	payload := createEntryRevisionPayloadForTest(t, `{"entry":{"polymer_entities":null}}`)

	// when
	fieldErrors := entryRevisionChangeErrors(payload)

	// then
	assert.Equal(t, []validation.FieldError{
		{Pointer: "/entry/polymer_entities", Code: validation.CodeRequired, Detail: "must not be null"},
	}, fieldErrors)
}

func modelMetadataRoundTripForTest(t *testing.T, metadata map[string]interface{}) map[string]interface{} {
	t.Helper()
	stored, err := modelMetadataFromRequest(&metadata)
	require.NoError(t, err)
	encoded, err := json.Marshal(stored)
	require.NoError(t, err)
	var reloaded domainmodels.ModelMetadata
	require.NoError(t, json.Unmarshal(encoded, &reloaded))
	response, err := modelMetadataForResponse(reloaded)
	require.NoError(t, err)
	return response
}
