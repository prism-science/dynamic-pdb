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
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

var (
	ErrModelRevisionNotFound = errors.New("db: model revision not found")
	ErrModelRevisionConflict = errors.New("db: model revision not in the expected state")
)

type ModelsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type ModelRevisionFilters struct {
	ID             *uuid.UUID
	EntryID        *string
	ModelID        *string
	State          *models.RevisionState
	States         []models.RevisionState
	ModelState     *models.ModelState
	EntryState     *models.EntryState
	CreatedBy      *uuid.UUID
	IdempotencyKey *string
	Limit          *int
	Offset         *int
}

func NewModelsRepository(database *sqlx.DB, queriers *QuerierProvider) *ModelsRepository {
	return &ModelsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *ModelsRepository) NextID(ctx context.Context, entryID string) (string, error) {
	querier := r.queriers.Querier(ctx, r.db)
	var lockedEntryID string
	if err := querier.GetContext(ctx, &lockedEntryID, `select id from entries where id = $1 for update`, entryID); err != nil {
		return "", fmt.Errorf("lock entry for model ID generation: %w", err)
	}

	var modelNumber int
	if err := querier.GetContext(ctx, &modelNumber, `select count(*) + 1 from models where entry_id = $1`, entryID); err != nil {
		return "", fmt.Errorf("get next model number: %w", err)
	}

	modelID, err := models.NewModelID(lockedEntryID, modelNumber)
	if err != nil {
		return "", fmt.Errorf("generate model ID: %w", err)
	}
	return modelID, nil
}

func (r *ModelsRepository) Create(
	ctx context.Context,
	entryID string,
	revision models.ModelRevision,
) (*models.ModelRevision, error) {
	metadata, err := marshalJSON(revision.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare model revision metadata: %w", err)
	}
	modelState := revision.ModelState
	if modelState == "" {
		modelState = models.ModelStateActive
	}

	query := `with ensured_model as (
			    insert into models(id, entry_id, state, created_by, created_at)
			    values (:model_id, :entry_id, 'new', :created_by, :created_at)
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
			    model_state,
			    change_summary,
			    published_at,
			    title,
			    thumbnail_image_url,
			    metadata,
			    idempotency_key,
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
			    :model_state,
			    :change_summary,
			    :published_at,
			    :title,
			    :thumbnail_image_url,
			    cast(:metadata as jsonb),
			    :idempotency_key,
			    :created_by,
			    :created_at,
			    :updated_at
			  )
			  returning id, model_id, parent_revision_id, primary_artifact_id, revision_number,
			            state, model_state, change_summary, published_at, title,
			            thumbnail_image_url, metadata, idempotency_key, created_by, created_at, updated_at`

	var row modelRevisionRow
	args := map[string]any{
		"id":                  revision.ID,
		"entry_id":            entryID,
		"model_id":            revision.ModelID,
		"parent_revision_id":  revision.ParentRevisionID,
		"primary_artifact_id": revision.PrimaryArtifactID,
		"revision_number":     revision.RevisionNumber,
		"state":               string(revision.State),
		"model_state":         string(modelState),
		"change_summary":      revision.ChangeSummary,
		"published_at":        revision.PublishedAt,
		"title":               revision.Title,
		"thumbnail_image_url": revision.ThumbnailImageURL,
		"metadata":            metadata,
		"idempotency_key":     revision.IdempotencyKey,
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
	row.EntryID = entryID

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

func (r *ModelsRepository) SetRevisionState(
	ctx context.Context,
	revisionID uuid.UUID,
	from []models.RevisionState,
	to models.RevisionState,
) (*models.ModelRevision, error) {
	fromStates := make([]string, 0, len(from))
	for _, state := range from {
		fromStates = append(fromStates, string(state))
	}
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx,
		`update model_revisions
		 set state = $3, updated_at = now()
		 where id = $1
		   and state = any($2::text[])`,
		revisionID,
		pq.Array(fromStates),
		string(to),
	)
	if err != nil {
		return nil, fmt.Errorf("set model revision state: %w", err)
	}
	updatedRows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("count updated model revisions: %w", err)
	}
	if updatedRows == 0 {
		return nil, ErrModelRevisionConflict
	}
	updated, err := r.Get(ctx, ModelRevisionFilters{ID: &revisionID})
	if err != nil {
		return nil, fmt.Errorf("get updated model revision: %w", err)
	}
	return updated, nil
}

func (r *ModelsRepository) ActivateRevision(
	ctx context.Context,
	revisionID uuid.UUID,
) (*models.ModelRevision, error) {
	querier := r.queriers.Querier(ctx, r.db)
	if _, err := querier.ExecContext(
		ctx,
		`update model_revisions
		 set state = 'archived', updated_at = now()
		 where model_id = (select target.model_id from model_revisions target where target.id = $1)
		   and state = 'active'`,
		revisionID,
	); err != nil {
		return nil, fmt.Errorf("archive active model revisions: %w", err)
	}

	result, err := querier.ExecContext(
		ctx,
		`update model_revisions
		 set state = 'active',
		     published_at = now(),
		     revision_number = coalesce(
		         (select max(previous.revision_number)
		          from model_revisions previous
		          where previous.model_id = model_revisions.model_id), 0) + 1,
		     updated_at = now()
		 where id = $1
		   and state = 'in_review'`,
		revisionID,
	)
	if err != nil {
		return nil, fmt.Errorf("activate model revision: %w", err)
	}
	updatedRows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("count activated model revisions: %w", err)
	}
	if updatedRows == 0 {
		return nil, ErrModelRevisionConflict
	}
	updated, err := r.Get(ctx, ModelRevisionFilters{ID: &revisionID})
	if err != nil {
		return nil, fmt.Errorf("get activated model revision: %w", err)
	}

	if _, err := querier.ExecContext(
		ctx,
		`update models set state = $2 where id = $1`,
		updated.ModelID,
		string(updated.ModelState),
	); err != nil {
		return nil, fmt.Errorf("apply active model states: %w", err)
	}
	return updated, nil
}

func (r *ModelsRepository) RejectRevision(
	ctx context.Context,
	revisionID uuid.UUID,
) (*models.ModelRevision, error) {
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx,
		`update model_revisions
		 set state = 'rejected',
		     updated_at = now()
		 where id = $1
		   and state = 'in_review'`,
		revisionID,
	)
	if err != nil {
		return nil, fmt.Errorf("reject model revision: %w", err)
	}
	updatedRows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("count rejected model revisions: %w", err)
	}
	if updatedRows == 0 {
		return nil, ErrModelRevisionConflict
	}
	updated, err := r.Get(ctx, ModelRevisionFilters{ID: &revisionID})
	if err != nil {
		return nil, fmt.Errorf("get rejected model revision: %w", err)
	}
	return updated, nil
}

const modelRevisionReturningColumns = `model_revisions.id, mo.entry_id as entry_id, model_revisions.model_id,
			model_revisions.parent_revision_id, model_revisions.primary_artifact_id, model_revisions.revision_number,
			model_revisions.state, model_revisions.model_state, model_revisions.change_summary, model_revisions.published_at, model_revisions.title,
			model_revisions.thumbnail_image_url, model_revisions.metadata,
			model_revisions.idempotency_key, model_revisions.created_by, model_revisions.created_at, model_revisions.updated_at`

// GetLive returns the latest non-archived revision of a model.
func (r *ModelsRepository) GetLive(ctx context.Context, modelID string) (*models.ModelRevision, error) {
	revisions, err := r.List(ctx, ModelRevisionFilters{
		ModelID: &modelID,
		States:  liveRevisionStates,
	})
	if err != nil {
		return nil, fmt.Errorf("list live model revisions: %w", err)
	}
	if len(revisions) == 0 {
		return nil, ErrModelRevisionNotFound
	}
	return &revisions[len(revisions)-1], nil
}

// SaveDraft rewrites the editable fields (and optionally the state) of a model's
// draft. Only pending or rejected revisions can be written.
func (r *ModelsRepository) SaveDraft(ctx context.Context, revision models.ModelRevision) (*models.ModelRevision, error) {
	metadata, err := marshalJSON(revision.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare model revision metadata: %w", err)
	}

	query := `update model_revisions
			  set title = :title,
			      thumbnail_image_url = :thumbnail_image_url,
			      primary_artifact_id = :primary_artifact_id,
			      metadata = cast(:metadata as jsonb),
			      state = :state,
			      updated_at = now()
			  from models mo
			  where mo.id = model_revisions.model_id
			    and model_revisions.id = :id
			    and model_revisions.state in ('pending', 'rejected')
			  returning ` + modelRevisionReturningColumns

	updated, err := r.getRevision(ctx, query, map[string]any{
		"id":                  revision.ID,
		"title":               revision.Title,
		"thumbnail_image_url": revision.ThumbnailImageURL,
		"primary_artifact_id": revision.PrimaryArtifactID,
		"metadata":            metadata,
		"state":               string(revision.State),
	})
	if errors.Is(err, ErrModelRevisionNotFound) {
		return nil, ErrModelRevisionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("save model draft: %w", err)
	}
	return updated, nil
}

func (r *ModelsRepository) getRevision(
	ctx context.Context,
	query string,
	args map[string]any,
) (*models.ModelRevision, error) {
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind model revision query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	var row modelRevisionRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModelRevisionNotFound
		}
		return nil, fmt.Errorf("get model revision: %w", err)
	}
	revision, err := modelRevisionFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode model revision: %w", err)
	}
	return revision, nil
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
	if filters.ModelState != nil {
		conditions = append(conditions, "models.state = :model_state")
		args["model_state"] = string(*filters.ModelState)
	}
	if filters.EntryState != nil {
		conditions = append(conditions, "entries.state = :entry_state")
		args["entry_state"] = string(*filters.EntryState)
	}
	if len(filters.States) > 0 {
		states := make([]string, 0, len(filters.States))
		for _, state := range filters.States {
			states = append(states, string(state))
		}
		conditions = append(conditions, "model_revisions.state = any(:states::text[])")
		args["states"] = pq.StringArray(states)
	}
	if filters.CreatedBy != nil {
		conditions = append(conditions, "model_revisions.created_by = :created_by")
		args["created_by"] = *filters.CreatedBy
	}
	if filters.IdempotencyKey != nil {
		conditions = append(conditions, "model_revisions.idempotency_key = :idempotency_key")
		args["idempotency_key"] = *filters.IdempotencyKey
	}
	query := `select model_revisions.id, models.entry_id, model_revisions.model_id, model_revisions.parent_revision_id,
			         model_revisions.primary_artifact_id, model_revisions.revision_number,
			         model_revisions.state, model_revisions.model_state, model_revisions.change_summary, model_revisions.published_at,
			         model_revisions.title, model_revisions.thumbnail_image_url,
			         model_revisions.metadata, model_revisions.idempotency_key, model_revisions.created_by, model_revisions.created_at,
			         model_revisions.updated_at
			  from model_revisions
			  join models on models.id = model_revisions.model_id`
	if filters.EntryState != nil {
		query += "\njoin entries on entries.id = models.entry_id"
	}
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
		EntryID:           row.EntryID,
		ModelID:           row.ModelID,
		ParentRevisionID:  uuidPtrFromSQL(row.ParentRevisionID),
		PrimaryArtifactID: uuidPtrFromSQL(row.PrimaryArtifactID),
		RevisionNumber:    intPtrFromSQL(row.RevisionNumber),
		State:             models.RevisionState(row.State),
		ModelState:        models.ModelState(row.ModelState),
		ChangeSummary:     stringPtrFromSQL(row.ChangeSummary),
		PublishedAt:       timePtrFromSQL(row.PublishedAt),
		Title:             stringPtrFromSQL(row.Title),
		ThumbnailImageURL: stringPtrFromSQL(row.ThumbnailImageURL),
		Metadata:          metadata,
		IdempotencyKey:    stringPtrFromSQL(row.IdempotencyKey),
		CreatedBy:         row.CreatedBy,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}, nil
}

type modelRevisionRow struct {
	ID                uuid.UUID      `db:"id"`
	EntryID           string         `db:"entry_id"`
	ModelID           string         `db:"model_id"`
	ParentRevisionID  uuid.NullUUID  `db:"parent_revision_id"`
	PrimaryArtifactID uuid.NullUUID  `db:"primary_artifact_id"`
	RevisionNumber    sql.NullInt64  `db:"revision_number"`
	State             string         `db:"state"`
	ModelState        string         `db:"model_state"`
	ChangeSummary     sql.NullString `db:"change_summary"`
	PublishedAt       sql.NullTime   `db:"published_at"`
	Title             sql.NullString `db:"title"`
	ThumbnailImageURL sql.NullString `db:"thumbnail_image_url"`
	Metadata          []byte         `db:"metadata"`
	IdempotencyKey    sql.NullString `db:"idempotency_key"`
	CreatedBy         uuid.UUID      `db:"created_by"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}
