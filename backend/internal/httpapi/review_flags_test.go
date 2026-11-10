package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

// reviewFlagsTestServer has model search and selections on and no database: every case here is
// answered before a request reads one.
func reviewFlagsTestServer(authorizer *authorizerStub) *Server {
	server := selectionsTestServer(true)
	server.authorizer = authorizer
	return server
}

func newFlaggedSearchRequest(user *domainmodels.User) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/v1/models?flagged=true", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	if user != nil {
		request = request.WithContext(WithUser(request.Context(), user))
	}
	return request
}

func Test_should_answer_403_naming_flagged_without_reading_roles_when_an_anonymous_caller_filters_by_flags(t *testing.T) {
	for _, flagged := range []bool{true, false} {
		t.Run(fmt.Sprintf("flagged=%t", flagged), func(t *testing.T) {
			// given
			authorizer := &authorizerStub{hasAnyRoleResult: true}
			server := reviewFlagsTestServer(authorizer)
			recorder := httptest.NewRecorder()

			// when
			server.ListModelsAcrossEntries(recorder, newFlaggedSearchRequest(nil), ListModelsAcrossEntriesParams{Flagged: ptr(flagged)})

			// then
			assert.Equal(t, http.StatusForbidden, recorder.Code)
			assert.Equal(t, "Authorization", recorder.Header().Get("Vary"))
			errorObject := decodeSingleErrorObject(t, recorder)
			require.NotNil(t, errorObject.Code)
			assert.Equal(t, "FORBIDDEN", *errorObject.Code)
			require.NotNil(t, errorObject.Detail)
			assert.Contains(t, *errorObject.Detail, "remove flagged")
			assert.Zero(t, authorizer.anyRoleCalls)
		})
	}
}

func Test_should_answer_403_when_a_signed_in_caller_without_reviewer_or_admin_role_filters_by_flags(t *testing.T) {
	for _, flagged := range []bool{true, false} {
		t.Run(fmt.Sprintf("flagged=%t", flagged), func(t *testing.T) {
			// given
			user := &domainmodels.User{ID: uuid.New()}
			authorizer := &authorizerStub{hasAnyRoleResult: false}
			server := reviewFlagsTestServer(authorizer)
			recorder := httptest.NewRecorder()

			// when
			server.ListModelsAcrossEntries(recorder, newFlaggedSearchRequest(user), ListModelsAcrossEntriesParams{Flagged: ptr(flagged)})

			// then
			assert.Equal(t, http.StatusForbidden, recorder.Code)
			assert.Equal(t, user.ID, authorizer.userID)
			assert.ElementsMatch(t, []domainmodels.RoleKey{domainmodels.RoleKeyReviewer, domainmodels.RoleKeyAdmin}, authorizer.anyRoles)
		})
	}
}

func Test_should_answer_500_when_the_callers_roles_cannot_be_read_for_a_flagged_search(t *testing.T) {
	// given
	user := &domainmodels.User{ID: uuid.New()}
	server := reviewFlagsTestServer(&authorizerStub{hasAnyRoleErr: errors.New("connection refused")})
	recorder := httptest.NewRecorder()

	// when
	server.ListModelsAcrossEntries(recorder, newFlaggedSearchRequest(user), ListModelsAcrossEntriesParams{Flagged: ptr(true)})

	// then
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func Test_should_allow_the_flagged_filter_only_when_the_caller_sees_review_flags(t *testing.T) {
	// given
	flagged := &parsedModelSearch{filters: db.ModelSearch{Flagged: ptr(true)}, parameters: []string{"flagged"}}
	other := &parsedModelSearch{parameters: []string{"model_type"}}

	// when / then
	assert.NoError(t, checkFlaggedFilter(nil, false))
	assert.NoError(t, checkFlaggedFilter(other, false))
	assert.NoError(t, checkFlaggedFilter(flagged, true))
	assert.ErrorIs(t, checkFlaggedFilter(flagged, false), errFlaggedFilterForbidden)
}

func Test_should_tell_everyone_whether_a_record_is_flagged_and_only_reviewers_the_flags_when_flag_fields_are_built(t *testing.T) {
	// given
	flags := []domainmodels.ReviewFlag{{
		ObjectType: "polymer_entity", ObjectID: "7", Issue: "mutations_need_review",
		FlaggedAt: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC),
	}}

	// when
	publicFlagged, publicFlags := reviewFlagFields(flags, false)
	reviewerFlagged, reviewerFlags := reviewFlagFields(flags, true)
	unflagged, unflaggedFlags := reviewFlagFields(nil, true)

	// then
	assert.True(t, publicFlagged)
	assert.Nil(t, publicFlags)
	assert.True(t, reviewerFlagged)
	require.NotNil(t, reviewerFlags)
	assert.Equal(t, []ReviewFlag{{
		ObjectType: "polymer_entity", ObjectId: "7", Issue: "mutations_need_review", FlaggedAt: flags[0].FlaggedAt,
	}}, *reviewerFlags)
	assert.False(t, unflagged)
	require.NotNil(t, unflaggedFlags)
	assert.Empty(t, *unflaggedFlags)
}

func Test_should_answer_403_naming_flagged_and_the_search_when_an_anonymous_caller_saves_a_flagged_selection(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()

	// when
	reviewFlagsTestServer(&authorizerStub{}).CreateSelection(recorder,
		newSelectionRequest(`{"searches": ["model_type=Ensemble", "flagged=true"]}`))

	// then
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Code)
	assert.Equal(t, "FORBIDDEN", *errorObject.Code)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "remove flagged from search 1")
}

func Test_should_read_a_flagged_selection_search_when_the_caller_has_the_reviewer_or_admin_role(t *testing.T) {
	// given
	user := &domainmodels.User{ID: uuid.New()}
	server := reviewFlagsTestServer(&authorizerStub{hasAnyRoleResult: true})
	ctx := WithUser(t.Context(), user)

	// when
	freeze, err := server.selectionFreezeFromRequest(ctx, CreateSelectionRequest{Searches: &[]string{"flagged=true"}})

	// then
	require.NoError(t, err)
	require.Len(t, freeze.Searches, 1)
	require.NotNil(t, freeze.Searches[0].Flagged)
	assert.True(t, *freeze.Searches[0].Flagged)
}

func Test_should_answer_as_for_the_public_when_the_callers_roles_cannot_be_read_for_a_response_that_can_fall_back(t *testing.T) {
	// given
	user := &domainmodels.User{ID: uuid.New()}
	server := reviewFlagsTestServer(&authorizerStub{hasAnyRoleResult: true, hasAnyRoleErr: errors.New("connection refused")})
	ctx := WithUser(t.Context(), user)

	// when
	sees := server.seesReviewFlagsOrPublic(ctx)

	// then
	assert.False(t, sees)
}

func Test_should_not_read_roles_when_no_selection_search_uses_the_flagged_filter(t *testing.T) {
	// given
	user := &domainmodels.User{ID: uuid.New()}
	authorizer := &authorizerStub{hasAnyRoleErr: errors.New("connection refused")}
	server := reviewFlagsTestServer(authorizer)
	ctx := WithUser(t.Context(), user)

	// when
	freeze, err := server.selectionFreezeFromRequest(ctx, CreateSelectionRequest{Searches: &[]string{"model_type=Ensemble"}})

	// then
	require.NoError(t, err)
	require.Len(t, freeze.Searches, 1)
	assert.Zero(t, authorizer.anyRoleCalls)
}
