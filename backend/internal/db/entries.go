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
	ErrEntryRevisionNotFound          = errors.New("db: entry revision not found")
	ErrEntryRevisionOwnershipMismatch = errors.New("db: entry revision ownership mismatch")
)

type EntriesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type EntryRevisionFilters struct {
	ID              *uuid.UUID
	EntryID         *uuid.UUID
	State           *models.RevisionState
	EntryState      *models.EntryState
	CreatedBy       *uuid.UUID
	PDBIDs          []string
	Limit           *int
	Offset          *int
	Query           string
	ProteinSequence string
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

	query := `with ensured_entry as (
			    insert into entries(id, created_by, created_at)
			    values (:entry_id, :created_by, :created_at)
			    on conflict (id) do nothing
			    returning id
			  )
			  insert into entry_revisions(
			    id,
			    entry_id,
			    parent_revision_id,
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
			    :entry_id,
			    :parent_revision_id,
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
			  returning id, entry_id, parent_revision_id, revision_number, state, change_summary,
			            published_at, name, description, thumbnail_image_url, metadata, created_by,
			            created_at, updated_at`

	var row entryRevisionRow
	args := map[string]any{
		"id":                  revision.ID,
		"entry_id":            revision.EntryID,
		"parent_revision_id":  revision.ParentRevisionID,
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

func (r *EntriesRepository) Delete(ctx context.Context, entryID, revisionID, ownerID uuid.UUID) error {
	query := `with target_revision as (
			    select id, entry_id, created_by
			    from entry_revisions
			    where entry_id = $1 and id = $2
			  ),
			  authorized_revision as (
			    select id, entry_id
			    from target_revision
			    where created_by = $3
			  ),
			  updated_revision as (
			    update entry_revisions
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
		entryID,
		revisionID,
		ownerID,
		models.RevisionStateDeleted,
	); err != nil {
		return fmt.Errorf("mark entry revision deleted: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrEntryRevisionNotFound
	}
	if result.DeletedCount == 0 {
		return ErrEntryRevisionOwnershipMismatch
	}
	return nil
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
	EntryID           uuid.UUID      `db:"entry_id"`
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
