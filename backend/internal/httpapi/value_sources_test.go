package httpapi

import (
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

func Test_should_record_value_read_from_the_model_file_with_its_artifact_and_sha256(t *testing.T) {
	// given
	data := modelDataWithFile(t, "mmcif", new("abc123"))
	data.ValueSources = &ValueSources{
		"/metrics/r_work":   {ArtifactId: *data.PrimaryArtifactId},
		"/metadata/authors": {ArtifactId: *data.PrimaryArtifactId},
	}

	// when
	sources, fieldErrors := newModelRevisionSources(validation.Root.Field("model"), data, domainmodels.ModelMetadata{
		Authors: []string{"Doe, J."},
	})

	// then
	require.Empty(t, fieldErrors)
	artifactID := *data.PrimaryArtifactId
	fileSource := domainmodels.FieldSource{Source: domainmodels.FieldSourceFile, ArtifactID: &artifactID, SHA256: new("abc123")}
	assert.Equal(t, domainmodels.FieldSources{
		domainmodels.ArchiveMetricField(domainmodels.MetricKeyRWork): fileSource,
		domainmodels.ModelFieldAuthors:                               fileSource,
		domainmodels.ArchiveMetricField(domainmodels.MetricKeyRFree): {Source: domainmodels.FieldSourceDepositor},
	}, sources)
}

func Test_should_keep_values_the_depositors_when_value_sources_cannot_be_recorded(t *testing.T) {
	// given
	runInput := uuid.New()
	cases := map[string]struct {
		data   func(t *testing.T) CreateModelData
		detail string
	}{
		"unknown field": {
			data: func(t *testing.T) CreateModelData {
				data := modelDataWithFile(t, "mmcif", new("abc123"))
				data.ValueSources = &ValueSources{"/metrics/rscc": {ArtifactId: *data.PrimaryArtifactId}}
				return data
			},
			detail: "is not a field whose source is recorded",
		},
		"field not set": {
			data: func(t *testing.T) CreateModelData {
				data := modelDataWithFile(t, "mmcif", new("abc123"))
				data.ValueSources = &ValueSources{"/metadata/authors": {ArtifactId: *data.PrimaryArtifactId}}
				return data
			},
			detail: "names a field this request does not set",
		},
		"run input instead of the model file": {
			data: func(t *testing.T) CreateModelData {
				data := modelDataWithFile(t, "mmcif", new("abc123"))
				*data.Artifacts = append(*data.Artifacts, CreateArtifactRequest{Id: runInput, Name: "1abc.cif", Format: new("mmcif"), Sha256: new("def456")})
				data.ValueSources = &ValueSources{"/metrics/r_work": {ArtifactId: runInput}}
				return data
			},
			detail: "must be the request's primary artifact: a model's values are read only from its own coordinate file",
		},
		"primary artifact not a coordinate file": {
			data: func(t *testing.T) CreateModelData {
				data := modelDataWithFile(t, "mtz", new("abc123"))
				data.ValueSources = &ValueSources{"/metrics/r_work": {ArtifactId: *data.PrimaryArtifactId}}
				return data
			},
			detail: "the primary artifact is not a PDB or mmCIF coordinate file",
		},
		"primary artifact without sha256": {
			data: func(t *testing.T) CreateModelData {
				data := modelDataWithFile(t, "pdb", nil)
				data.ValueSources = &ValueSources{"/metrics/r_work": {ArtifactId: *data.PrimaryArtifactId}}
				return data
			},
			detail: "the primary artifact has no sha256",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			data := testCase.data(t)
			key := sortedKeys(*data.ValueSources)[0]

			// when
			sources, fieldErrors := newModelRevisionSources(validation.Root.Field("model"), data, domainmodels.ModelMetadata{})

			// then
			require.Len(t, fieldErrors, 1)
			assert.Equal(t, validation.Root.Field("model").Field("value_sources").Field(key).Field("artifact_id"), fieldErrors[0].Pointer)
			assert.Equal(t, validation.CodeInvalidValue, fieldErrors[0].Code)
			assert.Equal(t, testCase.detail, fieldErrors[0].Detail)
			for pointer, source := range sources {
				assert.Equal(t, domainmodels.FieldSourceDepositor, source.Source, pointer)
			}
		})
	}
}

func Test_should_report_value_sources_with_the_other_model_request_errors(t *testing.T) {
	// given
	data := modelDataWithFile(t, "mmcif", new("abc123"))
	data.ValueSources = &ValueSources{"/metrics/r_work": {ArtifactId: uuid.New()}}

	// when
	fieldErrors := createModelRequestErrors(validation.Root.Field("model"), data)

	// then
	pointers := make([]validation.Pointer, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		pointers = append(pointers, fieldError.Pointer)
	}
	assert.Contains(t, pointers, validation.Pointer("/model/value_sources/~1metrics~1r_work/artifact_id"))
}

// modelDataWithFile is a model create request with one coordinate file as its primary
// artifact, and R-work and R-free scores computed by named software.
func modelDataWithFile(t *testing.T, format string, sha256 *string) CreateModelData {
	t.Helper()
	fileID := uuid.New()
	return CreateModelData{
		Title:             "Refined model",
		PrimaryArtifactId: &fileID,
		Artifacts: &[]CreateArtifactRequest{{
			Id: fileID, Name: "model." + format, Level: L2, Format: new(format), Sha256: sha256,
		}},
		Metrics: &[]CreateMetricRequest{
			{Id: uuid.New(), Key: MetricKeyRWork, Value: new(0.19), SoftwareName: new("phenix.refine")},
			{Id: uuid.New(), Key: MetricKeyRFree, Value: new(0.23), SoftwareName: new("phenix.refine")},
		},
	}
}

func Test_should_answer_404_when_upload_extraction_is_turned_off(t *testing.T) {
	// given
	server := &Server{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/files/extract", strings.NewReader(`{}`))

	// when
	server.ExtractUploadedFile(recorder, request)

	// then
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"code":"EXTRACTION_OFF"`)
}

func Test_should_record_entry_value_read_from_one_of_its_structure_files(t *testing.T) {
	// given
	fileID := uuid.New()
	data := CreateEntryData{
		Title: "Entry",
		Artifacts: &[]CreateArtifactRequest{
			{Id: fileID, Name: "model.cif", Level: L0, Format: new("cif"), Sha256: new("abc123")},
		},
		ValueSources: &ValueSources{
			"/metadata/ligands": {ArtifactId: fileID},
			"/metadata/method":  {ArtifactId: uuid.New()},
		},
	}
	sources := domainmodels.FieldSources{
		domainmodels.EntryFieldLigands: {Source: domainmodels.FieldSourceDepositor},
		domainmodels.EntryFieldMethod:  {Source: domainmodels.FieldSourceDepositor},
	}

	// when
	result, fieldErrors := newEntryRevisionSources(validation.Root.Field("entry"), data, sources)

	// then
	require.Len(t, fieldErrors, 1)
	assert.Equal(t, validation.Pointer("/entry/value_sources/~1metadata~1method/artifact_id"), fieldErrors[0].Pointer)
	assert.Equal(t, "the artifact is not among this request's artifacts", fieldErrors[0].Detail)
	assert.Equal(t, domainmodels.FieldSources{
		domainmodels.EntryFieldLigands: {Source: domainmodels.FieldSourceFile, ArtifactID: &fileID, SHA256: new("abc123")},
		domainmodels.EntryFieldMethod:  {Source: domainmodels.FieldSourceDepositor},
	}, result)
}

func Test_should_keep_archive_source_when_file_value_equals_the_archives(t *testing.T) {
	// given
	fileID := uuid.New()
	archive := domainmodels.FieldSource{Source: domainmodels.FieldSourceRCSB, Accession: new("1ABC")}
	sources := domainmodels.FieldSources{domainmodels.EntryFieldResolution: archive}
	fileSources := domainmodels.FieldSources{
		domainmodels.EntryFieldResolution: {Source: domainmodels.FieldSourceFile, ArtifactID: &fileID},
	}

	// when
	merged := withFileSources(sources, fileSources)

	// then
	assert.Equal(t, archive, merged[domainmodels.EntryFieldResolution])
}
