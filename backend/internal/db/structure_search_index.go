package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

type StructureSearchIndexRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type structureSearchIndexRow struct {
	StructureID uuid.UUID `db:"structure_id"`
	ModelType   string    `db:"model_type"`
	ModelID     string    `db:"model_id"`
	UpdatedAt   time.Time `db:"updated_at"`
	SearchText  string    `db:"search_text"`
}

const (
	maxStructureSearchTextChars = 128 * 1024

	structureSearchModelTypeStructure = "structure"
	structureSearchModelTypeModel     = "model"
	structureSearchModelTypeEntity    = "entity"
)

func NewStructureSearchIndexRepository(database *sqlx.DB, queriers *QuerierProvider) *StructureSearchIndexRepository {
	return &StructureSearchIndexRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *StructureSearchIndexRepository) IndexStructure(ctx context.Context, structure models.Structure) error {
	return r.saveSearchRow(ctx, structureSearchIndexRow{
		StructureID: structure.ID,
		ModelType:   structureSearchModelTypeStructure,
		ModelID:     "",
		UpdatedAt:   time.Now().UTC(),
		SearchText:  searchTextFromParts(structure.Name, stringFromPtr(structure.Description)),
	})
}

func (r *StructureSearchIndexRepository) IndexModel(ctx context.Context, model models.Model) error {
	return r.saveSearchRow(ctx, structureSearchIndexRow{
		StructureID: model.StructureID,
		ModelType:   structureSearchModelTypeModel,
		ModelID:     model.ID.String(),
		UpdatedAt:   time.Now().UTC(),
		SearchText:  searchTextFromParts(model.Name, stringFromPtr(model.Description)),
	})
}

func (r *StructureSearchIndexRepository) IndexEntity(ctx context.Context, entity models.Entity) error {
	payloadParts, err := entityPayloadSearchParts(entity)
	if err != nil {
		return fmt.Errorf("prepare entity search fields: %w", err)
	}

	parts := []string{entity.Name}
	parts = append(parts, payloadParts...)

	return r.saveSearchRow(ctx, structureSearchIndexRow{
		StructureID: entity.StructureID,
		ModelType:   structureSearchModelTypeEntity,
		ModelID:     entity.ID.String(),
		UpdatedAt:   time.Now().UTC(),
		SearchText:  searchTextFromParts(parts...),
	})
}

func entityPayloadSearchParts(entity models.Entity) ([]string, error) {
	switch entity.Type {
	case models.EntityTypeData:
		payload, err := entity.Data()
		if err != nil {
			return nil, fmt.Errorf("get data payload: %w", err)
		}
		return []string{
			strings.Join(payload.Authors, " "),
			stringFromPtr(payload.Affiliation),
		}, nil
	case models.EntityTypeModel:
		payload, err := entity.Model()
		if err != nil {
			return nil, fmt.Errorf("get model payload: %w", err)
		}
		return []string{
			strings.Join(payload.Authors, " "),
			stringFromPtr(payload.Affiliation),
		}, nil
	case models.EntityTypeProgram:
		payload, err := entity.Program()
		if err != nil {
			return nil, fmt.Errorf("get program payload: %w", err)
		}
		return []string{
			payload.Name,
			payload.Version,
			payload.Description,
		}, nil
	case models.EntityTypeMetrics:
		if _, err := entity.Metrics(); err != nil {
			return nil, fmt.Errorf("get metrics payload: %w", err)
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: %s", models.ErrUnexpectedEntityType, entity.Type)
	}
}

func (r *StructureSearchIndexRepository) saveSearchRow(ctx context.Context, row structureSearchIndexRow) error {
	query := `insert into structure_search_index(structure_id, model_type, model_id, updated_at, search_text, search_tsv)
			  values (:structure_id, :model_type, :model_id, :updated_at, :search_text, to_tsvector('simple', :search_text))
			  on conflict (structure_id, model_type, model_id)
			  do update set updated_at = excluded.updated_at,
			                search_text = excluded.search_text,
			                search_tsv = excluded.search_tsv`

	row.SearchText = truncateSearchText(row.SearchText)
	if _, err := r.queriers.Querier(ctx, r.db).NamedExecContext(ctx, query, row); err != nil {
		return fmt.Errorf("failed to save structure search row: %w", err)
	}

	return nil
}

func searchTextFromParts(parts ...string) string {
	nonEmptyParts := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			nonEmptyParts = append(nonEmptyParts, part)
		}
	}
	return strings.Join(nonEmptyParts, " ")
}

func truncateSearchText(value string) string {
	runes := []rune(value)
	if len(runes) <= maxStructureSearchTextChars {
		return value
	}
	return string(runes[:maxStructureSearchTextChars])
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
