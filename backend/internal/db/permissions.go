package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

type PermissionsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewPermissionsRepository(database *sqlx.DB, queriers *QuerierProvider) *PermissionsRepository {
	return &PermissionsRepository{
		db:       database,
		queriers: queriers,
	}
}

type PermissionFilters struct {
	ID     *uuid.UUID
	RoleID *uuid.UUID
	UserID *uuid.UUID
	Keys   []models.PermissionKey
}

func (r *PermissionsRepository) List(
	ctx context.Context,
	filters PermissionFilters,
) ([]models.Permission, error) {
	query, args := permissionListQuery(filters)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind permission list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	rows := make([]permissionRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}

	permissions := make([]models.Permission, 0, len(rows))
	for _, row := range rows {
		permissions = append(permissions, models.Permission{
			ID:          row.ID,
			Key:         models.PermissionKey(row.Key),
			Name:        row.Name,
			Description: row.Description,
			CreatedAt:   row.CreatedAt,
		})
	}
	return permissions, nil
}

func permissionListQuery(filters PermissionFilters) (string, map[string]any) {
	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select distinct permissions.id, permissions.key, permissions.name,
			         permissions.description, permissions.created_at
			  from permissions`

	if filters.RoleID != nil || filters.UserID != nil {
		query += "\njoin role_permissions on role_permissions.permission_id = permissions.id"
	}
	if filters.UserID != nil {
		query += "\njoin user_roles on user_roles.role_id = role_permissions.role_id"
		conditions = append(conditions, "user_roles.user_id = :user_id")
		args["user_id"] = *filters.UserID
	}
	if filters.RoleID != nil {
		conditions = append(conditions, "role_permissions.role_id = :role_id")
		args["role_id"] = *filters.RoleID
	}
	if filters.ID != nil {
		conditions = append(conditions, "permissions.id = :id")
		args["id"] = *filters.ID
	}
	if len(filters.Keys) > 0 {
		conditions = append(conditions, "permissions.key = any(cast(:keys as text[]))")
		args["keys"] = pq.Array(permissionKeyStrings(filters.Keys))
	}
	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by permissions.key asc"

	return query, args
}

func permissionKeyStrings(keys []models.PermissionKey) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, string(key))
	}
	return values
}

type permissionRow struct {
	ID          uuid.UUID `db:"id"`
	Key         string    `db:"key"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	CreatedAt   time.Time `db:"created_at"`
}
