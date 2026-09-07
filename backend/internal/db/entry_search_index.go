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
	EntryID    string    `db:"entry_id"`
	ModelType  string    `db:"model_type"`
	ModelID    string    `db:"model_id"`
	UpdatedAt  time.Time `db:"updated_at"`
	SearchText string    `db:"search_text"`
}

const (
	maxEntrySearchTextChars = 128 * 1024

	entrySearchModelTypeEntryRevision = "entry_revision"
	entrySearchModelTypeModelRevision = "model_revision"
)

func NewEntrySearchIndexRepository(database *sqlx.DB, queriers *QuerierProvider) *EntrySearchIndexRepository {
	return &EntrySearchIndexRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *EntrySearchIndexRepository) IndexEntryRevision(
	ctx context.Context,
	revision models.EntryRevision,
) error {
	return r.saveSearchRow(ctx, entrySearchIndexRow{
		EntryID:    revision.EntryID,
		ModelType:  entrySearchModelTypeEntryRevision,
		ModelID:    revision.ID.String(),
		UpdatedAt:  time.Now().UTC(),
		SearchText: searchTextFromParts(entryRevisionSearchParts(revision)...),
	})
}

func (r *EntrySearchIndexRepository) IndexModelRevision(
	ctx context.Context,
	revision models.ModelRevision,
) error {
	return r.saveSearchRow(ctx, entrySearchIndexRow{
		EntryID:    revision.EntryID,
		ModelType:  entrySearchModelTypeModelRevision,
		ModelID:    revision.ID.String(),
		UpdatedAt:  time.Now().UTC(),
		SearchText: searchTextFromParts(modelRevisionSearchParts(revision)...),
	})
}

func (r *EntrySearchIndexRepository) DeleteEntryRevision(
	ctx context.Context,
	entryID string,
	revisionID uuid.UUID,
) error {
	if err := r.deleteSearchRow(ctx, entryID, entrySearchModelTypeEntryRevision, revisionID.String()); err != nil {
		return fmt.Errorf("delete entry revision search row: %w", err)
	}
	return nil
}

func (r *EntrySearchIndexRepository) DeleteEntry(ctx context.Context, entryID string) error {
	query := `delete from entry_search_index where entry_id = $1`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, entryID); err != nil {
		return fmt.Errorf("delete entry search rows: %w", err)
	}
	return nil
}

func (r *EntrySearchIndexRepository) DeleteModelRevision(
	ctx context.Context,
	revision models.ModelRevision,
) error {
	if err := r.deleteSearchRow(ctx, revision.EntryID, entrySearchModelTypeModelRevision, revision.ID.String()); err != nil {
		return fmt.Errorf("delete model revision search row: %w", err)
	}
	return nil
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
		return fmt.Errorf("save entry search row: %w", err)
	}
	return nil
}

func (r *EntrySearchIndexRepository) deleteSearchRow(
	ctx context.Context,
	entryID string,
	modelType string,
	modelID string,
) error {
	query := `delete from entry_search_index
			  where entry_id = $1 and model_type = $2 and model_id = $3`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, entryID, modelType, modelID); err != nil {
		return fmt.Errorf("delete entry search row: %w", err)
	}
	return nil
}

func entryRevisionSearchParts(revision models.EntryRevision) []string {
	parts := []string{
		stringFromPtr(revision.Title),
		stringFromPtr(revision.Metadata.SpaceGroup),
	}

	if revision.Metadata.Method != nil {
		parts = append(parts, string(*revision.Metadata.Method))
	}
	for _, externalRef := range revision.Metadata.ExternalRefs {
		parts = append(parts, externalRef)
	}

	return parts
}

func modelRevisionSearchParts(revision models.ModelRevision) []string {
	parts := []string{
		stringFromPtr(revision.Title),
		strings.Join(revision.Metadata.Authors, " "),
		stringFromPtr(revision.Metadata.Affiliation),
		strings.Join(revision.Metadata.Ligands, " "),
	}

	if revision.Metadata.Purpose != nil {
		parts = append(parts, string(*revision.Metadata.Purpose))
	}
	if revision.Metadata.ModelType != nil {
		parts = append(parts, string(*revision.Metadata.ModelType))
	}

	return parts
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
