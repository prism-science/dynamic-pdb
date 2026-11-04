package httpapi

import (
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
