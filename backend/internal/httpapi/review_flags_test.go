package httpapi

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

// reviewFlagsTestServer has model search and selections on and no database: every case here is
// answered before a request reads one.
func reviewFlagsTestServer(authorizer *authorizerStub) *Server {
	server := selectionsTestServer(true)
	server.authorizer = authorizer
	return server
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

// Researcher answer of 17 Nov, Q4: every caller may filter by flags, so a selection search with
// the flagged filter is read for anyone, and saving one never reads the caller's roles.
func Test_should_read_a_flagged_selection_search_from_every_caller_without_reading_roles(t *testing.T) {
	signedIn := &domainmodels.User{ID: uuid.New()}
	cases := []struct {
		name       string
		user       *domainmodels.User
		authorizer *authorizerStub
	}{
		{name: "anonymous", authorizer: &authorizerStub{}},
		{name: "signed in without a role", user: signedIn, authorizer: &authorizerStub{hasAnyRoleResult: false}},
		{name: "reviewer or admin", user: signedIn, authorizer: &authorizerStub{hasAnyRoleResult: true}},
		{name: "roles cannot be read", user: signedIn, authorizer: &authorizerStub{hasAnyRoleErr: errors.New("connection refused")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// given
			server := reviewFlagsTestServer(tc.authorizer)
			ctx := t.Context()
			if tc.user != nil {
				ctx = WithUser(ctx, tc.user)
			}

			// when
			freeze, err := server.selectionFreezeFromRequest(ctx, CreateSelectionRequest{
				Searches: &[]string{"flagged=true", "flagged=false&model_type=Ensemble"},
			})

			// then
			require.NoError(t, err)
			require.Len(t, freeze.Searches, 2)
			require.NotNil(t, freeze.Searches[0].Flagged)
			assert.True(t, *freeze.Searches[0].Flagged)
			require.NotNil(t, freeze.Searches[1].Flagged)
			assert.False(t, *freeze.Searches[1].Flagged)
			assert.Zero(t, tc.authorizer.anyRoleCalls)
		})
	}
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
