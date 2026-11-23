package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

type Authorizer interface {
	HasRole(ctx context.Context, userID uuid.UUID, role models.RoleKey) (bool, error)
	// HasAnyRole reports whether the user holds at least one of the roles.
	HasAnyRole(ctx context.Context, userID uuid.UUID, roles ...models.RoleKey) (bool, error)
	Can(ctx context.Context, userID uuid.UUID, permission models.PermissionKey) (bool, error)
}

type authorizer struct {
	database *db.DB
}

var _ Authorizer = (*authorizer)(nil)

func NewAuthorizer(database *db.DB) Authorizer {
	return &authorizer{database: database}
}

func (a *authorizer) HasRole(
	ctx context.Context,
	userID uuid.UUID,
	roleKey models.RoleKey,
) (bool, error) {
	roles, err := a.database.Roles.List(ctx, db.RoleFilters{
		UserID: &userID,
		Keys:   []models.RoleKey{roleKey},
	})
	if err != nil {
		return false, fmt.Errorf("auth: check user role: %w", err)
	}
	return len(roles) > 0, nil
}

// errNoRolesToCheck is a HasAnyRole call without roles. Listing a user's roles with no key would
// list all of them, so any role would pass.
var errNoRolesToCheck = errors.New("auth: no roles to check")

func (a *authorizer) HasAnyRole(
	ctx context.Context,
	userID uuid.UUID,
	roleKeys ...models.RoleKey,
) (bool, error) {
	if len(roleKeys) == 0 {
		return false, errNoRolesToCheck
	}
	roles, err := a.database.Roles.List(ctx, db.RoleFilters{
		UserID: &userID,
		Keys:   roleKeys,
	})
	if err != nil {
		return false, fmt.Errorf("auth: check user roles: %w", err)
	}
	return len(roles) > 0, nil
}

func (a *authorizer) Can(
	ctx context.Context,
	userID uuid.UUID,
	permissionKey models.PermissionKey,
) (bool, error) {
	permissions, err := a.database.Permissions.List(ctx, db.PermissionFilters{
		UserID: &userID,
		Keys:   []models.PermissionKey{permissionKey},
	})
	if err != nil {
		return false, fmt.Errorf("auth: check user permission: %w", err)
	}
	return len(permissions) > 0, nil
}
