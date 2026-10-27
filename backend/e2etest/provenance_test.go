package e2etest

import (
	"net/http"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/httpapi"
)

// computedModelFixture is an active entry holding one structure-factors file, and an
// active Multiconformer model whose refinement run reads that entry file and writes
// the model's coordinates.
type computedModelFixture struct {
	ownerToken  string
	entryID     string
	modelID     string
	entryFileID uuid.UUID
	modelFileID uuid.UUID
	runID       uuid.UUID
}

func (s *SchemaValidationSuite) createComputedModelFixture(githubID int64, name string) computedModelFixture {
	s.T().Helper()
	fixture := computedModelFixture{
		ownerToken:  issueEntryTokenForGitHubIDForTest(s.T(), name+"-owner", githubID),
		entryFileID: uuid.New(),
		modelFileID: uuid.New(),
		runID:       uuid.New(),
	}
	entry := createAndActivateEntryForTest(s.T(), fixture.ownerToken, map[string]any{
		"entry": map[string]any{
			"title": name + " entry",
			"artifacts": []map[string]any{
				artifactRequest(fixture.entryFileID, "data", "L0", "structure_factors_cif", "s3://entry/sf.cif", nil),
			},
		},
	})
	fixture.entryID = entry.EntryId
	model := createAndActivateModelForTest(s.T(), fixture.ownerToken, fixture.entryID, map[string]any{
		"model": map[string]any{
			"title":     name + " model",
			"metadata":  map[string]any{"model_type": "Multiconformer"},
			"artifacts": []map[string]any{artifactRequest(fixture.modelFileID, "model", "L2", "cif", "s3://model/m.cif", nil)},
			"runs":      []map[string]any{refinementRunForTest(fixture.runID, fixture.entryFileID, fixture.modelFileID)},
		},
	})
	fixture.modelID = model.ModelId
	return fixture
}

func refinementRunForTest(runID, inputID, outputID uuid.UUID) map[string]any {
	return map[string]any{
		"id": runID, "name": "refine", "software_name": "PHENIX", "software_version": "1.21",
		"artifacts": []map[string]any{
			{"artifact_id": inputID, "direction": "input"},
			{"artifact_id": outputID, "direction": "output"},
		},
	}
}

func (s *SchemaValidationSuite) Test_should_accept_run_input_from_entry_file_when_computed_model_is_created() {
	// given
	fixture := s.createComputedModelFixture(8430, "provenance entry input")
	path := "/v1/entries/" + fixture.entryID + "/models/" + fixture.modelID + "/artifacts"

	// when
	artifacts := getJSONPageForTest[httpapi.ModelArtifactCollectionDocument](s.T(), path, "")

	// then
	s.Require().Len(artifacts.Meta.Runs, 1)
	s.Equal(fixture.runID, artifacts.Meta.Runs[0].Id)
	s.ElementsMatch([]httpapi.RunArtifact{
		{RunId: fixture.runID, ArtifactId: fixture.entryFileID, Direction: httpapi.Input},
		{RunId: fixture.runID, ArtifactId: fixture.modelFileID, Direction: httpapi.Output},
	}, artifacts.Meta.Relations)
}

func (s *SchemaValidationSuite) Test_should_reject_run_output_and_write_nothing_when_output_is_an_entry_file() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "provenance-entry-output-owner", 8431)
	entryFileID, modelFileID := uuid.New(), uuid.New()
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"title":     "entry output entry",
			"artifacts": []map[string]any{artifactRequest(entryFileID, "data", "L0", "structure_factors_cif", "s3://e/sf.cif", nil)},
		},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models", map[string]any{
		"model": map[string]any{
			"title":     "model whose run writes an entry file",
			"metadata":  map[string]any{"model_type": "Multiconformer"},
			"artifacts": []map[string]any{artifactRequest(modelFileID, "model", "L2", "cif", "s3://m/m.cif", nil)},
			"runs":      []map[string]any{refinementRunForTest(uuid.New(), modelFileID, entryFileID)},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("/model/runs/0/artifacts/1/artifact_id", errors[0].Source.Pointer)
	s.Equal("must be an artifact of the same model revision", stringValue(errors[0].Detail))
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
}

func (s *SchemaValidationSuite) Test_should_check_runs_against_active_revision_when_revision_change_sets_runs() {
	// given
	fixture := s.createComputedModelFixture(8432, "provenance revision runs")

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(),
		"/v1/entries/"+fixture.entryID+"/models/"+fixture.modelID+"/revisions",
		map[string]any{"model": map[string]any{
			"runs": []map[string]any{{
				"id": uuid.New(), "name": "rebuild", "software_name": "qFit",
				"artifacts": []map[string]any{
					{"artifact_id": fixture.entryFileID, "direction": "input"},
					{"artifact_id": fixture.entryFileID, "direction": "output"},
				},
			}},
		}},
		fixture.ownerToken,
	)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	pointers := make([]string, 0, len(errors))
	for _, fieldError := range errors {
		pointers = append(pointers, fieldError.Source.Pointer)
	}
	s.Equal([]string{"/model/runs/0/software_version", "/model/runs/0/artifacts/1/artifact_id"}, pointers)
}

func (s *SchemaValidationSuite) Test_should_reject_revision_metric_when_it_names_no_software_or_run() {
	// given
	fixture := s.createComputedModelFixture(8433, "provenance metric without software")

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+fixture.entryID+"/models/"+fixture.modelID+"/revisions",
		map[string]any{"model": map[string]any{
			"metrics": []map[string]any{{"id": uuid.New(), "key": "r_free", "value": 0.2}},
		}},
		fixture.ownerToken,
	)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("/model/metrics/0/software_name", errors[0].Source.Pointer)
}

func (s *SchemaValidationSuite) Test_should_accept_revision_metric_when_run_id_names_a_run_it_inherits() {
	// given
	fixture := s.createComputedModelFixture(8436, "provenance inherited run")

	// when
	linked := createAndActivateModelRevisionForTest(s.T(), fixture.ownerToken, fixture.entryID, fixture.modelID,
		map[string]any{"model": map[string]any{
			"metrics": []map[string]any{{"id": uuid.New(), "key": "r_free", "value": 0.2, "run_id": fixture.runID}},
		}},
	)

	// then
	s.NotEqual(uuid.Nil, linked.RevisionId)
	model := getModelForTest(s.T(), fixture.entryID, fixture.modelID)
	s.Require().Len(model.Metrics, 1)
	s.Equal(&fixture.runID, model.Metrics[0].RunId)
}

func (s *SchemaValidationSuite) Test_should_require_runs_when_revision_change_turns_deposited_model_computed() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "provenance-type-change-owner", 8437)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "type change entry"},
	})
	model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "archive model", "metadata": map[string]any{"model_type": "Deposited"}},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models/"+model.ModelId+"/revisions",
		map[string]any{"model": map[string]any{"metadata": map[string]any{"model_type": "Multiconformer"}}},
		ownerToken,
	)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("/model/runs", errors[0].Source.Pointer)
}

func (s *SchemaValidationSuite) Test_should_reject_run_replacement_when_kept_metric_names_a_dropped_run() {
	// given
	fixture := s.createComputedModelFixture(8438, "provenance dropped run")
	createAndActivateModelRevisionForTest(s.T(), fixture.ownerToken, fixture.entryID, fixture.modelID,
		map[string]any{"model": map[string]any{
			"metrics": []map[string]any{{"id": uuid.New(), "key": "r_free", "value": 0.2, "run_id": fixture.runID}},
		}},
	)

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+fixture.entryID+"/models/"+fixture.modelID+"/revisions",
		map[string]any{"model": map[string]any{
			"runs": []map[string]any{refinementRunForTest(uuid.New(), fixture.entryFileID, fixture.modelFileID)},
		}},
		fixture.ownerToken,
	)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("/model/runs", errors[0].Source.Pointer)
	s.Contains(stringValue(errors[0].Detail), "the kept r_free metric names as its producer")
}

func (s *SchemaValidationSuite) Test_should_not_check_inherited_runs_when_revision_change_sets_only_title() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "provenance-old-model-owner", 8434)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "old model entry"},
	})
	modelFileID := uuid.New()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries/"+entry.EntryId+"/models",
		map[string]any{"model": map[string]any{
			"title":     "model stored before the run rules",
			"metadata":  map[string]any{"model_type": "Multiconformer"},
			"artifacts": []map[string]any{artifactRequest(modelFileID, "model", "L2", "cif", "s3://old/m.cif", nil)},
			"runs": []map[string]any{{
				"id": uuid.New(), "name": "qFit",
				"artifacts": []map[string]any{{"artifact_id": modelFileID, "direction": "output"}},
			}},
		}},
		ownerToken,
	)
	created := decodeJSONResponse[httpapi.CreateModelRevisionDocument](s.T(), response, http.StatusCreated)
	activateModelRevisionForTest(s.T(), created.Data.Attributes)

	// when
	renamed := createModelRevisionForTest(s.T(), ownerToken, entry.EntryId, created.Data.Attributes.ModelId,
		map[string]any{"model": map[string]any{"title": "renamed old model"}},
	)

	// then
	s.NotEqual(uuid.Nil, renamed.RevisionId)
}

func (s *SchemaValidationSuite) Test_should_reject_metric_run_id_with_400_when_mode_is_log_only_and_run_does_not_exist() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "provenance-log-only-run-id-owner", 8435)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "log-only metric run id entry"},
	})

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries/"+entry.EntryId+"/models",
		map[string]any{"model": map[string]any{
			"title":    "model whose metric names a missing run",
			"metadata": map[string]any{"model_type": "Deposited"},
			"metrics":  []map[string]any{{"id": uuid.New(), "key": "r_free", "value": 0.2, "run_id": uuid.New()}},
		}},
		ownerToken,
	)

	// then
	s.Equal(http.StatusBadRequest, response.StatusCode)
	assertErrorResponse(s.T(), response, "BAD_REQUEST", "metric run_id must name a run of the same model revision")
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
}
