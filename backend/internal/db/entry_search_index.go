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

	entrySearchModelTypeEntry      = "entry"
	entrySearchModelTypeExperiment = "experiment"
	entrySearchModelTypeEntity     = "entity"
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
		SearchText: searchTextFromParts(entry.ID.String(), entry.Name, stringFromPtr(entry.Description), stringFromPtr(entry.ThumbnailImageURL)),
	})
}

func (r *EntrySearchIndexRepository) IndexExperiment(ctx context.Context, experiment models.Experiment) error {
	return r.saveSearchRow(ctx, entrySearchIndexRow{
		EntryID:    experiment.EntryID,
		ModelType:  entrySearchModelTypeExperiment,
		ModelID:    experiment.ID.String(),
		UpdatedAt:  time.Now().UTC(),
		SearchText: searchTextFromParts(experiment.ID.String(), experiment.Name, stringFromPtr(experiment.Description), stringFromPtr(experiment.ThumbnailImageURL)),
	})
}

func (r *EntrySearchIndexRepository) IndexEntity(ctx context.Context, entity models.Entity) error {
	payload, err := marshalEntityPayload(entity.Payload)
	if err != nil {
		return fmt.Errorf("prepare entity search payload: %w", err)
	}

	parts := []string{
		entity.ID.String(),
		uuidPtrString(entity.ExperimentID),
		string(entity.Type),
		entityLevelPtrString(entity.Level),
		entity.Name,
		payload,
	}

	return r.saveSearchRow(ctx, entrySearchIndexRow{
		EntryID:    entity.EntryID,
		ModelType:  entrySearchModelTypeEntity,
		ModelID:    entity.ID.String(),
		UpdatedAt:  time.Now().UTC(),
		SearchText: searchTextFromParts(parts...),
	})
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

func uuidPtrString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func entityLevelPtrString(value *models.EntityLevel) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
