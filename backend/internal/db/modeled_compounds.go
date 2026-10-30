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
	"dynamic-pdb/lib/cif"
)

var ErrModeledCompoundSetNotFound = errors.New("db: modeled compound set not found")

// modeledCompoundRecordInsertBatch keeps one insert under PostgreSQL's limit of 65535 bind
// parameters (11 columns per row).
const modeledCompoundRecordInsertBatch = 2000

const modeledCompoundRecordColumns = `artifact_id, ordinal, ccd_id, label_asym_id, auth_asym_id, auth_seq_id,
	pdb_ins_code, entity_id, source, needs_review, review_reason`

// ModeledCompoundsRepository is the modeled_compound_sets and modeled_compound_records
// tables: what the model-compounds job read from each model file in our bucket.
type ModeledCompoundsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewModeledCompoundsRepository(database *sqlx.DB, queriers *QuerierProvider) *ModeledCompoundsRepository {
	return &ModeledCompoundsRepository{
		db:       database,
		queriers: queriers,
	}
}

type modeledCompoundClaimRow struct {
	ArtifactID uuid.UUID      `db:"artifact_id"`
	Attempts   int            `db:"attempts"`
	Name       string         `db:"name"`
	URI        sql.NullString `db:"uri"`
	Format     sql.NullString `db:"format"`
}

// ClaimDue takes up to limit pending sets that are due at now, oldest first. A claim moves
// next_attempt_at to now+lease and counts the attempt; a claim that is never recorded
// becomes due again when the lease ends.
func (r *ModeledCompoundsRepository) ClaimDue(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
) ([]models.ModeledCompoundClaim, error) {
	if limit <= 0 {
		return nil, errors.New("claim limit must be positive")
	}
	if lease <= 0 {
		return nil, errors.New("claim lease must be positive")
	}
	query := `with picked as (
			    select artifact_id
			    from modeled_compound_sets
			    where status = 'pending' and next_attempt_at <= $1
			    order by next_attempt_at, artifact_id
			    limit $2
			    for update skip locked
			  ), claimed as (
			    update modeled_compound_sets
			    set next_attempt_at = $3, attempts = attempts + 1
			    where artifact_id in (select artifact_id from picked)
			    returning artifact_id, attempts
			  )
			  select claimed.artifact_id, claimed.attempts, artifacts.name, artifacts.uri, artifacts.format
			  from claimed
			  join artifacts on artifacts.id = claimed.artifact_id
			  order by claimed.artifact_id`

	rows := make([]modeledCompoundClaimRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, now, limit, now.Add(lease)); err != nil {
		return nil, fmt.Errorf("claim due modeled compound sets: %w", err)
	}
	claims := make([]models.ModeledCompoundClaim, 0, len(rows))
	for _, row := range rows {
		claims = append(claims, models.ModeledCompoundClaim{
			ArtifactID: row.ArtifactID,
			Name:       row.Name,
			URI:        stringPtrFromSQL(row.URI),
			Format:     stringPtrFromSQL(row.Format),
			Attempts:   row.Attempts,
		})
	}
	return claims, nil
}

// RecordFailure records why a claimed file could not be read. With a retry time the set goes
// back to pending until then; without one it is failed for good.
func (r *ModeledCompoundsRepository) RecordFailure(
	ctx context.Context,
	artifactID uuid.UUID,
	message string,
	retryAt *time.Time,
) error {
	query := `update modeled_compound_sets
			  set status = 'failed', last_error = $2
			  where artifact_id = $1 and status = 'pending'`
	args := []any{artifactID, message}
	if retryAt != nil {
		query = `update modeled_compound_sets
				 set next_attempt_at = $3, last_error = $2
				 where artifact_id = $1 and status = 'pending'`
		args = append(args, *retryAt)
	}
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("record modeled compound failure: %w", err)
	}
	if err := requireOneRow(result, artifactID); err != nil {
		return fmt.Errorf("record modeled compound failure: %w", err)
	}
	return nil
}

type modeledCompoundSetRow struct {
	ArtifactID    uuid.UUID      `db:"artifact_id"`
	Status        string         `db:"status"`
	RunID         uuid.NullUUID  `db:"run_id"`
	ParserVersion sql.NullString `db:"parser_version"`
	Complete      sql.NullBool   `db:"complete"`
	Warnings      []byte         `db:"warnings"`
	Attempts      int            `db:"attempts"`
	LastError     sql.NullString `db:"last_error"`
	ReadAt        sql.NullTime   `db:"read_at"`
}

type modeledCompoundRecordRow struct {
	ArtifactID   uuid.UUID      `db:"artifact_id"`
	Ordinal      int            `db:"ordinal"`
	CCDID        string         `db:"ccd_id"`
	LabelAsymID  sql.NullString `db:"label_asym_id"`
	AuthAsymID   sql.NullString `db:"auth_asym_id"`
	AuthSeqID    sql.NullString `db:"auth_seq_id"`
	InsCode      sql.NullString `db:"pdb_ins_code"`
	EntityID     sql.NullString `db:"entity_id"`
	Source       string         `db:"source"`
	NeedsReview  bool           `db:"needs_review"`
	ReviewReason sql.NullString `db:"review_reason"`
}

// Get returns the set of a model file with its copies, or ErrModeledCompoundSetNotFound when
// the file has none (it is not in our bucket).
func (r *ModeledCompoundsRepository) Get(ctx context.Context, artifactID uuid.UUID) (*models.ModeledCompoundSet, error) {
	query := `select sets.artifact_id, sets.status, sets.run_id, runs.software_version as parser_version,
			         sets.complete, sets.warnings, sets.attempts, sets.last_error, sets.read_at
			  from modeled_compound_sets sets
			  left join runs on runs.id = sets.run_id
			  where sets.artifact_id = $1`
	var row modeledCompoundSetRow
	querier := r.queriers.Querier(ctx, r.db)
	if err := querier.GetContext(ctx, &row, query, artifactID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModeledCompoundSetNotFound
		}
		return nil, fmt.Errorf("get modeled compound set: %w", err)
	}
	set, err := row.toModel()
	if err != nil {
		return nil, fmt.Errorf("read modeled compound set %s: %w", artifactID, err)
	}

	records := make([]modeledCompoundRecordRow, 0)
	recordsQuery := `select ` + modeledCompoundRecordColumns + `
			  from modeled_compound_records
			  where artifact_id = $1
			  order by ordinal asc`
	if err := querier.SelectContext(ctx, &records, recordsQuery, artifactID); err != nil {
		return nil, fmt.Errorf("list modeled compound records: %w", err)
	}
	set.Copies = make([]models.ModeledCompoundCopy, 0, len(records))
	for _, record := range records {
		copied, err := record.toModel()
		if err != nil {
			return nil, fmt.Errorf("read modeled compound record %d of %s: %w", record.Ordinal, artifactID, err)
		}
		set.Copies = append(set.Copies, copied)
	}
	return set, nil
}

// CompoundIDs returns the distinct CCD IDs a model file models, in ID order: none for a
// file without a done set.
func (r *ModeledCompoundsRepository) CompoundIDs(ctx context.Context, artifactID uuid.UUID) ([]string, error) {
	query := `select distinct records.ccd_id
			  from modeled_compound_records records
			  join modeled_compound_sets sets on sets.artifact_id = records.artifact_id
			  where records.artifact_id = $1 and sets.status = 'done'
			  order by records.ccd_id`
	ids := make([]string, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &ids, query, artifactID); err != nil {
		return nil, fmt.Errorf("list modeled compound ids: %w", err)
	}
	return ids, nil
}

func (row modeledCompoundSetRow) toModel() (*models.ModeledCompoundSet, error) {
	status := models.ModeledCompoundStatus(row.Status)
	if err := status.Validate(); err != nil {
		return nil, err
	}
	warnings := []string{}
	if err := unmarshalJSON(row.Warnings, &warnings); err != nil {
		return nil, fmt.Errorf("decode warnings: %w", err)
	}
	set := &models.ModeledCompoundSet{
		ArtifactID:    row.ArtifactID,
		Status:        status,
		ParserVersion: stringPtrFromSQL(row.ParserVersion),
		Warnings:      warnings,
		Attempts:      row.Attempts,
		LastError:     stringPtrFromSQL(row.LastError),
	}
	if row.RunID.Valid {
		set.RunID = &row.RunID.UUID
	}
	if row.Complete.Valid {
		set.Complete = &row.Complete.Bool
	}
	if row.ReadAt.Valid {
		set.ReadAt = &row.ReadAt.Time
	}
	return set, nil
}

func (row modeledCompoundRecordRow) toModel() (models.ModeledCompoundCopy, error) {
	source := models.ModeledCompoundSource(row.Source)
	if err := source.Validate(); err != nil {
		return models.ModeledCompoundCopy{}, err
	}
	return models.ModeledCompoundCopy{
		CCDID:        row.CCDID,
		LabelAsymID:  row.LabelAsymID.String,
		AuthAsymID:   row.AuthAsymID.String,
		AuthSeqID:    row.AuthSeqID.String,
		InsCode:      row.InsCode.String,
		EntityID:     row.EntityID.String,
		Source:       source,
		NeedsReview:  row.NeedsReview,
		ReviewReason: row.ReviewReason.String,
	}, nil
}

// addRecords stores the copies of one set, replacing any it had.
func (r *ModeledCompoundsRepository) addRecords(ctx context.Context, artifactID uuid.UUID, copies []cif.CompoundCopy) error {
	querier := r.queriers.Querier(ctx, r.db)
	if _, err := querier.ExecContext(ctx, `delete from modeled_compound_records where artifact_id = $1`, artifactID); err != nil {
		return fmt.Errorf("delete old modeled compound records: %w", err)
	}
	for start := 0; start < len(copies); start += modeledCompoundRecordInsertBatch {
		batch := copies[start:min(start+modeledCompoundRecordInsertBatch, len(copies))]
		args := make([]any, 0, len(batch)*11)
		placeholders := make([]string, 0, len(batch))
		for offset, copied := range batch {
			source, err := modeledCompoundSource(copied.Source)
			if err != nil {
				return fmt.Errorf("copy %d: %w", start+offset, err)
			}
			recordArgs := []any{
				artifactID, start + offset, copied.CCDID,
				nullIfEmpty(copied.LabelAsymID), nullIfEmpty(copied.AuthAsymID), nullIfEmpty(copied.AuthSeqID),
				nullIfEmpty(copied.InsCode), nullIfEmpty(copied.EntityID), string(source),
				copied.NeedsReview, nullIfEmpty(copied.ReviewReason),
			}
			rowPlaceholders := make([]string, len(recordArgs))
			for index := range recordArgs {
				rowPlaceholders[index] = fmt.Sprintf("$%d", len(args)+index+1)
			}
			args = append(args, recordArgs...)
			placeholders = append(placeholders, "("+strings.Join(rowPlaceholders, ", ")+")")
		}
		query := "insert into modeled_compound_records(" + modeledCompoundRecordColumns + ") values " +
			strings.Join(placeholders, ", ")
		if _, err := querier.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert modeled compound records: %w", err)
		}
	}
	return nil
}

// markDone records a finished read: the parser run, whether the list is complete, and its
// warnings.
func (r *ModeledCompoundsRepository) markDone(
	ctx context.Context,
	artifactID uuid.UUID,
	runID uuid.UUID,
	result cif.ModeledCompounds,
	readAt time.Time,
) error {
	// marshalJSON writes nil as an object; the column holds a list.
	warnings, err := marshalJSON(append([]string{}, result.Warnings...))
	if err != nil {
		return fmt.Errorf("encode warnings: %w", err)
	}
	query := `update modeled_compound_sets
			  set status = 'done', run_id = $2, complete = $3, warnings = cast($4 as jsonb),
			      read_at = $5, last_error = null
			  where artifact_id = $1 and status = 'pending'`
	outcome, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, artifactID, runID, result.Complete, warnings, readAt)
	if err != nil {
		return fmt.Errorf("mark modeled compound set done: %w", err)
	}
	if err := requireOneRow(outcome, artifactID); err != nil {
		return fmt.Errorf("mark modeled compound set done: %w", err)
	}
	return nil
}

func modeledCompoundSource(source cif.CopySource) (models.ModeledCompoundSource, error) {
	switch source {
	case cif.SourceNonpolyScheme:
		return models.ModeledCompoundSourceNonpolyScheme, nil
	case cif.SourceBranchScheme:
		return models.ModeledCompoundSourceBranchScheme, nil
	case cif.SourceAtomSite:
		return models.ModeledCompoundSourceAtomSite, nil
	case cif.SourcePDBHetatm:
		return models.ModeledCompoundSourcePDBHetatm, nil
	default:
		return "", fmt.Errorf("unknown copy source %q", source)
	}
}

// requireOneRow fails unless a statement changed exactly one set: a set that is no longer
// pending was finished by another run, or does not exist.
func requireOneRow(result sql.Result, artifactID uuid.UUID) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count changed rows: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("artifact %s: %w", artifactID, ErrModeledCompoundSetNotFound)
	}
	return nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// SaveModeledCompoundsParams is one finished read of a model file.
type SaveModeledCompoundsParams struct {
	ArtifactID uuid.UUID
	// CreatedBy owns the run that records the parser.
	CreatedBy uuid.UUID
	Result    cif.ModeledCompounds
	ReadAt    time.Time
}

// SaveModeledCompounds stores the copies of a model file, marks its set done under a new run
// naming the parser and its version, and refreshes the search rows of the active model
// revisions with that file, so a search finds them by the compounds the file models. It all
// commits together or not at all. It returns how many revisions it re-indexed.
func (d *DB) SaveModeledCompounds(ctx context.Context, params SaveModeledCompoundsParams) (int, error) {
	reindexed := 0
	err := d.Do(ctx, func(ctx context.Context) error {
		run, err := d.Runs.Create(ctx, models.Run{
			ID:              uuid.New(),
			Name:            "modeled compound read",
			SoftwareName:    ptrTo(cif.ParserName),
			SoftwareVersion: ptrTo(cif.ParserVersion),
			Parameters:      map[string]any{"artifact_id": params.ArtifactID.String()},
			Metadata:        map[string]any{"copies": len(params.Result.Copies), "complete": params.Result.Complete},
			StartedAt:       &params.ReadAt,
			FinishedAt:      &params.ReadAt,
			CreatedBy:       params.CreatedBy,
			CreatedAt:       params.ReadAt,
			UpdatedAt:       params.ReadAt,
		})
		if err != nil {
			return fmt.Errorf("create parser run: %w", err)
		}
		if err := d.ModeledCompounds.addRecords(ctx, params.ArtifactID, params.Result.Copies); err != nil {
			return fmt.Errorf("store copies: %w", err)
		}
		if err := d.ModeledCompounds.markDone(ctx, params.ArtifactID, run.ID, params.Result, params.ReadAt); err != nil {
			return fmt.Errorf("finish the set: %w", err)
		}
		activeState := models.RevisionStateActive
		activeModelState := models.ModelStateActive
		revisions, err := d.Models.List(ctx, ModelRevisionFilters{
			PrimaryArtifactID:  &params.ArtifactID,
			State:              &activeState,
			ModelState:         &activeModelState,
			ExcludeResidueData: true,
		})
		if err != nil {
			return fmt.Errorf("list active revisions of the file: %w", err)
		}
		for _, revision := range revisions {
			if err := d.EntrySearch.IndexModelRevision(ctx, revision); err != nil {
				return fmt.Errorf("index model revision %s: %w", revision.ID, err)
			}
		}
		reindexed = len(revisions)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("save modeled compounds of %s: %w", params.ArtifactID, err)
	}
	return reindexed, nil
}
