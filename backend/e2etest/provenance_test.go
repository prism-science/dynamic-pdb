package e2etest

import (
	"net/http"
	"strings"
	"testing"

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

func createComputedModelFixtureForTest(t *testing.T, githubID int64, name string) computedModelFixture {
	t.Helper()
	fixture := computedModelFixture{
		ownerToken:  issueEntryTokenForGitHubIDForTest(t, name+"-owner", githubID),
		entryFileID: uuid.New(),
		modelFileID: uuid.New(),
		runID:       uuid.New(),
	}
	entry := createAndActivateEntryForTest(t, fixture.ownerToken, map[string]any{
		"entry": map[string]any{
			"title": name + " entry",
			"artifacts": []map[string]any{
				artifactRequest(fixture.entryFileID, "data", "L0", "structure_factors_cif", "s3://entry/sf.cif", nil),
			},
		},
	})
	fixture.entryID = entry.EntryId
	model := createAndActivateModelForTest(t, fixture.ownerToken, fixture.entryID, map[string]any{
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
	fixture := createComputedModelFixtureForTest(s.T(), 8430, "provenance entry input")
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
	fixture := createComputedModelFixtureForTest(s.T(), 8432, "provenance revision runs")

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
	fixture := createComputedModelFixtureForTest(s.T(), 8433, "provenance metric without software")

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
	fixture := createComputedModelFixtureForTest(s.T(), 8436, "provenance inherited run")

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
	fixture := createComputedModelFixtureForTest(s.T(), 8438, "provenance dropped run")
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

func (s *SchemaValidationSuite) Test_should_record_score_read_from_the_model_file_as_the_files_when_value_sources_names_it() {
	// given: a computed model whose R-work its own coordinate file states
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "value-sources-owner", 8440)
	entryFileID, modelFileID, runID := uuid.New(), uuid.New(), uuid.New()
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"title":     "value sources entry",
			"artifacts": []map[string]any{artifactRequest(entryFileID, "data", "L0", "structure_factors_cif", "s3://e/sf.cif", nil)},
		},
	})
	modelFile := artifactRequest(modelFileID, "model", "L2", "cif", "s3://m/m.cif", nil)
	modelFile["sha256"] = "3b2a8f0c9d1e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90"

	// when
	created := createModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title":               "model with file scores",
			"metadata":            map[string]any{"model_type": "Multiconformer"},
			"primary_artifact_id": modelFileID,
			"artifacts":           []map[string]any{modelFile},
			"runs":                []map[string]any{refinementRunForTest(runID, entryFileID, modelFileID)},
			"metrics": []map[string]any{
				{"id": uuid.New(), "key": "r_work", "value": 0.19, "run_id": runID},
				{"id": uuid.New(), "key": "r_free", "value": 0.23, "run_id": runID},
			},
			"value_sources": map[string]any{"/metrics/r_work": map[string]any{"artifact_id": modelFileID}},
		},
	})

	// then
	path := "/v1/entries/" + entry.EntryId + "/models/" + created.ModelId + "/revisions/" + created.RevisionId.String()
	revision := getModelRevisionForTest(s.T(), path, adminToken)
	s.Require().NotNil(revision.FieldSources)
	s.Equal(httpapi.FieldSources{
		"/metrics/r_work": {
			Source:     httpapi.FieldSourceSourceFile,
			ArtifactId: &modelFileID,
			Sha256:     new("3b2a8f0c9d1e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f90"),
		},
		"/metrics/r_free": {Source: httpapi.FieldSourceSourceDepositor},
	}, *revision.FieldSources)
}

func (s *SchemaValidationSuite) Test_should_reject_score_source_and_write_nothing_when_it_names_a_run_input() {
	// given: the archive's mmCIF reaches the deposit as an input of the refinement
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "value-sources-run-input-owner", 8441)
	entryFileID, archiveFileID, modelFileID, runID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"title":     "value sources run input entry",
			"artifacts": []map[string]any{artifactRequest(entryFileID, "data", "L0", "structure_factors_cif", "s3://e/sf.cif", nil)},
		},
	})
	modelFile := artifactRequest(modelFileID, "model", "L2", "cif", "s3://m/m.cif", nil)
	modelFile["sha256"] = strings.Repeat("a", 64)
	archiveFile := artifactRequest(archiveFileID, "1abc.cif", "L2", "cif", "s3://m/1abc.cif", nil)
	archiveFile["sha256"] = strings.Repeat("b", 64)
	run := refinementRunForTest(runID, entryFileID, modelFileID)
	run["artifacts"] = append(run["artifacts"].([]map[string]any), map[string]any{"artifact_id": archiveFileID, "direction": "input"})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models", map[string]any{
		"model": map[string]any{
			"title":               "model claiming an archive score",
			"metadata":            map[string]any{"model_type": "Multiconformer"},
			"primary_artifact_id": modelFileID,
			"artifacts":           []map[string]any{modelFile, archiveFile},
			"runs":                []map[string]any{run},
			"metrics":             []map[string]any{{"id": uuid.New(), "key": "r_work", "value": 0.19, "run_id": runID}},
			"value_sources":       map[string]any{"/metrics/r_work": map[string]any{"artifact_id": archiveFileID}},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("/model/value_sources/~1metrics~1r_work/artifact_id", errors[0].Source.Pointer)
	s.Contains(stringValue(errors[0].Detail), "must be the request's primary artifact")
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
}
