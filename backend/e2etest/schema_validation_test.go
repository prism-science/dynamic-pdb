package e2etest

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

type SchemaValidationSuite struct {
	baseSuite
}

func TestSchemaValidation(t *testing.T) {
	suite.Run(t, new(SchemaValidationSuite))
}

var schemaMetricValues = map[string]float64{
	"r_work": 0.18, "r_free": 0.21, "cc": 0.84, "rscc": 0.95, "clashscore": 0,
	"ramachandran_outliers": 0.2, "side_chain_outliers": 1.1, "molprobity_score": 1.42,
	"wilson_b": 23.5, "rmsf": 0.6,
}

func schemaModelRequest(title string, coordinatesID, structureFactorsID uuid.UUID, metrics []map[string]any) map[string]any {
	return map[string]any{
		"title": title,
		"metadata": map[string]any{
			"external_refs": map[string]any{"pdb": "1ABC", "ext": "lab-42"},
			"purpose":       "Reference",
			"model_type":    "Deposited",
		},
		"primary_artifact_id": coordinatesID,
		"artifacts": []map[string]any{
			{
				"id": coordinatesID, "name": "model.cif", "level": "L2", "type": "model", "format": "cif",
				"uri": "s3://model/model.cif", "sha256": strings.Repeat("ab", 32), "size_bytes": 2048,
				"metadata": map[string]any{"chain_count": float64(2)},
			},
			{
				"id": structureFactorsID, "name": "data.mtz", "level": "L1", "type": "structure_factors",
				"format": "mtz", "uri": "s3://model/data.mtz", "size_bytes": 0,
			},
		},
		"runs": []map[string]any{{
			"id": uuid.New(), "name": "refinement",
			"artifacts": []map[string]any{
				{"artifact_id": structureFactorsID, "direction": "input"},
				{"artifact_id": coordinatesID, "direction": "output"},
			},
		}},
		"metrics": metrics,
	}
}

func schemaMetricsRequest() []map[string]any {
	metrics := make([]map[string]any, 0, len(schemaMetricValues))
	for key, value := range schemaMetricValues {
		metrics = append(metrics, map[string]any{"id": uuid.New(), "key": key, "value": value})
	}
	return metrics
}

func (s *SchemaValidationSuite) Test_should_round_trip_record_unchanged_when_record_matches_schema() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-round-trip-owner", 8401)
	coordinatesID := uuid.New()
	structureFactorsID := uuid.New()
	request := map[string]any{
		"entry": map[string]any{"title": "schema round trip", "method": "SOLUTION NMR", "resolution": 1.85},
		"model_operations": []map[string]any{{
			"op":   "add",
			"data": schemaModelRequest("deposited model", coordinatesID, structureFactorsID, schemaMetricsRequest()),
		}},
	}

	// when
	created := createAndActivateEntryForTest(s.T(), ownerToken, request)
	s.Require().Len(created.ModelResults, 1)
	activateModelRevisionForTest(s.T(), httpapi.CreateModelRevisionAttributes{
		EntryId: created.EntryId, ModelId: created.ModelResults[0].ModelId, RevisionId: created.ModelResults[0].ModelRevisionId,
	})

	// then
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	entryResponse := getWithToken(s.T(), "/v1/entries/"+created.EntryId, "")
	entry := decodeJSONResponse[httpapi.EntryDocument](s.T(), entryResponse, http.StatusOK).Data.Attributes
	s.Equal("schema round trip", stringValue(entry.Title))
	s.Equal("SOLUTION NMR", stringValue(entry.Method))
	s.Require().NotNil(entry.Resolution)
	s.InDelta(1.85, *entry.Resolution, 0)

	model := getModelForTest(s.T(), created.EntryId, created.ModelResults[0].ModelId)
	s.Equal("Reference", model.Metadata["purpose"])
	s.Equal("Deposited", model.Metadata["model_type"])
	s.Equal(map[string]any{"pdb": "1ABC", "ext": "lab-42"}, model.Metadata["external_refs"])
	storedMetrics := make(map[string]float64, len(model.Metrics))
	for _, metric := range model.Metrics {
		storedMetrics[metric.Key] = metric.Value
	}
	s.Equal(schemaMetricValues, storedMetrics)

	//nolint:bodyclose // decodeJSONResponse closes the response body.
	artifactsResponse := getWithToken(s.T(), fmt.Sprintf(
		"/v1/entries/%s/models/%s/artifacts", created.EntryId, created.ModelResults[0].ModelId,
	), "")
	artifacts := decodeModelArtifactCollectionForTest(s.T(), artifactsResponse)
	coordinates := artifactByID(artifacts.Items, coordinatesID)
	s.Require().NotNil(coordinates)
	s.Equal("model.cif", coordinates.Name)
	s.Equal(httpapi.ArtifactLevel("L2"), coordinates.Level)
	s.Equal(httpapi.ArtifactTypeModel, coordinates.Type)
	s.Equal("cif", stringValue(coordinates.Format))
	s.Equal(strings.Repeat("ab", 32), stringValue(coordinates.Sha256))
	s.Require().NotNil(coordinates.SizeBytes)
	s.Equal(int64(2048), *coordinates.SizeBytes)
	s.Equal(map[string]any{"chain_count": float64(2)}, coordinates.Metadata)
	structureFactors := artifactByID(artifacts.Items, structureFactorsID)
	s.Require().NotNil(structureFactors)
	s.Require().NotNil(structureFactors.SizeBytes)
	s.Equal(int64(0), *structureFactors.SizeBytes)
}

func (s *SchemaValidationSuite) Test_should_accept_model_when_type_deposited_and_purpose_reference() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-deposited-owner", 8402)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "deposited reference entry", "method": testEntryMethod},
	})
	model := schemaModelRequest("deposited reference model", uuid.New(), uuid.New(), nil)

	// when
	created := createModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{"model": model})

	// then
	s.Equal(httpapi.RevisionStateInReview, created.State)
}

func (s *SchemaValidationSuite) Test_should_write_no_rows_when_create_entry_violates_schema() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-no-rows-owner", 8403)
	title := "schema no rows " + uuid.NewString()
	coordinatesID := uuid.New()
	structureFactorsID := uuid.New()
	metrics := []map[string]any{
		{"id": uuid.New(), "key": "r_free", "value": 0.21},
		{"id": uuid.New(), "key": "r_factor", "value": 0.2},
		{"id": uuid.New(), "key": "rmsf"},
	}
	model := schemaModelRequest(title, coordinatesID, structureFactorsID, metrics)
	request := map[string]any{
		"entry":            map[string]any{"title": title, "method": testEntryMethod},
		"model_operations": []map[string]any{{"op": "add", "data": model}},
	}

	// when
	//nolint:bodyclose // errorPointersForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", request, ownerToken)

	// then
	s.Require().Equal(http.StatusUnprocessableEntity, response.StatusCode)
	s.Equal([]string{
		"/model_operations/0/data/metrics/1/key",
		"/model_operations/0/data/metrics/2/value",
	}, errorPointersForTest(s.T(), response))
	s.Equal(map[string]int{
		"entry_revisions": 0, "model_revisions": 0, "artifacts": 0, "metrics": 0, "runs": 0,
	}, schemaRowCounts(s.T(), title, []uuid.UUID{coordinatesID, structureFactorsID}, metrics, model))
}

func schemaRowCounts(
	t *testing.T,
	title string,
	artifactIDs []uuid.UUID,
	metrics []map[string]any,
	model map[string]any,
) map[string]int {
	t.Helper()
	connection, err := sql.Open("postgres", "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password sslmode=disable")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, connection.Close())
	}()
	metricIDs := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		metricIDs = append(metricIDs, metric["id"].(uuid.UUID).String())
	}
	runIDs := make([]string, 0)
	for _, run := range model["runs"].([]map[string]any) {
		runIDs = append(runIDs, run["id"].(uuid.UUID).String())
	}
	artifactIDStrings := make([]string, 0, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		artifactIDStrings = append(artifactIDStrings, artifactID.String())
	}
	queries := map[string]struct {
		query    string
		argument any
	}{
		"entry_revisions": {"select count(*) from entry_revisions where title = $1", title},
		"model_revisions": {"select count(*) from model_revisions where title = $1", title},
		"artifacts":       {"select count(*) from artifacts where id::text = any($1)", pq.Array(artifactIDStrings)},
		"metrics":         {"select count(*) from metrics where id::text = any($1)", pq.Array(metricIDs)},
		"runs":            {"select count(*) from runs where id::text = any($1)", pq.Array(runIDs)},
	}
	counts := make(map[string]int, len(queries))
	for table, query := range queries {
		var count int
		require.NoError(t, connection.QueryRow(query.query, query.argument).Scan(&count))
		counts[table] = count
	}
	return counts
}
