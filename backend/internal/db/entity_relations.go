package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

type EntityRelationsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewEntityRelationsRepository(database *sqlx.DB, queriers *QuerierProvider) *EntityRelationsRepository {
	return &EntityRelationsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *EntityRelationsRepository) Create(ctx context.Context, relation models.EntityRelation) (*models.EntityRelation, error) {
	query := `insert into entity_relations(id, source_entity_id, target_entity_id, relation_type, created_at, updated_at)
			  values (:id, :source_entity_id, :target_entity_id, :relation_type, :created_at, :updated_at)
			  returning id, source_entity_id, target_entity_id, relation_type, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row entityRelationRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":               relation.ID,
		"source_entity_id": relation.SourceEntityID,
		"target_entity_id": relation.TargetEntityID,
		"relation_type":    string(relation.RelationType),
		"created_at":       relation.CreatedAt,
		"updated_at":       relation.UpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert entity relation: %w", err)
	}

	created := entityRelationFromRow(&row)
	return &created, nil
}

func (r *EntityRelationsRepository) List(ctx context.Context, entryID uuid.UUID) ([]models.EntityRelation, error) {
	query := `select er.id, er.source_entity_id, er.target_entity_id, er.relation_type,
				 er.created_at, er.updated_at
			  from entity_relations er
			  join entities e on e.id = er.source_entity_id
			  where e.entry_id = $1
			  order by er.created_at asc, er.id asc`

	rows, err := r.queriers.Querier(ctx, r.db).QueryxContext(ctx, query, entryID)
	if err != nil {
		return nil, fmt.Errorf("failed to list entity relations: %w", err)
	}
	defer rows.Close()

	relations := make([]models.EntityRelation, 0)
	for rows.Next() {
		var row entityRelationRow
		if err := rows.StructScan(&row); err != nil {
			return nil, fmt.Errorf("scan entity relation row: %w", err)
		}
		relations = append(relations, entityRelationFromRow(&row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity relation rows: %w", err)
	}

	return relations, nil
}

func entityRelationFromRow(row *entityRelationRow) models.EntityRelation {
	return models.EntityRelation{
		ID:             row.ID,
		SourceEntityID: row.SourceEntityID,
		TargetEntityID: row.TargetEntityID,
		RelationType:   models.RelationType(row.RelationType),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

type entityRelationRow struct {
	ID             uuid.UUID `db:"id"`
	SourceEntityID uuid.UUID `db:"source_entity_id"`
	TargetEntityID uuid.UUID `db:"target_entity_id"`
	RelationType   string    `db:"relation_type"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}
