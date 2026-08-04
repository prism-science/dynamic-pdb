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

type RunsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type RunFilters struct {
	ID              *uuid.UUID
	ModelRevisionID *uuid.UUID
	CreatedBy       *uuid.UUID
	Limit           *int
	Offset          *int
}

type RunArtifactLink struct {
	RunID      uuid.UUID
	ArtifactID uuid.UUID
	Direction  models.RunArtifactDirection
}

func NewRunsRepository(database *sqlx.DB, queriers *QuerierProvider) *RunsRepository {
	return &RunsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *RunsRepository) Create(ctx context.Context, run models.Run) (*models.Run, error) {
	parameters, err := marshalJSON(run.Parameters)
	if err != nil {
		return nil, fmt.Errorf("prepare run parameters: %w", err)
	}

	metadata, err := marshalJSON(run.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare run metadata: %w", err)
	}

	query := `insert into runs(
			    id,
			    name,
			    software_name,
			    software_version,
			    command,
			    parameters,
			    metadata,
			    started_at,
			    finished_at,
			    created_by,
			    created_at,
			    updated_at
			  )
			  values (
			    :id,
			    :name,
			    :software_name,
			    :software_version,
			    :command,
			    cast(:parameters as jsonb),
			    cast(:metadata as jsonb),
			    :started_at,
			    :finished_at,
			    :created_by,
			    :created_at,
			    :updated_at
			  )
			  returning id, name, software_name, software_version, command, parameters, metadata,
			            started_at, finished_at, created_by, created_at, updated_at`

	var row runRow
	args := map[string]any{
		"id":               run.ID,
		"name":             run.Name,
		"software_name":    run.SoftwareName,
		"software_version": run.SoftwareVersion,
		"command":          run.Command,
		"parameters":       parameters,
		"metadata":         metadata,
		"started_at":       run.StartedAt,
		"finished_at":      run.FinishedAt,
		"created_by":       run.CreatedBy,
		"created_at":       run.CreatedAt,
		"updated_at":       run.UpdatedAt,
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind run insert query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}

	created, err := runFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode inserted run: %w", err)
	}
	return created, nil
}

func (r *RunsRepository) List(ctx context.Context, filters RunFilters) ([]models.Run, error) {
	query, args, err := runListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build run list query: %w", err)
	}

	rows := make([]runRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind run list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}

	runs := make([]models.Run, 0, len(rows))
	for _, row := range rows {
		run, err := runFromRow(&row)
		if err != nil {
			return nil, fmt.Errorf("decode run: %w", err)
		}
		runs = append(runs, *run)
	}
	return runs, nil
}

func (r *RunsRepository) AttachToModelRevision(
	ctx context.Context,
	modelRevisionID uuid.UUID,
	runID uuid.UUID,
) error {
	query := `insert into model_revision_runs(model_revision_id, run_id)
			  values ($1, $2)
			  on conflict (model_revision_id, run_id) do nothing`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, modelRevisionID, runID); err != nil {
		return fmt.Errorf("attach run to model revision: %w", err)
	}
	return nil
}

func (r *RunsRepository) AttachArtifact(
	ctx context.Context,
	runID uuid.UUID,
	artifactID uuid.UUID,
	direction models.RunArtifactDirection,
) error {
	query := `insert into run_artifacts(run_id, artifact_id, direction)
			  values ($1, $2, $3)
			  on conflict (run_id, artifact_id)
			  do update set direction = excluded.direction`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, runID, artifactID, string(direction)); err != nil {
		return fmt.Errorf("attach artifact to run: %w", err)
	}
	return nil
}

func (r *RunsRepository) ListArtifactLinks(
	ctx context.Context,
	modelRevisionID uuid.UUID,
) ([]RunArtifactLink, error) {
	query := `select run_artifacts.run_id, run_artifacts.artifact_id, run_artifacts.direction
			  from run_artifacts
			  join model_revision_runs on model_revision_runs.run_id = run_artifacts.run_id
			  where model_revision_runs.model_revision_id = $1
			  order by run_artifacts.run_id asc, run_artifacts.artifact_id asc`

	rows := make([]runArtifactLinkRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, modelRevisionID); err != nil {
		return nil, fmt.Errorf("list run artifact links: %w", err)
	}

	links := make([]RunArtifactLink, 0, len(rows))
	for _, row := range rows {
		links = append(links, RunArtifactLink{
			RunID:      row.RunID,
			ArtifactID: row.ArtifactID,
			Direction:  models.RunArtifactDirection(row.Direction),
		})
	}
	return links, nil
}

func runListQuery(filters RunFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select id, name, software_name, software_version, command, parameters, metadata,
			         started_at, finished_at, created_by, created_at, updated_at
			  from runs`

	if filters.ModelRevisionID != nil {
		query += "\njoin model_revision_runs on model_revision_runs.run_id = runs.id"
		conditions = append(conditions, "model_revision_runs.model_revision_id = :model_revision_id")
		args["model_revision_id"] = *filters.ModelRevisionID
	}
	if filters.ID != nil {
		conditions = append(conditions, "runs.id = :id")
		args["id"] = *filters.ID
	}
	if filters.CreatedBy != nil {
		conditions = append(conditions, "runs.created_by = :created_by")
		args["created_by"] = *filters.CreatedBy
	}

	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by runs.created_at asc, runs.id asc"

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

func runFromRow(row *runRow) (*models.Run, error) {
	parameters := map[string]any{}
	if err := unmarshalJSON(row.Parameters, &parameters); err != nil {
		return nil, fmt.Errorf("decode parameters: %w", err)
	}

	metadata := map[string]any{}
	if err := unmarshalJSON(row.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	return &models.Run{
		ID:              row.ID,
		Name:            row.Name,
		SoftwareName:    stringPtrFromSQL(row.SoftwareName),
		SoftwareVersion: stringPtrFromSQL(row.SoftwareVersion),
		Command:         stringPtrFromSQL(row.Command),
		Parameters:      parameters,
		Metadata:        metadata,
		StartedAt:       timePtrFromSQL(row.StartedAt),
		FinishedAt:      timePtrFromSQL(row.FinishedAt),
		CreatedBy:       row.CreatedBy,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}, nil
}

type runRow struct {
	ID              uuid.UUID      `db:"id"`
	Name            string         `db:"name"`
	SoftwareName    sql.NullString `db:"software_name"`
	SoftwareVersion sql.NullString `db:"software_version"`
	Command         sql.NullString `db:"command"`
	Parameters      []byte         `db:"parameters"`
	Metadata        []byte         `db:"metadata"`
	StartedAt       sql.NullTime   `db:"started_at"`
	FinishedAt      sql.NullTime   `db:"finished_at"`
	CreatedBy       uuid.UUID      `db:"created_by"`
	CreatedAt       time.Time      `db:"created_at"`
	UpdatedAt       time.Time      `db:"updated_at"`
}

type runArtifactLinkRow struct {
	RunID      uuid.UUID `db:"run_id"`
	ArtifactID uuid.UUID `db:"artifact_id"`
	Direction  string    `db:"direction"`
}
