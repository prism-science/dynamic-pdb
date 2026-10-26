package e2etest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
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

func (s *SchemaValidationSuite) Test_should_reject_metric_when_key_not_in_schema() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-metric-key-owner", 8401)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "metric key entry"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models", map[string]any{
		"model": map[string]any{
			"title":   "model with unknown metric",
			"metrics": []map[string]any{{"id": uuid.New(), "key": "resolution", "value": 2.1}},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("422", errors[0].Status)
	s.Equal("INVALID_VALUE", stringValue(errors[0].Code))
	s.Require().NotNil(errors[0].Source)
	s.Equal("/model/metrics/0/key", errors[0].Source.Pointer)
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
}

func (s *SchemaValidationSuite) Test_should_reject_metric_when_value_missing() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-metric-value-owner", 8402)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "metric value entry"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models", map[string]any{
		"model": map[string]any{
			"title":   "model with metric missing its value",
			"metrics": []map[string]any{{"id": uuid.New(), "key": "r_free"}},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("REQUIRED", stringValue(errors[0].Code))
	s.Equal("/model/metrics/0/value", errors[0].Source.Pointer)
}

func (s *SchemaValidationSuite) Test_should_accept_model_when_type_deposited_and_purpose_reference() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-deposited-owner", 8403)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "deposited model entry", "resolution": 1.8},
	})
	metrics := make([]map[string]any, 0, 10)
	for index, key := range []string{
		"r_work", "r_free", "cc", "rscc", "clashscore", "ramachandran_outliers",
		"side_chain_outliers", "molprobity_score", "wilson_b", "rmsf",
	} {
		metrics = append(metrics, map[string]any{"id": uuid.New(), "key": key, "value": float64(index) / 10})
	}

	// when
	created := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title": "deposited model",
			"metadata": map[string]any{
				"purpose": "Reference", "model_type": "Deposited", "altloc_fraction": 0.25, "atom_count": 2512,
			},
			"metrics": metrics,
		},
	})

	// then
	model := getModelForTest(s.T(), entry.EntryId, created.ModelId)
	s.Equal("Reference", model.Metadata["purpose"])
	s.Equal("Deposited", model.Metadata["model_type"])
	s.InDelta(0.25, model.Metadata["altloc_fraction"], 1e-9)
	s.InDelta(2512, model.Metadata["atom_count"], 1e-9)
	s.Require().Len(model.Metrics, 10)
	wilsonB := metricByKey(model.Metrics, "wilson_b")
	s.Require().NotNil(wilsonB)
	s.InDelta(0.8, wilsonB.Value, 1e-9)
}

func (s *SchemaValidationSuite) Test_should_return_every_field_error_and_write_nothing_when_entry_request_is_invalid() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-no-partial-write-owner", 8404)
	ownerID := tokenUserIDForTest(s.T(), ownerToken)

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{"title": "entry that must not be saved", "method": "X-ray crystallography", "resolution": -2},
		"model_operations": []map[string]any{{
			"op": "add",
			"data": map[string]any{
				"title":    "model that must not be saved",
				"metadata": map[string]any{"purpose": "Docking"},
				"metrics":  []map[string]any{{"id": uuid.New(), "key": "r_free", "value": 0.2}},
				"runs": []map[string]any{{
					"id": uuid.New(), "name": "refine",
					"artifacts": []map[string]any{{"artifact_id": uuid.New(), "direction": "sideways"}},
				}},
			},
		}},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	pointers := make([]string, 0, len(errors))
	for _, item := range errors {
		s.Require().NotNil(item.Source)
		pointers = append(pointers, item.Source.Pointer)
	}
	s.Equal([]string{
		"/entry/resolution",
		"/model_operations/0/data/metadata/purpose",
		"/model_operations/0/data/runs/0/artifacts/0/direction",
	}, pointers)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	revisionsResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", ownerID), ownerToken,
	)
	s.Empty(decodeEntryRevisionGroupsForTest(s.T(), revisionsResponse))
}

func (s *SchemaValidationSuite) Test_should_accept_unknown_metric_key_when_mode_is_log_only() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-log-only-owner", 8406)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "log-only entry"},
	})

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries/"+entry.EntryId+"/models",
		map[string]any{"model": map[string]any{
			"title":   "model accepted in log-only mode",
			"metrics": []map[string]any{{"id": uuid.New(), "key": "resolution", "value": 2.1}},
		}},
		ownerToken,
	)

	// then
	created := decodeJSONResponse[httpapi.CreateModelRevisionDocument](s.T(), response, http.StatusCreated)
	s.NotEqual(uuid.Nil, created.Data.Attributes.RevisionId)
}

func (s *SchemaValidationSuite) Test_should_return_409_when_mode_is_log_only_and_model_repeats_a_metric_key() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-log-only-duplicate-metric-owner", 8409)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "log-only duplicate metric entry"},
	})

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries/"+entry.EntryId+"/models",
		map[string]any{"model": map[string]any{
			"title": "model with a repeated metric key",
			"metrics": []map[string]any{
				{"id": uuid.New(), "key": "r_free", "value": 0.2},
				{"id": uuid.New(), "key": "r_free", "value": 0.3},
			},
		}},
		ownerToken,
	)

	// then
	s.Equal(http.StatusConflict, response.StatusCode)
	assertErrorResponse(s.T(), response, "CONFLICT", "a model revision can have only one metric per key")
}

func (s *SchemaValidationSuite) Test_should_reject_unknown_run_artifact_direction_with_400_when_mode_is_log_only() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-log-only-direction-owner", 8410)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "log-only direction entry"},
	})
	artifactID := uuid.New()

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries/"+entry.EntryId+"/models",
		map[string]any{"model": map[string]any{
			"title":     "model with an unknown run direction",
			"artifacts": []map[string]any{artifactRequest(artifactID, "model", "L2", "cif", "s3://model/direction.cif", nil)},
			"runs": []map[string]any{{
				"id": uuid.New(), "name": "refine",
				"artifacts": []map[string]any{{"artifact_id": artifactID, "direction": "sideways"}},
			}},
		}},
		ownerToken,
	)

	// then
	s.Equal(http.StatusBadRequest, response.StatusCode)
	assertErrorResponse(s.T(), response, "BAD_REQUEST", "run artifact direction")
}

func (s *SchemaValidationSuite) Test_should_reject_missing_metric_value_with_400_when_mode_is_log_only() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-log-only-value-owner", 8407)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "log-only missing value entry"},
	})

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries/"+entry.EntryId+"/models",
		map[string]any{"model": map[string]any{
			"title":   "model with metric missing its value",
			"metrics": []map[string]any{{"id": uuid.New(), "key": "r_free"}},
		}},
		ownerToken,
	)

	// then
	s.Equal(http.StatusBadRequest, response.StatusCode)
	assertErrorResponse(s.T(), response, "BAD_REQUEST", "metric value is required")
}

func (s *SchemaValidationSuite) Test_should_reject_artifact_and_write_nothing_when_type_missing() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-artifact-type-owner", 8408)
	ownerID := tokenUserIDForTest(s.T(), ownerToken)
	artifact := artifactRequest(uuid.New(), "sequence.fasta", "L0", "fasta", "s3://entry/sequence.fasta", nil)
	delete(artifact, "type")

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{
			"title": "entry with untyped artifact", "method": "X-ray crystallography", "artifacts": []map[string]any{artifact},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("REQUIRED", stringValue(errors[0].Code))
	s.Require().NotNil(errors[0].Source)
	s.Equal("/entry/artifacts/0/type", errors[0].Source.Pointer)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	revisionsResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", ownerID), ownerToken,
	)
	s.Empty(decodeEntryRevisionGroupsForTest(s.T(), revisionsResponse))
}

func (s *SchemaValidationSuite) Test_should_reject_artifact_when_format_not_lowercase() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-artifact-format-owner", 8409)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "artifact format entry"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models", map[string]any{
		"model": map[string]any{
			"title": "model with uppercase format",
			"artifacts": []map[string]any{
				artifactRequest(uuid.New(), "model.cif", "L2", "MMCIF", "s3://model/model.cif", nil),
			},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("INVALID_FORMAT", stringValue(errors[0].Code))
	s.Require().NotNil(errors[0].Source)
	s.Equal("/model/artifacts/0/format", errors[0].Source.Pointer)
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
}

func (s *SchemaValidationSuite) Test_should_store_type_other_when_artifact_type_missing_and_mode_is_log_only() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-log-only-artifact-owner", 8410)
	artifactID := uuid.New()
	artifact := artifactRequest(artifactID, "sequence.fasta", "L0", "fasta", "s3://entry/sequence.fasta", nil)
	delete(artifact, "type")

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries",
		map[string]any{"entry": map[string]any{
			"title":     "log-only untyped artifact entry",
			"artifacts": []map[string]any{artifact},
		}},
		ownerToken,
	)

	// then
	created := decodeJSONResponse[httpapi.CreateEntryRevisionDocument](s.T(), response, http.StatusCreated)
	activateEntryRevisionForTest(s.T(), created.Data.Attributes)
	page := getJSONPageForTest[httpapi.ArtifactCollectionDocument](
		s.T(), "/v1/entries/"+created.Data.Attributes.EntryId+"/artifacts", "",
	)
	stored := artifactByID(artifactsFromData(page.Data), artifactID)
	s.Require().NotNil(stored)
	s.Equal(httpapi.ArtifactTypeOther, stored.Type)
}

func (s *SchemaValidationSuite) Test_should_report_every_model_metadata_error_when_one_field_has_wrong_type() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-metadata-type-owner", 8411)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "metadata type entry"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models", map[string]any{
		"model": map[string]any{
			"title":    "model with mistyped metadata",
			"metadata": map[string]any{"atom_count": "many", "purpose": "Docking"},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	pointers := make([]string, 0, len(errors))
	for _, item := range errors {
		s.Require().NotNil(item.Source)
		pointers = append(pointers, item.Source.Pointer)
	}
	s.Equal([]string{"/model/metadata/atom_count", "/model/metadata/purpose"}, pointers)
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
}

func decodeErrorResponseForTest(t *testing.T, response *http.Response, expectedStatus int) []httpapi.ErrorObject {
	t.Helper()
	defer func() {
		require.NoError(t, response.Body.Close())
	}()
	require.Equal(t, expectedStatus, response.StatusCode)
	var body httpapi.Error
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	return body.Errors
}

func (s *SchemaValidationSuite) Test_should_return_every_typed_metadata_error_and_write_nothing_when_metadata_is_invalid() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-typed-metadata-owner", 8421)
	ownerID := tokenUserIDForTest(s.T(), ownerToken)
	artifact := artifactRequest(uuid.New(), "model.cif", "L2", "cif", "s3://entry/model.cif", map[string]any{"colour": "blue"})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{
			"title":            "entry that must not be saved",
			"polymer_entities": []map[string]any{{"label_entity_id": "1", "mutations": []string{"C54T", "nope"}}},
		},
		"model_operations": []map[string]any{{
			"op": "add",
			"data": map[string]any{
				"title":     "model that must not be saved",
				"metadata":  map[string]any{"lab_note": "x", "__reserved": 1, "extensions": map[string]any{"__deep": 1}},
				"artifacts": []map[string]any{artifact},
				"runs":      []map[string]any{{"id": uuid.New(), "name": "refine", "metadata": map[string]any{"notes": "x"}}},
			},
		}},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	pointers := make([]string, 0, len(errors))
	for _, item := range errors {
		s.Require().NotNil(item.Source)
		pointers = append(pointers, item.Source.Pointer)
	}
	s.ElementsMatch([]string{
		"/entry/method",
		"/entry/polymer_entities/0/mutations/1",
		"/model_operations/0/data/metadata/lab_note",
		"/model_operations/0/data/metadata/__reserved",
		"/model_operations/0/data/metadata/extensions/__deep",
		"/model_operations/0/data/artifacts/0/metadata/colour",
		"/model_operations/0/data/runs/0/metadata/notes",
	}, pointers)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	revisionsResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", ownerID), ownerToken,
	)
	s.Empty(decodeEntryRevisionGroupsForTest(s.T(), revisionsResponse))
}

func (s *SchemaValidationSuite) Test_should_round_trip_extensions_and_distinct_empty_lists_when_model_metadata_is_stored() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "schema-round-trip-owner", 8422)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "round trip entry", "method": "Neutron diffraction"},
	})

	// when
	created := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title": "round trip model",
			"metadata": map[string]any{
				"ligands":    []string{},
				"cofactors":  nil,
				"extensions": map[string]any{"lab": map[string]any{"id": "x"}},
			},
		},
	})

	// then
	model := getModelForTest(s.T(), entry.EntryId, created.ModelId)
	s.Equal([]any{}, model.Metadata["ligands"])
	s.Contains(model.Metadata, "cofactors")
	s.Nil(model.Metadata["cofactors"])
	s.Equal(map[string]any{"lab": map[string]any{"id": "x"}}, model.Metadata["extensions"])
}
