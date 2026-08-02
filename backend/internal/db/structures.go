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
	ErrStructureNotFound          = errors.New("db: structure not found")
	ErrStructureOwnershipMismatch = errors.New("db: structure ownership mismatch")
)

type StructuresRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type StructureFilters struct {
	Limit           *int
	Offset          *int
	Query           string
	ProteinSequence string
}

func NewStructuresRepository(database *sqlx.DB, queriers *QuerierProvider) *StructuresRepository {
	return &StructuresRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *StructuresRepository) Create(ctx context.Context, structure models.Structure) (*models.Structure, error) {
	query := `insert into structures(id, created_by, name, description, thumbnail_image_url, created_at, updated_at)
			  values (:id, :created_by, :name, :description, :thumbnail_image_url, :created_at, :updated_at)
			  returning id, created_by, name, description, thumbnail_image_url, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row structureRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":                  structure.ID,
		"created_by":          structure.CreatedBy,
		"name":                structure.Name,
		"description":         nullableString(structure.Description),
		"thumbnail_image_url": nullableString(structure.ThumbnailImageURL),
		"created_at":          structure.CreatedAt,
		"updated_at":          structure.UpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert structure: %w", err)
	}

	return structureFromRow(&row), nil
}

func (r *StructuresRepository) Get(ctx context.Context, id uuid.UUID) (*models.Structure, error) {
	query := `select id, created_by, name, description, thumbnail_image_url, created_at, updated_at
			  from structures
			  where id = $1`

	var row structureRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStructureNotFound
		}
		return nil, fmt.Errorf("failed to get structure: %w", err)
	}

	return structureFromRow(&row), nil
}

func (r *StructuresRepository) Delete(ctx context.Context, id, ownerID uuid.UUID) error {
	query := `with target_structure as (
			    select id, created_by
			    from structures
			    where id = $1
			  ),
			  authorized_structure as (
			    select id
			    from target_structure
			    where created_by = $2
			  ),
			  target_entities as (
			    select entities.id
			    from entities
			    join authorized_structure on authorized_structure.id = entities.structure_id
			  ),
			  deleted_relations as (
			    delete from entity_relations
			    where source_entity_id in (select id from target_entities)
			       or target_entity_id in (select id from target_entities)
			    returning id
			  ),
			  deleted_search as (
			    delete from structure_search_index
			    using authorized_structure
			    where structure_search_index.structure_id = authorized_structure.id
			      and (select count(*) from deleted_relations) >= 0
			    returning structure_search_index.structure_id
			  ),
			  deleted_entities as (
			    delete from entities
			    using authorized_structure
			    where entities.structure_id = authorized_structure.id
			      and (select count(*) from deleted_search) >= 0
			    returning entities.id
			  ),
			  deleted_models as (
			    delete from models
			    using authorized_structure
			    where models.structure_id = authorized_structure.id
			      and (select count(*) from deleted_entities) >= 0
			    returning models.id
			  ),
			  deleted_structure as (
			    delete from structures
			    using authorized_structure
			    where structures.id = authorized_structure.id
			      and (select count(*) from deleted_models) >= 0
			    returning structures.id
			  )
			  select
			    (select count(*) from target_structure) as matched_count,
			    (select count(*) from deleted_structure) as deleted_count`

	var result deleteResult
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &result, query, id, ownerID); err != nil {
		return fmt.Errorf("failed to delete structure graph: %w", err)
	}
	if result.MatchedCount == 0 {
		return ErrStructureNotFound
	}
	if result.DeletedCount == 0 {
		return ErrStructureOwnershipMismatch
	}
	return nil
}

func (r *StructuresRepository) List(ctx context.Context, filters StructureFilters) ([]models.Structure, error) {
	query, args, err := structureListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build structure list query: %w", err)
	}

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryxContext(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to list structures: %w", err)
	}
	defer rows.Close()

	structures := make([]models.Structure, 0)
	for rows.Next() {
		var row structureRow
		if err := rows.StructScan(&row); err != nil {
			return nil, fmt.Errorf("scan structure row: %w", err)
		}
		structures = append(structures, *structureFromRow(&row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate structure rows: %w", err)
	}

	return structures, nil
}

func structureListQuery(filters StructureFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	query := `select id, created_by, name, description, thumbnail_image_url, created_at, updated_at
			  from structures`

	queryText := strings.TrimSpace(filters.Query)
	proteinSequence := strings.ToUpper(strings.Join(strings.Fields(filters.ProteinSequence), ""))
	if queryText != "" && proteinSequence != "" {
		return "", nil, errors.New("query and protein sequence cannot be combined")
	}

	if queryText != "" {
		args["search_query"] = queryText
		query += "\nwhere " + structureSearchCondition()
	}
	if proteinSequence != "" {
		args["protein_sequence"] = proteinSequence
		query += "\nwhere " + structureProteinSequenceSearchCondition()
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

func structureSearchCondition() string {
	return `exists (
		select 1
		from structure_search_index idx
		where idx.structure_id = structures.id
		  and idx.search_tsv @@ plainto_tsquery('simple', :search_query)
	)`
}

func structureProteinSequenceSearchCondition() string {
	return `exists (
		select 1
		from entities sequence_entity
		where sequence_entity.structure_id = structures.id
		  and sequence_entity.type = 'data'
		  and sequence_entity.payload ->> 'type' = 'fasta'
		  and jsonb_typeof(sequence_entity.payload #> '{metadata,sequence}') = 'string'
		  and upper(sequence_entity.payload #>> '{metadata,sequence}')
		      like '%' || :protein_sequence || '%'
	)`
}

func structureFromRow(row *structureRow) *models.Structure {
	return &models.Structure{
		ID:                row.ID,
		CreatedBy:         row.CreatedBy,
		Name:              row.Name,
		Description:       stringPtrFromNull(row.Description),
		ThumbnailImageURL: stringPtrFromNull(row.ThumbnailImageURL),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringPtrFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

type structureRow struct {
	ID                uuid.UUID      `db:"id"`
	CreatedBy         uuid.UUID      `db:"created_by"`
	Name              string         `db:"name"`
	Description       sql.NullString `db:"description"`
	ThumbnailImageURL sql.NullString `db:"thumbnail_image_url"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}

type deleteResult struct {
	MatchedCount int64 `db:"matched_count"`
	DeletedCount int64 `db:"deleted_count"`
}
