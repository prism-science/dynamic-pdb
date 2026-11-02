package e2etest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

// StoredIDsSuite covers model revisions that send back the artifacts, runs and metrics the
// active revision already has, as a client does when it keeps some of them.
type StoredIDsSuite struct {
	baseSuite
}

func TestStoredIDs(t *testing.T) {
	suite.Run(t, new(StoredIDsSuite))
}

func (s *StoredIDsSuite) Test_should_keep_stored_artifacts_runs_and_metrics_when_revision_sends_them_back_unchanged() {
	// given
	fixture := createComputedModelFixtureForTest(s.T(), 8701, "stored ids kept")
	metricID := s.addRefinementMetric(fixture)
	stored := s.storedModelRequest(fixture)
	newFileID := uuid.New()
	stored.artifacts = append(stored.artifacts,
		artifactRequest(newFileID, "notes", "L2", "txt", "s3://model/notes.txt", nil))

	// when
	created := createAndActivateModelRevisionForTest(s.T(), fixture.ownerToken, fixture.entryID, fixture.modelID,
		map[string]any{"model": map[string]any{
			"artifacts": stored.artifacts,
			"runs":      stored.runs,
			"metrics":   stored.metrics,
		}},
	)

	// then
	s.Equal(fixture.modelID, created.ModelId)
	contents := s.modelContents(fixture)
	artifactIDs := make([]uuid.UUID, 0, len(contents.Items))
	for _, artifact := range contents.Items {
		artifactIDs = append(artifactIDs, artifact.Id)
	}
	s.ElementsMatch([]uuid.UUID{fixture.modelFileID, newFileID}, artifactIDs)
	s.Require().Len(contents.Runs, 1)
	s.Equal(fixture.runID, contents.Runs[0].Id)
	s.ElementsMatch(
		[]string{fixture.entryFileID.String() + " input", fixture.modelFileID.String() + " output"},
		runLinksForTest(contents.Relations, fixture.runID),
	)
	metrics := getModelForTest(s.T(), fixture.entryID, fixture.modelID).Metrics
	s.Require().Len(metrics, 1)
	s.Equal(metricID, metrics[0].Id)
	s.Require().NotNil(metrics[0].RunId)
	s.Equal(fixture.runID, *metrics[0].RunId)
}

func (s *StoredIDsSuite) Test_should_return_409_when_revision_sends_stored_artifact_id_with_other_values() {
	// given
	fixture := createComputedModelFixtureForTest(s.T(), 8702, "stored ids artifact changed")
	stored := s.storedModelRequest(fixture)
	stored.artifacts[0]["name"] = "renamed model file"

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := postJSONWithToken(s.T(), modelRevisionsPathForTest(fixture),
		map[string]any{"model": map[string]any{"artifacts": stored.artifacts}}, fixture.ownerToken)

	// then
	s.Equal(http.StatusConflict, response.StatusCode)
	assertErrorResponse(s.T(), response, "CONFLICT", "stored artifacts cannot change")
}

func (s *StoredIDsSuite) Test_should_return_409_when_revision_sends_stored_run_id_with_other_values() {
	// given
	fixture := createComputedModelFixtureForTest(s.T(), 8703, "stored ids run changed")
	stored := s.storedModelRequest(fixture)
	stored.runs[0]["name"] = "refine again"

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := postJSONWithToken(s.T(), modelRevisionsPathForTest(fixture),
		map[string]any{"model": map[string]any{"runs": stored.runs}}, fixture.ownerToken)

	// then
	s.Equal(http.StatusConflict, response.StatusCode)
	assertErrorResponse(s.T(), response, "CONFLICT", "stored runs cannot change")
}

func (s *StoredIDsSuite) Test_should_return_409_when_revision_sends_stored_metric_id_with_other_values() {
	// given
	fixture := createComputedModelFixtureForTest(s.T(), 8704, "stored ids metric changed")
	s.addRefinementMetric(fixture)
	stored := s.storedModelRequest(fixture)
	stored.metrics[0]["value"] = 0.25

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := postJSONWithToken(s.T(), modelRevisionsPathForTest(fixture),
		map[string]any{"model": map[string]any{"metrics": stored.metrics}}, fixture.ownerToken)

	// then
	s.Equal(http.StatusConflict, response.StatusCode)
	assertErrorResponse(s.T(), response, "CONFLICT", "stored metrics cannot change")
}

func (s *StoredIDsSuite) Test_should_return_409_when_revision_sends_id_of_artifact_the_model_does_not_have() {
	// given
	fixture := createComputedModelFixtureForTest(s.T(), 8705, "stored ids foreign artifact")
	stored := s.storedModelRequest(fixture)
	stored.artifacts = append(stored.artifacts,
		artifactRequest(fixture.entryFileID, "entry file as model file", "L0", "txt", "s3://model/other.txt", nil))

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := postJSONWithToken(s.T(), modelRevisionsPathForTest(fixture),
		map[string]any{"model": map[string]any{"artifacts": stored.artifacts}}, fixture.ownerToken)

	// then
	s.Equal(http.StatusConflict, response.StatusCode)
	assertErrorResponse(s.T(), response, "CONFLICT", "is already stored")
}

func (s *StoredIDsSuite) Test_should_return_409_when_new_model_sends_id_of_stored_artifact() {
	// given
	fixture := createComputedModelFixtureForTest(s.T(), 8706, "stored ids new model")

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+fixture.entryID+"/models", map[string]any{
		"model": map[string]any{
			"title":     "second model",
			"metadata":  map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{artifactRequest(fixture.modelFileID, "model", "L2", "cif", "s3://model/m.cif", nil)},
		},
	}, fixture.ownerToken)

	// then
	s.Equal(http.StatusConflict, response.StatusCode)
	assertErrorResponse(s.T(), response, "CONFLICT", "is already stored")
}

// storedModelRequestForTest is the active revision's artifacts, runs and metrics as a client
// sends them back: the values the API returned, with their ids.
type storedModelRequestForTest struct {
	artifacts []map[string]any
	runs      []map[string]any
	metrics   []map[string]any
}

func (s *StoredIDsSuite) storedModelRequest(fixture computedModelFixture) storedModelRequestForTest {
	s.T().Helper()
	contents := s.modelContents(fixture)
	var request storedModelRequestForTest
	for _, artifact := range contents.Items {
		request.artifacts = append(request.artifacts, map[string]any{
			"id": artifact.Id, "name": artifact.Name, "level": artifact.Level, "type": artifact.Type,
			"uri": artifact.Uri, "sha256": artifact.Sha256, "format": artifact.Format,
			"size_bytes": artifact.SizeBytes, "metadata": artifact.Metadata,
		})
	}
	for _, run := range contents.Runs {
		links := make([]map[string]any, 0)
		for _, relation := range contents.Relations {
			if relation.RunId == run.Id {
				links = append(links, map[string]any{"artifact_id": relation.ArtifactId, "direction": relation.Direction})
			}
		}
		request.runs = append(request.runs, map[string]any{
			"id": run.Id, "name": run.Name, "software_name": run.SoftwareName,
			"software_version": run.SoftwareVersion, "command": run.Command, "parameters": run.Parameters,
			"metadata": run.Metadata, "started_at": run.StartedAt, "finished_at": run.FinishedAt,
			"artifacts": links,
		})
	}
	for _, metric := range getModelForTest(s.T(), fixture.entryID, fixture.modelID).Metrics {
		request.metrics = append(request.metrics, map[string]any{
			"id": metric.Id, "key": metric.Key, "value": metric.Value, "software_name": metric.SoftwareName,
			"software_version": metric.SoftwareVersion, "run_id": metric.RunId,
		})
	}
	return request
}

func (s *StoredIDsSuite) modelContents(fixture computedModelFixture) modelArtifactCollectionForTest {
	s.T().Helper()
	//nolint:bodyclose // decodeModelArtifactCollectionForTest closes the response body.
	response := getWithToken(s.T(),
		fmt.Sprintf("/v1/entries/%s/models/%s/artifacts", fixture.entryID, fixture.modelID), "")
	return decodeModelArtifactCollectionForTest(s.T(), response)
}

// addRefinementMetric activates a revision that adds an r_free metric produced by the
// fixture's run, and returns the metric's id.
func (s *StoredIDsSuite) addRefinementMetric(fixture computedModelFixture) uuid.UUID {
	s.T().Helper()
	metricID := uuid.New()
	createAndActivateModelRevisionForTest(s.T(), fixture.ownerToken, fixture.entryID, fixture.modelID,
		map[string]any{"model": map[string]any{
			"metrics": []map[string]any{{"id": metricID, "key": "r_free", "value": 0.2, "run_id": fixture.runID}},
		}},
	)
	return metricID
}

func modelRevisionsPathForTest(fixture computedModelFixture) string {
	return "/v1/entries/" + fixture.entryID + "/models/" + fixture.modelID + "/revisions"
}

// runLinksForTest lists a run's artifact links as "<artifact id> <direction>".
func runLinksForTest(relations []httpapi.RunArtifact, runID uuid.UUID) []string {
	links := make([]string, 0)
	for _, relation := range relations {
		if relation.RunId == runID {
			links = append(links, relation.ArtifactId.String()+" "+string(relation.Direction))
		}
	}
	return links
}
