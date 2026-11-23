package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
)

func Test_should_return_seeded_roles_and_permissions_when_authorization_data_listed(t *testing.T) {
	// given
	ctx := context.Background()

	// when
	roles, err := testDB.Roles.List(ctx, db.RoleFilters{})

	// then
	require.NoError(t, err)
	require.Len(t, roles, 2)
	assert.Equal(t, models.RoleKeyAdmin, roles[0].Key)
	assert.Equal(t, models.RoleKeyReviewer, roles[1].Key)

	adminPermissions, err := testDB.Permissions.List(ctx, db.PermissionFilters{RoleID: &roles[0].ID})
	require.NoError(t, err)
	assert.Equal(t, []models.PermissionKey{
		models.PermissionKeyRolesAssign,
		models.PermissionKeyRolesRevoke,
	}, permissionKeys(adminPermissions))

	reviewerPermissions, err := testDB.Permissions.List(ctx, db.PermissionFilters{RoleID: &roles[1].ID})
	require.NoError(t, err)
	assert.Equal(t, []models.PermissionKey{
		models.PermissionKeyRevisionsApprove,
		models.PermissionKeyRevisionsReject,
	}, permissionKeys(reviewerPermissions))
}

func Test_should_return_effective_permissions_when_roles_assigned_to_user(t *testing.T) {
	// given
	ctx := context.Background()
	user := createAuthorizationTestUser(t)
	grantor := createAuthorizationTestUser(t)
	admin := getAuthorizationTestRole(t, models.RoleKeyAdmin)
	reviewer := getAuthorizationTestRole(t, models.RoleKeyReviewer)

	// when
	err := testDB.Roles.AssignToUser(ctx, user.ID, admin.ID, &grantor.ID)
	require.NoError(t, err)
	err = testDB.Roles.AssignToUser(ctx, user.ID, reviewer.ID, &grantor.ID)
	require.NoError(t, err)
	err = testDB.Roles.AssignToUser(ctx, user.ID, reviewer.ID, &grantor.ID)
	require.NoError(t, err)

	// then
	roles, err := testDB.Roles.List(ctx, db.RoleFilters{UserID: &user.ID})
	require.NoError(t, err)
	assert.Equal(t, []models.RoleKey{
		models.RoleKeyAdmin,
		models.RoleKeyReviewer,
	}, roleKeys(roles))

	permissions, err := testDB.Permissions.List(ctx, db.PermissionFilters{UserID: &user.ID})
	require.NoError(t, err)
	assert.Equal(t, []models.PermissionKey{
		models.PermissionKeyRevisionsApprove,
		models.PermissionKeyRevisionsReject,
		models.PermissionKeyRolesAssign,
		models.PermissionKeyRolesRevoke,
	}, permissionKeys(permissions))

	authorizer := auth.NewAuthorizer(testDB)
	hasAdminRole, err := authorizer.HasRole(ctx, user.ID, models.RoleKeyAdmin)
	require.NoError(t, err)
	assert.True(t, hasAdminRole)

	canApprove, err := authorizer.Can(ctx, user.ID, models.PermissionKeyRevisionsApprove)
	require.NoError(t, err)
	assert.True(t, canApprove)
}

func Test_should_remove_role_and_permissions_when_role_revoked_from_user(t *testing.T) {
	// given
	ctx := context.Background()
	user := createAuthorizationTestUser(t)
	reviewer := getAuthorizationTestRole(t, models.RoleKeyReviewer)
	require.NoError(t, testDB.Roles.AssignToUser(ctx, user.ID, reviewer.ID, nil))

	// when
	err := testDB.Roles.RevokeFromUser(ctx, user.ID, reviewer.ID)
	require.NoError(t, err)
	err = testDB.Roles.RevokeFromUser(ctx, user.ID, reviewer.ID)
	require.NoError(t, err)

	// then
	roles, err := testDB.Roles.List(ctx, db.RoleFilters{UserID: &user.ID})
	require.NoError(t, err)
	assert.Empty(t, roles)

	permissions, err := testDB.Permissions.List(ctx, db.PermissionFilters{UserID: &user.ID})
	require.NoError(t, err)
	assert.Empty(t, permissions)

	authorizer := auth.NewAuthorizer(testDB)
	hasReviewerRole, err := authorizer.HasRole(ctx, user.ID, models.RoleKeyReviewer)
	require.NoError(t, err)
	assert.False(t, hasReviewerRole)

	canApprove, err := authorizer.Can(ctx, user.ID, models.PermissionKeyRevisionsApprove)
	require.NoError(t, err)
	assert.False(t, canApprove)
}

func Test_should_find_any_of_the_roles_only_when_the_user_holds_one(t *testing.T) {
	// given
	ctx := context.Background()
	authorizer := auth.NewAuthorizer(testDB)
	reviewerOrAdmin := []models.RoleKey{models.RoleKeyReviewer, models.RoleKeyAdmin}
	signedIn := createAuthorizationTestUser(t)
	admin := createAuthorizationTestUser(t)
	reviewer := createAuthorizationTestUser(t)
	require.NoError(t, testDB.Roles.AssignToUser(ctx, admin.ID, getAuthorizationTestRole(t, models.RoleKeyAdmin).ID, nil))
	require.NoError(t, testDB.Roles.AssignToUser(ctx, reviewer.ID, getAuthorizationTestRole(t, models.RoleKeyReviewer).ID, nil))

	// when
	signedInHasOne, signedInErr := authorizer.HasAnyRole(ctx, signedIn.ID, reviewerOrAdmin...)
	adminHasOne, adminErr := authorizer.HasAnyRole(ctx, admin.ID, reviewerOrAdmin...)
	reviewerHasOne, reviewerErr := authorizer.HasAnyRole(ctx, reviewer.ID, reviewerOrAdmin...)
	adminIsReviewer, adminReviewerErr := authorizer.HasAnyRole(ctx, admin.ID, models.RoleKeyReviewer)
	_, noRolesErr := authorizer.HasAnyRole(ctx, admin.ID)

	// then
	require.NoError(t, signedInErr)
	assert.False(t, signedInHasOne)
	require.NoError(t, adminErr)
	assert.True(t, adminHasOne)
	require.NoError(t, reviewerErr)
	assert.True(t, reviewerHasOne)
	require.NoError(t, adminReviewerErr)
	assert.False(t, adminIsReviewer)
	require.Error(t, noRolesErr)
}

func Test_should_return_empty_roles_when_role_key_does_not_exist(t *testing.T) {
	// given / when
	roles, err := testDB.Roles.List(context.Background(), db.RoleFilters{
		Keys: []models.RoleKey{"missing"},
	})

	// then
	require.NoError(t, err)
	assert.Empty(t, roles)
}

func getAuthorizationTestRole(t *testing.T, key models.RoleKey) *models.Role {
	t.Helper()

	roles, err := testDB.Roles.List(context.Background(), db.RoleFilters{
		Keys: []models.RoleKey{key},
	})
	require.NoError(t, err)
	require.Len(t, roles, 1)
	return &roles[0]
}

func createAuthorizationTestUser(t *testing.T) *models.User {
	t.Helper()

	now := time.Now().UTC()
	user, err := testDB.Users.Create(context.Background(), models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "github", Value: uuid.NewString()},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	return user
}

func roleKeys(roles []models.Role) []models.RoleKey {
	keys := make([]models.RoleKey, 0, len(roles))
	for _, role := range roles {
		keys = append(keys, role.Key)
	}
	return keys
}

func permissionKeys(permissions []models.Permission) []models.PermissionKey {
	keys := make([]models.PermissionKey, 0, len(permissions))
	for _, permission := range permissions {
		keys = append(keys, permission.Key)
	}
	return keys
}
