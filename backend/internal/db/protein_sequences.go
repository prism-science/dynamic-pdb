package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

type ProteinSequencesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type ProteinSequenceFilters struct {
	EntryRevisionID *uuid.UUID
	ProcessingState *models.ProteinSequenceProcessingState
	AfterID         *uuid.UUID
	OrderByID       bool
	Limit           *int
	Offset          *int
}

type proteinSequenceRow struct {
	ID               uuid.UUID `db:"id"`
	EntryRevisionID  uuid.UUID `db:"entry_revision_id"`
	SourceArtifactID uuid.UUID `db:"source_artifact_id"`
	RecordIndex      int       `db:"record_index"`
	Header           string    `db:"header"`
	Sequence         string    `db:"sequence"`
	ProcessingState  string    `db:"processing_state"`
	CreatedAt        time.Time `db:"created_at"`
}

type proteinSequenceCreateParams struct {
	ID               uuid.UUID `db:"id"`
	EntryRevisionID  uuid.UUID `db:"entry_revision_id"`
	SourceArtifactID uuid.UUID `db:"source_artifact_id"`
	RecordIndex      int       `db:"record_index"`
	Header           string    `db:"header"`
	Sequence         string    `db:"sequence"`
	ProcessingState  string    `db:"processing_state"`
	CreatedAt        time.Time `db:"created_at"`
}

func NewProteinSequencesRepository(
	database *sqlx.DB,
	queriers *QuerierProvider,
) *ProteinSequencesRepository {
	return &ProteinSequencesRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *ProteinSequencesRepository) Create(
	ctx context.Context,
	entryRevisionID uuid.UUID,
	sourceArtifactID uuid.UUID,
	records []models.FASTARecord,
) error {
	if len(records) == 0 {
		return nil
	}

	now := time.Now().UTC()
	params := make([]proteinSequenceCreateParams, 0, len(records))
	for recordIndex, record := range records {
		params = append(params, proteinSequenceCreateParams{
			ID:               uuid.New(),
			EntryRevisionID:  entryRevisionID,
			SourceArtifactID: sourceArtifactID,
			RecordIndex:      recordIndex,
			Header:           record.Header,
			Sequence:         record.Sequence,
			ProcessingState:  string(models.ProteinSequenceProcessingStatePending),
			CreatedAt:        now,
		})
	}

	query := `insert into protein_sequences(
			    id,
			    entry_revision_id,
			    source_artifact_id,
			    record_index,
			    header,
			    sequence,
			    processing_state,
			    created_at
			  )
			  values (:id, :entry_revision_id, :source_artifact_id, :record_index, :header, :sequence, :processing_state, :created_at)`
	if _, err := r.queriers.Querier(ctx, r.db).NamedExecContext(ctx, query, params); err != nil {
		return fmt.Errorf("insert protein sequences: %w", err)
	}
	return nil
}

func (r *ProteinSequencesRepository) List(
	ctx context.Context,
	filters ProteinSequenceFilters,
) ([]models.ProteinSequence, error) {
	query, args, err := proteinSequenceListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build protein sequence list query: %w", err)
	}
	boundQuery, queryArgs, err := sqlx.Named(query, args)
	if err != nil {
		return nil, fmt.Errorf("bind protein sequence list query: %w", err)
	}
	boundQuery = sqlx.Rebind(sqlx.DOLLAR, boundQuery)

	rows := make([]proteinSequenceRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, boundQuery, queryArgs...); err != nil {
		return nil, fmt.Errorf("list protein sequences: %w", err)
	}

	return proteinSequencesFromRows(rows), nil
}

func (r *ProteinSequencesRepository) MoveEntryRevisionArtifacts(
	ctx context.Context,
	fromRevisionID, toRevisionID uuid.UUID,
	sourceArtifactIDs []uuid.UUID,
) error {
	if len(sourceArtifactIDs) == 0 {
		return nil
	}

	query, args, err := sqlx.In(
		`update protein_sequences
		 set entry_revision_id = ?
		 where entry_revision_id = ?
		   and source_artifact_id in (?)`,
		toRevisionID,
		fromRevisionID,
		sourceArtifactIDs,
	)
	if err != nil {
		return fmt.Errorf("bind protein sequence revision move query: %w", err)
	}
	query = sqlx.Rebind(sqlx.DOLLAR, query)
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("move protein sequences to entry revision: %w", err)
	}
	return nil
}

func (r *ProteinSequencesRepository) MoveEntryRevision(
	ctx context.Context,
	fromRevisionID, toRevisionID uuid.UUID,
) error {
	query := `update protein_sequences
		      set entry_revision_id = $2
		      where entry_revision_id = $1`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx,
		query,
		fromRevisionID,
		toRevisionID,
	); err != nil {
		return fmt.Errorf("move protein sequences to entry revision: %w", err)
	}
	return nil
}

func (r *ProteinSequencesRepository) Delete(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}

	query, args, err := sqlx.In(`delete from protein_sequences where id in (?)`, ids)
	if err != nil {
		return fmt.Errorf("bind protein sequence delete query: %w", err)
	}
	query = sqlx.Rebind(sqlx.DOLLAR, query)
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete protein sequences: %w", err)
	}
	return nil
}

func (r *ProteinSequencesRepository) UpdateProcessingState(
	ctx context.Context,
	ids []uuid.UUID,
	state models.ProteinSequenceProcessingState,
) error {
	if len(ids) == 0 {
		return nil
	}

	query, args, err := sqlx.In(
		`update protein_sequences
		 set processing_state = ?
		 where id in (?)`,
		string(state),
		ids,
	)
	if err != nil {
		return fmt.Errorf("bind protein sequence processing state update query: %w", err)
	}
	query = sqlx.Rebind(sqlx.DOLLAR, query)

	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update protein sequence processing state: %w", err)
	}
	return nil
}

func proteinSequenceListQuery(filters ProteinSequenceFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	args := map[string]any{}
	conditions := make([]string, 0)
	query := `select id, entry_revision_id, source_artifact_id, record_index, header, sequence,
			         processing_state, created_at
			  from protein_sequences`

	if filters.EntryRevisionID != nil {
		conditions = append(conditions, "entry_revision_id = :entry_revision_id")
		args["entry_revision_id"] = *filters.EntryRevisionID
	}
	if filters.ProcessingState != nil {
		conditions = append(conditions, "processing_state = :processing_state")
		args["processing_state"] = string(*filters.ProcessingState)
	}
	if filters.AfterID != nil {
		conditions = append(conditions, "id > :after_id")
		args["after_id"] = *filters.AfterID
	}

	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	if filters.OrderByID {
		query += "\norder by id asc"
	} else {
		query += "\norder by created_at asc, source_artifact_id asc, record_index asc, id asc"
	}

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

func proteinSequencesFromRows(rows []proteinSequenceRow) []models.ProteinSequence {
	sequences := make([]models.ProteinSequence, 0, len(rows))
	for _, row := range rows {
		sequences = append(sequences, models.ProteinSequence{
			ID:               row.ID,
			EntryRevisionID:  row.EntryRevisionID,
			SourceArtifactID: row.SourceArtifactID,
			RecordIndex:      row.RecordIndex,
			Header:           row.Header,
			Sequence:         row.Sequence,
			ProcessingState:  models.ProteinSequenceProcessingState(row.ProcessingState),
			CreatedAt:        row.CreatedAt,
		})
	}
	return sequences
}
