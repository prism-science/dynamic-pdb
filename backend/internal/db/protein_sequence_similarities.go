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

type ProteinSequenceSimilaritiesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type ProteinSequenceSimilarityRunFilters struct {
	State  *models.ProteinSequenceSimilarityRunState
	Limit  *int
	Offset *int
}

type ProteinSequenceSimilarityFilters struct {
	SourceSequenceID *uuid.UUID
	Limit            *int
	Offset           *int
}

func NewProteinSequenceSimilaritiesRepository(
	database *sqlx.DB,
	queriers *QuerierProvider,
) *ProteinSequenceSimilaritiesRepository {
	return &ProteinSequenceSimilaritiesRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *ProteinSequenceSimilaritiesRepository) CreateRun(
	ctx context.Context,
	run models.ProteinSequenceSimilarityRun,
) (*models.ProteinSequenceSimilarityRun, error) {
	parameters, err := marshalJSON(run.Parameters)
	if err != nil {
		return nil, fmt.Errorf("prepare protein sequence similarity run parameters: %w", err)
	}

	query := `insert into protein_sequence_similarity_runs(
			    id,
			    tool,
			    parameters,
			    state,
			    error_message,
			    started_at,
			    finished_at,
			    created_at
			  )
			  values (
			    :id,
			    :tool,
			    cast(:parameters as jsonb),
			    :state,
			    :error_message,
			    :started_at,
			    :finished_at,
			    :created_at
			  )
			  returning id, tool, parameters, state, error_message, started_at, finished_at, created_at`

	var row proteinSequenceSimilarityRunRow
	args := map[string]any{
		"id":            run.ID,
		"tool":          run.Tool,
		"parameters":    parameters,
		"state":         string(run.State),
		"error_message": run.ErrorMessage,
		"started_at":    run.StartedAt,
		"finished_at":   run.FinishedAt,
		"created_at":    run.CreatedAt,
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind protein sequence similarity run insert query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("insert protein sequence similarity run: %w", err)
	}

	created, err := proteinSequenceSimilarityRunFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode inserted protein sequence similarity run: %w", err)
	}
	return created, nil
}

func (r *ProteinSequenceSimilaritiesRepository) UpdateRunState(
	ctx context.Context,
	id uuid.UUID,
	state models.ProteinSequenceSimilarityRunState,
	errorMessage *string,
	finishedAt *time.Time,
) error {
	query := `update protein_sequence_similarity_runs
			  set state = :state,
			      error_message = :error_message,
			      finished_at = :finished_at
			  where id = :id`
	args := map[string]any{
		"id":            id,
		"state":         string(state),
		"error_message": errorMessage,
		"finished_at":   finishedAt,
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return fmt.Errorf("bind protein sequence similarity run state update query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, boundQuery, queryArgs...); err != nil {
		return fmt.Errorf("update protein sequence similarity run state: %w", err)
	}
	return nil
}

func (r *ProteinSequenceSimilaritiesRepository) ListRuns(
	ctx context.Context,
	filters ProteinSequenceSimilarityRunFilters,
) ([]models.ProteinSequenceSimilarityRun, error) {
	query, args, err := proteinSequenceSimilarityRunListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build protein sequence similarity run list query: %w", err)
	}

	rows := make([]proteinSequenceSimilarityRunRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind protein sequence similarity run list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list protein sequence similarity runs: %w", err)
	}

	runs := make([]models.ProteinSequenceSimilarityRun, 0, len(rows))
	for _, row := range rows {
		run, err := proteinSequenceSimilarityRunFromRow(&row)
		if err != nil {
			return nil, fmt.Errorf("decode protein sequence similarity run: %w", err)
		}
		runs = append(runs, *run)
	}
	return runs, nil
}

func (r *ProteinSequenceSimilaritiesRepository) Create(
	ctx context.Context,
	similarities []models.ProteinSequenceSimilarity,
) error {
	if len(similarities) == 0 {
		return nil
	}

	now := time.Now().UTC()
	params := make([]proteinSequenceSimilarityCreateParams, 0, len(similarities))
	for _, similarity := range similarities {
		metadata, err := marshalJSON(similarity.Metadata)
		if err != nil {
			return fmt.Errorf("prepare protein sequence similarity metadata: %w", err)
		}

		id := similarity.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		createdAt := similarity.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		params = append(params, proteinSequenceSimilarityCreateParams{
			ID:                id,
			RunID:             similarity.RunID,
			SourceSequenceID:  similarity.SourceSequenceID,
			SimilarSequenceID: similarity.SimilarSequenceID,
			Tool:              similarity.Tool,
			Score:             similarity.Score,
			Metadata:          metadata,
			CreatedAt:         createdAt,
		})
	}

	query := `insert into protein_sequence_similarities(
			    id,
			    run_id,
			    source_sequence_id,
			    similar_sequence_id,
			    tool,
			    score,
			    metadata,
			    created_at
			  )
			  values (
			    :id,
			    :run_id,
			    :source_sequence_id,
			    :similar_sequence_id,
			    :tool,
			    :score,
			    cast(:metadata as jsonb),
			    :created_at
			  )
			  on conflict (source_sequence_id, similar_sequence_id, tool) do nothing`
	if _, err := r.queriers.Querier(ctx, r.db).NamedExecContext(ctx, query, params); err != nil {
		return fmt.Errorf("insert protein sequence similarities: %w", err)
	}
	return nil
}

func (r *ProteinSequenceSimilaritiesRepository) List(
	ctx context.Context,
	filters ProteinSequenceSimilarityFilters,
) ([]models.ProteinSequenceSimilarity, error) {
	query, args, err := proteinSequenceSimilarityListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build protein sequence similarity list query: %w", err)
	}

	rows := make([]proteinSequenceSimilarityRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind protein sequence similarity list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list protein sequence similarities: %w", err)
	}

	similarities := make([]models.ProteinSequenceSimilarity, 0, len(rows))
	for _, row := range rows {
		similarity, err := proteinSequenceSimilarityFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("decode protein sequence similarity: %w", err)
		}
		similarities = append(similarities, *similarity)
	}
	return similarities, nil
}

func proteinSequenceSimilarityRunListQuery(
	filters ProteinSequenceSimilarityRunFilters,
) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select id, tool, parameters, state, error_message, started_at, finished_at, created_at
			  from protein_sequence_similarity_runs`

	if filters.State != nil {
		conditions = append(conditions, "state = :state")
		args["state"] = string(*filters.State)
	}

	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
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

func proteinSequenceSimilarityListQuery(
	filters ProteinSequenceSimilarityFilters,
) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select id, run_id, source_sequence_id, similar_sequence_id, tool, score, metadata, created_at
			  from protein_sequence_similarities`

	if filters.SourceSequenceID != nil {
		conditions = append(conditions, "source_sequence_id = :source_sequence_id")
		args["source_sequence_id"] = *filters.SourceSequenceID
	}

	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by score desc, created_at asc, source_sequence_id asc, similar_sequence_id asc"

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

func proteinSequenceSimilarityRunFromRow(
	row *proteinSequenceSimilarityRunRow,
) (*models.ProteinSequenceSimilarityRun, error) {
	parameters := map[string]any{}
	if err := unmarshalJSON(row.Parameters, &parameters); err != nil {
		return nil, fmt.Errorf("decode parameters: %w", err)
	}

	return &models.ProteinSequenceSimilarityRun{
		ID:           row.ID,
		Tool:         row.Tool,
		Parameters:   parameters,
		State:        models.ProteinSequenceSimilarityRunState(row.State),
		ErrorMessage: stringPtrFromSQL(row.ErrorMessage),
		StartedAt:    timePtrFromSQL(row.StartedAt),
		FinishedAt:   timePtrFromSQL(row.FinishedAt),
		CreatedAt:    row.CreatedAt,
	}, nil
}

func proteinSequenceSimilarityFromRow(row proteinSequenceSimilarityRow) (*models.ProteinSequenceSimilarity, error) {
	metadata := map[string]any{}
	if err := unmarshalJSON(row.Metadata, &metadata); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	return &models.ProteinSequenceSimilarity{
		ID:                row.ID,
		RunID:             row.RunID,
		SourceSequenceID:  row.SourceSequenceID,
		SimilarSequenceID: row.SimilarSequenceID,
		Tool:              row.Tool,
		Score:             row.Score,
		Metadata:          metadata,
		CreatedAt:         row.CreatedAt,
	}, nil
}

type proteinSequenceSimilarityRunRow struct {
	ID           uuid.UUID      `db:"id"`
	Tool         string         `db:"tool"`
	Parameters   []byte         `db:"parameters"`
	State        string         `db:"state"`
	ErrorMessage sql.NullString `db:"error_message"`
	StartedAt    sql.NullTime   `db:"started_at"`
	FinishedAt   sql.NullTime   `db:"finished_at"`
	CreatedAt    time.Time      `db:"created_at"`
}

type proteinSequenceSimilarityRow struct {
	ID                uuid.UUID `db:"id"`
	RunID             uuid.UUID `db:"run_id"`
	SourceSequenceID  uuid.UUID `db:"source_sequence_id"`
	SimilarSequenceID uuid.UUID `db:"similar_sequence_id"`
	Tool              string    `db:"tool"`
	Score             float64   `db:"score"`
	Metadata          []byte    `db:"metadata"`
	CreatedAt         time.Time `db:"created_at"`
}

type proteinSequenceSimilarityCreateParams struct {
	ID                uuid.UUID `db:"id"`
	RunID             uuid.UUID `db:"run_id"`
	SourceSequenceID  uuid.UUID `db:"source_sequence_id"`
	SimilarSequenceID uuid.UUID `db:"similar_sequence_id"`
	Tool              string    `db:"tool"`
	Score             float64   `db:"score"`
	Metadata          string    `db:"metadata"`
	CreatedAt         time.Time `db:"created_at"`
}
