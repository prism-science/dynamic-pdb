package e2etest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
)

// ReviewDecisionSuite covers the no-self-approval rule: publishing a change always takes a second
// person, so a reviewer cannot approve a revision they created, but can reject it.
type ReviewDecisionSuite struct {
	baseSuite
}

func TestReviewDecision(t *testing.T) {
	suite.Run(t, new(ReviewDecisionSuite))
}

func (s *ReviewDecisionSuite) Test_should_refuse_approval_when_reviewer_created_the_entry_revision() {
	// given
	reviewerToken := issueReviewerTokenForTest(s.T(), "self-review-entry-reviewer", 8501)
	created := createEntryForTest(s.T(), reviewerToken, map[string]any{
		"entry": map[string]any{"title": "entry its reviewer submitted"},
	})
	path := fmt.Sprintf("/v1/entries/%s/revisions/%s", created.EntryId, created.RevisionId)

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := patchJSONWithToken(s.T(), path, map[string]any{"state": "active"}, reviewerToken)

	// then
	s.Equal(http.StatusForbidden, response.StatusCode)
	assertErrorResponse(s.T(), response, "SELF_REVIEW", "a reviewer cannot approve a revision they created")
	stored, err := testDatabase.Entries.Get(s.T().Context(), db.EntryRevisionFilters{ID: &created.RevisionId})
	s.Require().NoError(err)
	s.Equal(models.RevisionStateInReview, stored.State)
}

func (s *ReviewDecisionSuite) Test_should_refuse_approval_when_reviewer_created_the_model_revision() {
	// given
	reviewerToken := issueReviewerTokenForTest(s.T(), "self-review-model-reviewer", 8502)
	entry := createAndActivateEntryForTest(s.T(), reviewerToken, map[string]any{
		"entry": map[string]any{"title": "entry for a reviewer's own model"},
	})
	created := createModelForTest(s.T(), reviewerToken, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model its reviewer submitted", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	path := fmt.Sprintf("/v1/entries/%s/models/%s/revisions/%s", created.EntryId, created.ModelId, created.RevisionId)

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := patchJSONWithToken(s.T(), path, map[string]any{"state": "active"}, reviewerToken)

	// then
	s.Equal(http.StatusForbidden, response.StatusCode)
	assertErrorResponse(s.T(), response, "SELF_REVIEW", "a reviewer cannot approve a revision they created")
	stored, err := testDatabase.Models.Get(s.T().Context(), db.ModelRevisionFilters{ID: &created.RevisionId})
	s.Require().NoError(err)
	s.Equal(models.RevisionStateInReview, stored.State)
}

func (s *ReviewDecisionSuite) Test_should_approve_when_another_reviewer_created_the_revision() {
	// given
	authorToken := issueReviewerTokenForTest(s.T(), "self-review-author", 8503)
	created := createEntryForTest(s.T(), authorToken, map[string]any{
		"entry": map[string]any{"title": "entry a second reviewer approves"},
	})
	path := fmt.Sprintf("/v1/entries/%s/revisions/%s", created.EntryId, created.RevisionId)

	// when
	approved := updateEntryRevisionStateForTest(s.T(), path, adminToken, "active")

	// then
	s.Equal(httpapi.RevisionStateActive, approved.State)
}

func (s *ReviewDecisionSuite) Test_should_reject_when_reviewer_created_the_revision() {
	// given
	reviewerToken := issueReviewerTokenForTest(s.T(), "self-review-withdraw-reviewer", 8504)
	entry := createAndActivateEntryForTest(s.T(), reviewerToken, map[string]any{
		"entry": map[string]any{"title": "entry its reviewer withdraws a change to"},
	})
	model := createAndActivateModelForTest(s.T(), reviewerToken, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model its reviewer withdraws a change to", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	entryRevision := createEntryRevisionForTest(s.T(), reviewerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"title": "entry change its reviewer withdraws"},
	})
	modelRevision := createModelRevisionForTest(s.T(), reviewerToken, entry.EntryId, model.ModelId, map[string]any{
		"model": map[string]any{"title": "model change its reviewer withdraws"},
	})

	// when
	rejectedEntry := updateEntryRevisionStateForTest(s.T(),
		fmt.Sprintf("/v1/entries/%s/revisions/%s", entry.EntryId, entryRevision.RevisionId),
		reviewerToken, "rejected")
	rejectedModel := updateModelRevisionStateForTest(s.T(),
		fmt.Sprintf("/v1/entries/%s/models/%s/revisions/%s", entry.EntryId, model.ModelId, modelRevision.RevisionId),
		reviewerToken, "rejected")

	// then
	s.Equal(httpapi.RevisionStateRejected, rejectedEntry.State)
	s.Equal(httpapi.RevisionStateRejected, rejectedModel.State)
}

// issueReviewerTokenForTest signs in a new user and gives them the reviewer role. The role is
// read from the database on every decision, so the token issued before the grant already works.
func issueReviewerTokenForTest(t *testing.T, accessToken string, githubID int64) string {
	t.Helper()
	token := issueEntryTokenForGitHubIDForTest(t, accessToken, githubID)
	roles, err := testDatabase.Roles.List(t.Context(), db.RoleFilters{
		Keys: []models.RoleKey{models.RoleKeyReviewer},
	})
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.NoError(t, testDatabase.Roles.AssignToUser(t.Context(), tokenUserIDForTest(t, token), roles[0].ID, nil))
	return token
}
