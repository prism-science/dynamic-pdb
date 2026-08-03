package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

type ProteinSequencesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type proteinSequenceRow struct {
	ID               uuid.UUID `db:"id"`
	EntryRevisionID  uuid.UUID `db:"entry_revision_id"`
	SourceArtifactID uuid.UUID `db:"source_artifact_id"`
	RecordIndex      int       `db:"record_index"`
	Header           string    `db:"header"`
	Sequence         string    `db:"sequence"`
	CreatedAt        time.Time `db:"created_at"`
}

type proteinSequenceCreateParams struct {
	ID               uuid.UUID `db:"id"`
	EntryRevisionID  uuid.UUID `db:"entry_revision_id"`
	SourceArtifactID uuid.UUID `db:"source_artifact_id"`
	RecordIndex      int       `db:"record_index"`
	Header           string    `db:"header"`
	Sequence         string    `db:"sequence"`
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
			    created_at
			  )
			  values (:id, :entry_revision_id, :source_artifact_id, :record_index, :header, :sequence, :created_at)`
	if _, err := r.queriers.Querier(ctx, r.db).NamedExecContext(ctx, query, params); err != nil {
		return fmt.Errorf("insert protein sequences: %w", err)
	}
	return nil
}

func (r *ProteinSequencesRepository) List(
	ctx context.Context,
	entryRevisionID uuid.UUID,
) ([]models.ProteinSequence, error) {
	query := `select id, entry_revision_id, source_artifact_id, record_index, header, sequence, created_at
			  from protein_sequences
			  where entry_revision_id = $1
			  order by created_at asc, source_artifact_id asc, record_index asc`

	rows := make([]proteinSequenceRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, entryRevisionID); err != nil {
		return nil, fmt.Errorf("list protein sequences by entry revision: %w", err)
	}

	sequences := make([]models.ProteinSequence, 0, len(rows))
	for _, row := range rows {
		sequences = append(sequences, models.ProteinSequence{
			ID:               row.ID,
			EntryRevisionID:  row.EntryRevisionID,
			SourceArtifactID: row.SourceArtifactID,
			RecordIndex:      row.RecordIndex,
			Header:           row.Header,
			Sequence:         row.Sequence,
			CreatedAt:        row.CreatedAt,
		})
	}
	return sequences, nil
}
