package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

var ErrPolymerEntityNotFound = errors.New("db: polymer entity not found")

type PolymerEntitiesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type PolymerEntityFilters struct {
	ID                *uuid.UUID
	EntryRevisionID   *uuid.UUID
	ProteinSequenceID *uuid.UUID
	Limit             *int
	Offset            *int
}

func NewPolymerEntitiesRepository(
	database *sqlx.DB,
	queriers *QuerierProvider,
) *PolymerEntitiesRepository {
	return &PolymerEntitiesRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *PolymerEntitiesRepository) Create(
	ctx context.Context,
	entity models.PolymerEntity,
) (*models.PolymerEntity, error) {
	metadata, err := marshalJSON(entity.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare polymer entity metadata: %w", err)
	}

	query := `insert into polymer_entities(id, protein_sequence_id, metadata, created_at)
			  values (:id, :protein_sequence_id, cast(:metadata as jsonb), :created_at)
			  returning id, protein_sequence_id, metadata, created_at`
	args := map[string]any{
		"id":                  entity.ID,
		"protein_sequence_id": entity.ProteinSequenceID,
		"metadata":            metadata,
		"created_at":          entity.CreatedAt,
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind polymer entity insert query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	var row polymerEntityRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("insert polymer entity: %w", err)
	}

	created, err := polymerEntityFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode inserted polymer entity: %w", err)
	}
	return created, nil
}

func (r *PolymerEntitiesRepository) Get(
	ctx context.Context,
	filters PolymerEntityFilters,
) (*models.PolymerEntity, error) {
	entities, err := r.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list polymer entities for get: %w", err)
	}
	if len(entities) == 0 {
		return nil, ErrPolymerEntityNotFound
	}
	if len(entities) > 1 {
		return nil, fmt.Errorf("polymer entity get returned %d rows", len(entities))
	}
	return &entities[0], nil
}

func (r *PolymerEntitiesRepository) List(
	ctx context.Context,
	filters PolymerEntityFilters,
) ([]models.PolymerEntity, error) {
	query, args, err := polymerEntityListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build polymer entity list query: %w", err)
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind polymer entity list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	rows := make([]polymerEntityRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list polymer entities: %w", err)
	}

	entities := make([]models.PolymerEntity, 0, len(rows))
	for _, row := range rows {
		entity, err := polymerEntityFromRow(&row)
		if err != nil {
			return nil, fmt.Errorf("decode polymer entity: %w", err)
		}
		entities = append(entities, *entity)
	}
	return entities, nil
}

func (r *PolymerEntitiesRepository) AttachToEntryRevision(
	ctx context.Context,
	entryRevisionID uuid.UUID,
	polymerEntityID uuid.UUID,
) error {
	query := `insert into entry_revision_polymer_entities(entry_revision_id, polymer_entity_id)
			  values ($1, $2)
			  on conflict (entry_revision_id, polymer_entity_id) do nothing`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx,
		query,
		entryRevisionID,
		polymerEntityID,
	); err != nil {
		return fmt.Errorf("attach polymer entity to entry revision: %w", err)
	}
	return nil
}

func (r *PolymerEntitiesRepository) CopyEntryRevisionLinks(
	ctx context.Context,
	fromRevisionID uuid.UUID,
	toRevisionID uuid.UUID,
) error {
	query := `insert into entry_revision_polymer_entities(entry_revision_id, polymer_entity_id)
			  select $2, polymer_entity_id
			  from entry_revision_polymer_entities
			  where entry_revision_id = $1
			  on conflict (entry_revision_id, polymer_entity_id) do nothing`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx,
		query,
		fromRevisionID,
		toRevisionID,
	); err != nil {
		return fmt.Errorf("copy entry revision polymer entity links: %w", err)
	}
	return nil
}

func polymerEntityListQuery(filters PolymerEntityFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select polymer_entities.id, polymer_entities.protein_sequence_id,
			         polymer_entities.metadata, polymer_entities.created_at
			  from polymer_entities`

	if filters.EntryRevisionID != nil {
		query += "\njoin entry_revision_polymer_entities on entry_revision_polymer_entities.polymer_entity_id = polymer_entities.id"
		conditions = append(conditions, "entry_revision_polymer_entities.entry_revision_id = :entry_revision_id")
		args["entry_revision_id"] = *filters.EntryRevisionID
	}
	if filters.ID != nil {
		conditions = append(conditions, "polymer_entities.id = :id")
		args["id"] = *filters.ID
	}
	if filters.ProteinSequenceID != nil {
		conditions = append(conditions, "polymer_entities.protein_sequence_id = :protein_sequence_id")
		args["protein_sequence_id"] = *filters.ProteinSequenceID
	}
	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by polymer_entities.created_at asc, polymer_entities.id asc"

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

func polymerEntityFromRow(row *polymerEntityRow) (*models.PolymerEntity, error) {
	metadata := models.PolymerEntityMetadata{}
	if err := unmarshalJSON(row.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	return &models.PolymerEntity{
		ID:                row.ID,
		ProteinSequenceID: row.ProteinSequenceID,
		Metadata:          metadata,
		CreatedAt:         row.CreatedAt,
	}, nil
}

type polymerEntityRow struct {
	ID                uuid.UUID `db:"id"`
	ProteinSequenceID uuid.UUID `db:"protein_sequence_id"`
	Metadata          []byte    `db:"metadata"`
	CreatedAt         time.Time `db:"created_at"`
}
