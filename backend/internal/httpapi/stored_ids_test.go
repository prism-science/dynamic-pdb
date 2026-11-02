package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

func storedArtifactForTest(id uuid.UUID) domainmodels.Artifact {
	return domainmodels.Artifact{
		ID: id, Name: "model", Level: domainmodels.ArtifactLevel("L2"), Type: domainmodels.ArtifactTypeOther,
		URI: new("s3://model/m.cif"), Format: new("cif"), SizeBytes: new(int64(120)),
		Metadata: map[string]any{"extensions": map[string]any{"b": 2.0, "a": "x"}},
	}
}

func artifactRequestForTest(id uuid.UUID) CreateArtifactRequest {
	metadata := ArtifactMetadata{"extensions": map[string]any{"a": "x", "b": 2.0}}
	return CreateArtifactRequest{
		Id: id, Name: " model ", Level: ArtifactLevel("L2"),
		Uri: new("s3://model/m.cif"), Format: new("cif"), SizeBytes: new(int64(120)), Metadata: &metadata,
	}
}

func Test_should_keep_stored_artifact_when_request_echoes_it_without_type(t *testing.T) {
	// given
	id := uuid.New()

	// when
	err := checkStoredArtifactUnchanged(storedArtifactForTest(id), artifactRequestForTest(id))

	// then
	require.NoError(t, err)
}

func Test_should_refuse_stored_artifact_with_409_error_when_request_changes_its_uri(t *testing.T) {
	// given
	id := uuid.New()
	request := artifactRequestForTest(id)
	request.Uri = new("s3://model/other.cif")

	// when
	err := checkStoredArtifactUnchanged(storedArtifactForTest(id), request)

	// then
	require.ErrorIs(t, err, errIDInUse)
	assert.Contains(t, err.Error(), "stored artifacts cannot change")
}

func Test_should_keep_stored_run_when_request_lists_its_artifact_links_in_another_order(t *testing.T) {
	// given
	runID, inputID, outputID := uuid.New(), uuid.New(), uuid.New()
	startedAt := time.Date(2026, 10, 30, 12, 0, 0, 123456000, time.UTC)
	stored := validation.ModelRun{
		Run: domainmodels.Run{
			ID: runID, Name: "refine", SoftwareName: new("PHENIX"), StartedAt: &startedAt,
			Parameters: map[string]any{"cycles": 5.0},
		},
		Artifacts: []validation.RunArtifactLink{
			{ArtifactID: inputID, Direction: domainmodels.RunArtifactDirectionInput},
			{ArtifactID: outputID, Direction: domainmodels.RunArtifactDirectionOutput},
		},
	}
	requestStartedAt := startedAt.Add(789 * time.Nanosecond)
	request := CreateRunRequest{
		Id: runID, Name: "refine", SoftwareName: new("PHENIX"), StartedAt: &requestStartedAt,
		Parameters: &map[string]interface{}{"cycles": 5.0},
		Artifacts: &[]CreateRunArtifactRequest{
			{ArtifactId: outputID, Direction: RunArtifactDirection("output")},
			{ArtifactId: inputID, Direction: RunArtifactDirection("input")},
		},
	}

	// when
	err := checkStoredRunUnchanged(stored, request)

	// then
	require.NoError(t, err)
}

func Test_should_refuse_stored_run_with_409_error_when_request_drops_an_artifact_link(t *testing.T) {
	// given
	runID, inputID := uuid.New(), uuid.New()
	stored := validation.ModelRun{
		Run: domainmodels.Run{ID: runID, Name: "refine"},
		Artifacts: []validation.RunArtifactLink{
			{ArtifactID: inputID, Direction: domainmodels.RunArtifactDirectionInput},
		},
	}

	// when
	err := checkStoredRunUnchanged(stored, CreateRunRequest{Id: runID, Name: "refine"})

	// then
	require.ErrorIs(t, err, errIDInUse)
	assert.Contains(t, err.Error(), "stored runs cannot change")
}

func Test_should_refuse_stored_metric_with_400_error_when_request_has_no_value(t *testing.T) {
	// given
	id := uuid.New()
	stored := domainmodels.Metric{ID: id, Key: domainmodels.MetricKey("r_free")}

	// when
	err := checkStoredMetricUnchanged(stored, CreateMetricRequest{Id: id, Key: MetricKey("r_free")})

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_refuse_stored_metric_with_409_error_when_request_changes_its_run(t *testing.T) {
	// given
	id, runID := uuid.New(), uuid.New()
	stored := domainmodels.Metric{ID: id, Key: domainmodels.MetricKey("r_free"), Value: 0.2, RunID: &runID}
	request := CreateMetricRequest{Id: id, Key: MetricKey("r_free"), Value: new(0.2), RunId: new(uuid.New())}

	// when
	err := checkStoredMetricUnchanged(stored, request)

	// then
	require.ErrorIs(t, err, errIDInUse)
	assert.Contains(t, err.Error(), "stored metrics cannot change")
}

func Test_should_treat_missing_and_empty_json_objects_as_the_same(t *testing.T) {
	// given
	var missing map[string]any

	// when
	same, err := sameJSONObject(missing, map[string]any{})

	// then
	require.NoError(t, err)
	assert.True(t, same)
}
