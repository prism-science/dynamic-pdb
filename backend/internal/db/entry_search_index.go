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

type EntrySearchIndexRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type entrySearchIndexRow struct {
	EntryID    uuid.UUID `db:"entry_id"`
	ModelType  string    `db:"model_type"`
	ModelID    string    `db:"model_id"`
	UpdatedAt  time.Time `db:"updated_at"`
	SearchText string    `db:"search_text"`
}

const (
	maxEntrySearchTextChars = 128 * 1024

	entrySearchModelTypeEntry  = "entry"
	entrySearchModelTypeModel  = "model"
	entrySearchModelTypeEntity = "entity"
)

func NewEntrySearchIndexRepository(database *sqlx.DB, queriers *QuerierProvider) *EntrySearchIndexRepository {
	return &EntrySearchIndexRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *EntrySearchIndexRepository) IndexEntry(ctx context.Context, entry models.Entry) error {
	return r.saveSearchRow(ctx, entrySearchIndexRow{
		EntryID:    entry.ID,
		ModelType:  entrySearchModelTypeEntry,
		ModelID:    "",
		UpdatedAt:  time.Now().UTC(),
		SearchText: searchTextFromParts(entry.Name, stringFromPtr(entry.Description)),
	})
}

func (r *EntrySearchIndexRepository) IndexModel(ctx context.Context, model models.Model) error {
	return r.saveSearchRow(ctx, entrySearchIndexRow{
		EntryID:    model.EntryID,
		ModelType:  entrySearchModelTypeModel,
		ModelID:    model.ID.String(),
		UpdatedAt:  time.Now().UTC(),
		SearchText: searchTextFromParts(model.Name, stringFromPtr(model.Description)),
	})
}

func (r *EntrySearchIndexRepository) IndexEntity(ctx context.Context, entity models.Entity) error {
	payloadParts, err := entityPayloadSearchParts(entity)
	if err != nil {
		return fmt.Errorf("prepare entity search fields: %w", err)
	}

	parts := []string{entity.Name}
	parts = append(parts, payloadParts...)

	return r.saveSearchRow(ctx, entrySearchIndexRow{
		EntryID:    entity.EntryID,
		ModelType:  entrySearchModelTypeEntity,
		ModelID:    entity.ID.String(),
		UpdatedAt:  time.Now().UTC(),
		SearchText: searchTextFromParts(parts...),
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

func (r *EntrySearchIndexRepository) saveSearchRow(ctx context.Context, row entrySearchIndexRow) error {
	query := `insert into entry_search_index(entry_id, model_type, model_id, updated_at, search_text, search_tsv)
			  values (:entry_id, :model_type, :model_id, :updated_at, :search_text, to_tsvector('simple', :search_text))
			  on conflict (entry_id, model_type, model_id)
			  do update set updated_at = excluded.updated_at,
			                search_text = excluded.search_text,
			                search_tsv = excluded.search_tsv`

	row.SearchText = truncateSearchText(row.SearchText)
	if _, err := r.queriers.Querier(ctx, r.db).NamedExecContext(ctx, query, row); err != nil {
		return fmt.Errorf("failed to save entry search row: %w", err)
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
	if len(runes) <= maxEntrySearchTextChars {
		return value
	}
	return string(runes[:maxEntrySearchTextChars])
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
