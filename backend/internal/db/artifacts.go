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
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

type ArtifactsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type ArtifactFilters struct {
	ID              *uuid.UUID
	EntryRevisionID *uuid.UUID
	ModelRevisionID *uuid.UUID
	CreatedBy       *uuid.UUID
	Levels          []models.ArtifactLevel
	Formats         []string
	Limit           *int
	Offset          *int
}

func NewArtifactsRepository(database *sqlx.DB, queriers *QuerierProvider) *ArtifactsRepository {
	return &ArtifactsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *ArtifactsRepository) Create(ctx context.Context, artifact models.Artifact) (*models.Artifact, error) {
	metadata, err := marshalJSON(artifact.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare artifact metadata: %w", err)
	}

	query := `insert into artifacts(
			    id,
			    name,
			    level,
			    uri,
			    sha256,
			    format,
			    size_bytes,
			    metadata,
			    created_by,
			    created_at
			  )
			  values (
			    :id,
			    :name,
			    :level,
			    :uri,
			    :sha256,
			    :format,
			    :size_bytes,
			    cast(:metadata as jsonb),
			    :created_by,
			    :created_at
			  )
			  returning id, name, level, uri, sha256, format, size_bytes, metadata, created_by, created_at`

	var row artifactRow
	args := map[string]any{
		"id":         artifact.ID,
		"name":       artifact.Name,
		"level":      string(artifact.Level),
		"uri":        artifact.URI,
		"sha256":     artifact.SHA256,
		"format":     artifact.Format,
		"size_bytes": artifact.SizeBytes,
		"metadata":   metadata,
		"created_by": artifact.CreatedBy,
		"created_at": artifact.CreatedAt,
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind artifact insert query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("insert artifact: %w", err)
	}

	created, err := artifactFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode inserted artifact: %w", err)
	}
	return created, nil
}

func (r *ArtifactsRepository) List(ctx context.Context, filters ArtifactFilters) ([]models.Artifact, error) {
	query, args, err := artifactListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build artifact list query: %w", err)
	}

	rows := make([]artifactRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind artifact list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}

	artifacts := make([]models.Artifact, 0, len(rows))
	for _, row := range rows {
		artifact, err := artifactFromRow(&row)
		if err != nil {
			return nil, fmt.Errorf("decode artifact: %w", err)
		}
		artifacts = append(artifacts, *artifact)
	}
	return artifacts, nil
}

func (r *ArtifactsRepository) AttachToEntryRevision(
	ctx context.Context,
	entryRevisionID uuid.UUID,
	artifactID uuid.UUID,
) error {
	query := `insert into entry_revision_artifacts(entry_revision_id, artifact_id)
			  values ($1, $2)
			  on conflict (entry_revision_id, artifact_id) do nothing`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, entryRevisionID, artifactID); err != nil {
		return fmt.Errorf("attach artifact to entry revision: %w", err)
	}
	return nil
}

func (r *ArtifactsRepository) AttachToModelRevision(
	ctx context.Context,
	modelRevisionID uuid.UUID,
	artifactID uuid.UUID,
) error {
	query := `insert into model_revision_artifacts(model_revision_id, artifact_id)
			  values ($1, $2)
			  on conflict (model_revision_id, artifact_id) do nothing`

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, modelRevisionID, artifactID); err != nil {
		return fmt.Errorf("attach artifact to model revision: %w", err)
	}
	return nil
}

func artifactListQuery(filters ArtifactFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select id, name, level, uri, sha256, format, size_bytes, metadata, created_by, created_at
			  from artifacts`

	if filters.EntryRevisionID != nil {
		query += "\njoin entry_revision_artifacts on entry_revision_artifacts.artifact_id = artifacts.id"
		conditions = append(conditions, "entry_revision_artifacts.entry_revision_id = :entry_revision_id")
		args["entry_revision_id"] = *filters.EntryRevisionID
	}
	if filters.ModelRevisionID != nil {
		query += "\njoin model_revision_artifacts on model_revision_artifacts.artifact_id = artifacts.id"
		conditions = append(conditions, "model_revision_artifacts.model_revision_id = :model_revision_id")
		args["model_revision_id"] = *filters.ModelRevisionID
	}
	if filters.ID != nil {
		conditions = append(conditions, "artifacts.id = :id")
		args["id"] = *filters.ID
	}
	if filters.CreatedBy != nil {
		conditions = append(conditions, "artifacts.created_by = :created_by")
		args["created_by"] = *filters.CreatedBy
	}
	if len(filters.Levels) > 0 {
		conditions = append(conditions, "artifacts.level = any(cast(:levels as text[]))")
		args["levels"] = pq.Array(artifactLevelStrings(filters.Levels))
	}
	if len(filters.Formats) > 0 {
		conditions = append(conditions, "artifacts.format = any(cast(:formats as text[]))")
		args["formats"] = pq.Array(filters.Formats)
	}

	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by artifacts.created_at asc, artifacts.id asc"

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

func artifactFromRow(row *artifactRow) (*models.Artifact, error) {
	format := stringPtrFromSQL(row.Format)
	metadata, err := artifactMetadataFromStorage(format, row.Metadata)
	if err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	return &models.Artifact{
		ID:        row.ID,
		Name:      row.Name,
		Level:     models.ArtifactLevel(row.Level),
		URI:       stringPtrFromSQL(row.URI),
		SHA256:    stringPtrFromSQL(row.SHA256),
		Format:    format,
		SizeBytes: int64PtrFromSQL(row.SizeBytes),
		Metadata:  metadata,
		CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt,
	}, nil
}

func artifactMetadataFromStorage(format *string, data []byte) (any, error) {
	if format != nil && *format == "fasta" {
		var metadata models.FASTAMetadata
		if err := unmarshalJSON(data, &metadata); err != nil {
			return nil, fmt.Errorf("decode FASTA metadata: %w", err)
		}
		return &metadata, nil
	}

	metadata := map[string]any{}
	if err := unmarshalJSON(data, &metadata); err != nil {
		return nil, fmt.Errorf("decode generic metadata: %w", err)
	}
	return metadata, nil
}

func artifactLevelStrings(values []models.ArtifactLevel) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

type artifactRow struct {
	ID        uuid.UUID      `db:"id"`
	Name      string         `db:"name"`
	Level     string         `db:"level"`
	URI       sql.NullString `db:"uri"`
	SHA256    sql.NullString `db:"sha256"`
	Format    sql.NullString `db:"format"`
	SizeBytes sql.NullInt64  `db:"size_bytes"`
	Metadata  []byte         `db:"metadata"`
	CreatedBy uuid.UUID      `db:"created_by"`
	CreatedAt time.Time      `db:"created_at"`
}
