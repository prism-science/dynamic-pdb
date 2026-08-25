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

type RolesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewRolesRepository(database *sqlx.DB, queriers *QuerierProvider) *RolesRepository {
	return &RolesRepository{
		db:       database,
		queriers: queriers,
	}
}

type RoleFilters struct {
	ID     *uuid.UUID
	UserID *uuid.UUID
	Keys   []models.RoleKey
}

func (r *RolesRepository) List(ctx context.Context, filters RoleFilters) ([]models.Role, error) {
	query, args := roleListQuery(filters)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind role list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	rows := make([]roleRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	return rolesFromRows(rows), nil
}

func roleListQuery(filters RoleFilters) (string, map[string]any) {
	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select roles.id, roles.key, roles.name, roles.created_at
			  from roles`

	if filters.UserID != nil {
		query += "\njoin user_roles on user_roles.role_id = roles.id"
		conditions = append(conditions, "user_roles.user_id = :user_id")
		args["user_id"] = *filters.UserID
	}
	if filters.ID != nil {
		conditions = append(conditions, "roles.id = :id")
		args["id"] = *filters.ID
	}
	if len(filters.Keys) > 0 {
		conditions = append(conditions, "roles.key = any(cast(:keys as text[]))")
		args["keys"] = pq.Array(roleKeyStrings(filters.Keys))
	}
	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by roles.key asc"

	return query, args
}

func roleKeyStrings(keys []models.RoleKey) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, string(key))
	}
	return values
}

func (r *RolesRepository) AssignToUser(
	ctx context.Context,
	userID uuid.UUID,
	roleID uuid.UUID,
	grantedBy *uuid.UUID,
) error {
	query := `insert into user_roles(user_id, role_id, granted_by)
			  values ($1, $2, $3)
			  on conflict (user_id, role_id) do nothing`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, userID, roleID, grantedBy); err != nil {
		return fmt.Errorf("assign role to user: %w", err)
	}
	return nil
}

func (r *RolesRepository) RevokeFromUser(ctx context.Context, userID uuid.UUID, roleID uuid.UUID) error {
	query := `delete from user_roles
			  where user_id = $1 and role_id = $2`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, userID, roleID); err != nil {
		return fmt.Errorf("revoke role from user: %w", err)
	}
	return nil
}

func rolesFromRows(rows []roleRow) []models.Role {
	roles := make([]models.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, *roleFromRow(&row))
	}
	return roles
}

func roleFromRow(row *roleRow) *models.Role {
	return &models.Role{
		ID:        row.ID,
		Key:       models.RoleKey(row.Key),
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
	}
}

type roleRow struct {
	ID        uuid.UUID `db:"id"`
	Key       string    `db:"key"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}
