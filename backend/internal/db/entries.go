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
	ErrEntryRevisionNotFound = errors.New("db: entry revision not found")
	ErrEntryRevisionConflict = errors.New("db: entry revision not in the expected state")
)

type EntriesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type EntryRevisionFilters struct {
	ID              *uuid.UUID
	EntryID         *string
	State           *models.RevisionState
	States          []models.RevisionState
	EntryState      *models.EntryState
	CreatedBy       *uuid.UUID
	PDBIDs          []string
	Limit           *int
	Offset          *int
	Query           string
	ProteinSequence string
}

func (r *EntriesRepository) SetRevisionState(
	ctx context.Context,
	entryID string,
	revisionID uuid.UUID,
	from []models.RevisionState,
	to models.RevisionState,
) (*models.EntryRevision, error) {
	fromStates := make([]string, 0, len(from))
	for _, state := range from {
		fromStates = append(fromStates, string(state))
	}
	query := `update entry_revisions
			  set state = $4, updated_at = now()
			  where id = $2
			    and entry_id = $1
			    and state = any($3::text[])
			  returning id, entry_id, parent_revision_id, revision_number, state, entry_state, change_summary,
			            published_at, name, description, thumbnail_image_url, metadata, created_by,
			            created_at, updated_at`
	updated, err := r.getRevisionWithArgs(ctx, query, entryID, revisionID, pq.Array(fromStates), string(to))
	if errors.Is(err, ErrEntryRevisionNotFound) {
		return nil, ErrEntryRevisionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("set entry revision state: %w", err)
	}
	return updated, nil
}

func (r *EntriesRepository) ActivateRevision(
	ctx context.Context,
	entryID string,
	revisionID uuid.UUID,
) (*models.EntryRevision, error) {
	querier := r.queriers.Querier(ctx, r.db)
	if _, err := querier.ExecContext(
		ctx,
		`update entry_revisions
		 set state = 'archived', updated_at = now()
		 where entry_id = $1 and state = 'active'`,
		entryID,
	); err != nil {
		return nil, fmt.Errorf("archive active entry revision: %w", err)
	}

	query := `update entry_revisions
			  set state = 'active',
			      published_at = now(),
			      revision_number = coalesce(
			          (select max(revision_number) from entry_revisions where entry_id = $1), 0) + 1,
			      updated_at = now()
			  where id = $2
			    and entry_id = $1
			    and state = 'in_review'
			  returning id, entry_id, parent_revision_id, revision_number, state, entry_state, change_summary,
			            published_at, name, description, thumbnail_image_url, metadata, created_by,
			            created_at, updated_at`
	updated, err := r.getRevisionWithArgs(ctx, query, entryID, revisionID)
	if errors.Is(err, ErrEntryRevisionNotFound) {
		return nil, ErrEntryRevisionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("activate entry revision: %w", err)
	}
	if _, err := querier.ExecContext(
		ctx,
		`update entries set state = $2 where id = $1`,
		entryID,
		string(updated.EntryState),
	); err != nil {
		return nil, fmt.Errorf("apply active entry state: %w", err)
	}
	return updated, nil
}

func (r *EntriesRepository) RejectRevision(
	ctx context.Context,
	entryID string,
	revisionID uuid.UUID,
) (*models.EntryRevision, error) {
	query := `update entry_revisions
			  set state = 'rejected',
			      updated_at = now()
			  where id = $2
			    and entry_id = $1
			    and state = 'in_review'
			  returning id, entry_id, parent_revision_id, revision_number, state, entry_state, change_summary,
			            published_at, name, description, thumbnail_image_url, metadata, created_by,
			            created_at, updated_at`
	updated, err := r.getRevisionWithArgs(ctx, query, entryID, revisionID)
	if errors.Is(err, ErrEntryRevisionNotFound) {
		return nil, ErrEntryRevisionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("reject entry revision: %w", err)
	}
	return updated, nil
}

var liveRevisionStates = []models.RevisionState{
	models.RevisionStatePending,
	models.RevisionStateInReview,
	models.RevisionStateActive,
	models.RevisionStateRejected,
}

func NewEntriesRepository(database *sqlx.DB, queriers *QuerierProvider) *EntriesRepository {
	return &EntriesRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *EntriesRepository) Create(ctx context.Context, revision models.EntryRevision) (*models.EntryRevision, error) {
	metadata, err := marshalJSON(revision.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare entry revision metadata: %w", err)
	}
	entryState := revision.EntryState
	if entryState == "" {
		entryState = models.EntryStateActive
	}

	query := `with ensured_entry as (
			    insert into entries(id, state, created_by, created_at)
			    values (:entry_id, 'new', :created_by, :created_at)
			    on conflict (id) do nothing
			    returning id
			  )
			  insert into entry_revisions(
			    id,
			    entry_id,
			    parent_revision_id,
			    revision_number,
			    state,
			    entry_state,
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
			    :entry_id,
			    :parent_revision_id,
			    :revision_number,
			    :state,
			    :entry_state,
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
			  returning id, entry_id, parent_revision_id, revision_number, state, entry_state, change_summary,
			            published_at, name, description, thumbnail_image_url, metadata, created_by,
			            created_at, updated_at`

	var row entryRevisionRow
	args := map[string]any{
		"id":                  revision.ID,
		"entry_id":            revision.EntryID,
		"parent_revision_id":  revision.ParentRevisionID,
		"revision_number":     revision.RevisionNumber,
		"state":               string(revision.State),
		"entry_state":         string(entryState),
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
		return nil, fmt.Errorf("bind entry revision insert query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("insert entry revision: %w", err)
	}

	created, err := entryRevisionFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode inserted entry revision: %w", err)
	}
	return created, nil
}

func (r *EntriesRepository) Get(ctx context.Context, filters EntryRevisionFilters) (*models.EntryRevision, error) {
	revisions, err := r.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list entry revisions for get: %w", err)
	}
	if len(revisions) == 0 {
		return nil, ErrEntryRevisionNotFound
	}
	if len(revisions) > 1 {
		return nil, fmt.Errorf("entry revision get returned %d rows", len(revisions))
	}
	return &revisions[0], nil
}

func (r *EntriesRepository) Lock(ctx context.Context, entryID string) error {
	var lockedEntryID string
	if err := r.queriers.Querier(ctx, r.db).GetContext(
		ctx,
		&lockedEntryID,
		`select id from entries where id = $1 for update`,
		entryID,
	); err != nil {
		return fmt.Errorf("lock entry: %w", err)
	}
	return nil
}

func (r *EntriesRepository) List(
	ctx context.Context,
	filters EntryRevisionFilters,
) ([]models.EntryRevision, error) {
	query, args, err := entryRevisionListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build entry revision list query: %w", err)
	}

	rows := make([]entryRevisionRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind entry revision list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list entry revisions: %w", err)
	}

	revisions := make([]models.EntryRevision, 0, len(rows))
	for _, row := range rows {
		revision, err := entryRevisionFromRow(&row)
		if err != nil {
			return nil, fmt.Errorf("decode entry revision: %w", err)
		}
		revisions = append(revisions, *revision)
	}
	return revisions, nil
}

// GetLive returns the latest non-archived revision of an entry. Because an
// update is always a brand-new entry, there is at most one such revision.
func (r *EntriesRepository) GetLive(ctx context.Context, entryID string) (*models.EntryRevision, error) {
	revisions, err := r.List(ctx, EntryRevisionFilters{
		EntryID: &entryID,
		States:  liveRevisionStates,
	})
	if err != nil {
		return nil, fmt.Errorf("list live entry revisions: %w", err)
	}
	if len(revisions) == 0 {
		return nil, ErrEntryRevisionNotFound
	}
	// List orders by created_at asc; the newest live revision is authoritative.
	return &revisions[len(revisions)-1], nil
}

// SaveDraft rewrites the editable fields (and optionally the state) of an
// entry's draft. Only revisions still in the pending or rejected state can be
// written; anything else yields ErrEntryRevisionConflict.
func (r *EntriesRepository) SaveDraft(ctx context.Context, revision models.EntryRevision) (*models.EntryRevision, error) {
	metadata, err := marshalJSON(revision.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare entry revision metadata: %w", err)
	}

	query := `update entry_revisions
			  set name = :name,
			      description = :description,
			      thumbnail_image_url = :thumbnail_image_url,
			      metadata = cast(:metadata as jsonb),
			      state = :state,
			      updated_at = now()
			  where id = :id
			    and state in ('pending', 'rejected')
			  returning id, entry_id, parent_revision_id, revision_number, state, change_summary,
			            published_at, name, description, thumbnail_image_url, metadata, created_by,
			            created_at, updated_at`

	updated, err := r.getRevision(ctx, query, map[string]any{
		"id":                  revision.ID,
		"name":                revision.Name,
		"description":         revision.Description,
		"thumbnail_image_url": revision.ThumbnailImageURL,
		"metadata":            metadata,
		"state":               string(revision.State),
	})
	if errors.Is(err, ErrEntryRevisionNotFound) {
		return nil, ErrEntryRevisionConflict
	}
	if err != nil {
		return nil, fmt.Errorf("save entry draft: %w", err)
	}
	return updated, nil
}

func (r *EntriesRepository) getRevision(
	ctx context.Context,
	query string,
	args map[string]any,
) (*models.EntryRevision, error) {
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind entry revision query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	var row entryRevisionRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntryRevisionNotFound
		}
		return nil, fmt.Errorf("get entry revision: %w", err)
	}
	revision, err := entryRevisionFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode entry revision: %w", err)
	}
	return revision, nil
}

func (r *EntriesRepository) getRevisionWithArgs(
	ctx context.Context,
	query string,
	args ...any,
) (*models.EntryRevision, error) {
	var row entryRevisionRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, args...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntryRevisionNotFound
		}
		return nil, fmt.Errorf("get entry revision: %w", err)
	}
	revision, err := entryRevisionFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode entry revision: %w", err)
	}
	return revision, nil
}

func entryRevisionListQuery(filters EntryRevisionFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)

	queryText := strings.TrimSpace(filters.Query)
	proteinSequence := strings.ToUpper(strings.Join(strings.Fields(filters.ProteinSequence), ""))
	if queryText != "" && proteinSequence != "" {
		return "", nil, errors.New("query and protein sequence cannot be combined")
	}

	if filters.ID != nil {
		conditions = append(conditions, "id = :id")
		args["id"] = *filters.ID
	}
	if filters.EntryID != nil {
		conditions = append(conditions, "entry_id = :entry_id")
		args["entry_id"] = *filters.EntryID
	}
	if filters.State != nil {
		conditions = append(conditions, "state = :state")
		args["state"] = string(*filters.State)
	}
	if filters.EntryState != nil {
		conditions = append(conditions, `exists (
			select 1
			from entries e
			where e.id = entry_revisions.entry_id
			  and e.state = :entry_state
		)`)
		args["entry_state"] = string(*filters.EntryState)
	}
	if len(filters.States) > 0 {
		states := make([]string, 0, len(filters.States))
		for _, state := range filters.States {
			states = append(states, string(state))
		}
		conditions = append(conditions, "state = any(:states::text[])")
		args["states"] = pq.StringArray(states)
	}
	if filters.EntryState != nil {
		conditions = append(conditions, `exists (
			select 1
			from entries e
			where e.id = entry_revisions.entry_id
			  and e.state = :entry_state
		)`)
		args["entry_state"] = string(*filters.EntryState)
	}
	if filters.CreatedBy != nil {
		conditions = append(conditions, "created_by = :created_by")
		args["created_by"] = *filters.CreatedBy
	}
	pdbIDs := normalizedPDBIDs(filters.PDBIDs)
	if len(pdbIDs) > 0 {
		conditions = append(conditions, "upper(metadata #>> '{external_refs,pdb}') = any(:pdb_ids)")
		args["pdb_ids"] = pq.Array(pdbIDs)
	}
	if queryText != "" {
		conditions = append(conditions, `exists (
			select 1
			from entry_search_index idx
			where idx.entry_id = entry_revisions.entry_id
			  and idx.search_tsv @@ plainto_tsquery('simple', :search_query)
		)`)
		args["search_query"] = queryText
	}
	if proteinSequence != "" {
		conditions = append(conditions, `exists (
			select 1
			from protein_sequences protein_sequence
			where protein_sequence.entry_revision_id = entry_revisions.id
			  and protein_sequence.sequence like '%' || :protein_sequence || '%'
		)`)
		args["protein_sequence"] = proteinSequence
	}

	query := `select id, entry_id, parent_revision_id, revision_number, state,
			         entry_state,
			         change_summary,
			         published_at, name, description, thumbnail_image_url, metadata, created_by,
			         created_at, updated_at
			  from entry_revisions`
	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by created_at desc, id desc"

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

func normalizedPDBIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToUpper(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func entryRevisionFromRow(row *entryRevisionRow) (*models.EntryRevision, error) {
	var metadata models.EntryMetadata
	if err := unmarshalJSON(row.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	return &models.EntryRevision{
		ID:                row.ID,
		EntryID:           row.EntryID,
		ParentRevisionID:  uuidPtrFromSQL(row.ParentRevisionID),
		RevisionNumber:    intPtrFromSQL(row.RevisionNumber),
		State:             models.RevisionState(row.State),
		EntryState:        models.EntryState(row.EntryState),
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

type entryRevisionRow struct {
	ID                uuid.UUID      `db:"id"`
	EntryID           string         `db:"entry_id"`
	ParentRevisionID  uuid.NullUUID  `db:"parent_revision_id"`
	RevisionNumber    sql.NullInt64  `db:"revision_number"`
	State             string         `db:"state"`
	EntryState        string         `db:"entry_state"`
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
