package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

var ErrModelNotFound = errors.New("db: model not found")

type ModelsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type ModelFilters struct {
	EntryID *uuid.UUID
	Limit   *int
	Offset  *int
}

func NewModelsRepository(database *sqlx.DB, queriers *QuerierProvider) *ModelsRepository {
	return &ModelsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *ModelsRepository) Create(ctx context.Context, model models.Model) (*models.Model, error) {
	query := `insert into models(id, entry_id, name, description, thumbnail_image_url, created_at, updated_at)
			  values (:id, :entry_id, :name, :description, :thumbnail_image_url, :created_at, :updated_at)
			  returning id, entry_id, name, description, thumbnail_image_url, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row modelRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":                  model.ID,
		"entry_id":            model.EntryID,
		"name":                model.Name,
		"description":         nullableString(model.Description),
		"thumbnail_image_url": nullableString(model.ThumbnailImageURL),
		"created_at":          model.CreatedAt,
		"updated_at":          model.UpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert model: %w", err)
	}

	return modelFromRow(&row), nil
}

func (r *ModelsRepository) Get(ctx context.Context, entryID, id uuid.UUID) (*models.Model, error) {
	query := `select id, entry_id, name, description, thumbnail_image_url, created_at, updated_at
			  from models
			  where entry_id = $1 and id = $2`

	var row modelRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, entryID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelNotFound
		}
		return nil, fmt.Errorf("failed to get model: %w", err)
	}

	return modelFromRow(&row), nil
}

func (r *ModelsRepository) List(ctx context.Context, filters ModelFilters) ([]models.Model, error) {
	query, args, err := modelListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build model list query: %w", err)
	}

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryxContext(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to list models: %w", err)
	}
	defer rows.Close()

	models := make([]models.Model, 0)
	for rows.Next() {
		var row modelRow
		if err := rows.StructScan(&row); err != nil {
			return nil, fmt.Errorf("scan model row: %w", err)
		}
		models = append(models, *modelFromRow(&row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model rows: %w", err)
	}

	return models, nil
}

func modelListQuery(filters ModelFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	conditions := make([]string, 0)
	args := map[string]any{}
	if filters.EntryID != nil {
		conditions = append(conditions, "entry_id = :entry_id")
		args["entry_id"] = *filters.EntryID
	}

	query := `select id, entry_id, name, description, thumbnail_image_url, created_at, updated_at
			  from models`
	if len(conditions) > 0 {
		query += "\nwhere " + conditions[0]
	}
	query += "\norder by created_at asc, id asc"

	if filters.Limit != nil {
		query += "\nlimit :limit"
		args["limit"] = *filters.Limit
	}
	if filters.Offset != nil {
		query += "\noffset :offset"
		args["offset"] = *filters.Offset
	}

	return query, args, nil
}

func modelFromRow(row *modelRow) *models.Model {
	return &models.Model{
		ID:                row.ID,
		EntryID:           row.EntryID,
		Name:              row.Name,
		Description:       stringPtrFromNull(row.Description),
		ThumbnailImageURL: stringPtrFromNull(row.ThumbnailImageURL),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

type modelRow struct {
	ID                uuid.UUID      `db:"id"`
	EntryID           uuid.UUID      `db:"entry_id"`
	Name              string         `db:"name"`
	Description       sql.NullString `db:"description"`
	ThumbnailImageURL sql.NullString `db:"thumbnail_image_url"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}
