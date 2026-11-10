package e2etest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
)

// ReviewFlagsSuite covers who sees review flags (researcher answer 7): everyone sees whether a
// record is flagged; only reviewers and admins see the issues and may use the flagged filter.
type ReviewFlagsSuite struct {
	baseSuite
}

func TestReviewFlags(t *testing.T) {
	suite.Run(t, new(ReviewFlagsSuite))
}

// reviewFlagIssueForTest is the issue every flag of this suite has. Responses to callers who may
// not see flags must not contain it anywhere.
const reviewFlagIssueForTest = "sync_validation_failed"

// reviewFlagCallerForTest is one kind of caller: the GitHub access token it signs in with and
// its API token ("" for anonymous), and whether it may see review flag issues.
type reviewFlagCallerForTest struct {
	name            string
	accessToken     string
	token           string
	seesReviewFlags bool
}

// reviewFlagCallersForTest signs in a user with no role, a reviewer and an admin. Each test gets
// its own users, so GitHub IDs start at firstGitHubID.
func reviewFlagCallersForTest(t *testing.T, prefix string, firstGitHubID int64) []reviewFlagCallerForTest {
	t.Helper()
	signedIn, reviewer, admin := prefix+"-signed-in", prefix+"-reviewer", prefix+"-admin"
	return []reviewFlagCallerForTest{
		{name: "anonymous", seesReviewFlags: false},
		{
			name: "signed in", accessToken: signedIn, seesReviewFlags: false,
			token: issueEntryTokenForGitHubIDForTest(t, signedIn, firstGitHubID),
		},
		{
			name: "reviewer", accessToken: reviewer, seesReviewFlags: true,
			token: issueReviewerTokenForTest(t, reviewer, firstGitHubID+1),
		},
		{
			name: "admin", accessToken: admin, seesReviewFlags: true,
			token: issueAdminOnlyTokenForTest(t, admin, firstGitHubID+2),
		},
	}
}

// issueAdminOnlyTokenForTest signs in a new user and gives them the admin role and no other.
func issueAdminOnlyTokenForTest(t *testing.T, accessToken string, githubID int64) string {
	t.Helper()
	token := issueEntryTokenForGitHubIDForTest(t, accessToken, githubID)
	roles, err := testDatabase.Roles.List(t.Context(), db.RoleFilters{Keys: []models.RoleKey{models.RoleKeyAdmin}})
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.NoError(t, testDatabase.Roles.AssignToUser(t.Context(), tokenUserIDForTest(t, token), roles[0].ID, nil))
	return token
}

// flaggedRepresentationForTest is one model of the flagged entry, named by what it represents.
type flaggedRepresentationForTest struct {
	name      string
	entryID   string
	modelID   string
	modelType string
}

// flaggedEntryForTest makes an active entry with a unique ligand and one active model of each
// representation (Deposited, Single Conformer, Multiconformer, Ensemble, and a re-refinement:
// a second deposition refined from the same data), and opens a review flag on every model.
func flaggedEntryForTest(t *testing.T, ownerToken string) (ligand string, representations []flaggedRepresentationForTest) {
	t.Helper()
	ligand = randomCompoundIDForTest()
	entryFileID := uuid.New()
	entry := createAndActivateEntryForTest(t, ownerToken, map[string]any{"entry": map[string]any{
		"title": "review flag visibility entry", "ligands": []string{ligand}, "cofactors": []string{},
		"artifacts": []map[string]any{
			artifactRequest(entryFileID, "data", "L0", "structure_factors_cif", "s3://entry/review-flags-sf.cif", nil),
		},
	}})
	kinds := []struct {
		name     string
		metadata map[string]any
	}{
		{name: "Deposited", metadata: map[string]any{"model_type": "Deposited"}},
		{name: "Single Conformer", metadata: map[string]any{"model_type": "Single Conformer"}},
		{name: "Multiconformer", metadata: map[string]any{"model_type": "Multiconformer"}},
		{name: "Ensemble", metadata: map[string]any{"model_type": "Ensemble"}},
		{name: "re-refined", metadata: map[string]any{"model_type": "Deposited", "purpose": "Refinement"}},
	}
	for _, kind := range kinds {
		model := map[string]any{"title": kind.name + " flagged model", "metadata": kind.metadata}
		if kind.metadata["model_type"] != "Deposited" {
			// A computed model needs a run with its software, input and output.
			modelFileID := uuid.New()
			model["artifacts"] = []map[string]any{
				artifactRequest(modelFileID, "model", "L2", "cif", "s3://model/review-flags.cif", nil),
			}
			model["runs"] = []map[string]any{refinementRunForTest(uuid.New(), entryFileID, modelFileID)}
		}
		created := createAndActivateModelForTest(t, ownerToken, entry.EntryId, map[string]any{"model": model})
		require.NoError(t, testDatabase.MigrationReviews.Open(context.Background(), models.MigrationReview{
			ObjectType: "model", ObjectID: created.ModelId, Issue: reviewFlagIssueForTest, Details: map[string]any{},
		}))
		representations = append(representations, flaggedRepresentationForTest{
			name: kind.name, entryID: entry.EntryId, modelID: created.ModelId,
			modelType: kind.metadata["model_type"].(string),
		})
	}
	return ligand, representations
}

// getRawForTest sends a GET as the token's user and returns the status and the body as sent.
func getRawForTest(t *testing.T, baseURL, path, token string) (int, []byte) {
	t.Helper()
	response := jsonRequestToServerWithToken(t, baseURL, http.MethodGet, path, nil, token)
	defer func() {
		require.NoError(t, response.Body.Close())
	}()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, body
}

func (s *ReviewFlagsSuite) Test_should_show_flag_issues_only_to_reviewers_and_admins_when_each_representation_is_searched_and_read() {
	// given
	callers := reviewFlagCallersForTest(s.T(), "review-flags-read", 8760)
	ligand, representations := flaggedEntryForTest(s.T(), callers[1].token)

	for _, caller := range callers {
		for _, representation := range representations {
			s.Run(caller.name+"/"+representation.name, func() {
				// when
				searchPath := "/v1/models?limit=100&ligand_or_cofactor=" + ligand + "&model_type=" + url.QueryEscape(representation.modelType)
				searchStatus, searchBody := getRawForTest(s.T(), modelSearchServer.URL, searchPath, caller.token)
				detailPath := "/v1/entries/" + representation.entryID + "/models/" + representation.modelID
				detailStatus, detailBody := getRawForTest(s.T(), testServer.URL, detailPath, caller.token)

				// then
				s.Require().Equal(http.StatusOK, searchStatus)
				var search httpapi.ModelSearchCollectionDocument
				s.Require().NoError(json.Unmarshal(searchBody, &search))
				index := slices.IndexFunc(search.Data, func(item httpapi.ModelSearchData) bool { return item.Id == representation.modelID })
				s.Require().GreaterOrEqual(index, 0, "the search finds the model")
				result := search.Data[index].Attributes
				s.Require().NotNil(result.FlaggedForReview)
				s.True(*result.FlaggedForReview)

				s.Require().Equal(http.StatusOK, detailStatus)
				var detail httpapi.ModelDocument
				s.Require().NoError(json.Unmarshal(detailBody, &detail))
				s.Require().NotNil(detail.Data.Attributes.FlaggedForReview)
				s.True(*detail.Data.Attributes.FlaggedForReview)
				s.NotNil(detail.Data.Attributes.MissingInfo, "information missing stays public")

				if !caller.seesReviewFlags {
					s.Nil(result.ReviewFlags)
					s.Nil(detail.Data.Attributes.ReviewFlags)
					s.NotContains(string(searchBody), reviewFlagIssueForTest)
					s.NotContains(string(searchBody), "review_flags")
					s.NotContains(string(detailBody), reviewFlagIssueForTest)
					s.NotContains(string(detailBody), "review_flags")
					return
				}
				for _, flags := range []*[]httpapi.ReviewFlag{result.ReviewFlags, detail.Data.Attributes.ReviewFlags} {
					s.Require().NotNil(flags)
					s.Require().Len(*flags, 1)
					s.Equal("model", (*flags)[0].ObjectType)
					s.Equal(representation.modelID, (*flags)[0].ObjectId)
					s.Equal(reviewFlagIssueForTest, (*flags)[0].Issue)
				}
			})
		}
	}
}

func (s *ReviewFlagsSuite) Test_should_let_only_reviewers_and_admins_filter_by_flags_when_each_representation_is_searched() {
	// given
	callers := reviewFlagCallersForTest(s.T(), "review-flags-filter", 8763)
	ligand, representations := flaggedEntryForTest(s.T(), callers[1].token)

	for _, caller := range callers {
		for _, representation := range representations {
			s.Run(caller.name+"/"+representation.name, func() {
				// when
				path := "/v1/models?flagged=true&limit=100&ligand_or_cofactor=" + ligand + "&model_type=" + url.QueryEscape(representation.modelType)
				//nolint:bodyclose // decodeJSONResponse closes the response body.
				response := jsonRequestToServerWithToken(s.T(), modelSearchServer.URL, http.MethodGet, path, nil, caller.token)

				// then
				if !caller.seesReviewFlags {
					refused := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusForbidden)
					s.Require().Len(refused.Errors, 1)
					s.Require().NotNil(refused.Errors[0].Detail)
					s.Contains(*refused.Errors[0].Detail, "flagged")
					return
				}
				found := decodeJSONResponse[httpapi.ModelSearchCollectionDocument](s.T(), response, http.StatusOK)
				ids := make([]string, 0, len(found.Data))
				for _, item := range found.Data {
					ids = append(ids, item.Id)
				}
				s.Contains(ids, representation.modelID)
			})
		}
	}
}

func (s *ReviewFlagsSuite) Test_should_grant_review_flags_read_only_to_reviewers_and_admins_when_users_sign_in() {
	// given
	callers := reviewFlagCallersForTest(s.T(), "review-flags-sign-in", 8766)

	for _, caller := range callers[1:] {
		s.Run(caller.name, func() {
			// when: the user signs in again, now holding the role the caller list gave them.
			permissions := issueTokenForTest(s.T(), caller.accessToken).Permissions

			// then
			s.Equal(caller.seesReviewFlags, slices.Contains(permissions, string(models.PermissionKeyReviewFlagsRead)))
		})
	}
}

func (s *ReviewFlagsSuite) Test_should_refuse_a_flagged_selection_search_from_anyone_but_reviewers_and_admins_when_a_selection_is_saved() {
	// given
	callers := reviewFlagCallersForTest(s.T(), "review-flags-selection", 8769)
	ligand, _ := flaggedEntryForTest(s.T(), callers[1].token)
	body := map[string]any{"searches": []string{"flagged=true&model_type=Deposited&ligand_or_cofactor=" + ligand}}

	for _, caller := range callers {
		s.Run(caller.name, func() {
			// when
			//nolint:bodyclose // decodeJSONResponse closes the response body.
			response := jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodPost, "/v1/selections", body, caller.token)

			// then
			if !caller.seesReviewFlags {
				refused := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusForbidden)
				s.Require().Len(refused.Errors, 1)
				s.Require().NotNil(refused.Errors[0].Detail)
				s.Contains(*refused.Errors[0].Detail, "remove flagged from search 0")
				return
			}
			saved := decodeJSONResponse[httpapi.SelectionDocument](s.T(), response, http.StatusCreated)
			s.NotEmpty(saved.Data.Id)
		})
	}
}

func (s *ReviewFlagsSuite) Test_should_hide_the_issues_of_entry_and_model_version_flags_from_the_public_when_a_model_is_searched_and_read() {
	// given: flags on the entry and on the model's current revision, not on the model itself.
	callers := reviewFlagCallersForTest(s.T(), "review-flags-objects", 8772)
	ligand := randomCompoundIDForTest()
	entry := createAndActivateEntryForTest(s.T(), callers[1].token, map[string]any{"entry": map[string]any{
		"title": "review flag objects entry", "ligands": []string{ligand}, "cofactors": []string{},
	}})
	model := createAndActivateModelForTest(s.T(), callers[1].token, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "review flag objects model", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	for _, review := range []models.MigrationReview{
		{ObjectType: "entry", ObjectID: entry.EntryId, Issue: reviewFlagIssueForTest, Details: map[string]any{}},
		{ObjectType: "model_revision", ObjectID: model.RevisionId.String(), Issue: reviewFlagIssueForTest, Details: map[string]any{}},
	} {
		s.Require().NoError(testDatabase.MigrationReviews.Open(context.Background(), review))
	}

	for _, caller := range callers {
		s.Run(caller.name, func() {
			// when
			searchStatus, searchBody := getRawForTest(s.T(), modelSearchServer.URL, "/v1/models?ligand_or_cofactor="+ligand, caller.token)
			detailStatus, detailBody := getRawForTest(s.T(), testServer.URL,
				"/v1/entries/"+entry.EntryId+"/models/"+model.ModelId, caller.token)

			// then
			s.Require().Equal(http.StatusOK, searchStatus)
			s.Require().Equal(http.StatusOK, detailStatus)
			var search httpapi.ModelSearchCollectionDocument
			s.Require().NoError(json.Unmarshal(searchBody, &search))
			s.Require().Len(search.Data, 1)
			s.Require().NotNil(search.Data[0].Attributes.FlaggedForReview)
			s.True(*search.Data[0].Attributes.FlaggedForReview)
			if !caller.seesReviewFlags {
				s.NotContains(string(searchBody), reviewFlagIssueForTest)
				s.NotContains(string(detailBody), reviewFlagIssueForTest)
				return
			}
			var detail httpapi.ModelDocument
			s.Require().NoError(json.Unmarshal(detailBody, &detail))
			for _, flags := range []*[]httpapi.ReviewFlag{search.Data[0].Attributes.ReviewFlags, detail.Data.Attributes.ReviewFlags} {
				s.Require().NotNil(flags)
				objectTypes := make([]string, 0, len(*flags))
				for _, flag := range *flags {
					objectTypes = append(objectTypes, flag.ObjectType)
				}
				s.ElementsMatch([]string{"entry", "model_revision"}, objectTypes)
			}
		})
	}
}
