package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/validation"
)

func Test_should_point_to_nested_metric_when_entry_model_operation_metric_is_invalid(t *testing.T) {
	// given
	deposited := &map[string]interface{}{"model_type": "Deposited"}
	request := CreateEntryRequest{
		Entry: CreateEntryData{Title: "Lysozyme", Method: "X-ray crystallography"},
		ModelOperations: &[]AddModelOperation{
			{Op: AddModelOperationOpAdd, Data: AddModelData{Title: "first", Metadata: deposited}},
			{Op: AddModelOperationOpAdd, Data: AddModelData{
				Title:    "second",
				Metadata: deposited,
				Metrics:  &[]CreateMetricRequest{{Id: uuid.New(), Key: "resolution", Value: new(2.0)}},
			}},
		},
	}

	// when
	fieldErrors := createEntryRequestErrors(request)

	// then
	assert.Equal(t, []validation.Pointer{"/model_operations/1/data/metrics/0/key"}, pointersOf(fieldErrors))
}

func Test_should_reject_metric_when_value_missing(t *testing.T) {
	// given
	data := CreateModelData{
		Title:    "model",
		Metadata: &map[string]interface{}{"model_type": "Deposited"},
		Metrics:  &[]CreateMetricRequest{{Id: uuid.New(), Key: "r_free"}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Equal(t, []validation.FieldError{{
		Pointer: "/model/metrics/0/value",
		Code:    validation.CodeRequired,
		Detail:  "is required",
	}}, fieldErrors)
}

func Test_should_report_every_error_at_once_when_create_model_request_has_several(t *testing.T) {
	// given
	level := ArtifactLevel("L7")
	data := CreateModelData{
		Title:    " ",
		Metadata: &map[string]interface{}{"purpose": "Docking", "altloc_fraction": 2},
		Artifacts: &[]CreateArtifactRequest{
			{Id: uuid.Nil, Name: "model.cif", Level: level},
		},
		Metrics: &[]CreateMetricRequest{
			{Id: uuid.New(), Key: "r_free", Value: new(0.2)},
			{Id: uuid.New(), Key: "r_free", Value: new(0.3)},
		},
		Runs: &[]CreateRunRequest{{
			Id:        uuid.New(),
			Name:      "refine",
			Artifacts: &[]CreateRunArtifactRequest{{ArtifactId: uuid.Nil, Direction: "sideways"}},
		}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Equal(t, []validation.Pointer{
		"/model/title",
		"/model/metadata/purpose",
		"/model/metadata/altloc_fraction",
		"/model/artifacts/0/id",
		"/model/artifacts/0/level",
		"/model/artifacts/0/type",
		"/model/metrics/1/key",
		"/model/metrics/0/software_name",
		"/model/metrics/1/software_name",
		"/model/runs/0/artifacts/0/artifact_id",
		"/model/runs/0/artifacts/0/direction",
		"/model/runs/0/software_name",
		"/model/runs/0/software_version",
		"/model/runs/0/artifacts",
		"/model/runs/0/artifacts",
	}, pointersOf(fieldErrors))
}

func Test_should_require_metric_software_and_run_provenance_when_computed_model_is_created(t *testing.T) {
	// given
	inputID, unknownID := uuid.New(), uuid.New()
	data := CreateModelData{
		Title:     "model",
		Metadata:  &map[string]interface{}{"model_type": "Multiconformer"},
		Artifacts: &[]CreateArtifactRequest{{Id: inputID, Name: "data.mtz", Level: "L0", Type: new(ArtifactType("structure_factors"))}},
		Metrics:   &[]CreateMetricRequest{{Id: uuid.New(), Key: "r_free", Value: new(0.21)}},
		Runs: &[]CreateRunRequest{{
			Id:           uuid.New(),
			Name:         "qFit",
			SoftwareName: new("qFit"),
			Artifacts: &[]CreateRunArtifactRequest{
				{ArtifactId: inputID, Direction: "input"},
				{ArtifactId: unknownID, Direction: "output"},
			},
		}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Equal(t, []validation.Pointer{
		"/model/metrics/0/software_name",
		"/model/runs/0/software_version",
	}, pointersOf(fieldErrors))
}

func Test_should_accept_metric_run_id_when_it_names_a_run_in_the_same_request(t *testing.T) {
	// given
	inputID, outputID, runID := uuid.New(), uuid.New(), uuid.New()
	data := CreateModelData{
		Title:    "model",
		Metadata: &map[string]interface{}{"model_type": "Ensemble"},
		Artifacts: &[]CreateArtifactRequest{
			{Id: inputID, Name: "data.mtz", Level: "L0", Type: new(ArtifactType("structure_factors"))},
			{Id: outputID, Name: "model.cif", Level: "L2", Type: new(ArtifactType("model"))},
		},
		Metrics: &[]CreateMetricRequest{{Id: uuid.New(), Key: "r_free", Value: new(0.21), RunId: &runID}},
		Runs: &[]CreateRunRequest{{
			Id:              runID,
			Name:            "refine",
			SoftwareName:    new("PHENIX"),
			SoftwareVersion: new("1.21"),
			Artifacts: &[]CreateRunArtifactRequest{
				{ArtifactId: inputID, Direction: "input"},
				{ArtifactId: outputID, Direction: "output"},
			},
		}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_not_require_metric_software_or_runs_when_created_model_is_deposited(t *testing.T) {
	// given
	data := CreateModelData{
		Title:    "Deposited model",
		Metadata: &map[string]interface{}{"model_type": "Deposited", "purpose": "Reference"},
		Metrics:  &[]CreateMetricRequest{{Id: uuid.New(), Key: "r_free", Value: new(0.21)}},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_metadata_error_when_model_metadata_has_wrong_type(t *testing.T) {
	// given
	data := CreateModelData{
		Title:    "model",
		Metadata: &map[string]interface{}{"model_type": "Deposited", "atom_count": "many"},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	require.Len(t, fieldErrors, 1)
	assert.Equal(t, validation.Pointer("/model/metadata/atom_count"), fieldErrors[0].Pointer)
	assert.Equal(t, validation.CodeInvalidValue, fieldErrors[0].Code)
}

func Test_should_report_other_metadata_errors_when_one_metadata_field_has_wrong_type(t *testing.T) {
	// given
	data := CreateModelData{
		Title:    "model",
		Metadata: &map[string]interface{}{"model_type": "Deposited", "atom_count": "many", "purpose": "Docking"},
	}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	assert.Equal(t, []validation.Pointer{"/model/metadata/atom_count", "/model/metadata/purpose"}, pointersOf(fieldErrors))
	assert.Equal(t, []validation.Code{validation.CodeInvalidValue, validation.CodeInvalidValue}, codesOf(fieldErrors))
}

func Test_should_require_artifact_type_when_request_omits_it(t *testing.T) {
	// given
	request := CreateEntryRequest{
		Entry: CreateEntryData{
			Title:     "Lysozyme",
			Method:    "X-ray crystallography",
			Artifacts: &[]CreateArtifactRequest{{Id: uuid.New(), Name: "sequence.fasta", Level: "L0"}},
		},
	}

	// when
	fieldErrors := createEntryRequestErrors(request)

	// then
	assert.Equal(t, []validation.FieldError{{
		Pointer: "/entry/artifacts/0/type",
		Code:    validation.CodeRequired,
		Detail:  "is required",
	}}, fieldErrors)
}

func Test_should_validate_only_present_fields_when_model_revision_change_is_checked(t *testing.T) {
	// given
	payload := createModelRevisionPayloadForTest(t, `{"model":{"metrics":[{"id":"`+uuid.NewString()+`","key":"cc","value":0.8}]}}`)

	// when
	fieldErrors := modelRevisionChangeErrors(payload)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_null_fields_when_model_revision_change_sets_them_to_null(t *testing.T) {
	// given
	payload := createModelRevisionPayloadForTest(t,
		`{"model":{"title":null,"metadata":null,"artifacts":null,"metrics":null,"runs":null}}`)

	// when
	fieldErrors := modelRevisionChangeErrors(payload)

	// then
	assert.ElementsMatch(t, []validation.FieldError{
		{Pointer: "/model/title", Code: validation.CodeRequired, Detail: "is required"},
		{Pointer: "/model/metadata", Code: validation.CodeRequired, Detail: "must not be null"},
		{Pointer: "/model/artifacts", Code: validation.CodeRequired, Detail: "must not be null"},
		{Pointer: "/model/metrics", Code: validation.CodeRequired, Detail: "must not be null"},
		{Pointer: "/model/runs", Code: validation.CodeRequired, Detail: "must not be null"},
	}, fieldErrors)
}

func Test_should_reject_entry_revision_change_when_resolution_is_negative(t *testing.T) {
	// given
	payload := createEntryRevisionPayloadForTest(t, `{"entry":{"resolution":-1.5,"title":"  "}}`)

	// when
	fieldErrors := entryRevisionChangeErrors(payload)

	// then
	assert.Equal(t, []validation.Pointer{"/entry/title", "/entry/resolution"}, pointersOf(fieldErrors))
}

func Test_should_write_unprocessable_entity_with_pointers_when_validation_fails(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()
	err := fmt.Errorf("create model: %w", &validation.Error{Fields: []validation.FieldError{
		{Pointer: "/model/metrics/0/key", Code: validation.CodeInvalidValue, Detail: "bad key"},
		{Pointer: "/model/title", Code: validation.CodeRequired, Detail: "is required"},
	}})

	// when
	handled := (&Server{}).writeMutationError(t.Context(), recorder, err, "create model")

	// then
	require.True(t, handled)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	var body Error
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Len(t, body.Errors, 2)
	assert.Equal(t, ErrorObject{
		Status: "422",
		Code:   new("INVALID_VALUE"),
		Detail: new("bad key"),
		Source: &ErrorSource{Pointer: "/model/metrics/0/key"},
	}, body.Errors[0])
	assert.Equal(t, "/model/title", body.Errors[1].Source.Pointer)
}

func createModelRevisionPayloadForTest(t *testing.T, body string) createModelRevisionPayload {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	payload, err := decodeCreateModelRevisionPayload(request)
	require.NoError(t, err)
	return payload
}

func createEntryRevisionPayloadForTest(t *testing.T, body string) createEntryRevisionPayload {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	payload, err := decodeCreateEntryRevisionPayload(request)
	require.NoError(t, err)
	return payload
}

func pointersOf(fieldErrors []validation.FieldError) []validation.Pointer {
	pointers := make([]validation.Pointer, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		pointers = append(pointers, fieldError.Pointer)
	}
	return pointers
}

func codesOf(fieldErrors []validation.FieldError) []validation.Code {
	codes := make([]validation.Code, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		codes = append(codes, fieldError.Code)
	}
	return codes
}
