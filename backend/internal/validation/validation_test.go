package validation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

func validArtifact() validation.Artifact {
	return validation.Artifact{
		ID:        uuid.New(),
		Name:      "model.cif",
		Level:     models.ArtifactLevelL1,
		Type:      models.ArtifactTypeModel,
		Format:    new("cif"),
		SizeBytes: new(int64(10)),
		SHA256:    new("4f2a5a4c3d9b2f9e3c0d7a8e1b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c"),
		Metadata:  map[string]any{"source": "upload"},
	}
}

func validModel() validation.Model {
	artifact := validArtifact()
	purpose := models.ModelPurposeRefinement
	modelType := models.StructureModelTypeSingleConformer
	return validation.Model{
		Title: "model",
		Metadata: models.ModelMetadata{
			ExternalRefs: map[models.ModelSource]string{models.ModelSourcePDB: "1ABC"},
			Purpose:      &purpose,
			ModelType:    &modelType,
		},
		Artifacts: []validation.Artifact{artifact},
		Runs: []validation.Run{{
			ID:   uuid.New(),
			Name: "refine",
			Artifacts: []validation.RunArtifact{
				{ArtifactID: artifact.ID, Direction: models.RunArtifactDirectionOutput},
			},
		}},
		Metrics: []validation.Metric{
			{ID: uuid.New(), Key: models.MetricKeyRFree, Value: new(0.21)},
			{ID: uuid.New(), Key: models.MetricKeyRWork, Value: new(0.18)},
		},
	}
}

func validCreateEntry() validation.CreateEntry {
	fasta := validArtifact()
	fasta.Type = models.ArtifactTypeFASTA
	fasta.Format = new("fasta")
	return validation.CreateEntry{
		Entry: validation.Entry{
			Title:      "entry",
			Method:     new("X-ray crystallography"),
			Resolution: new(1.8),
			Artifacts:  []validation.Artifact{fasta},
		},
		Models: []validation.Model{validModel()},
	}
}

func pointers(fieldErrors []validation.FieldError) []string {
	result := make([]string, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		result = append(result, fieldError.Pointer)
	}
	return result
}

func requireSingleError(t *testing.T, fieldErrors []validation.FieldError, pointer string, code validation.Code) {
	t.Helper()
	require.Len(t, fieldErrors, 1, "errors: %v", fieldErrors)
	assert.Equal(t, pointer, fieldErrors[0].Pointer)
	assert.Equal(t, code, fieldErrors[0].Code)
	assert.NotEmpty(t, fieldErrors[0].Detail)
}

func Test_should_accept_entry_when_record_is_valid(t *testing.T) {
	// given
	request := validCreateEntry()

	// when
	fieldErrors := validation.ValidateCreateEntry(request)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_entry_when_title_missing(t *testing.T) {
	// given
	request := validCreateEntry()
	request.Entry.Title = "  "

	// when
	fieldErrors := validation.ValidateCreateEntry(request)

	// then
	requireSingleError(t, fieldErrors, "/entry/title", validation.CodeRequired)
}

func Test_should_reject_entry_when_method_missing(t *testing.T) {
	// given
	request := validCreateEntry()
	request.Entry.Method = nil

	// when
	fieldErrors := validation.ValidateCreateEntry(request)

	// then
	requireSingleError(t, fieldErrors, "/entry/method", validation.CodeRequired)
}

func Test_should_accept_entry_when_method_is_free_text(t *testing.T) {
	// given
	request := validCreateEntry()
	request.Entry.Method = new("SOLUTION NMR")

	// when
	fieldErrors := validation.ValidateCreateEntry(request)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_entry_when_resolution_not_positive(t *testing.T) {
	// given
	request := validCreateEntry()
	request.Entry.Resolution = new(0.0)

	// when
	fieldErrors := validation.ValidateCreateEntry(request)

	// then
	requireSingleError(t, fieldErrors, "/entry/resolution", validation.CodeOutOfRange)
}

func Test_should_reject_metric_when_key_not_in_schema(t *testing.T) {
	// given
	request := validCreateEntry()
	request.Models[0].Metrics[1].Key = "r_factor"

	// when
	fieldErrors := validation.ValidateCreateEntry(request)

	// then
	requireSingleError(t, fieldErrors, "/model_operations/0/data/metrics/1/key", validation.CodeNotInSchema)
}

func Test_should_reject_metric_when_value_missing(t *testing.T) {
	// given
	model := validModel()
	model.Metrics[0].Value = nil

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/metrics/0/value", validation.CodeRequired)
}

func Test_should_accept_metric_when_value_is_zero(t *testing.T) {
	// given
	model := validModel()
	model.Metrics[0].Value = new(0.0)

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_accept_metric_when_key_is_any_schema_key(t *testing.T) {
	keys := []models.MetricKey{
		models.MetricKeyRWork, models.MetricKeyRFree, models.MetricKeyCC, models.MetricKeyRSCC,
		models.MetricKeyClashscore, models.MetricKeyRamachandranOutliers, models.MetricKeySideChainOutliers,
		models.MetricKeyMolProbityScore, models.MetricKeyWilsonB, models.MetricKeyRMSF,
	}
	for _, key := range keys {
		t.Run(string(key), func(t *testing.T) {
			// given
			metric := validation.Metric{ID: uuid.New(), Key: key, Value: new(1.0)}

			// when
			fieldErrors := validation.ValidateMetric("/metric", metric)

			// then
			assert.Empty(t, fieldErrors)
		})
	}
}

func Test_should_reject_metric_when_key_duplicated_on_model_revision(t *testing.T) {
	// given
	model := validModel()
	model.Metrics[1].Key = models.MetricKeyRFree

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/metrics/1/key", validation.CodeDuplicate)
}

func Test_should_accept_model_when_type_deposited_and_purpose_reference(t *testing.T) {
	// given
	model := validModel()
	purpose := models.ModelPurposeReference
	modelType := models.StructureModelTypeDeposited
	model.Metadata.Purpose = &purpose
	model.Metadata.ModelType = &modelType

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_model_when_title_missing(t *testing.T) {
	// given
	model := validModel()
	model.Title = ""

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/title", validation.CodeRequired)
}

func Test_should_reject_model_when_purpose_not_in_schema(t *testing.T) {
	// given
	model := validModel()
	purpose := models.ModelPurpose("Validation")
	model.Metadata.Purpose = &purpose

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/metadata/purpose", validation.CodeNotInSchema)
}

func Test_should_reject_model_when_type_not_in_schema(t *testing.T) {
	// given
	model := validModel()
	modelType := models.StructureModelType("multiconformer")
	model.Metadata.ModelType = &modelType

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/metadata/model_type", validation.CodeNotInSchema)
}

func Test_should_reject_model_when_external_ref_source_not_in_schema(t *testing.T) {
	// given
	model := validModel()
	model.Metadata.ExternalRefs["emdb"] = "EMD-1234"

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/metadata/external_refs/emdb", validation.CodeNotInSchema)
}

func Test_should_reject_artifact_when_field_violates_schema(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*validation.Artifact)
		pointer string
		code    validation.Code
	}{
		{"id missing", func(a *validation.Artifact) { a.ID = uuid.Nil }, "/artifact/id", validation.CodeRequired},
		{"name missing", func(a *validation.Artifact) { a.Name = " " }, "/artifact/name", validation.CodeRequired},
		{"level missing", func(a *validation.Artifact) { a.Level = "" }, "/artifact/level", validation.CodeRequired},
		{"level not in schema", func(a *validation.Artifact) { a.Level = "L4" }, "/artifact/level", validation.CodeNotInSchema},
		{"type missing", func(a *validation.Artifact) { a.Type = "" }, "/artifact/type", validation.CodeRequired},
		{"type not in schema", func(a *validation.Artifact) { a.Type = "assembly" }, "/artifact/type", validation.CodeNotInSchema},
		{"format not lowercase", func(a *validation.Artifact) { a.Format = new("CIF") }, "/artifact/format", validation.CodeInvalidFormat},
		{"size negative", func(a *validation.Artifact) { a.SizeBytes = new(int64(-1)) }, "/artifact/size_bytes", validation.CodeOutOfRange},
		{"sha256 uppercase", func(a *validation.Artifact) {
			a.SHA256 = new("4F2A5A4C3D9B2F9E3C0D7A8E1B6C5D4E3F2A1B0C9D8E7F6A5B4C3D2E1F0A9B8C")
		}, "/artifact/sha256", validation.CodeInvalidFormat},
		{"sha256 short", func(a *validation.Artifact) { a.SHA256 = new("abc") }, "/artifact/sha256", validation.CodeInvalidFormat},
		{"reserved metadata key", func(a *validation.Artifact) { a.Metadata["__checksum"] = "x" }, "/artifact/metadata/__checksum", validation.CodeReservedKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			artifact := validArtifact()
			test.mutate(&artifact)

			// when
			fieldErrors := validation.ValidateArtifact("/artifact", artifact)

			// then
			requireSingleError(t, fieldErrors, test.pointer, test.code)
		})
	}
}

func Test_should_accept_artifact_when_format_is_not_a_known_format(t *testing.T) {
	// given
	artifact := validArtifact()
	artifact.Format = new("mrc")
	artifact.SizeBytes = new(int64(0))
	artifact.SHA256 = nil

	// when
	fieldErrors := validation.ValidateArtifact("/artifact", artifact)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_escape_pointer_when_metadata_key_contains_slash_or_tilde(t *testing.T) {
	// given
	artifact := validArtifact()
	artifact.Metadata["__a/b~c"] = true

	// when
	fieldErrors := validation.ValidateArtifact("/artifact", artifact)

	// then
	requireSingleError(t, fieldErrors, "/artifact/metadata/__a~1b~0c", validation.CodeReservedKey)
}

func Test_should_reject_run_when_direction_not_in_schema(t *testing.T) {
	// given
	model := validModel()
	model.Runs[0].Artifacts[0].Direction = "inout"

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/runs/0/artifacts/0/direction", validation.CodeNotInSchema)
}

func Test_should_reject_run_when_artifact_belongs_to_another_model_revision(t *testing.T) {
	// given
	model := validModel()
	model.Runs[0].Artifacts[0].ArtifactID = uuid.New()

	// when
	fieldErrors := validation.ValidateCreateModel(model)

	// then
	requireSingleError(t, fieldErrors, "/model/runs/0/artifacts/0/artifact_id", validation.CodeUnknownReference)
}

func Test_should_skip_run_artifact_membership_when_change_keeps_model_artifacts(t *testing.T) {
	// given
	runs := []validation.Run{{
		ID:        uuid.New(),
		Name:      "refine",
		Artifacts: []validation.RunArtifact{{ArtifactID: uuid.New(), Direction: models.RunArtifactDirectionInput}},
	}}
	change := validation.ModelChange{Runs: validation.Present(&runs)}

	// when
	fieldErrors := validation.ValidateModelRevisionChange(change)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_run_artifact_when_change_replaces_model_artifacts_without_it(t *testing.T) {
	// given
	artifacts := []validation.Artifact{validArtifact()}
	runs := []validation.Run{{
		ID:        uuid.New(),
		Name:      "refine",
		Artifacts: []validation.RunArtifact{{ArtifactID: uuid.New(), Direction: models.RunArtifactDirectionInput}},
	}}
	change := validation.ModelChange{Artifacts: validation.Present(&artifacts), Runs: validation.Present(&runs)}

	// when
	fieldErrors := validation.ValidateModelRevisionChange(change)

	// then
	requireSingleError(t, fieldErrors, "/model/runs/0/artifacts/0/artifact_id", validation.CodeUnknownReference)
}

func Test_should_reject_uniprot_mapping_when_release_missing_for_source(t *testing.T) {
	for _, source := range []models.UniProtReferenceSource{
		models.UniProtReferenceSourceSIFTS,
		models.UniProtReferenceSourceAlignment,
	} {
		t.Run(string(source), func(t *testing.T) {
			// given
			mapping := models.PolymerEntityUniProtReference{Accession: "P69905", Source: source}

			// when
			fieldErrors := validation.ValidateUniProtMapping("/mapping", mapping)

			// then
			requireSingleError(t, fieldErrors, "/mapping/unp_release", validation.CodeRequired)
		})
	}
}

func Test_should_accept_uniprot_mapping_when_struct_ref_has_no_release(t *testing.T) {
	// given
	mapping := models.PolymerEntityUniProtReference{Accession: "P69905", Source: models.UniProtReferenceSourceStructRef}

	// when
	fieldErrors := validation.ValidateUniProtMapping("/mapping", mapping)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_uniprot_mapping_when_source_not_in_schema(t *testing.T) {
	// given
	mapping := models.PolymerEntityUniProtReference{Accession: "P69905", Source: "rcsb", UniProtRelease: new("2024_01")}

	// when
	fieldErrors := validation.ValidateUniProtMapping("/mapping", mapping)

	// then
	requireSingleError(t, fieldErrors, "/mapping/source", validation.CodeNotInSchema)
}

func Test_should_collect_all_errors_when_record_has_several_violations(t *testing.T) {
	// given
	request := validCreateEntry()
	request.Entry.Title = ""
	request.Entry.Method = nil
	request.Models[0].Metrics[0].Key = "r_factor"
	request.Models[0].Metrics[1].Value = nil
	request.Models[0].Artifacts[0].Format = new("CIF")

	// when
	fieldErrors := validation.ValidateCreateEntry(request)

	// then
	assert.Equal(t, []string{
		"/entry/title",
		"/entry/method",
		"/model_operations/0/data/artifacts/0/format",
		"/model_operations/0/data/metrics/0/key",
		"/model_operations/0/data/metrics/1/value",
	}, pointers(fieldErrors))
}

func Test_should_validate_only_present_fields_when_entry_revision_changes(t *testing.T) {
	// given
	change := validation.EntryChange{Resolution: validation.Present(new(2.1))}

	// when
	fieldErrors := validation.ValidateEntryRevisionChange(change)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_entry_revision_change_when_title_or_method_is_cleared(t *testing.T) {
	// given
	change := validation.EntryChange{
		Title:      validation.Present[string](nil),
		Method:     validation.Present(new(" ")),
		Resolution: validation.Present(new(-1.0)),
	}

	// when
	fieldErrors := validation.ValidateEntryRevisionChange(change)

	// then
	assert.Equal(t, []string{"/entry/title", "/entry/method", "/entry/resolution"}, pointers(fieldErrors))
}

func Test_should_reject_model_revision_change_when_metric_violates_schema(t *testing.T) {
	// given
	metrics := []validation.Metric{{ID: uuid.New(), Key: "bogus", Value: new(1.0)}}
	change := validation.ModelChange{Metrics: validation.Present(&metrics)}

	// when
	fieldErrors := validation.ValidateModelRevisionChange(change)

	// then
	requireSingleError(t, fieldErrors, "/model/metrics/0/key", validation.CodeNotInSchema)
}

func Test_should_return_error_with_all_violations_when_mode_is_enforce(t *testing.T) {
	// given
	fieldErrors := []validation.FieldError{{Pointer: "/entry/title", Code: validation.CodeRequired, Detail: "entry title is required"}}

	// when
	err := validation.ModeEnforce.Apply(context.Background(), slog.Default(), fieldErrors)

	// then
	var validationErr *validation.Error
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, fieldErrors, validationErr.FieldErrors)
}

func Test_should_log_every_violation_and_allow_write_when_mode_is_log_only(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	fieldErrors := []validation.FieldError{
		{Pointer: "/entry/title", Code: validation.CodeRequired, Detail: "entry title is required"},
		{Pointer: "/entry/method", Code: validation.CodeRequired, Detail: "entry method is required"},
	}

	// when
	err := validation.ModeLogOnly.Apply(context.Background(), logger, fieldErrors, "path", "/v1/entries")

	// then
	require.NoError(t, err)
	decoder := json.NewDecoder(&logs)
	for _, fieldError := range fieldErrors {
		var entry map[string]any
		require.NoError(t, decoder.Decode(&entry))
		assert.Equal(t, fieldError.Pointer, entry["pointer"])
		assert.Equal(t, string(fieldError.Code), entry["code"])
		assert.Equal(t, "/v1/entries", entry["path"])
	}
	assert.False(t, decoder.More())
}

func Test_should_reject_validation_mode_when_unknown(t *testing.T) {
	// given
	mode := validation.Mode("warn")

	// when
	err := mode.Validate()

	// then
	require.EqualError(t, err, "unknown validation mode: warn")
}
