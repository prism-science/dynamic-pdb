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

type SimilarEntryFilters struct {
	EntryID string
	Limit   *int
	Offset  *int
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

func (r *ProteinSequenceSimilaritiesRepository) DeleteForProteinSequences(
	ctx context.Context,
	sequenceIDs []uuid.UUID,
) error {
	if len(sequenceIDs) == 0 {
		return nil
	}

	query, args, err := sqlx.In(
		`delete from protein_sequence_similarities
		 where source_sequence_id in (?)
		    or similar_sequence_id in (?)`,
		sequenceIDs,
		sequenceIDs,
	)
	if err != nil {
		return fmt.Errorf("bind protein sequence similarity delete query: %w", err)
	}
	query = sqlx.Rebind(sqlx.DOLLAR, query)
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete protein sequence similarities: %w", err)
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

func (r *ProteinSequenceSimilaritiesRepository) CreateSimilarityStagingTable(ctx context.Context) error {
	query := `create temporary table protein_sequence_similarities_staging (
			    id uuid not null,
			    run_id uuid not null,
			    source_sequence_id uuid not null,
			    similar_sequence_id uuid not null,
			    tool text not null,
			    score double precision not null,
			    metadata jsonb not null,
			    created_at timestamptz not null
			  ) on commit drop`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query); err != nil {
		return fmt.Errorf("create protein sequence similarities staging table: %w", err)
	}
	return nil
}

func (r *ProteinSequenceSimilaritiesRepository) CopyToSimilarityStaging(
	ctx context.Context,
	similarities []models.ProteinSequenceSimilarity,
) (err error) {
	if len(similarities) == 0 {
		return nil
	}

	statement, err := r.queriers.Querier(ctx, r.db).PreparexContext(ctx, pq.CopyIn(
		"protein_sequence_similarities_staging",
		"id",
		"run_id",
		"source_sequence_id",
		"similar_sequence_id",
		"tool",
		"score",
		"metadata",
		"created_at",
	))
	if err != nil {
		return fmt.Errorf("prepare copy protein sequence similarities to staging: %w", err)
	}
	defer func() {
		if closeErr := statement.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close copy protein sequence similarities statement: %w", closeErr)
		}
	}()

	now := time.Now().UTC()
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
		if _, err := statement.ExecContext(
			ctx,
			id,
			similarity.RunID,
			similarity.SourceSequenceID,
			similarity.SimilarSequenceID,
			similarity.Tool,
			similarity.Score,
			metadata,
			createdAt,
		); err != nil {
			return fmt.Errorf("copy protein sequence similarity to staging: %w", err)
		}
	}
	if _, err := statement.ExecContext(ctx); err != nil {
		return fmt.Errorf("flush protein sequence similarities staging copy: %w", err)
	}
	return nil
}

func (r *ProteinSequenceSimilaritiesRepository) CreateFromSimilarityStaging(ctx context.Context) (int64, error) {
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
			  select
			    id,
			    run_id,
			    source_sequence_id,
			    similar_sequence_id,
			    tool,
			    score,
			    metadata,
			    created_at
			  from protein_sequence_similarities_staging
			  on conflict (source_sequence_id, similar_sequence_id, tool) do nothing`
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("insert protein sequence similarities from staging: %w", err)
	}
	insertedCount, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read inserted protein sequence similarities count: %w", err)
	}
	return insertedCount, nil
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

func (r *ProteinSequenceSimilaritiesRepository) ListSimilarEntries(
	ctx context.Context,
	filters SimilarEntryFilters,
) ([]models.SimilarEntry, error) {
	query, args, err := similarEntryListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build similar entry list query: %w", err)
	}

	rows := make([]similarEntryMatchRow, 0)
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind similar entry list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list similar entries: %w", err)
	}

	entries := make([]models.SimilarEntry, 0)
	entryIndexes := map[string]int{}
	for _, row := range rows {
		entryIndex, ok := entryIndexes[row.EntryID]
		if !ok {
			entry, err := similarEntryFromRow(row)
			if err != nil {
				return nil, fmt.Errorf("decode similar entry: %w", err)
			}
			entryIndexes[row.EntryID] = len(entries)
			entries = append(entries, *entry)
			entryIndex = len(entries) - 1
		}

		match, err := proteinSequenceSimilarityMatchFromRow(row)
		if err != nil {
			return nil, fmt.Errorf("decode protein sequence similarity match: %w", err)
		}
		entries[entryIndex].Matches = append(entries[entryIndex].Matches, *match)
	}
	return entries, nil
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

func similarEntryListQuery(filters SimilarEntryFilters) (string, map[string]any, error) {
	if strings.TrimSpace(filters.EntryID) == "" {
		return "", nil, errors.New("entry id is required")
	}
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{
		"entry_id":     filters.EntryID,
		"active_state": string(models.RevisionStateActive),
	}
	query := `with source_sequences as (
			    select protein_sequences.id
			    from protein_sequences
			    join entry_revisions on entry_revisions.id = protein_sequences.entry_revision_id
			    where entry_revisions.entry_id = :entry_id
			      and entry_revisions.state = :active_state
			  ),
			  similar_entries as (
			    select similar_entry_revisions.entry_id,
			           max(protein_sequence_similarities.score) as score
			    from protein_sequence_similarities
			    join source_sequences on source_sequences.id = protein_sequence_similarities.source_sequence_id
			    join protein_sequences similar_sequences
			      on similar_sequences.id = protein_sequence_similarities.similar_sequence_id
			    join entry_revisions similar_entry_revisions
			      on similar_entry_revisions.id = similar_sequences.entry_revision_id
			    where similar_entry_revisions.state = :active_state
			      and similar_entry_revisions.entry_id <> :entry_id
			    group by similar_entry_revisions.entry_id
			    order by score desc, similar_entry_revisions.entry_id asc`
	if filters.Limit != nil {
		query += "\nlimit :limit"
		args["limit"] = *filters.Limit
	}
	if filters.Offset != nil {
		query += "\noffset :offset"
		args["offset"] = *filters.Offset
	}
	query += `
			  )
			  select
			    similar_entries.score as entry_score,
			    entry_revisions.id as entry_revision_id,
			    entry_revisions.entry_id,
			    entry_revisions.parent_revision_id,
			    entry_revisions.revision_number,
			    entry_revisions.state as entry_state,
			    entry_revisions.change_summary,
			    entry_revisions.published_at,
			    entry_revisions.name as entry_name,
			    entry_revisions.description as entry_description,
			    entry_revisions.thumbnail_image_url as entry_thumbnail_image_url,
			    entry_revisions.metadata as entry_metadata,
			    entry_revisions.created_by as entry_created_by,
			    entry_revisions.created_at as entry_created_at,
			    entry_revisions.updated_at as entry_updated_at,
			    protein_sequence_similarities.id as similarity_id,
			    protein_sequence_similarities.run_id as similarity_run_id,
			    protein_sequence_similarities.source_sequence_id,
			    protein_sequence_similarities.similar_sequence_id,
			    protein_sequence_similarities.tool as similarity_tool,
			    protein_sequence_similarities.score as similarity_score,
			    protein_sequence_similarities.metadata as similarity_metadata,
			    protein_sequence_similarities.created_at as similarity_created_at,
			    similar_sequences.source_artifact_id as similar_sequence_source_artifact_id,
			    similar_sequences.record_index as similar_sequence_record_index,
			    similar_sequences.header as similar_sequence_header,
			    similar_sequences.sequence as similar_sequence_sequence,
			    similar_sequences.processing_state as similar_sequence_processing_state,
			    similar_sequences.created_at as similar_sequence_created_at
			  from similar_entries
			  join entry_revisions on entry_revisions.entry_id = similar_entries.entry_id
			    and entry_revisions.state = :active_state
			  join protein_sequences similar_sequences on similar_sequences.entry_revision_id = entry_revisions.id
			  join protein_sequence_similarities
			    on protein_sequence_similarities.similar_sequence_id = similar_sequences.id
			  join source_sequences on source_sequences.id = protein_sequence_similarities.source_sequence_id
			  order by similar_entries.score desc,
			           similar_entries.entry_id asc,
			           protein_sequence_similarities.score desc,
			           protein_sequence_similarities.created_at asc,
			           protein_sequence_similarities.id asc`

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

func similarEntryFromRow(row similarEntryMatchRow) (*models.SimilarEntry, error) {
	metadata := models.EntryMetadata{}
	if err := unmarshalJSON(row.EntryMetadata, &metadata); err != nil {
		return nil, fmt.Errorf("decode entry metadata: %w", err)
	}

	return &models.SimilarEntry{
		Entry: models.EntryRevision{
			ID:                row.EntryRevisionID,
			EntryID:           row.EntryID,
			ParentRevisionID:  uuidPtrFromSQL(row.ParentRevisionID),
			RevisionNumber:    intPtrFromSQL(row.RevisionNumber),
			State:             models.RevisionState(row.EntryState),
			ChangeSummary:     stringPtrFromSQL(row.ChangeSummary),
			PublishedAt:       timePtrFromSQL(row.PublishedAt),
			Name:              row.EntryName,
			Description:       stringPtrFromSQL(row.EntryDescription),
			ThumbnailImageURL: stringPtrFromSQL(row.EntryThumbnailImageURL),
			Metadata:          metadata,
			CreatedBy:         row.EntryCreatedBy,
			CreatedAt:         row.EntryCreatedAt,
			UpdatedAt:         row.EntryUpdatedAt,
		},
		Score:   row.EntryScore,
		Matches: make([]models.ProteinSequenceSimilarityMatch, 0),
	}, nil
}

func proteinSequenceSimilarityMatchFromRow(
	row similarEntryMatchRow,
) (*models.ProteinSequenceSimilarityMatch, error) {
	metadata := map[string]any{}
	if err := unmarshalJSON(row.SimilarityMetadata, &metadata); err != nil {
		return nil, fmt.Errorf("decode similarity metadata: %w", err)
	}

	return &models.ProteinSequenceSimilarityMatch{
		SourceSequenceID: row.SourceSequenceID,
		SimilarSequence: models.ProteinSequence{
			ID:               row.SimilarSequenceID,
			EntryRevisionID:  row.EntryRevisionID,
			SourceArtifactID: row.SimilarSequenceSourceArtifactID,
			RecordIndex:      row.SimilarSequenceRecordIndex,
			Header:           row.SimilarSequenceHeader,
			Sequence:         row.SimilarSequenceSequence,
			ProcessingState:  models.ProteinSequenceProcessingState(row.SimilarSequenceProcessingState),
			CreatedAt:        row.SimilarSequenceCreatedAt,
		},
		Similarity: models.ProteinSequenceSimilarity{
			ID:                row.SimilarityID,
			RunID:             row.SimilarityRunID,
			SourceSequenceID:  row.SourceSequenceID,
			SimilarSequenceID: row.SimilarSequenceID,
			Tool:              row.SimilarityTool,
			Score:             row.SimilarityScore,
			Metadata:          metadata,
			CreatedAt:         row.SimilarityCreatedAt,
		},
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

type similarEntryMatchRow struct {
	EntryScore                      float64        `db:"entry_score"`
	EntryRevisionID                 uuid.UUID      `db:"entry_revision_id"`
	EntryID                         string         `db:"entry_id"`
	ParentRevisionID                uuid.NullUUID  `db:"parent_revision_id"`
	RevisionNumber                  sql.NullInt64  `db:"revision_number"`
	EntryState                      string         `db:"entry_state"`
	ChangeSummary                   sql.NullString `db:"change_summary"`
	PublishedAt                     sql.NullTime   `db:"published_at"`
	EntryName                       string         `db:"entry_name"`
	EntryDescription                sql.NullString `db:"entry_description"`
	EntryThumbnailImageURL          sql.NullString `db:"entry_thumbnail_image_url"`
	EntryMetadata                   []byte         `db:"entry_metadata"`
	EntryCreatedBy                  uuid.UUID      `db:"entry_created_by"`
	EntryCreatedAt                  time.Time      `db:"entry_created_at"`
	EntryUpdatedAt                  time.Time      `db:"entry_updated_at"`
	SimilarityID                    uuid.UUID      `db:"similarity_id"`
	SimilarityRunID                 uuid.UUID      `db:"similarity_run_id"`
	SourceSequenceID                uuid.UUID      `db:"source_sequence_id"`
	SimilarSequenceID               uuid.UUID      `db:"similar_sequence_id"`
	SimilarityTool                  string         `db:"similarity_tool"`
	SimilarityScore                 float64        `db:"similarity_score"`
	SimilarityMetadata              []byte         `db:"similarity_metadata"`
	SimilarityCreatedAt             time.Time      `db:"similarity_created_at"`
	SimilarSequenceSourceArtifactID uuid.UUID      `db:"similar_sequence_source_artifact_id"`
	SimilarSequenceRecordIndex      int            `db:"similar_sequence_record_index"`
	SimilarSequenceHeader           string         `db:"similar_sequence_header"`
	SimilarSequenceSequence         string         `db:"similar_sequence_sequence"`
	SimilarSequenceProcessingState  string         `db:"similar_sequence_processing_state"`
	SimilarSequenceCreatedAt        time.Time      `db:"similar_sequence_created_at"`
}
