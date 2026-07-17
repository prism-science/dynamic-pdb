package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/types"
)

var ErrUserNotFound = errors.New("db: user not found")

type UsersRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewUsersRepository(database *sqlx.DB, queriers *QuerierProvider) *UsersRepository {
	return &UsersRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *UsersRepository) Create(ctx context.Context, user auth.User) (*auth.User, error) {
	query := `insert into users(id, source, external_ref, email, display_name, avatar_url, created_at, updated_at)
			  values (:id, :source, :external_ref, :email, :display_name, :avatar_url, :created_at, :updated_at)
			  on conflict (source, external_ref) do update set source = excluded.source
			  returning id, source, external_ref, email, display_name, avatar_url, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row userRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":           user.ID,
		"source":       user.ExternalRef.Source,
		"external_ref": user.ExternalRef.Value,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"avatar_url":   user.AvatarURL,
		"created_at":   user.CreatedAt,
		"updated_at":   user.UpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to upsert user: %w", err)
	}
	return userFromRow(&row), nil
}

func (r *UsersRepository) Get(ctx context.Context, id uuid.UUID) (*auth.User, error) {
	query := `select id, source, external_ref, email, display_name, avatar_url, created_at, updated_at
			  from users
			  where id = $1`

	var row userRow
	if err := r.db.GetContext(ctx, &row, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return userFromRow(&row), nil
}

func userFromRow(row *userRow) *auth.User {
	return &auth.User{
		ID: row.ID,
		ExternalRef: types.ExternalRef{
			Source: row.Source,
			Value:  row.ExternalRef,
		},
		Email:       row.Email.String,
		DisplayName: row.DisplayName.String,
		AvatarURL:   row.AvatarURL.String,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

type userRow struct {
	ID          uuid.UUID      `db:"id"`
	Source      string         `db:"source"`
	ExternalRef string         `db:"external_ref"`
	Email       sql.NullString `db:"email"`
	DisplayName sql.NullString `db:"display_name"`
	AvatarURL   sql.NullString `db:"avatar_url"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
}
