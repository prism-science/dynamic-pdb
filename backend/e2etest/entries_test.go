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
	entryID := uuid.New()
	entryArtifactID := uuid.New()
	modelID := uuid.New()
	modelArtifactID := uuid.New()
	metricID := uuid.New()
	runID := uuid.New()

	// when
	entry := createEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"id": entryID, "name": "independent entry",
			"artifacts": []map[string]any{
				artifactRequest(entryArtifactID, "entry FASTA", "L0", "fasta", "s3://entry/sequence.fasta", map[string]any{
					"records": []map[string]any{{"header": "entry", "sequence": "MACDEFGHIK"}},
				}),
			},
		},
		"model_operations": []map[string]any{{
			"op": "add",
			"data": map[string]any{
				"model_id": modelID, "name": "independent model", "primary_artifact_id": modelArtifactID,
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
	s.Equal(entryID, entry.EntryId)
	s.Require().Len(entry.ModelResults, 1)
	createdModel := entry.ModelResults[0]
	s.Equal(httpapi.ModelOperationResultOpAdd, createdModel.Op)
	s.Equal(modelID, createdModel.ModelId)
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
	artifacts := decodeJSONResponse[httpapi.ModelArtifactListResponse](s.T(), artifactsResponse, http.StatusOK)
	s.Require().NotNil(artifactByID(artifacts.Items, modelArtifactID))
	s.Require().NotNil(runByID(artifacts.Runs, runID))
	s.True(runArtifactLinkExists(artifacts.Relations, runID, entryArtifactID, httpapi.Input))
	s.True(runArtifactLinkExists(artifacts.Relations, runID, modelArtifactID, httpapi.Output))
}

func (s *EntriesSuite) Test_should_reconcile_protein_sequences_only_when_model_revision_activated() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "protein-model-owner", 8102)
	entryID := uuid.New()
	createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"id": entryID, "name": "protein model entry"},
	})
	modelID := uuid.New()
	initialArtifactID := uuid.New()
	createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{
			"id": modelID, "name": "protein model",
			"artifacts": []map[string]any{
				artifactRequest(initialArtifactID, "initial FASTA", "L2", "fasta", "s3://model/initial.fasta", map[string]any{
					"records": []map[string]any{{"header": "initial", "sequence": "MACDEFGHIK"}},
				}),
			},
		},
	})
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
	entryID := uuid.New()
	createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"id": entryID, "name": "shared entry"},
	})
	modelID := uuid.New()
	createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"id": modelID, "name": "shared model"},
	})

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
		s.T(), "/v1/entries/"+entryID.String()+"/revisions",
		map[string]any{"entry": map[string]any{"state": "deleted"}}, contributorToken,
	)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", contributorID), contributorToken,
	)
	groups := decodeJSONResponse[httpapi.EntryRevisionGroupListResponse](s.T(), response, http.StatusOK)

	// then
	item := entryRevisionGroupByEntryID(groups.Items, entryID)
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
	entryID := uuid.New()
	createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"id": entryID, "name": "original entry"},
	})
	modelID := uuid.New()
	createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"id": modelID, "name": "original model"},
	})
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
	entryID := uuid.New()
	createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"id": entryID, "name": "revision group entry"},
	})
	entryRevision := createEntryRevisionForTest(s.T(), ownerToken, entryID, map[string]any{
		"entry": map[string]any{"description": "in-review entry change"},
	})
	modelRevision := createModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"id": uuid.New(), "name": "in-review model"},
	})

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	userResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", ownerID), ownerToken,
	)
	userGroups := decodeJSONResponse[httpapi.EntryRevisionGroupListResponse](s.T(), userResponse, http.StatusOK)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	adminResponse := getWithToken(s.T(), "/v1/entries/revisions?state=in_review", adminToken)
	adminGroups := decodeJSONResponse[httpapi.EntryRevisionGroupListResponse](s.T(), adminResponse, http.StatusOK)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	otherResponse := getWithToken(
		s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", otherID), otherToken,
	)
	otherGroups := decodeJSONResponse[httpapi.EntryRevisionGroupListResponse](s.T(), otherResponse, http.StatusOK)

	// then
	userItem := entryRevisionGroupByEntryID(userGroups.Items, entryID)
	s.Require().NotNil(userItem)
	s.True(entryRevisionSummaryContainsID(userItem.EntryRevisions, entryRevision.RevisionId))
	s.True(modelRevisionSummaryContainsID(userItem.ModelRevisions, modelRevision.RevisionId))
	adminItem := entryRevisionGroupByEntryID(adminGroups.Items, entryID)
	s.Require().NotNil(adminItem)
	s.True(entryRevisionSummaryContainsID(adminItem.EntryRevisions, entryRevision.RevisionId))
	s.True(modelRevisionSummaryContainsID(adminItem.ModelRevisions, modelRevision.RevisionId))
	s.Nil(entryRevisionGroupByEntryID(otherGroups.Items, entryID))

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
	entryID := uuid.New()
	createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"id": entryID, "name": "model rejection entry"},
	})
	modelID := uuid.New()
	createAndActivateModelForTest(s.T(), ownerToken, entryID, map[string]any{
		"model": map[string]any{"id": modelID, "name": "active model"},
	})
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

func createEntryForTest(t *testing.T, token string, request map[string]any) httpapi.CreateEntryRevisionResponse {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(t, "/v1/entries", request, token)
	return decodeJSONResponse[httpapi.CreateEntryRevisionResponse](t, response, http.StatusCreated)
}

func createAndActivateEntryForTest(
	t *testing.T,
	ownerToken string,
	request map[string]any,
) httpapi.CreateEntryRevisionResponse {
	t.Helper()
	created := createEntryForTest(t, ownerToken, request)
	activateEntryRevisionForTest(t, created)
	return created
}

func activateEntryRevisionForTest(
	t *testing.T,
	created httpapi.CreateEntryRevisionResponse,
) {
	t.Helper()
	adminPath := fmt.Sprintf("/v1/entries/%s/revisions/%s", created.EntryId, created.RevisionId)
	updateEntryRevisionStateForTest(t, adminPath, adminToken, "active")
}

func createEntryRevisionForTest(
	t *testing.T,
	token string,
	entryID uuid.UUID,
	request map[string]any,
) httpapi.CreateEntryRevisionResponse {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(t, "/v1/entries/"+entryID.String()+"/revisions", request, token)
	return decodeJSONResponse[httpapi.CreateEntryRevisionResponse](t, response, http.StatusCreated)
}

func createModelForTest(
	t *testing.T,
	token string,
	entryID uuid.UUID,
	request map[string]any,
) httpapi.CreateModelRevisionResponse {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(t, "/v1/entries/"+entryID.String()+"/models", request, token)
	return decodeJSONResponse[httpapi.CreateModelRevisionResponse](t, response, http.StatusCreated)
}

func createAndActivateModelForTest(
	t *testing.T,
	ownerToken string,
	entryID uuid.UUID,
	request map[string]any,
) httpapi.CreateModelRevisionResponse {
	t.Helper()
	created := createModelForTest(t, ownerToken, entryID, request)
	activateModelRevisionForTest(t, created)
	return created
}

func createModelRevisionForTest(
	t *testing.T,
	token string,
	entryID, modelID uuid.UUID,
	request map[string]any,
) httpapi.CreateModelRevisionResponse {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(
		t,
		fmt.Sprintf("/v1/entries/%s/models/%s/revisions", entryID, modelID),
		request,
		token,
	)
	return decodeJSONResponse[httpapi.CreateModelRevisionResponse](t, response, http.StatusCreated)
}

func createAndActivateModelRevisionForTest(
	t *testing.T,
	ownerToken string,
	entryID, modelID uuid.UUID,
	request map[string]any,
) httpapi.CreateModelRevisionResponse {
	t.Helper()
	created := createModelRevisionForTest(t, ownerToken, entryID, modelID, request)
	activateModelRevisionForTest(t, created)
	return created
}

func activateModelRevisionForTest(
	t *testing.T,
	created httpapi.CreateModelRevisionResponse,
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
) httpapi.EntryRevision {
	t.Helper()
	request := map[string]any{"state": state}
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := patchJSONWithToken(t, path, request, token)
	return decodeJSONResponse[httpapi.EntryRevision](t, response, http.StatusOK)
}

func updateModelRevisionStateForTest(
	t *testing.T,
	path, token, state string,
) httpapi.ModelRevision {
	t.Helper()
	request := map[string]any{"state": state}
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := patchJSONWithToken(t, path, request, token)
	return decodeJSONResponse[httpapi.ModelRevision](t, response, http.StatusOK)
}

func getModelRevisionForTest(t *testing.T, path, token string) httpapi.ModelRevision {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, path, token)
	return decodeJSONResponse[httpapi.ModelRevision](t, response, http.StatusOK)
}

func getEntryForTest(t *testing.T, entryID uuid.UUID) httpapi.Entry {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, "/v1/entries/"+entryID.String(), "")
	return decodeJSONResponse[httpapi.Entry](t, response, http.StatusOK)
}

func getModelForTest(t *testing.T, entryID, modelID uuid.UUID) httpapi.Model {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, fmt.Sprintf("/v1/entries/%s/models/%s", entryID, modelID), "")
	return decodeJSONResponse[httpapi.Model](t, response, http.StatusOK)
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

func issueTokenForTest(t *testing.T, accessToken string) httpapi.TokenResponse {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := ExchangeGithubToken(t, accessToken)
	return decodeJSONResponse[httpapi.TokenResponse](t, response, http.StatusOK)
}

func entryRevisionGroupByEntryID(
	items []httpapi.EntryRevisionGroup,
	entryID uuid.UUID,
) *httpapi.EntryRevisionGroup {
	for index := range items {
		if items[index].Entry.Id == entryID {
			return &items[index]
		}
	}
	return nil
}

func entryRevisionSummaryContainsID(items []httpapi.EntryRevisionSummary, revisionID uuid.UUID) bool {
	for _, item := range items {
		if item.Id == revisionID {
			return true
		}
	}
	return false
}

func modelRevisionSummaryContainsID(items []httpapi.ModelRevisionSummary, revisionID uuid.UUID) bool {
	for _, item := range items {
		if item.Id == revisionID {
			return true
		}
	}
	return false
}

func artifactByID(artifacts []httpapi.Artifact, id uuid.UUID) *httpapi.Artifact {
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
	require.Equal(t, code, body.Code)
	require.Contains(t, strings.ToLower(body.Message), strings.ToLower(message))
}
