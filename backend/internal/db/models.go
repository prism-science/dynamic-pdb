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

var (
	ErrModelNotFound          = errors.New("db: model not found")
	ErrModelOwnershipMismatch = errors.New("db: model ownership mismatch")
)

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
	query := `insert into models(id, entry_id, created_by, name, description, thumbnail_image_url, created_at, updated_at)
			  values (:id, :entry_id, :created_by, :name, :description, :thumbnail_image_url, :created_at, :updated_at)
			  returning id, entry_id, created_by, name, description, thumbnail_image_url, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row modelRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":                  model.ID,
		"entry_id":            model.EntryID,
		"created_by":          model.CreatedBy,
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
	query := `select id, entry_id, created_by, name, description, thumbnail_image_url, created_at, updated_at
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

func (r *ModelsRepository) Delete(ctx context.Context, entryID, id, ownerID uuid.UUID) error {
	query := `with target_model as (
			    select id, entry_id, created_by
			    from models
			    where entry_id = $1 and id = $2
			  ),
			  authorized_model as (
			    select id, entry_id
			    from target_model
			    where created_by = $3
			  ),
			  target_entities as (
			    select entities.id
			    from entities
			    join authorized_model on authorized_model.id = entities.model_id
			     and authorized_model.entry_id = entities.entry_id
			  ),
			  deleted_relations as (
			    delete from entity_relations
			    where source_entity_id in (select id from target_entities)
			       or target_entity_id in (select id from target_entities)
			    returning id
			  ),
			  deleted_search as (
			    delete from entry_search_index
			    using authorized_model
			    where entry_search_index.entry_id = authorized_model.entry_id
			      and (
			        (entry_search_index.model_type = $4 and entry_search_index.model_id = authorized_model.id::text)
			        or (
			          entry_search_index.model_type = $5
			          and entry_search_index.model_id in (select id::text from target_entities)
			        )
			      )
			      and (select count(*) from deleted_relations) >= 0
			    returning entry_search_index.entry_id
			  ),
			  deleted_entities as (
			    delete from entities
			    using authorized_model
			    where entities.entry_id = authorized_model.entry_id
			      and entities.model_id = authorized_model.id
			      and (select count(*) from deleted_search) >= 0
			    returning entities.id
			  ),
			  deleted_model as (
			    delete from models
			    using authorized_model
			    where models.entry_id = authorized_model.entry_id
			      and models.id = authorized_model.id
			      and (select count(*) from deleted_entities) >= 0
			    returning models.id
			  )
			  select
			    (select count(*) from target_model) as matched_count,
			    (select count(*) from deleted_model) as deleted_count`

	var result deleteResult
	if err := r.queriers.Querier(ctx, r.db).GetContext(
		ctx,
		&result,
		query,
		entryID,
		id,
		ownerID,
		entrySearchModelTypeModel,
		entrySearchModelTypeEntity,
	); err != nil {
		return fmt.Errorf("failed to delete model graph: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrModelNotFound
	}
	if result.DeletedCount == 0 {
		return ErrModelOwnershipMismatch
	}
	return nil
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

	query := `select id, entry_id, created_by, name, description, thumbnail_image_url, created_at, updated_at
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
		CreatedBy:         row.CreatedBy,
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
	CreatedBy         uuid.UUID      `db:"created_by"`
	Name              string         `db:"name"`
	Description       sql.NullString `db:"description"`
	ThumbnailImageURL sql.NullString `db:"thumbnail_image_url"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}
