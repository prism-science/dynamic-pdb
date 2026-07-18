package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

var ErrExperimentNotFound = errors.New("db: experiment not found")

type ExperimentsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type ExperimentFilters struct {
	EntryID *uuid.UUID
	Limit   *int
	Offset  *int
}

func NewExperimentsRepository(database *sqlx.DB, queriers *QuerierProvider) *ExperimentsRepository {
	return &ExperimentsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *ExperimentsRepository) Create(ctx context.Context, experiment models.Experiment) (*models.Experiment, error) {
	query := `insert into experiments(id, entry_id, name, thumbnail_image_url, created_at, updated_at)
			  values (:id, :entry_id, :name, :thumbnail_image_url, :created_at, :updated_at)
			  returning id, entry_id, name, thumbnail_image_url, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row experimentRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":                  experiment.ID,
		"entry_id":            experiment.EntryID,
		"name":                experiment.Name,
		"thumbnail_image_url": nullableString(experiment.ThumbnailImageURL),
		"created_at":          experiment.CreatedAt,
		"updated_at":          experiment.UpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert experiment: %w", err)
	}

	return experimentFromRow(&row), nil
}

func (r *ExperimentsRepository) Get(ctx context.Context, entryID, id uuid.UUID) (*models.Experiment, error) {
	query := `select id, entry_id, name, thumbnail_image_url, created_at, updated_at
			  from experiments
			  where entry_id = $1 and id = $2`

	var row experimentRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, entryID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrExperimentNotFound
		}
		return nil, fmt.Errorf("failed to get experiment: %w", err)
	}

	return experimentFromRow(&row), nil
}

func (r *ExperimentsRepository) List(ctx context.Context, filters ExperimentFilters) ([]models.Experiment, error) {
	query, args, err := experimentListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build experiment list query: %w", err)
	}

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryxContext(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to list experiments: %w", err)
	}
	defer rows.Close()

	experiments := make([]models.Experiment, 0)
	for rows.Next() {
		var row experimentRow
		if err := rows.StructScan(&row); err != nil {
			return nil, fmt.Errorf("scan experiment row: %w", err)
		}
		experiments = append(experiments, *experimentFromRow(&row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate experiment rows: %w", err)
	}

	return experiments, nil
}

func experimentListQuery(filters ExperimentFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	conditions := make([]string, 0)
	args := map[string]any{}
	if filters.EntryID != nil {
		conditions = append(conditions, "entry_id = :entry_id")
		args["entry_id"] = *filters.EntryID
	}

	query := `select id, entry_id, name, thumbnail_image_url, created_at, updated_at
			  from experiments`
	if len(conditions) > 0 {
		query += "\nwhere " + conditions[0]
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

func experimentFromRow(row *experimentRow) *models.Experiment {
	return &models.Experiment{
		ID:                row.ID,
		EntryID:           row.EntryID,
		Name:              row.Name,
		ThumbnailImageURL: stringPtrFromNull(row.ThumbnailImageURL),
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

type experimentRow struct {
	ID                uuid.UUID      `db:"id"`
	EntryID           uuid.UUID      `db:"entry_id"`
	Name              string         `db:"name"`
	ThumbnailImageURL sql.NullString `db:"thumbnail_image_url"`
	CreatedAt         time.Time      `db:"created_at"`
	UpdatedAt         time.Time      `db:"updated_at"`
}
