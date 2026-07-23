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

var ErrEntryNotFound = errors.New("db: entry not found")

type EntriesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type EntryFilters struct {
	Limit  *int
	Offset *int
	Query  string
}

func NewEntriesRepository(database *sqlx.DB, queriers *QuerierProvider) *EntriesRepository {
	return &EntriesRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *EntriesRepository) Create(ctx context.Context, entry models.Entry) (*models.Entry, error) {
	query := `insert into entries(id, created_by, name, description, thumbnail_image_url, created_at, updated_at)
			  values (:id, :created_by, :name, :description, :thumbnail_image_url, :created_at, :updated_at)
			  returning id, created_by, name, description, thumbnail_image_url, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row entryRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":                  entry.ID,
		"created_by":          entry.CreatedBy,
		"name":                entry.Name,
		"description":         nullableString(entry.Description),
		"thumbnail_image_url": nullableString(entry.ThumbnailImageURL),
		"created_at":          entry.CreatedAt,
		"updated_at":          entry.UpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert entry: %w", err)
	}

	return entryFromRow(&row), nil
}

func (r *EntriesRepository) Get(ctx context.Context, id uuid.UUID) (*models.Entry, error) {
	query := `select id, created_by, name, description, thumbnail_image_url, created_at, updated_at
			  from entries
			  where id = $1`

	var row entryRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntryNotFound
		}
		return nil, fmt.Errorf("failed to get entry: %w", err)
	}

	return entryFromRow(&row), nil
}

func (r *EntriesRepository) List(ctx context.Context, filters EntryFilters) ([]models.Entry, error) {
	query, args, err := entryListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build entry list query: %w", err)
	}

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryxContext(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to list entries: %w", err)
	}
	defer rows.Close()

	entries := make([]models.Entry, 0)
	for rows.Next() {
		var row entryRow
		if err := rows.StructScan(&row); err != nil {
			return nil, fmt.Errorf("scan entry row: %w", err)
		}
		entries = append(entries, *entryFromRow(&row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entry rows: %w", err)
	}

	return entries, nil
}

func entryListQuery(filters EntryFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	query := `select id, created_by, name, description, thumbnail_image_url, created_at, updated_at
			  from entries`

	if queryText := strings.TrimSpace(filters.Query); queryText != "" {
		query += "\nwhere " + entrySearchCondition()
		args["search_query"] = queryText
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

func entrySearchCondition() string {
	return `exists (
		select 1
		from entry_search_index idx
		where idx.entry_id = entries.id
		  and idx.search_tsv @@ plainto_tsquery('simple', :search_query)
	)`
}

func entryFromRow(row *entryRow) *models.Entry {
	return &models.Entry{
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

type entryRow struct {
	ID                uuid.UUID      `db:"id"`
	CreatedBy         uuid.UUID      `db:"created_by"`
	Name              string         `db:"name"`
	Description       sql.NullString `db:"description"`
	ThumbnailImageURL sql.NullString `db:"thumbnail_image_url"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}
