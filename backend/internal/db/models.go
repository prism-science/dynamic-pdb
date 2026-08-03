package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

var (
	ErrModelNotFound                  = errors.New("db: model not found")
	ErrModelRevisionNotFound          = errors.New("db: model revision not found")
	ErrModelRevisionOwnershipMismatch = errors.New("db: model revision ownership mismatch")
)

type ModelsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type ModelRevisionFilters struct {
	ID        *uuid.UUID
	EntryID   *uuid.UUID
	ModelID   *uuid.UUID
	State     *models.RevisionState
	CreatedBy *uuid.UUID
	Limit     *int
	Offset    *int
}

func NewModelsRepository(database *sqlx.DB, queriers *QuerierProvider) *ModelsRepository {
	return &ModelsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *ModelsRepository) Create(
	ctx context.Context,
	entryID uuid.UUID,
	revision models.ModelRevision,
) (*models.ModelRevision, error) {
	metadata, err := marshalJSON(revision.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare model revision metadata: %w", err)
	}

	query := `with ensured_model as (
			    insert into models(id, entry_id, created_by, created_at)
			    values (:model_id, :entry_id, :created_by, :created_at)
			    on conflict (id) do nothing
			    returning id
			  )
			  insert into model_revisions(
			    id,
			    model_id,
			    parent_revision_id,
			    primary_artifact_id,
			    revision_number,
			    state,
			    change_summary,
			    published_at,
			    name,
			    description,
			    thumbnail_image_url,
			    metadata,
			    created_by,
			    created_at,
			    updated_at
			  )
			  values (
			    :id,
			    :model_id,
			    :parent_revision_id,
			    :primary_artifact_id,
			    :revision_number,
			    :state,
			    :change_summary,
			    :published_at,
			    :name,
			    :description,
			    :thumbnail_image_url,
			    cast(:metadata as jsonb),
			    :created_by,
			    :created_at,
			    :updated_at
			  )
			  returning id, model_id, parent_revision_id, primary_artifact_id, revision_number,
			            state, change_summary, published_at, name, description,
			            thumbnail_image_url, metadata, created_by, created_at, updated_at`

	var row modelRevisionRow
	args := map[string]any{
		"id":                  revision.ID,
		"entry_id":            entryID,
		"model_id":            revision.ModelID,
		"parent_revision_id":  revision.ParentRevisionID,
		"primary_artifact_id": revision.PrimaryArtifactID,
		"revision_number":     revision.RevisionNumber,
		"state":               string(revision.State),
		"change_summary":      revision.ChangeSummary,
		"published_at":        revision.PublishedAt,
		"name":                revision.Name,
		"description":         revision.Description,
		"thumbnail_image_url": revision.ThumbnailImageURL,
		"metadata":            metadata,
		"created_by":          revision.CreatedBy,
		"created_at":          revision.CreatedAt,
		"updated_at":          revision.UpdatedAt,
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind model revision insert query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("insert model revision: %w", err)
	}

	created, err := modelRevisionFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode inserted model revision: %w", err)
	}
	return created, nil
}

func (r *ModelsRepository) Get(ctx context.Context, filters ModelRevisionFilters) (*models.ModelRevision, error) {
	revisions, err := r.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list model revisions for get: %w", err)
	}
	if len(revisions) == 0 {
		return nil, ErrModelRevisionNotFound
	}
	if len(revisions) > 1 {
		return nil, fmt.Errorf("model revision get returned %d rows", len(revisions))
	}
	return &revisions[0], nil
}

func (r *ModelsRepository) List(
	ctx context.Context,
	filters ModelRevisionFilters,
) ([]models.ModelRevision, error) {
	query, args, err := modelRevisionListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build model revision list query: %w", err)
	}

	rows := make([]modelRevisionRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind model revision list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list model revisions: %w", err)
	}

	revisions := make([]models.ModelRevision, 0, len(rows))
	for _, row := range rows {
		revision, err := modelRevisionFromRow(&row)
		if err != nil {
			return nil, fmt.Errorf("decode model revision: %w", err)
		}
		revisions = append(revisions, *revision)
	}
	return revisions, nil
}

func (r *ModelsRepository) Delete(ctx context.Context, modelID, revisionID, ownerID uuid.UUID) error {
	query := `with target_revision as (
			    select id, model_id, created_by
			    from model_revisions
			    where model_id = $1 and id = $2
			  ),
			  authorized_revision as (
			    select id, model_id
			    from target_revision
			    where created_by = $3
			  ),
			  updated_revision as (
			    update model_revisions
			    set state = $4,
			        updated_at = now()
			    where id in (select id from authorized_revision)
			    returning id
			  )
			  select
			    (select count(*) from target_revision) as matched_count,
			    (select count(*) from updated_revision) as deleted_count`

	var result deleteResult
	if err := r.queriers.Querier(ctx, r.db).GetContext(
		ctx,
		&result,
		query,
		modelID,
		revisionID,
		ownerID,
		models.RevisionStateDeleted,
	); err != nil {
		return fmt.Errorf("mark model revision deleted: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrModelRevisionNotFound
	}
	if result.DeletedCount == 0 {
		return ErrModelRevisionOwnershipMismatch
	}
	return nil
}

func modelRevisionListQuery(filters ModelRevisionFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	conditions := make([]string, 0)
	args := map[string]any{}

	if filters.ID != nil {
		conditions = append(conditions, "model_revisions.id = :id")
		args["id"] = *filters.ID
	}
	if filters.EntryID != nil {
		conditions = append(conditions, "models.entry_id = :entry_id")
		args["entry_id"] = *filters.EntryID
	}
	if filters.ModelID != nil {
		conditions = append(conditions, "model_revisions.model_id = :model_id")
		args["model_id"] = *filters.ModelID
	}
	if filters.State != nil {
		conditions = append(conditions, "model_revisions.state = :state")
		args["state"] = string(*filters.State)
	}
	if filters.CreatedBy != nil {
		conditions = append(conditions, "model_revisions.created_by = :created_by")
		args["created_by"] = *filters.CreatedBy
	}

	query := `select model_revisions.id, model_revisions.model_id, model_revisions.parent_revision_id,
			         model_revisions.primary_artifact_id, model_revisions.revision_number,
			         model_revisions.state, model_revisions.change_summary, model_revisions.published_at,
			         model_revisions.name, model_revisions.description, model_revisions.thumbnail_image_url,
			         model_revisions.metadata, model_revisions.created_by, model_revisions.created_at,
			         model_revisions.updated_at
			  from model_revisions
			  join models on models.id = model_revisions.model_id`
	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by model_revisions.created_at asc, model_revisions.id asc"

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

func modelRevisionFromRow(row *modelRevisionRow) (*models.ModelRevision, error) {
	var metadata models.ModelMetadata
	if err := unmarshalJSON(row.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	return &models.ModelRevision{
		ID:                row.ID,
		ModelID:           row.ModelID,
		ParentRevisionID:  uuidPtrFromSQL(row.ParentRevisionID),
		PrimaryArtifactID: uuidPtrFromSQL(row.PrimaryArtifactID),
		RevisionNumber:    intPtrFromSQL(row.RevisionNumber),
		State:             models.RevisionState(row.State),
		ChangeSummary:     stringPtrFromSQL(row.ChangeSummary),
		PublishedAt:       timePtrFromSQL(row.PublishedAt),
		Name:              row.Name,
		Description:       stringPtrFromSQL(row.Description),
		ThumbnailImageURL: stringPtrFromSQL(row.ThumbnailImageURL),
		Metadata:          metadata,
		CreatedBy:         row.CreatedBy,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}, nil
}

type modelRevisionRow struct {
	ID                uuid.UUID      `db:"id"`
	ModelID           uuid.UUID      `db:"model_id"`
	ParentRevisionID  uuid.NullUUID  `db:"parent_revision_id"`
	PrimaryArtifactID uuid.NullUUID  `db:"primary_artifact_id"`
	RevisionNumber    sql.NullInt64  `db:"revision_number"`
	State             string         `db:"state"`
	ChangeSummary     sql.NullString `db:"change_summary"`
	PublishedAt       sql.NullTime   `db:"published_at"`
	Name              string         `db:"name"`
	Description       sql.NullString `db:"description"`
	ThumbnailImageURL sql.NullString `db:"thumbnail_image_url"`
	Metadata          []byte         `db:"metadata"`
	CreatedBy         uuid.UUID      `db:"created_by"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}
