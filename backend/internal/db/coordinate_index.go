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

var ErrCoordinateIndexSetNotFound = errors.New("db: coordinate index set not found")

// coordinateIndexChainInsertBatch keeps one insert under PostgreSQL's limit of 65535 bind
// parameters (7 columns per row).
const coordinateIndexChainInsertBatch = 2000

const coordinateIndexChainColumns = `artifact_id, auth_asym_id, entity_id, deposited_length, length_source,
	modeled_residues, protein, protein_source`

// coordinateIndexResidueCountInsertBatch keeps one insert under PostgreSQL's limit of 65535 bind
// parameters (4 columns per row).
const coordinateIndexResidueCountInsertBatch = 10000

// ensembleTypedFile is true when the latest revision using a model file (artifact id $1) of one of
// the file's models is an Ensemble or has member_count, which validation allows only on Ensembles.
// Rejected revisions never count. A model whose latest revision moved to another file is judged by
// its latest revision that still uses this one.
const ensembleTypedFile = `coalesce((
	    select bool_or(latest.ensemble)
	    from (
	      select distinct on (typed.model_id)
	             (coalesce(typed.metadata ->> 'model_type', '') = 'Ensemble'
	              or jsonb_typeof(typed.metadata -> 'member_count') = 'number') as ensemble
	      from model_revisions typed
	      where typed.primary_artifact_id = %s
	        and typed.state in ('pending', 'in_review', 'active', 'archived')
	      order by typed.model_id, typed.created_at desc, typed.id desc
	    ) latest
	  ), false)`

// CoordinateIndexRepository is the coordinate_index_sets and coordinate_index_chains tables:
// what the coordinate-index job read from each model file in our bucket.
type CoordinateIndexRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewCoordinateIndexRepository(database *sqlx.DB, queriers *QuerierProvider) *CoordinateIndexRepository {
	return &CoordinateIndexRepository{
		db:       database,
		queriers: queriers,
	}
}

type coordinateIndexClaimRow struct {
	ArtifactID uuid.UUID      `db:"artifact_id"`
	Attempts   int            `db:"attempts"`
	Name       string         `db:"name"`
	URI        sql.NullString `db:"uri"`
	Format     sql.NullString `db:"format"`
	CopyStatus sql.NullString `db:"copy_status"`
	StoredURI  sql.NullString `db:"stored_uri"`
}

// ClaimDue takes up to limit pending sets that are due at now: uploads before fill rows, each
// oldest first, each with the checksum job's copy of its file when there is one. A claim moves
// next_attempt_at to now+lease and counts the attempt; a claim that is never recorded becomes due
// again when the lease ends. A due set already claimed maxAttempts times is failed instead: its
// reads ended the job each time, so another would too.
func (r *CoordinateIndexRepository) ClaimDue(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
	maxAttempts int,
) ([]models.CoordinateIndexClaim, error) {
	if limit <= 0 {
		return nil, errors.New("claim limit must be positive")
	}
	if lease <= 0 {
		return nil, errors.New("claim lease must be positive")
	}
	if maxAttempts <= 0 {
		return nil, errors.New("claim attempts must be positive")
	}
	query := `with lost as (
			    update coordinate_index_sets
			    set status = 'failed', last_error = $5
			    where status = 'pending' and next_attempt_at <= $1 and attempts >= $4
			  ), picked as (
			    select artifact_id
			    from coordinate_index_sets
			    where status = 'pending' and next_attempt_at <= $1 and attempts < $4
			    order by fill, next_attempt_at, artifact_id
			    limit $2
			    for update skip locked
			  ), claimed as (
			    update coordinate_index_sets
			    set next_attempt_at = $3, attempts = attempts + 1
			    where artifact_id in (select artifact_id from picked)
			    returning artifact_id, attempts
			  )
			  select claimed.artifact_id, claimed.attempts, artifacts.name, artifacts.uri, artifacts.format,
			         artifact_checksums.copy_status, artifact_checksums.stored_uri
			  from claimed
			  join artifacts on artifacts.id = claimed.artifact_id
			  left join artifact_checksums on artifact_checksums.artifact_id = claimed.artifact_id
			  order by claimed.artifact_id`

	rows := make([]coordinateIndexClaimRow, 0)
	lost := fmt.Sprintf(lostReadsReason, maxAttempts)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, now, limit, now.Add(lease), maxAttempts, lost); err != nil {
		return nil, fmt.Errorf("claim due coordinate index sets: %w", err)
	}
	claims := make([]models.CoordinateIndexClaim, 0, len(rows))
	for _, row := range rows {
		claim := models.CoordinateIndexClaim{
			ArtifactID: row.ArtifactID,
			Name:       row.Name,
			URI:        stringPtrFromSQL(row.URI),
			Format:     stringPtrFromSQL(row.Format),
			StoredURI:  stringPtrFromSQL(row.StoredURI),
			Attempts:   row.Attempts,
		}
		if row.CopyStatus.Valid {
			copyStatus := models.CopyStatus(row.CopyStatus.String)
			if err := copyStatus.Validate(); err != nil {
				return nil, fmt.Errorf("claim of artifact %s: %w", row.ArtifactID, err)
			}
			claim.CopyStatus = &copyStatus
		}
		claims = append(claims, claim)
	}
	return claims, nil
}

// HasPendingFill reports whether any set the fill queued is still pending.
func (r *CoordinateIndexRepository) HasPendingFill(ctx context.Context) (bool, error) {
	var pending bool
	query := `select exists (select 1 from coordinate_index_sets where status = 'pending' and fill)`
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &pending, query); err != nil {
		return false, fmt.Errorf("check pending fill sets: %w", err)
	}
	return pending, nil
}

// AddMissingSince gives a pending set to every model file in our bucket created since the given
// time that has none: uploads the API stored while an older backend, which made no sets, was
// still serving. It reads each user's recent artifacts through the (created_by, created_at)
// index, so it never scans the whole table. With archiveFiles it also takes the external model
// files (archive files on RCSB) whose copy the checksum job recorded since that time, however
// long after their artifact was created, through the index of copied rows by checked_at. It
// returns how many sets it added.
func (r *CoordinateIndexRepository) AddMissingSince(ctx context.Context, since time.Time, archiveFiles bool) (int64, error) {
	query := `insert into coordinate_index_sets (artifact_id)
			  select recent.id
			  from users
			  join lateral (
			    select artifacts.id, artifacts.type, artifacts.uri
			    from artifacts
			    where artifacts.created_by = users.id and artifacts.created_at >= $1
			  ) recent on true
			  where recent.type = 'model'
			    and strpos(recent.uri, '/artifacts/' || cast(recent.id as text) || '/') > 0
			  union
			  select artifacts.id
			  from artifact_checksums
			  join artifacts on artifacts.id = artifact_checksums.artifact_id
			  where $2
			    and artifact_checksums.copy_status = 'copied'
			    and artifact_checksums.checked_at >= $1
			    and artifacts.type = 'model'
			  on conflict (artifact_id) do nothing`
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, since, archiveFiles)
	if err != nil {
		return 0, fmt.Errorf("add missing coordinate index sets since %s: %w", since.Format(time.RFC3339), err)
	}
	added, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count added coordinate index sets: %w", err)
	}
	return added, nil
}

// AddFillAfter takes the next limit artifacts in id order after the given id and gives a pending
// fill set to each model file among them that has none: files in our bucket and, with
// archiveFiles, external model files the checksum job copied into our bucket. One page is one
// short statement, so the fill over every artifact never needs one long statement (the checksum
// job's lesson of 2026-10-27). It only inserts, so running it again is harmless.
func (r *CoordinateIndexRepository) AddFillAfter(
	ctx context.Context,
	after uuid.UUID,
	limit int,
	archiveFiles bool,
) (models.MissingSetsPage, error) {
	if limit <= 0 {
		return models.MissingSetsPage{}, errors.New("page limit must be positive")
	}
	query := `with page as (
			    select id, type, uri from artifacts where id > $1 order by id limit $2
			  ), added as (
			    insert into coordinate_index_sets (artifact_id, fill)
			    select page.id, true
			    from page
			    where page.type = 'model'
			      and (strpos(page.uri, '/artifacts/' || cast(page.id as text) || '/') > 0
			           or ($3 and exists (
			             select 1 from artifact_checksums
			             where artifact_checksums.artifact_id = page.id
			               and artifact_checksums.copy_status = 'copied')))
			    on conflict (artifact_id) do nothing
			    returning artifact_id
			  )
			  select (select count(*) from page) as scanned,
			         (select count(*) from added) as added,
			         coalesce((select id from page order by id desc limit 1), $4) as last_artifact_id`
	var row struct {
		Scanned        int       `db:"scanned"`
		Added          int64     `db:"added"`
		LastArtifactID uuid.UUID `db:"last_artifact_id"`
	}
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, after, limit, archiveFiles, uuid.Nil); err != nil {
		return models.MissingSetsPage{}, fmt.Errorf("add coordinate index fill sets after %s: %w", after, err)
	}
	return models.MissingSetsPage{Scanned: row.Scanned, Added: row.Added, LastArtifactID: row.LastArtifactID}, nil
}

// RequeueForNewEntries queues done sets again, ahead of the fill, of the files whose models now
// belong to other entries than when the file was read (fasta_entry_ids): a file read before its
// model existed, linked by a new model of another entry, or switched to by a model revision. The
// read took FASTA lengths from the wrong entries, or none. Only the files of model revisions
// created since the given time are looked at, found through the (created_by, created_at) index as
// in AddMissingSince; the weekly data sync's revisions keep their files and entries, so they queue
// nothing. A change to an entry's FASTA records through the API queues its files at once
// (RequeueEntry). It returns how many sets it queued.
func (r *CoordinateIndexRepository) RequeueForNewEntries(ctx context.Context, since time.Time, now time.Time) (int64, error) {
	query := `with recent_files as (
			    select distinct recent.primary_artifact_id as artifact_id
			    from users
			    join lateral (
			      select model_revisions.primary_artifact_id
			      from model_revisions
			      where model_revisions.created_by = users.id and model_revisions.created_at >= $1
			    ) recent on true
			    where recent.primary_artifact_id is not null
			  ), stale as (
			    select sets.artifact_id
			    from recent_files
			    join coordinate_index_sets sets on sets.artifact_id = recent_files.artifact_id
			    where sets.status = 'done'
			      and sets.fasta_entry_ids <> coalesce((
			        select array_agg(distinct models.entry_id order by models.entry_id)
			        from model_revisions
			        join models on models.id = model_revisions.model_id
			        where model_revisions.primary_artifact_id = sets.artifact_id
			      ), '{}')
			  )
			  update coordinate_index_sets
			  set status = 'pending', fill = false, attempts = 0, next_attempt_at = $2, last_error = null
			  from stale
			  where coordinate_index_sets.artifact_id = stale.artifact_id`
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, since, now)
	if err != nil {
		return 0, fmt.Errorf("queue coordinate index sets of models since %s: %w", since.Format(time.RFC3339), err)
	}
	queued, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count queued coordinate index sets: %w", err)
	}
	return queued, nil
}

// RequeueForEnsembleTypeChanges queues done sets again, ahead of the fill, whose last read with
// conformer counts saw another Ensemble state than the file's models have now
// (read_as_ensemble_type against ensembleTypedFile): ensembles get no counts, so that read stored
// the wrong ones, or none. It looks at two groups:
//   - the files of model revisions created since the given time, found as RequeueForNewEntries
//     finds them: a new revision that changes the type;
//   - every file read as an Ensemble's by its type (conformers 'ensemble_model_type'), whatever
//     its age, through a partial index: a review decision (a rejected Ensemble revision) changes a
//     revision's state, not its creation time, and such a file would otherwise keep no counts.
//     There are few: single-frame Ensemble files.
//
// The job calls it only while it stores conformer counts: a read without them leaves
// read_as_ensemble_type as it was, so the same files would be queued on every run. A counted
// file whose model becomes an Ensemble by a review decision alone keeps its counts until its next
// read; the residue filter leaves out Ensemble models by their type. It returns how many sets it
// queued.
func (r *CoordinateIndexRepository) RequeueForEnsembleTypeChanges(ctx context.Context, since time.Time, now time.Time) (int64, error) {
	query := `with recent_files as (
			    select distinct recent.primary_artifact_id as artifact_id
			    from users
			    join lateral (
			      select model_revisions.primary_artifact_id
			      from model_revisions
			      where model_revisions.created_by = users.id and model_revisions.created_at >= $1
			    ) recent on true
			    where recent.primary_artifact_id is not null
			  ), candidates as (
			    select artifact_id from recent_files
			    union
			    select artifact_id from coordinate_index_sets
			    where status = 'done' and conformers = 'ensemble_model_type'
			  ), stale as (
			    select sets.artifact_id
			    from candidates
			    join coordinate_index_sets sets on sets.artifact_id = candidates.artifact_id
			    where sets.status = 'done'
			      and sets.read_as_ensemble_type is not null
			      and sets.read_as_ensemble_type <> ` + fmt.Sprintf(ensembleTypedFile, "sets.artifact_id") + `
			  )
			  update coordinate_index_sets
			  set status = 'pending', fill = false, attempts = 0, next_attempt_at = $2, last_error = null
			  from stale
			  where coordinate_index_sets.artifact_id = stale.artifact_id`
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, since, now)
	if err != nil {
		return 0, fmt.Errorf("queue coordinate index sets whose ensemble type changed since %s: %w", since.Format(time.RFC3339), err)
	}
	queued, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count queued coordinate index sets: %w", err)
	}
	return queued, nil
}

// RequeueForConformersAfter takes the next limit sets in artifact order after the given id and
// sends each done one whose conformer counts are missing or from another cif.ResidueCountVersion
// back to pending as a fill row: the file is read again, its chains as before, with conformer
// counts. Attempts start over. Pending and failed sets are left as they are: a pending one is read
// with counts anyway, and a failed one failed on a read limit or a file that cannot be read. One
// page is one short statement; running it again is harmless.
func (r *CoordinateIndexRepository) RequeueForConformersAfter(
	ctx context.Context,
	after uuid.UUID,
	limit int,
	version string,
	now time.Time,
) (models.RequeuedSetsPage, error) {
	if limit <= 0 {
		return models.RequeuedSetsPage{}, errors.New("page limit must be positive")
	}
	if version == "" {
		return models.RequeuedSetsPage{}, errors.New("conformer count version must not be empty")
	}
	query := `with page as (
			    select artifact_id from coordinate_index_sets where artifact_id > $1 order by artifact_id limit $2
			  ), requeued as (
			    update coordinate_index_sets sets
			    set status = 'pending', fill = true, attempts = 0, next_attempt_at = $4, last_error = null
			    from page
			    where sets.artifact_id = page.artifact_id
			      and sets.status = 'done'
			      and sets.residues_version is distinct from $3
			    returning sets.artifact_id
			  )
			  select (select count(*) from page) as scanned,
			         (select count(*) from requeued) as requeued,
			         coalesce((select artifact_id from page order by artifact_id desc limit 1), $5) as last_artifact_id`
	var row struct {
		Scanned        int       `db:"scanned"`
		Requeued       int64     `db:"requeued"`
		LastArtifactID uuid.UUID `db:"last_artifact_id"`
	}
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, after, limit, version, now, uuid.Nil); err != nil {
		return models.RequeuedSetsPage{}, fmt.Errorf("queue coordinate index sets for conformer counts after %s: %w", after, err)
	}
	return models.RequeuedSetsPage{Scanned: row.Scanned, Requeued: row.Requeued, LastArtifactID: row.LastArtifactID}, nil
}

// ModelFileEnsembleType reports whether the latest revision using a model file of one of the file's
// models is an Ensemble or has member_count (ensembleTypedFile).
func (r *CoordinateIndexRepository) ModelFileEnsembleType(ctx context.Context, artifactID uuid.UUID) (bool, error) {
	var ensemble bool
	query := `select ` + fmt.Sprintf(ensembleTypedFile, "$1")
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &ensemble, query, artifactID); err != nil {
		return false, fmt.Errorf("read model types of model file %s: %w", artifactID, err)
	}
	return ensemble, nil
}

// RequeueEntry queues the model files of an entry's models again, ahead of the fill, after the API
// changed the entry's active FASTA records (created or deleted some): a chain whose length came
// from a record may now have another one, or none. Run it in the transaction that changes the
// records. It takes the row lock of each set, so a save of a file being read either finishes first
// (and the file is queued again) or waits and reads the new records (SaveCoordinateIndex). Failed
// sets stay failed. It returns how many sets it queued.
func (r *CoordinateIndexRepository) RequeueEntry(ctx context.Context, entryID string) (int64, error) {
	query := `update coordinate_index_sets
			  set status = 'pending', fill = false, attempts = 0, next_attempt_at = now(), last_error = null
			  from (
			    select distinct model_revisions.primary_artifact_id as artifact_id
			    from models
			    join model_revisions on model_revisions.model_id = models.id
			    where models.entry_id = $1 and model_revisions.primary_artifact_id is not null
			  ) files
			  where coordinate_index_sets.artifact_id = files.artifact_id
			    and coordinate_index_sets.status in ('pending', 'done')`
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, entryID)
	if err != nil {
		return 0, fmt.Errorf("queue coordinate index sets of entry %s: %w", entryID, err)
	}
	queued, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count queued coordinate index sets: %w", err)
	}
	return queued, nil
}

// RecordFailure records why a claimed file could not be read. With a retry time the set goes
// back to pending until then; without one it is failed for good.
func (r *CoordinateIndexRepository) RecordFailure(
	ctx context.Context,
	artifactID uuid.UUID,
	message string,
	retryAt *time.Time,
) error {
	query := `update coordinate_index_sets
			  set status = 'failed', last_error = $2
			  where artifact_id = $1 and status = 'pending'`
	args := []any{artifactID, message}
	if retryAt != nil {
		query = `update coordinate_index_sets
				 set next_attempt_at = $3, last_error = $2
				 where artifact_id = $1 and status = 'pending'`
		args = append(args, *retryAt)
	}
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("record coordinate index failure: %w", err)
	}
	if err := requireOneCoordinateIndexSet(result, artifactID); err != nil {
		return fmt.Errorf("record coordinate index failure: %w", err)
	}
	return nil
}

type fastaRecordLengthRow struct {
	EntryID string `db:"entry_id"`
	Header  string `db:"header"`
	Length  int    `db:"length"`
}

// ModelFileFASTA returns the entries of the models whose revisions use a model file, and the
// FASTA records of each entry's active revision.
func (r *CoordinateIndexRepository) ModelFileFASTA(ctx context.Context, artifactID uuid.UUID) (models.ModelFileFASTA, error) {
	querier := r.queriers.Querier(ctx, r.db)
	fasta := models.ModelFileFASTA{EntryIDs: []string{}, Records: []models.FASTARecordLength{}}
	entriesQuery := `select distinct models.entry_id
			  from model_revisions
			  join models on models.id = model_revisions.model_id
			  where model_revisions.primary_artifact_id = $1
			  order by 1`
	if err := querier.SelectContext(ctx, &fasta.EntryIDs, entriesQuery, artifactID); err != nil {
		return models.ModelFileFASTA{}, fmt.Errorf("list entries of model file %s: %w", artifactID, err)
	}
	if len(fasta.EntryIDs) == 0 {
		return fasta, nil
	}
	rows := make([]fastaRecordLengthRow, 0)
	recordsQuery := `select entry_revisions.entry_id, protein_sequences.header,
			         char_length(protein_sequences.sequence) as length
			  from entry_revisions
			  join protein_sequences on protein_sequences.entry_revision_id = entry_revisions.id
			  where entry_revisions.entry_id = any($1) and entry_revisions.state = 'active'
			  order by 1, protein_sequences.source_artifact_id, protein_sequences.record_index`
	if err := querier.SelectContext(ctx, &rows, recordsQuery, pq.Array(fasta.EntryIDs)); err != nil {
		return models.ModelFileFASTA{}, fmt.Errorf("list FASTA records of model file %s: %w", artifactID, err)
	}
	for _, row := range rows {
		fasta.Records = append(fasta.Records, models.FASTARecordLength{EntryID: row.EntryID, Header: row.Header, Length: row.Length})
	}
	return fasta, nil
}

type coordinateIndexSetRow struct {
	ArtifactID    uuid.UUID      `db:"artifact_id"`
	Status        string         `db:"status"`
	Fill          bool           `db:"fill"`
	ReaderVersion sql.NullString `db:"reader_version"`
	Warnings      []byte         `db:"warnings"`
	Attempts      int            `db:"attempts"`
	LastError     sql.NullString `db:"last_error"`
	ReadAt        sql.NullTime   `db:"read_at"`
	FASTAEntryIDs pq.StringArray `db:"fasta_entry_ids"`
	// The conformer columns: all null until a read with conformer counts.
	ResiduesVersion        sql.NullString `db:"residues_version"`
	Conformers             sql.NullString `db:"conformers"`
	ResidueKey             sql.NullString `db:"residue_key"`
	Residues               sql.NullInt64  `db:"residues"`
	MultiConformerResidues sql.NullInt64  `db:"multi_conformer_residues"`
	ReadAsEnsembleType     sql.NullBool   `db:"read_as_ensemble_type"`
}

type coordinateIndexResidueCountRow struct {
	ResidueType    string `db:"residue_type"`
	ConformerCount int    `db:"conformer_count"`
	Residues       int    `db:"residues"`
}

type coordinateIndexChainRow struct {
	ArtifactID      uuid.UUID      `db:"artifact_id"`
	AuthAsymID      string         `db:"auth_asym_id"`
	EntityID        sql.NullString `db:"entity_id"`
	DepositedLength sql.NullInt64  `db:"deposited_length"`
	LengthSource    sql.NullString `db:"length_source"`
	ModeledResidues int            `db:"modeled_residues"`
	Protein         bool           `db:"protein"`
	ProteinSource   string         `db:"protein_source"`
}

// Get returns the set of a model file with its chains, or ErrCoordinateIndexSetNotFound when the
// file has none.
func (r *CoordinateIndexRepository) Get(ctx context.Context, artifactID uuid.UUID) (*models.CoordinateIndexSet, error) {
	query := `select artifact_id, status, fill, reader_version, warnings, attempts, last_error, read_at, fasta_entry_ids,
			         residues_version, conformers, residue_key, residues, multi_conformer_residues, read_as_ensemble_type
			  from coordinate_index_sets
			  where artifact_id = $1`
	var row coordinateIndexSetRow
	querier := r.queriers.Querier(ctx, r.db)
	if err := querier.GetContext(ctx, &row, query, artifactID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCoordinateIndexSetNotFound
		}
		return nil, fmt.Errorf("get coordinate index set: %w", err)
	}
	set, err := row.toModel()
	if err != nil {
		return nil, fmt.Errorf("read coordinate index set %s: %w", artifactID, err)
	}
	chains := make([]coordinateIndexChainRow, 0)
	chainsQuery := `select ` + coordinateIndexChainColumns + `
			  from coordinate_index_chains
			  where artifact_id = $1
			  order by auth_asym_id`
	if err := querier.SelectContext(ctx, &chains, chainsQuery, artifactID); err != nil {
		return nil, fmt.Errorf("list coordinate index chains: %w", err)
	}
	set.Chains = make([]models.IndexedChain, 0, len(chains))
	for _, chainRow := range chains {
		chain, err := chainRow.toModel()
		if err != nil {
			return nil, fmt.Errorf("read chain %q of %s: %w", chainRow.AuthAsymID, artifactID, err)
		}
		set.Chains = append(set.Chains, chain)
	}
	if set.Conformers != nil {
		counts := make([]coordinateIndexResidueCountRow, 0)
		countsQuery := `select residue_type, conformer_count, residues
				  from coordinate_index_residue_counts
				  where artifact_id = $1
				  order by residue_type, conformer_count`
		if err := querier.SelectContext(ctx, &counts, countsQuery, artifactID); err != nil {
			return nil, fmt.Errorf("list coordinate index residue counts: %w", err)
		}
		set.Conformers.Counts = make([]models.ResidueConformerCount, 0, len(counts))
		for _, count := range counts {
			set.Conformers.Counts = append(set.Conformers.Counts, models.ResidueConformerCount{
				ResidueType: count.ResidueType, ConformerCount: count.ConformerCount, Residues: count.Residues,
			})
		}
	}
	return set, nil
}

func (row coordinateIndexSetRow) toModel() (*models.CoordinateIndexSet, error) {
	status := models.CoordinateIndexStatus(row.Status)
	if err := status.Validate(); err != nil {
		return nil, fmt.Errorf("read status: %w", err)
	}
	warnings := []string{}
	if err := unmarshalJSON(row.Warnings, &warnings); err != nil {
		return nil, fmt.Errorf("decode warnings: %w", err)
	}
	set := &models.CoordinateIndexSet{
		ArtifactID:    row.ArtifactID,
		Status:        status,
		Fill:          row.Fill,
		ReaderVersion: stringPtrFromSQL(row.ReaderVersion),
		Warnings:      warnings,
		Attempts:      row.Attempts,
		LastError:     stringPtrFromSQL(row.LastError),
	}
	if row.ReadAt.Valid {
		set.ReadAt = &row.ReadAt.Time
	}
	set.FASTAEntryIDs = append([]string{}, row.FASTAEntryIDs...)
	conformers, err := row.conformers()
	if err != nil {
		return nil, err
	}
	set.Conformers = conformers
	return set, nil
}

// conformers reads the conformer columns of a set; nil when it was never read with conformer
// counts. The counts themselves are read separately.
func (row coordinateIndexSetRow) conformers() (*models.FileConformers, error) {
	if !row.ResiduesVersion.Valid {
		return nil, nil
	}
	scope := models.ConformerScope(row.Conformers.String)
	if err := scope.Validate(); err != nil {
		return nil, fmt.Errorf("read conformer scope: %w", err)
	}
	conformers := &models.FileConformers{
		Version:            row.ResiduesVersion.String,
		Scope:              scope,
		ReadAsEnsembleType: row.ReadAsEnsembleType.Bool,
		Counts:             []models.ResidueConformerCount{},
	}
	if row.ResidueKey.Valid {
		key := models.ResidueKey(row.ResidueKey.String)
		if err := key.Validate(); err != nil {
			return nil, fmt.Errorf("read residue key: %w", err)
		}
		conformers.Key = &key
	}
	if row.Residues.Valid {
		residues := int(row.Residues.Int64)
		conformers.Residues = &residues
	}
	if row.MultiConformerResidues.Valid {
		multi := int(row.MultiConformerResidues.Int64)
		conformers.MultiConformerResidues = &multi
	}
	return conformers, nil
}

func (row coordinateIndexChainRow) toModel() (models.IndexedChain, error) {
	chain := models.IndexedChain{
		AuthAsymID:      row.AuthAsymID,
		EntityID:        row.EntityID.String,
		ModeledResidues: row.ModeledResidues,
		Protein:         row.Protein,
		ProteinSource:   models.ProteinSource(row.ProteinSource),
	}
	if err := chain.ProteinSource.Validate(); err != nil {
		return models.IndexedChain{}, fmt.Errorf("read protein source: %w", err)
	}
	if row.DepositedLength.Valid {
		length := int(row.DepositedLength.Int64)
		chain.DepositedLength = &length
	}
	if row.LengthSource.Valid {
		source := models.ChainLengthSource(row.LengthSource.String)
		if err := source.Validate(); err != nil {
			return models.IndexedChain{}, fmt.Errorf("read length source: %w", err)
		}
		chain.LengthSource = &source
	}
	return chain, nil
}

// replaceChains stores the chains of one set, replacing any it had.
func (r *CoordinateIndexRepository) replaceChains(ctx context.Context, artifactID uuid.UUID, chains []models.IndexedChain) error {
	querier := r.queriers.Querier(ctx, r.db)
	if _, err := querier.ExecContext(ctx, `delete from coordinate_index_chains where artifact_id = $1`, artifactID); err != nil {
		return fmt.Errorf("delete old coordinate index chains: %w", err)
	}
	for start := 0; start < len(chains); start += coordinateIndexChainInsertBatch {
		batch := chains[start:min(start+coordinateIndexChainInsertBatch, len(chains))]
		args := make([]any, 0, len(batch)*8)
		placeholders := make([]string, 0, len(batch))
		for offset, chain := range batch {
			var lengthSource any
			if chain.LengthSource != nil {
				if err := chain.LengthSource.Validate(); err != nil {
					return fmt.Errorf("chain %d: %w", start+offset, err)
				}
				lengthSource = string(*chain.LengthSource)
			}
			if err := chain.ProteinSource.Validate(); err != nil {
				return fmt.Errorf("chain %d: %w", start+offset, err)
			}
			chainArgs := []any{
				artifactID, chain.AuthAsymID, nullIfEmpty(chain.EntityID), chain.DepositedLength, lengthSource,
				chain.ModeledResidues, chain.Protein, string(chain.ProteinSource),
			}
			rowPlaceholders := make([]string, len(chainArgs))
			for index := range chainArgs {
				rowPlaceholders[index] = fmt.Sprintf("$%d", len(args)+index+1)
			}
			args = append(args, chainArgs...)
			placeholders = append(placeholders, "("+strings.Join(rowPlaceholders, ", ")+")")
		}
		query := "insert into coordinate_index_chains(" + coordinateIndexChainColumns + ") values " +
			strings.Join(placeholders, ", ")
		if _, err := querier.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert coordinate index chains: %w", err)
		}
	}
	return nil
}

// replaceConformers stores the conformer counts of one set and its conformer columns, replacing
// what an earlier read stored. The set must be pending (locked by lockPending).
func (r *CoordinateIndexRepository) replaceConformers(ctx context.Context, artifactID uuid.UUID, conformers models.FileConformers) error {
	if err := conformers.Scope.Validate(); err != nil {
		return fmt.Errorf("conformers: %w", err)
	}
	var key any
	if conformers.Key != nil {
		if err := conformers.Key.Validate(); err != nil {
			return fmt.Errorf("conformers: %w", err)
		}
		key = string(*conformers.Key)
	}
	querier := r.queriers.Querier(ctx, r.db)
	if _, err := querier.ExecContext(ctx, `delete from coordinate_index_residue_counts where artifact_id = $1`, artifactID); err != nil {
		return fmt.Errorf("delete old coordinate index residue counts: %w", err)
	}
	for start := 0; start < len(conformers.Counts); start += coordinateIndexResidueCountInsertBatch {
		batch := conformers.Counts[start:min(start+coordinateIndexResidueCountInsertBatch, len(conformers.Counts))]
		args := make([]any, 0, len(batch)*4)
		placeholders := make([]string, 0, len(batch))
		for _, count := range batch {
			next := len(args)
			placeholders = append(placeholders, fmt.Sprintf("($%d, $%d, $%d, $%d)", next+1, next+2, next+3, next+4))
			args = append(args, artifactID, count.ResidueType, count.ConformerCount, count.Residues)
		}
		query := "insert into coordinate_index_residue_counts(artifact_id, residue_type, conformer_count, residues) values " +
			strings.Join(placeholders, ", ")
		if _, err := querier.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert coordinate index residue counts: %w", err)
		}
	}
	query := `update coordinate_index_sets
			  set residues_version = $2, conformers = $3, residue_key = $4, residues = $5,
			      multi_conformer_residues = $6, read_as_ensemble_type = $7
			  where artifact_id = $1 and status = 'pending'`
	updated, err := querier.ExecContext(ctx, query, artifactID, conformers.Version, string(conformers.Scope), key,
		conformers.Residues, conformers.MultiConformerResidues, conformers.ReadAsEnsembleType)
	if err != nil {
		return fmt.Errorf("store conformer columns: %w", err)
	}
	if err := requireOneCoordinateIndexSet(updated, artifactID); err != nil {
		return fmt.Errorf("store conformer columns: %w", err)
	}
	return nil
}

// lockPending takes the row lock of a pending set for the rest of the transaction, waiting for a
// transaction that queues it again (RequeueEntry). A set that is no longer pending was finished by
// another run, or does not exist.
func (r *CoordinateIndexRepository) lockPending(ctx context.Context, artifactID uuid.UUID) error {
	var locked uuid.UUID
	query := `select artifact_id from coordinate_index_sets where artifact_id = $1 and status = 'pending' for update`
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &locked, query, artifactID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("artifact %s: %w", artifactID, ErrCoordinateIndexSetNotFound)
		}
		return fmt.Errorf("lock coordinate index set: %w", err)
	}
	return nil
}

// markDone records a finished read.
func (r *CoordinateIndexRepository) markDone(
	ctx context.Context,
	params SaveCoordinateIndexParams,
	warnings []string,
	entryIDs []string,
) error {
	// marshalJSON writes nil as an object; the column holds a list.
	encoded, err := marshalJSON(append([]string{}, warnings...))
	if err != nil {
		return fmt.Errorf("encode warnings: %w", err)
	}
	query := `update coordinate_index_sets
			  set status = 'done', reader_version = $2, warnings = cast($3 as jsonb), read_at = $4,
			      fasta_entry_ids = $5, last_error = null
			  where artifact_id = $1 and status = 'pending'`
	updated, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query,
		params.ArtifactID, params.ReaderVersion, encoded, params.ReadAt, pq.Array(append([]string{}, entryIDs...)))
	if err != nil {
		return fmt.Errorf("mark coordinate index set done: %w", err)
	}
	if err := requireOneCoordinateIndexSet(updated, params.ArtifactID); err != nil {
		return fmt.Errorf("mark coordinate index set done: %w", err)
	}
	return nil
}

// requireOneCoordinateIndexSet fails unless a statement changed exactly one set: a set that is no
// longer pending was finished by another run, or does not exist.
func requireOneCoordinateIndexSet(result sql.Result, artifactID uuid.UUID) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count changed rows: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("artifact %s: %w", artifactID, ErrCoordinateIndexSetNotFound)
	}
	return nil
}

// ResolvedIndex is what a read stores: its chains, lengths from the entry FASTA included, and its
// warnings.
type ResolvedIndex struct {
	Chains   []models.IndexedChain
	Warnings []string
}

// SaveCoordinateIndexParams is one finished read of a model file.
type SaveCoordinateIndexParams struct {
	ArtifactID    uuid.UUID
	ReaderVersion string
	ReadAt        time.Time
	// Resolve turns the read into what is stored, given the FASTA records of the entries of the
	// file's models as the save transaction sees them.
	Resolve func(fasta models.ModelFileFASTA) (ResolvedIndex, error)
	// Conformers, when set, also replaces the file's conformer counts with what it returns, given
	// whether a model using the file is an Ensemble as the save transaction sees it
	// (ModelFileEnsembleType). When nil, the stored counts stay as they are.
	Conformers func(ensembleType bool) (models.FileConformers, error)
}

// SaveCoordinateIndex locks the pending set of a model file, reads the entry FASTA records the
// chains may take lengths from, replaces the file's chains (and, with params.Conformers, its
// conformer counts) and marks the set done with the entries it read them from. It all commits together or not at all, so a search never sees a file half
// saved; until a set is saved again, the search keeps reading the chains of its last read. The
// records are read after the lock: a save racing an API change to them either reads the new
// records or finishes before the change queues the file again (RequeueEntry).
func (d *DB) SaveCoordinateIndex(ctx context.Context, params SaveCoordinateIndexParams) error {
	if params.Resolve == nil {
		return errors.New("save coordinate index: no resolve function")
	}
	err := d.Do(ctx, func(ctx context.Context) error {
		if err := d.CoordinateIndex.lockPending(ctx, params.ArtifactID); err != nil {
			return err
		}
		fasta, err := d.CoordinateIndex.ModelFileFASTA(ctx, params.ArtifactID)
		if err != nil {
			return fmt.Errorf("read entry FASTA records: %w", err)
		}
		resolved, err := params.Resolve(fasta)
		if err != nil {
			return fmt.Errorf("resolve chains: %w", err)
		}
		if err := d.CoordinateIndex.replaceChains(ctx, params.ArtifactID, resolved.Chains); err != nil {
			return fmt.Errorf("store chains: %w", err)
		}
		if params.Conformers != nil {
			ensembleType, err := d.CoordinateIndex.ModelFileEnsembleType(ctx, params.ArtifactID)
			if err != nil {
				return fmt.Errorf("read model types: %w", err)
			}
			conformers, err := params.Conformers(ensembleType)
			if err != nil {
				return fmt.Errorf("resolve conformers: %w", err)
			}
			if err := d.CoordinateIndex.replaceConformers(ctx, params.ArtifactID, conformers); err != nil {
				return fmt.Errorf("store conformers: %w", err)
			}
		}
		if err := d.CoordinateIndex.markDone(ctx, params, resolved.Warnings, fasta.EntryIDs); err != nil {
			return fmt.Errorf("finish the set: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save coordinate index of %s: %w", params.ArtifactID, err)
	}
	return nil
}
