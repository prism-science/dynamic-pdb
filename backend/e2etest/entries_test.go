package e2etest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/integrations/github"
)

type EntriesSuite struct {
	baseSuite
}

func TestEntries(t *testing.T) {
	suite.Run(t, new(EntriesSuite))
}

func (s *EntriesSuite) Test_should_publish_entry_and_model_through_independent_revisions() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "independent-revision-owner", 8101)
	ownerID := tokenUserIDForTest(s.T(), ownerToken)
	entryArtifactID := uuid.New()
	modelArtifactID := uuid.New()
	metricID := uuid.New()
	runID := uuid.New()

	// when
	entry := createEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"name": "independent entry",
			"artifacts": []map[string]any{
				artifactRequest(entryArtifactID, "entry FASTA", "L0", "fasta", "s3://entry/sequence.fasta", map[string]any{
					"records": []map[string]any{{"header": "entry", "sequence": "MACDEFGHIK"}},
				}),
			},
		},
		"model_operations": []map[string]any{{
			"op": "add",
			"data": map[string]any{
				"name": "independent model", "primary_artifact_id": modelArtifactID,
				"artifacts": []map[string]any{
					artifactRequest(modelArtifactID, "model coordinates", "L2", "cif", "s3://model/model.cif", nil),
				},
				"metrics": []map[string]any{{"id": metricID, "key": "r_free", "value": 0.21}},
				"runs": []map[string]any{{
					"id": runID, "name": "refinement",
					"artifacts": []map[string]any{
						{"artifact_id": entryArtifactID, "direction": "input"},
						{"artifact_id": modelArtifactID, "direction": "output"},
					},
				}},
			},
		}},
	})

	// then
	s.Regexp(`^dpdb_[0-9a-z]{8}$`, entry.EntryId)
	s.Require().Len(entry.ModelResults, 1)
	createdModel := entry.ModelResults[0]
	s.Equal(httpapi.ModelOperationResultOpAdd, createdModel.Op)
	s.Equal(entry.EntryId+"_m_001", createdModel.ModelId)
	entryID := entry.EntryId
	modelID := createdModel.ModelId
	activateEntryRevisionForTest(s.T(), entry)
	publicModelBeforeActivation := getWithToken(s.T(), fmt.Sprintf("/v1/entries/%s/models/%s", entryID, modelID), "")
	s.Equal(http.StatusNotFound, publicModelBeforeActivation.StatusCode)
	s.Require().NoError(publicModelBeforeActivation.Body.Close())

	userModelPath := fmt.Sprintf(
		"/v1/users/%s/entries/%s/models/%s/revisions/%s",
		ownerID, entryID, modelID, createdModel.ModelRevisionId,
	)
	submitted := getModelRevisionForTest(s.T(), userModelPath, ownerToken)
	s.Equal(httpapi.RevisionStateInReview, submitted.State)
	s.Equal(httpapi.ModelStateActive, submitted.ModelState)

	adminModelPath := fmt.Sprintf(
		"/v1/entries/%s/models/%s/revisions/%s",
		entryID, modelID, createdModel.ModelRevisionId,
	)
	active := updateModelRevisionStateForTest(s.T(), adminModelPath, adminToken, "active")
	s.Equal(httpapi.RevisionStateActive, active.State)
	s.Equal(httpapi.ModelStateActive, active.ModelState)

	publicEntry := getEntryForTest(s.T(), entryID)
	s.Equal(ownerID, publicEntry.CreatedBy)
	s.Require().Len(publicEntry.ProteinSequences, 1)
	s.Equal(entryArtifactID, publicEntry.ProteinSequences[0].SourceArtifactId)
	model := getModelForTest(s.T(), entryID, modelID)
	s.Equal("independent model", model.Name)
	s.Require().NotNil(metricByKey(model.Metrics, "r_free"))

	//nolint:bodyclose // decodeJSONResponse closes the response body.
	artifactsResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/entries/%s/models/%s/artifacts", entryID, modelID), "",
	)
	artifacts := decodeModelArtifactCollectionForTest(s.T(), artifactsResponse)
	s.Require().NotNil(artifactByID(artifacts.Items, modelArtifactID))
	s.Require().NotNil(runByID(artifacts.Runs, runID))
	s.True(runArtifactLinkExists(artifacts.Relations, runID, entryArtifactID, httpapi.Input))
	s.True(runArtifactLinkExists(artifacts.Relations, runID, modelArtifactID, httpapi.Output))
}

func (s *EntriesSuite) Test_should_redirect_public_files_from_the_backend() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "files-owner", 8113)
	entryArtifact := artifactRequest(
		uuid.New(), "entry FASTA", "L0", "fasta", "https://files.example/entry.fasta", nil,
	)
	entryArtifact["type"] = "fasta"
	modelArtifactID := uuid.New()
	modelArtifact := artifactRequest(
		modelArtifactID, "model coordinates", "L2", "cif", "https://files.example/model.cif", nil,
	)
	modelArtifact["type"] = "model"
	structureFactorsArtifact := artifactRequest(
		uuid.New(), "structure factors", "L2", "cif", "https://files.example/sf.cif", nil,
	)
	structureFactorsArtifact["type"] = "structure_factors"
	entry := createEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"name":      "files entry",
			"artifacts": []map[string]any{entryArtifact},
		},
		"model_operations": []map[string]any{{
			"op": "add",
			"data": map[string]any{
				"name":                "files model",
				"primary_artifact_id": modelArtifactID,
				"artifacts": []map[string]any{
					modelArtifact,
					structureFactorsArtifact,
				},
			},
		}},
	})
	activateEntryRevisionForTest(s.T(), entry)
	s.Require().Len(entry.ModelResults, 1)
	model := entry.ModelResults[0]
	modelPath := fmt.Sprintf(
		"/v1/entries/%s/models/%s/revisions/%s",
		entry.EntryId, model.ModelId, model.ModelRevisionId,
	)
	updateModelRevisionStateForTest(s.T(), modelPath, adminToken, "active")

	// when
	files := []struct {
		method   string
		path     string
		location string
	}{
		{method: http.MethodGet, path: fmt.Sprintf("/v1/files/%s.fasta", entry.EntryId), location: "https://files.example/entry.fasta"},
		{method: http.MethodGet, path: fmt.Sprintf("/v1/files/%s/%s/%s.cif", entry.EntryId, model.ModelId, entry.EntryId), location: "https://files.example/model.cif"},
		{method: http.MethodHead, path: fmt.Sprintf("/v1/files/%s/%s/%s-sf.cif", entry.EntryId, model.ModelId, entry.EntryId), location: "https://files.example/sf.cif"},
	}
	responses := make([]*http.Response, 0, len(files))
	for _, file := range files {
		//nolint:bodyclose // Closed in the assertion loop below.
		responses = append(responses, requestWithoutRedirect(s.T(), file.method, file.path))
	}
	oldStyleResponse := requestWithoutRedirect(
		s.T(), http.MethodGet, fmt.Sprintf("/v1/files/%s/%s/model.cif", entry.EntryId, model.ModelId),
	)

	// then
	for index, file := range files {
		response := responses[index]
		s.Equal(http.StatusFound, response.StatusCode)
		s.Equal(file.location, response.Header.Get("Location"))
		s.Equal("no-store", response.Header.Get("Cache-Control"))
		s.Require().NoError(response.Body.Close())
	}
	s.Equal(http.StatusNotFound, oldStyleResponse.StatusCode)
	s.Require().NoError(oldStyleResponse.Body.Close())
}

func (s *EntriesSuite) Test_should_return_existing_model_revision_when_idempotency_key_is_repeated() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "idempotent-model-owner", 8112)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"name": "idempotent model entry"},
	})
	entryID := entry.EntryId
	idempotencyKey := uuid.NewString()
	request := map[string]any{
		"model": map[string]any{
			"name": "idempotent model", "idempotency_key": idempotencyKey,
		},
	}

	// when
	first := createModelForTest(s.T(), ownerToken, entryID, request)
	activateModelRevisionForTest(s.T(), first)
	second := createModelForTest(s.T(), ownerToken, entryID, request)

	// then
	s.Equal(first.RevisionId, second.RevisionId)
	s.Equal(first.ModelId, second.ModelId)
	s.Equal(idempotencyKey, *second.IdempotencyKey)
	s.Equal(httpapi.RevisionStateActive, second.State)
}

func (s *EntriesSuite) Test_should_reconcile_protein_sequences_only_when_model_revision_activated() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "protein-model-owner", 8102)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"name": "protein model entry"},
	})
	entryID := entry.EntryId
	initialArtifactID := uuid.New()
	model := createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{
			"name": "protein model",
			"artifacts": []map[string]any{
				artifactRequest(initialArtifactID, "initial FASTA", "L2", "fasta", "s3://model/initial.fasta", map[string]any{
					"records": []map[string]any{{"header": "initial", "sequence": "MACDEFGHIK"}},
				}),
			},
		},
	})
	modelID := model.ModelId
	initialEntry := getEntryForTest(s.T(), entryID)
	s.Require().Len(initialEntry.ProteinSequences, 1)
	initialSequenceID := initialEntry.ProteinSequences[0].Id

	// when
	renamed := createAndActivateModelRevisionForTest(s.T(), ownerToken, entryID, modelID, map[string]any{
		"model": map[string]any{"name": "renamed protein model"},
	})
	entryAfterRename := getEntryForTest(s.T(), entryID)
	replacementArtifactID := uuid.New()
	replaced := createAndActivateModelRevisionForTest(s.T(), ownerToken, entryID, modelID, map[string]any{
		"model": map[string]any{
			"artifacts": []map[string]any{
				artifactRequest(replacementArtifactID, "replacement FASTA", "L2", "fasta", "s3://model/replacement.fasta", map[string]any{
					"records": []map[string]any{{"header": "replacement", "sequence": "MNPQRSTVWY"}},
				}),
			},
		},
	})
	entryAfterReplacement := getEntryForTest(s.T(), entryID)
	//nolint:bodyclose // assertErrorResponse closes the response body.
	deleteResponse := postJSONWithToken(
		s.T(), fmt.Sprintf("/v1/entries/%s/models/%s/revisions", entryID, modelID), map[string]any{
			"model": map[string]any{"state": "deleted"},
		}, ownerToken)
	entryAfterRejectedDeletion := getEntryForTest(s.T(), entryID)

	// then
	s.NotEqual(renamed.RevisionId, replaced.RevisionId)
	s.Require().Len(entryAfterRename.ProteinSequences, 1)
	s.Equal(initialSequenceID, entryAfterRename.ProteinSequences[0].Id)
	s.Require().Len(entryAfterReplacement.ProteinSequences, 1)
	s.NotEqual(initialSequenceID, entryAfterReplacement.ProteinSequences[0].Id)
	s.Equal(replacementArtifactID, entryAfterReplacement.ProteinSequences[0].SourceArtifactId)
	s.Equal("MNPQRSTVWY", entryAfterReplacement.ProteinSequences[0].Sequence)
	assertErrorResponse(s.T(), deleteResponse, "BAD_REQUEST", "revision field \"state\" is not allowed")
	s.Require().Len(entryAfterRejectedDeletion.ProteinSequences, 1)
	s.Equal(replacementArtifactID, entryAfterRejectedDeletion.ProteinSequences[0].SourceArtifactId)
	s.Equal("renamed protein model", getModelForTest(s.T(), entryID, modelID).Name)
}

func (s *EntriesSuite) Test_should_allow_any_authenticated_user_to_create_revisions() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "shared-entry-owner", 8110)
	contributorToken := issueEntryTokenForGitHubIDForTest(s.T(), "shared-entry-contributor", 8111)
	contributorID := tokenUserIDForTest(s.T(), contributorToken)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"name": "shared entry"},
	})
	entryID := entry.EntryId
	model := createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"name": "shared model"},
	})
	modelID := model.ModelId

	// when
	entryRevision := createEntryRevisionForTest(s.T(), contributorToken, entryID, map[string]any{
		"entry": map[string]any{"description": "contributed entry revision"},
	})
	modelRevision := createModelRevisionForTest(s.T(), contributorToken, entryID, modelID, map[string]any{
		"model": map[string]any{"description": "contributed model revision"},
	})
	newModelRevision := createModelForTest(s.T(), contributorToken, entryID, map[string]any{
		"model": map[string]any{"name": "contributed model"},
	})
	//nolint:bodyclose // assertErrorResponse closes the response body.
	deleteEntryResponse := postJSONWithToken(
		s.T(), "/v1/entries/"+entryID+"/revisions",
		map[string]any{"entry": map[string]any{"state": "deleted"}}, contributorToken,
	)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", contributorID), contributorToken,
	)
	groups := decodeEntryRevisionGroupsForTest(s.T(), response)

	// then
	item := entryRevisionGroupByEntryID(groups, entryID)
	s.Require().NotNil(item)
	s.True(entryRevisionSummaryContainsID(item.EntryRevisions, entryRevision.RevisionId))
	s.True(modelRevisionSummaryContainsID(item.ModelRevisions, modelRevision.RevisionId))
	s.True(modelRevisionSummaryContainsID(item.ModelRevisions, newModelRevision.RevisionId))
	assertErrorResponse(s.T(), deleteEntryResponse, "BAD_REQUEST", "revision field \"state\" is not allowed")
}

func (s *EntriesSuite) Test_should_keep_entry_and_model_revision_lifecycles_independent() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "separate-lifecycle-owner", 8103)
	ownerID := tokenUserIDForTest(s.T(), ownerToken)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"name": "original entry"},
	})
	entryID := entry.EntryId
	model := createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"name": "original model"},
	})
	modelID := model.ModelId
	modelRevision := createModelRevisionForTest(s.T(), ownerToken, entryID, modelID, map[string]any{
		"model": map[string]any{"name": "updated model"},
	})
	modelUserPath := fmt.Sprintf(
		"/v1/users/%s/entries/%s/models/%s/revisions/%s",
		ownerID, entryID, modelID, modelRevision.RevisionId,
	)
	entryRevision := createEntryRevisionForTest(s.T(), ownerToken, entryID, map[string]any{
		"entry": map[string]any{"name": "updated entry"},
	})

	// when
	activateEntryRevisionForTest(s.T(), entryRevision)
	pendingModel := getModelRevisionForTest(s.T(), modelUserPath, ownerToken)

	// then
	s.Equal("updated entry", getEntryForTest(s.T(), entryID).Name)
	s.Equal("original model", getModelForTest(s.T(), entryID, modelID).Name)
	s.Equal(httpapi.RevisionStateInReview, pendingModel.State)

	adminModelPath := fmt.Sprintf(
		"/v1/entries/%s/models/%s/revisions/%s",
		entryID, modelID, modelRevision.RevisionId,
	)
	updateModelRevisionStateForTest(s.T(), adminModelPath, adminToken, "active")
	s.Equal("updated model", getModelForTest(s.T(), entryID, modelID).Name)
}

func (s *EntriesSuite) Test_should_group_entry_and_model_revisions_by_entry() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "revision-group-owner", 8104)
	ownerID := tokenUserIDForTest(s.T(), ownerToken)
	otherToken := issueEntryTokenForGitHubIDForTest(s.T(), "revision-group-other", 8105)
	otherID := tokenUserIDForTest(s.T(), otherToken)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"name": "revision group entry"},
	})
	entryID := entry.EntryId
	entryRevision := createEntryRevisionForTest(s.T(), ownerToken, entryID, map[string]any{
		"entry": map[string]any{"description": "in-review entry change"},
	})
	modelRevision := createModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"name": "in-review model"},
	})

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	userResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", ownerID), ownerToken,
	)
	userGroups := decodeEntryRevisionGroupsForTest(s.T(), userResponse)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	adminResponse := getWithToken(s.T(), "/v1/entries/revisions?state=in_review", adminToken)
	adminGroups := decodeEntryRevisionGroupsForTest(s.T(), adminResponse)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	otherResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", otherID), otherToken,
	)
	otherGroups := decodeEntryRevisionGroupsForTest(s.T(), otherResponse)

	// then
	userItem := entryRevisionGroupByEntryID(userGroups, entryID)
	s.Require().NotNil(userItem)
	s.True(entryRevisionSummaryContainsID(userItem.EntryRevisions, entryRevision.RevisionId))
	s.True(modelRevisionSummaryContainsID(userItem.ModelRevisions, modelRevision.RevisionId))
	adminItem := entryRevisionGroupByEntryID(adminGroups, entryID)
	s.Require().NotNil(adminItem)
	s.True(entryRevisionSummaryContainsID(adminItem.EntryRevisions, entryRevision.RevisionId))
	s.True(modelRevisionSummaryContainsID(adminItem.ModelRevisions, modelRevision.RevisionId))
	s.Nil(entryRevisionGroupByEntryID(otherGroups, entryID))

	forbiddenResponse := getWithToken(s.T(), "/v1/entries/revisions?state=in_review", ownerToken)
	s.Equal(http.StatusForbidden, forbiddenResponse.StatusCode)
	s.Require().NoError(forbiddenResponse.Body.Close())
	missingStateResponse := getWithToken(s.T(), "/v1/entries/revisions", adminToken)
	s.Equal(http.StatusBadRequest, missingStateResponse.StatusCode)
	s.Require().NoError(missingStateResponse.Body.Close())
}

func (s *EntriesSuite) Test_should_reject_and_resubmit_model_revision_without_changing_active_model() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "model-rejection-owner", 8106)
	ownerID := tokenUserIDForTest(s.T(), ownerToken)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"name": "model rejection entry"},
	})
	entryID := entry.EntryId
	model := createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"name": "active model"},
	})
	modelID := model.ModelId
	revision := createModelRevisionForTest(s.T(), ownerToken, entryID, modelID, map[string]any{
		"model": map[string]any{"name": "proposed model"},
	})
	userPath := fmt.Sprintf(
		"/v1/users/%s/entries/%s/models/%s/revisions/%s",
		ownerID, entryID, modelID, revision.RevisionId,
	)
	adminPath := fmt.Sprintf(
		"/v1/entries/%s/models/%s/revisions/%s",
		entryID, modelID, revision.RevisionId,
	)

	// when
	rejected := updateModelRevisionStateForTest(s.T(), adminPath, adminToken, "rejected")
	resubmitted := updateModelRevisionStateForTest(s.T(), userPath, ownerToken, "in_review")
	active := updateModelRevisionStateForTest(s.T(), adminPath, adminToken, "active")

	// then
	s.Equal(httpapi.RevisionStateRejected, rejected.State)
	s.Equal(httpapi.RevisionStateInReview, resubmitted.State)
	s.Equal(httpapi.RevisionStateActive, active.State)
	s.Equal("proposed model", getModelForTest(s.T(), entryID, modelID).Name)
}

func (s *EntriesSuite) Test_should_reject_old_create_shape_and_require_authentication() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "invalid-create-owner", 8107)

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	oldShapeResponse := postJSONWithToken(s.T(), "/v1/entries", map[string]any{"name": "old shape"}, ownerToken)
	unauthenticatedResponse := postJSON(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{"name": "no token"},
	})

	// then
	s.Equal(http.StatusBadRequest, oldShapeResponse.StatusCode)
	assertErrorResponse(s.T(), oldShapeResponse, "BAD_REQUEST", "entry name is required")
	s.Equal(http.StatusUnauthorized, unauthenticatedResponse.StatusCode)
	s.Require().NoError(unauthenticatedResponse.Body.Close())
}

func createEntryForTest(t *testing.T, token string, request map[string]any) httpapi.CreateEntryRevisionAttributes {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(t, "/v1/entries", request, token)
	document := decodeJSONResponse[httpapi.CreateEntryRevisionDocument](t, response, http.StatusCreated)
	return document.Data.Attributes
}

func createAndActivateEntryForTest(
	t *testing.T,
	ownerToken string,
	request map[string]any,
) httpapi.CreateEntryRevisionAttributes {
	t.Helper()
	created := createEntryForTest(t, ownerToken, request)
	activateEntryRevisionForTest(t, created)
	return created
}

func activateEntryRevisionForTest(
	t *testing.T,
	created httpapi.CreateEntryRevisionAttributes,
) {
	t.Helper()
	adminPath := fmt.Sprintf("/v1/entries/%s/revisions/%s", created.EntryId, created.RevisionId)
	updateEntryRevisionStateForTest(t, adminPath, adminToken, "active")
}

func createEntryRevisionForTest(
	t *testing.T,
	token string,
	entryID string,
	request map[string]any,
) httpapi.CreateEntryRevisionAttributes {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(t, "/v1/entries/"+entryID+"/revisions", request, token)
	document := decodeJSONResponse[httpapi.CreateEntryRevisionDocument](t, response, http.StatusCreated)
	return document.Data.Attributes
}

func createModelForTest(
	t *testing.T,
	token string,
	entryID string,
	request map[string]any,
) httpapi.CreateModelRevisionAttributes {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(t, "/v1/entries/"+entryID+"/models", request, token)
	document := decodeJSONResponse[httpapi.CreateModelRevisionDocument](t, response, http.StatusCreated)
	return document.Data.Attributes
}

func createAndActivateModelForTest(
	t *testing.T,
	ownerToken string,
	entryID string,
	request map[string]any,
) httpapi.CreateModelRevisionAttributes {
	t.Helper()
	created := createModelForTest(t, ownerToken, entryID, request)
	activateModelRevisionForTest(t, created)
	return created
}

func createModelRevisionForTest(
	t *testing.T,
	token string,
	entryID, modelID string,
	request map[string]any,
) httpapi.CreateModelRevisionAttributes {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(
		t,
		fmt.Sprintf("/v1/entries/%s/models/%s/revisions", entryID, modelID),
		request,
		token,
	)
	document := decodeJSONResponse[httpapi.CreateModelRevisionDocument](t, response, http.StatusCreated)
	return document.Data.Attributes
}

func createAndActivateModelRevisionForTest(
	t *testing.T,
	ownerToken string,
	entryID, modelID string,
	request map[string]any,
) httpapi.CreateModelRevisionAttributes {
	t.Helper()
	created := createModelRevisionForTest(t, ownerToken, entryID, modelID, request)
	activateModelRevisionForTest(t, created)
	return created
}

func activateModelRevisionForTest(
	t *testing.T,
	created httpapi.CreateModelRevisionAttributes,
) {
	t.Helper()
	adminPath := fmt.Sprintf(
		"/v1/entries/%s/models/%s/revisions/%s",
		created.EntryId, created.ModelId, created.RevisionId,
	)
	updateModelRevisionStateForTest(t, adminPath, adminToken, "active")
}

func updateEntryRevisionStateForTest(
	t *testing.T,
	path, token, state string,
) httpapi.EntryRevisionAttributes {
	t.Helper()
	request := map[string]any{"state": state}
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := patchJSONWithToken(t, path, request, token)
	document := decodeJSONResponse[httpapi.EntryRevisionDocument](t, response, http.StatusOK)
	return document.Data.Attributes
}

func updateModelRevisionStateForTest(
	t *testing.T,
	path, token, state string,
) httpapi.ModelRevisionAttributes {
	t.Helper()
	request := map[string]any{"state": state}
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := patchJSONWithToken(t, path, request, token)
	document := decodeJSONResponse[httpapi.ModelRevisionDocument](t, response, http.StatusOK)
	return document.Data.Attributes
}

func getModelRevisionForTest(t *testing.T, path, token string) httpapi.ModelRevisionAttributes {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, path, token)
	document := decodeJSONResponse[httpapi.ModelRevisionDocument](t, response, http.StatusOK)
	return document.Data.Attributes
}

type entryForTest struct {
	CreatedBy        uuid.UUID
	Name             string
	ProteinSequences []httpapi.ProteinSequence
}

func getEntryForTest(t *testing.T, entryID string) entryForTest {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, "/v1/entries/"+entryID, "")
	document := decodeJSONResponse[httpapi.EntryDocument](t, response, http.StatusOK)
	return entryForTest{
		CreatedBy:        document.Data.Relationships.CreatedBy.Data.Id,
		Name:             document.Data.Attributes.Name,
		ProteinSequences: proteinSequencesFromEntryDocumentForTest(document),
	}
}

func proteinSequencesFromEntryDocumentForTest(document httpapi.EntryDocument) []httpapi.ProteinSequence {
	if document.Included == nil {
		return nil
	}

	sequences := make([]httpapi.ProteinSequence, 0, len(*document.Included))
	for _, resource := range *document.Included {
		sequences = append(sequences, httpapi.ProteinSequence{
			Id:               resource.Id,
			SourceArtifactId: resource.Relationships.SourceArtifact.Data.Id,
			RecordIndex:      resource.Attributes.RecordIndex,
			Header:           resource.Attributes.Header,
			Sequence:         resource.Attributes.Sequence,
			CreatedAt:        resource.Attributes.CreatedAt,
		})
	}
	return sequences
}

func getModelForTest(t *testing.T, entryID, modelID string) httpapi.ModelAttributes {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, fmt.Sprintf("/v1/entries/%s/models/%s", entryID, modelID), "")
	document := decodeJSONResponse[httpapi.ModelDocument](t, response, http.StatusOK)
	return document.Data.Attributes
}

func decodeJSONResponse[T any](t *testing.T, response *http.Response, expectedStatus int) T {
	t.Helper()
	defer func() {
		require.NoError(t, response.Body.Close())
	}()
	require.Equal(t, expectedStatus, response.StatusCode)
	var body T
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	return body
}

type modelArtifactCollectionForTest struct {
	Items     []httpapi.ArtifactAttributes
	Runs      []httpapi.Run
	Relations []httpapi.RunArtifact
}

func decodeModelArtifactCollectionForTest(t *testing.T, response *http.Response) modelArtifactCollectionForTest {
	t.Helper()
	document := decodeJSONResponse[httpapi.ModelArtifactCollectionDocument](t, response, http.StatusOK)
	return modelArtifactCollectionForTest{
		Items:     artifactsFromData(document.Data),
		Runs:      document.Meta.Runs,
		Relations: document.Meta.Relations,
	}
}

func artifactsFromData(data []httpapi.ArtifactData) []httpapi.ArtifactAttributes {
	items := make([]httpapi.ArtifactAttributes, 0, len(data))
	for _, item := range data {
		items = append(items, item.Attributes)
	}
	return items
}

func decodeEntryRevisionGroupsForTest(t *testing.T, response *http.Response) []httpapi.EntryRevisionGroupAttributes {
	t.Helper()
	document := decodeJSONResponse[httpapi.EntryRevisionGroupCollectionDocument](t, response, http.StatusOK)
	items := make([]httpapi.EntryRevisionGroupAttributes, 0, len(document.Data))
	for _, resource := range document.Data {
		items = append(items, resource.Attributes)
	}
	return items
}

func tokenUserIDForTest(t *testing.T, token string) uuid.UUID {
	t.Helper()
	userID, err := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL).Parse(token)
	require.NoError(t, err)
	return userID
}

func issueEntryTokenForTest(t *testing.T, accessToken string) string {
	t.Helper()
	return issueEntryTokenForGitHubIDForTest(t, accessToken, 8000)
}

func issueEntryTokenForGitHubIDForTest(t *testing.T, accessToken string, githubID int64) string {
	t.Helper()
	githubClient.On("GetUser", mock.Anything, accessToken).
		Return(github.User{ID: githubID, Login: accessToken, Name: "Entry Test", Email: accessToken + "@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, accessToken).
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)
	return issueTokenForTest(t, accessToken).AccessToken
}

func issueTokenForTest(t *testing.T, accessToken string) httpapi.TokenAttributes {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := ExchangeGithubToken(t, accessToken)
	document := decodeJSONResponse[httpapi.TokenDocument](t, response, http.StatusOK)
	return document.Data.Attributes
}

func entryRevisionGroupByEntryID(
	items []httpapi.EntryRevisionGroupAttributes,
	entryID string,
) *httpapi.EntryRevisionGroupAttributes {
	for index := range items {
		if items[index].Entry.Id == entryID {
			return &items[index]
		}
	}
	return nil
}

func entryRevisionSummaryContainsID(items []httpapi.EntryRevisionSummaryAttributes, revisionID uuid.UUID) bool {
	for _, item := range items {
		if item.Id == revisionID {
			return true
		}
	}
	return false
}

func modelRevisionSummaryContainsID(items []httpapi.ModelRevisionSummaryAttributes, revisionID uuid.UUID) bool {
	for _, item := range items {
		if item.Id == revisionID {
			return true
		}
	}
	return false
}

func artifactByID(artifacts []httpapi.ArtifactAttributes, id uuid.UUID) *httpapi.ArtifactAttributes {
	for index := range artifacts {
		if artifacts[index].Id == id {
			return &artifacts[index]
		}
	}
	return nil
}

func runByID(runs []httpapi.Run, id uuid.UUID) *httpapi.Run {
	for index := range runs {
		if runs[index].Id == id {
			return &runs[index]
		}
	}
	return nil
}

func metricByKey(metrics []httpapi.Metric, key string) *httpapi.Metric {
	for index := range metrics {
		if metrics[index].Key == key {
			return &metrics[index]
		}
	}
	return nil
}

func runArtifactLinkExists(
	links []httpapi.RunArtifact,
	runID uuid.UUID,
	artifactID uuid.UUID,
	direction httpapi.RunArtifactDirection,
) bool {
	for _, link := range links {
		if link.RunId == runID && link.ArtifactId == artifactID && link.Direction == direction {
			return true
		}
	}
	return false
}

func artifactRequest(
	id uuid.UUID,
	name string,
	level string,
	format string,
	uri string,
	metadata map[string]any,
) map[string]any {
	request := map[string]any{
		"id": id, "name": name, "level": level, "format": format, "uri": uri,
	}
	if metadata != nil {
		request["metadata"] = metadata
	}
	return request
}

func assertErrorResponse(t *testing.T, response *http.Response, code, message string) {
	t.Helper()
	defer func() {
		require.NoError(t, response.Body.Close())
	}()
	var body httpapi.Error
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Len(t, body.Errors, 1)
	require.NotNil(t, body.Errors[0].Code)
	require.NotNil(t, body.Errors[0].Detail)
	require.Equal(t, code, *body.Errors[0].Code)
	require.Contains(t, strings.ToLower(*body.Errors[0].Detail), strings.ToLower(message))
}
