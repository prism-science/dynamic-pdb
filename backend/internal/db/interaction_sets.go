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

var ErrInteractionSetNotFound = errors.New("db: interaction set not found")

// interactionRecordInsertBatch keeps one insert under PostgreSQL's limit of
// 65535 bind parameters (29 columns per row).
const interactionRecordInsertBatch = 1000

type InteractionSetsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type InteractionSetFilters struct {
	ID              *uuid.UUID
	ModelRevisionID *uuid.UUID
	RunID           *uuid.UUID
}

func NewInteractionSetsRepository(database *sqlx.DB, queriers *QuerierProvider) *InteractionSetsRepository {
	return &InteractionSetsRepository{
		db:       database,
		queriers: queriers,
	}
}

type interactionSetRow struct {
	ID              uuid.UUID `db:"id"`
	ModelRevisionID uuid.UUID `db:"model_revision_id"`
	RunID           uuid.UUID `db:"run_id"`
	Source          string    `db:"source"`
	CreatedAt       time.Time `db:"created_at"`
}

func (row interactionSetRow) toModel() models.InteractionSet {
	return models.InteractionSet{
		ID:              row.ID,
		ModelRevisionID: row.ModelRevisionID,
		RunID:           row.RunID,
		Source:          models.InteractionSource(row.Source),
		CreatedAt:       row.CreatedAt,
	}
}

func (r *InteractionSetsRepository) Create(
	ctx context.Context,
	set models.InteractionSet,
) (*models.InteractionSet, error) {
	if err := set.Source.Validate(); err != nil {
		return nil, fmt.Errorf("validate interaction set source: %w", err)
	}
	query := `insert into interaction_sets(id, model_revision_id, run_id, source, created_at)
			  values ($1, $2, $3, $4, $5)
			  returning id, model_revision_id, run_id, source, created_at`
	var row interactionSetRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(
		ctx, &row, query,
		set.ID, set.ModelRevisionID, set.RunID, string(set.Source), set.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("insert interaction set: %w", err)
	}
	created := row.toModel()
	return &created, nil
}

func (r *InteractionSetsRepository) Get(ctx context.Context, id uuid.UUID) (*models.InteractionSet, error) {
	sets, err := r.List(ctx, InteractionSetFilters{ID: &id})
	if err != nil {
		return nil, fmt.Errorf("list interaction sets for get: %w", err)
	}
	if len(sets) == 0 {
		return nil, ErrInteractionSetNotFound
	}
	return &sets[0], nil
}

func (r *InteractionSetsRepository) List(
	ctx context.Context,
	filters InteractionSetFilters,
) ([]models.InteractionSet, error) {
	query := `select id, model_revision_id, run_id, source, created_at from interaction_sets`
	conditions := make([]string, 0, 3)
	args := make([]any, 0, 3)
	addCondition := func(column string, value uuid.UUID) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if filters.ID != nil {
		addCondition("id", *filters.ID)
	}
	if filters.ModelRevisionID != nil {
		addCondition("model_revision_id", *filters.ModelRevisionID)
	}
	if filters.RunID != nil {
		addCondition("run_id", *filters.RunID)
	}
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by created_at asc, id asc"

	rows := make([]interactionSetRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("list interaction sets: %w", err)
	}
	sets := make([]models.InteractionSet, 0, len(rows))
	for _, row := range rows {
		sets = append(sets, row.toModel())
	}
	return sets, nil
}

type interactionRecordRow struct {
	ID                 uuid.UUID       `db:"id"`
	InteractionSetID   uuid.UUID       `db:"interaction_set_id"`
	Ordinal            int             `db:"ordinal"`
	SourceConnectionID string          `db:"source_connection_id"`
	ConnectionType     string          `db:"connection_type"`
	ALabelAsymID       sql.NullString  `db:"a_label_asym_id"`
	ALabelCompID       sql.NullString  `db:"a_label_comp_id"`
	ALabelSeqID        sql.NullInt64   `db:"a_label_seq_id"`
	ALabelAtomID       sql.NullString  `db:"a_label_atom_id"`
	ALabelAltID        sql.NullString  `db:"a_label_alt_id"`
	AAuthAsymID        sql.NullString  `db:"a_auth_asym_id"`
	AAuthCompID        sql.NullString  `db:"a_auth_comp_id"`
	AAuthSeqID         sql.NullString  `db:"a_auth_seq_id"`
	APDBInsCode        sql.NullString  `db:"a_pdb_ins_code"`
	BLabelAsymID       sql.NullString  `db:"b_label_asym_id"`
	BLabelCompID       sql.NullString  `db:"b_label_comp_id"`
	BLabelSeqID        sql.NullInt64   `db:"b_label_seq_id"`
	BLabelAtomID       sql.NullString  `db:"b_label_atom_id"`
	BLabelAltID        sql.NullString  `db:"b_label_alt_id"`
	BAuthAsymID        sql.NullString  `db:"b_auth_asym_id"`
	BAuthCompID        sql.NullString  `db:"b_auth_comp_id"`
	BAuthSeqID         sql.NullString  `db:"b_auth_seq_id"`
	BPDBInsCode        sql.NullString  `db:"b_pdb_ins_code"`
	ASymmetry          sql.NullString  `db:"a_symmetry"`
	BSymmetry          sql.NullString  `db:"b_symmetry"`
	DistanceAngstroms  sql.NullFloat64 `db:"distance_angstroms"`
	ValueOrder         sql.NullString  `db:"value_order"`
}

const interactionRecordColumns = `id, interaction_set_id, ordinal, source_connection_id, connection_type,
	a_label_asym_id, a_label_comp_id, a_label_seq_id, a_label_atom_id, a_label_alt_id,
	a_auth_asym_id, a_auth_comp_id, a_auth_seq_id, a_pdb_ins_code,
	b_label_asym_id, b_label_comp_id, b_label_seq_id, b_label_atom_id, b_label_alt_id,
	b_auth_asym_id, b_auth_comp_id, b_auth_seq_id, b_pdb_ins_code,
	a_symmetry, b_symmetry, distance_angstroms, value_order`

func interactionRecordArgs(record models.InteractionRecord) []any {
	a, b := record.PartnerA, record.PartnerB
	return []any{
		record.ID, record.InteractionSetID, record.Ordinal, record.SourceConnectionID, record.ConnectionType,
		nullText(a.LabelAsymID), nullText(a.LabelCompID), nullInt(a.LabelSeqID), nullText(a.LabelAtomID), nullText(a.LabelAltID),
		nullText(a.AuthAsymID), nullText(a.AuthCompID), nullText(a.AuthSeqID), nullText(a.InsCode),
		nullText(b.LabelAsymID), nullText(b.LabelCompID), nullInt(b.LabelSeqID), nullText(b.LabelAtomID), nullText(b.LabelAltID),
		nullText(b.AuthAsymID), nullText(b.AuthCompID), nullText(b.AuthSeqID), nullText(b.InsCode),
		nullText(record.SymmetryA), nullText(record.SymmetryB), nullFloat(record.DistanceAngstroms), nullText(record.ValueOrder),
	}
}

// AddRecords inserts the records of an interaction set. Call it inside a
// transaction when the set and its records must land together.
func (r *InteractionSetsRepository) AddRecords(ctx context.Context, records []models.InteractionRecord) error {
	for start := 0; start < len(records); start += interactionRecordInsertBatch {
		batch := records[start:min(start+interactionRecordInsertBatch, len(records))]
		args := make([]any, 0, len(batch)*27)
		placeholders := make([]string, 0, len(batch))
		for _, record := range batch {
			recordArgs := interactionRecordArgs(record)
			rowPlaceholders := make([]string, len(recordArgs))
			for index := range recordArgs {
				rowPlaceholders[index] = fmt.Sprintf("$%d", len(args)+index+1)
			}
			args = append(args, recordArgs...)
			placeholders = append(placeholders, "("+strings.Join(rowPlaceholders, ", ")+")")
		}
		query := "insert into interaction_records(" + interactionRecordColumns + ") values " +
			strings.Join(placeholders, ", ")
		if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert interaction records: %w", err)
		}
	}
	return nil
}

func (r *InteractionSetsRepository) ListRecords(
	ctx context.Context,
	interactionSetID uuid.UUID,
) ([]models.InteractionRecord, error) {
	query := `select ` + interactionRecordColumns + `
			  from interaction_records
			  where interaction_set_id = $1
			  order by ordinal asc`
	rows := make([]interactionRecordRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, interactionSetID); err != nil {
		return nil, fmt.Errorf("list interaction records: %w", err)
	}
	records := make([]models.InteractionRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, row.toModel())
	}
	return records, nil
}

func (row interactionRecordRow) toModel() models.InteractionRecord {
	return models.InteractionRecord{
		ID:                 row.ID,
		InteractionSetID:   row.InteractionSetID,
		Ordinal:            row.Ordinal,
		SourceConnectionID: row.SourceConnectionID,
		ConnectionType:     row.ConnectionType,
		PartnerA: models.InteractionPartner{
			LabelAsymID: row.ALabelAsymID.String,
			LabelCompID: row.ALabelCompID.String,
			LabelSeqID:  intPointer(row.ALabelSeqID),
			LabelAtomID: row.ALabelAtomID.String,
			LabelAltID:  row.ALabelAltID.String,
			AuthAsymID:  row.AAuthAsymID.String,
			AuthCompID:  row.AAuthCompID.String,
			AuthSeqID:   row.AAuthSeqID.String,
			InsCode:     row.APDBInsCode.String,
		},
		PartnerB: models.InteractionPartner{
			LabelAsymID: row.BLabelAsymID.String,
			LabelCompID: row.BLabelCompID.String,
			LabelSeqID:  intPointer(row.BLabelSeqID),
			LabelAtomID: row.BLabelAtomID.String,
			LabelAltID:  row.BLabelAltID.String,
			AuthAsymID:  row.BAuthAsymID.String,
			AuthCompID:  row.BAuthCompID.String,
			AuthSeqID:   row.BAuthSeqID.String,
			InsCode:     row.BPDBInsCode.String,
		},
		SymmetryA:         row.ASymmetry.String,
		SymmetryB:         row.BSymmetry.String,
		DistanceAngstroms: floatPointer(row.DistanceAngstroms),
		ValueOrder:        row.ValueOrder.String,
	}
}

func nullText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullInt(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}

func nullFloat(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func intPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func floatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}
