package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

// ArtifactChecksumsRepository is the checksum job's queue: one row per artifact in
// artifact_checksums, taken when due and finished with a recorded result.
type ArtifactChecksumsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewArtifactChecksumsRepository(database *sqlx.DB, queriers *QuerierProvider) *ArtifactChecksumsRepository {
	return &ArtifactChecksumsRepository{
		db:       database,
		queriers: queriers,
	}
}

// AddMissingAfter takes the next limit artifacts in id order after the given id and adds a
// pending row for each of them that has none. One page is one short statement, so filling
// the table for every artifact never needs one long statement. It only inserts, so running
// it again is harmless.
func (r *ArtifactChecksumsRepository) AddMissingAfter(
	ctx context.Context,
	after uuid.UUID,
	limit int,
) (models.MissingChecksumsPage, error) {
	if limit <= 0 {
		return models.MissingChecksumsPage{}, errors.New("page limit must be positive")
	}
	query := `with page as (
			    select id from artifacts where id > $1 order by id limit $2
			  ), added as (
			    insert into artifact_checksums(artifact_id)
			    select page.id
			    from page
			    where not exists (
			      select 1 from artifact_checksums where artifact_checksums.artifact_id = page.id
			    )
			    on conflict (artifact_id) do nothing
			    returning artifact_id
			  )
			  select (select count(*) from page) as scanned,
			         (select count(*) from added) as added,
			         coalesce((select id from page order by id desc limit 1), $3) as last_artifact_id`
	var row struct {
		Scanned        int       `db:"scanned"`
		Added          int64     `db:"added"`
		LastArtifactID uuid.UUID `db:"last_artifact_id"`
	}
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, after, limit, uuid.Nil); err != nil {
		return models.MissingChecksumsPage{}, fmt.Errorf("add missing artifact checksums: %w", err)
	}
	return models.MissingChecksumsPage{
		Scanned:        row.Scanned,
		Added:          row.Added,
		LastArtifactID: row.LastArtifactID,
	}, nil
}

// ClaimDue takes up to limit pending rows that are due at now, oldest first, together with
// every other due row whose artifact has the same URI, so each URI is read once. A claim
// moves next_attempt_at to now+lease and counts the attempt; a claim that is never recorded
// becomes due again when the lease ends.
func (r *ArtifactChecksumsRepository) ClaimDue(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
) ([]models.ChecksumClaim, error) {
	if limit <= 0 {
		return nil, errors.New("claim limit must be positive")
	}
	if lease <= 0 {
		return nil, errors.New("claim lease must be positive")
	}
	query := `with picked as (
			    select artifact_id
			    from artifact_checksums
			    where status = 'pending' and next_attempt_at <= $1
			    order by next_attempt_at, artifact_id
			    limit $2
			    for update skip locked
			  ), same_uri as (
			    select artifact_checksums.artifact_id
			    from artifacts
			    join artifact_checksums on artifact_checksums.artifact_id = artifacts.id
			    where artifacts.uri in (
			            select picked_artifacts.uri
			            from picked
			            join artifacts picked_artifacts on picked_artifacts.id = picked.artifact_id
			          )
			      and artifact_checksums.status = 'pending'
			      and artifact_checksums.next_attempt_at <= $1
			    for update of artifact_checksums skip locked
			  ), claimed as (
			    update artifact_checksums
			    set next_attempt_at = $3, attempts = attempts + 1
			    where artifact_id in (select artifact_id from picked union select artifact_id from same_uri)
			    returning artifact_id, attempts
			  )
			  select claimed.artifact_id, claimed.attempts, artifacts.name, artifacts.uri, artifacts.sha256, artifacts.size_bytes
			  from claimed
			  join artifacts on artifacts.id = claimed.artifact_id
			  order by artifacts.uri, claimed.artifact_id`

	rows := make([]checksumClaimRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, now, limit, now.Add(lease)); err != nil {
		return nil, fmt.Errorf("claim due artifact checksums: %w", err)
	}
	claims := make([]models.ChecksumClaim, 0, len(rows))
	for _, row := range rows {
		claims = append(claims, models.ChecksumClaim{
			ArtifactID: row.ArtifactID,
			Name:       row.Name,
			URI:        stringPtrFromSQL(row.URI),
			SHA256:     stringPtrFromSQL(row.SHA256),
			SizeBytes:  int64PtrFromSQL(row.SizeBytes),
			Attempts:   row.Attempts,
		})
	}
	return claims, nil
}

// Locations returns the entry, and for model files the model, of each artifact. Artifacts
// that are linked to no revision are left out.
func (r *ArtifactChecksumsRepository) Locations(
	ctx context.Context,
	artifactIDs []uuid.UUID,
) (map[uuid.UUID]models.ArtifactLocation, error) {
	locations := make(map[uuid.UUID]models.ArtifactLocation, len(artifactIDs))
	if len(artifactIDs) == 0 {
		return locations, nil
	}
	query := `select distinct on (artifact_id) artifact_id, entry_id, model_id
			  from (
			    select model_revision_artifacts.artifact_id, models.entry_id, models.id as model_id
			    from model_revision_artifacts
			    join model_revisions on model_revisions.id = model_revision_artifacts.model_revision_id
			    join models on models.id = model_revisions.model_id
			    where model_revision_artifacts.artifact_id = any($1)
			    union all
			    select entry_revision_artifacts.artifact_id, entry_revisions.entry_id, null
			    from entry_revision_artifacts
			    join entry_revisions on entry_revisions.id = entry_revision_artifacts.entry_revision_id
			    where entry_revision_artifacts.artifact_id = any($1)
			  ) links
			  order by artifact_id, model_id nulls last, entry_id`

	rows := make([]artifactLocationRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, pq.Array(artifactIDs)); err != nil {
		return nil, fmt.Errorf("read artifact locations: %w", err)
	}
	for _, row := range rows {
		locations[row.ArtifactID] = models.ArtifactLocation{
			EntryID: row.EntryID,
			ModelID: stringPtrFromSQL(row.ModelID),
		}
	}
	return locations, nil
}

// Record stores the results of claimed rows. A pending result keeps the row queued until
// NextAttemptAt. Run it in a transaction to record a group of results together.
func (r *ArtifactChecksumsRepository) Record(ctx context.Context, results []models.ChecksumResult) error {
	query := `update artifact_checksums
			  set status = $2,
			      sha256 = $3,
			      size_bytes = $4,
			      stored_uri = $5,
			      copy_status = $6,
			      last_error = $7,
			      checked_at = $8,
			      next_attempt_at = coalesce($9, next_attempt_at)
			  where artifact_id = $1`
	for _, result := range results {
		if err := validateChecksumResult(result); err != nil {
			return fmt.Errorf("record checksum of artifact %s: %w", result.ArtifactID, err)
		}
		var checkedAt *time.Time
		if result.Status != models.ChecksumStatusPending {
			checkedAt = &result.CheckedAt
		}
		var copyStatus *string
		if result.CopyStatus != nil {
			value := string(*result.CopyStatus)
			copyStatus = &value
		}
		updated, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query,
			result.ArtifactID, string(result.Status), result.SHA256, result.SizeBytes, result.StoredURI,
			copyStatus, result.Error, checkedAt, result.NextAttemptAt)
		if err != nil {
			return fmt.Errorf("record checksum of artifact %s: %w", result.ArtifactID, err)
		}
		count, err := updated.RowsAffected()
		if err != nil {
			return fmt.Errorf("count recorded checksum of artifact %s: %w", result.ArtifactID, err)
		}
		if count != 1 {
			return fmt.Errorf("record checksum of artifact %s: %w", result.ArtifactID, sql.ErrNoRows)
		}
	}
	return nil
}

// validateChecksumResult checks a result against the table's CHECK constraints, so a wrong
// result fails with a message that names the rule.
func validateChecksumResult(result models.ChecksumResult) error {
	switch result.Status {
	case models.ChecksumStatusVerified:
		if result.SHA256 == nil || result.SizeBytes == nil {
			return errors.New("a verified result needs sha256 and size")
		}
		return validateVerifiedCopy(result)
	case models.ChecksumStatusMismatch:
		if result.SHA256 == nil || result.SizeBytes == nil {
			return errors.New("a mismatch result needs sha256 and size")
		}
		if result.StoredURI != nil {
			return errors.New("a mismatch result keeps no copy")
		}
		return validateMismatchCopy(result)
	case models.ChecksumStatusFailed:
		if result.Error == nil {
			return errors.New("a failed result needs an error")
		}
		if result.CopyStatus != nil {
			return errors.New("a failed result has no copy status")
		}
	case models.ChecksumStatusPending:
		if result.NextAttemptAt == nil || result.Error == nil {
			return errors.New("a retry needs the next attempt time and an error")
		}
		if result.CopyStatus != nil {
			return errors.New("a retry has no copy status")
		}
	default:
		return fmt.Errorf("unknown checksum status: %q", result.Status)
	}
	return nil
}

// validateVerifiedCopy checks that a verified result has a copy status, and a copy exactly when
// the status is copied.
func validateVerifiedCopy(result models.ChecksumResult) error {
	if result.CopyStatus == nil {
		return errors.New("a verified result needs a copy status")
	}
	switch *result.CopyStatus {
	case models.CopyStatusCopied:
		if result.StoredURI == nil {
			return errors.New("a copied result needs the copy's uri")
		}
	case models.CopyStatusOwnStorage,
		models.CopyStatusOverLimit,
		models.CopyStatusCopyFailed,
		models.CopyStatusCopyOff:
		if result.StoredURI != nil {
			return fmt.Errorf("a verified result with copy status %s keeps no copy", *result.CopyStatus)
		}
	case models.CopyStatusMismatch:
		return errors.New("a verified result cannot have copy status mismatch")
	default:
		return fmt.Errorf("unknown copy status: %q", *result.CopyStatus)
	}
	return nil
}

// validateMismatchCopy checks that a mismatch result's copy status is own_storage or mismatch.
func validateMismatchCopy(result models.ChecksumResult) error {
	if result.CopyStatus == nil {
		return errors.New("a mismatch result needs a copy status")
	}
	switch *result.CopyStatus {
	case models.CopyStatusOwnStorage, models.CopyStatusMismatch:
		return nil
	case models.CopyStatusCopied,
		models.CopyStatusOverLimit,
		models.CopyStatusCopyFailed,
		models.CopyStatusCopyOff:
		return fmt.Errorf("a mismatch result cannot have copy status %s", *result.CopyStatus)
	default:
		return fmt.Errorf("unknown copy status: %q", *result.CopyStatus)
	}
}

type checksumClaimRow struct {
	ArtifactID uuid.UUID      `db:"artifact_id"`
	Attempts   int            `db:"attempts"`
	Name       string         `db:"name"`
	URI        sql.NullString `db:"uri"`
	SHA256     sql.NullString `db:"sha256"`
	SizeBytes  sql.NullInt64  `db:"size_bytes"`
}

type artifactLocationRow struct {
	ArtifactID uuid.UUID      `db:"artifact_id"`
	EntryID    string         `db:"entry_id"`
	ModelID    sql.NullString `db:"model_id"`
}

// RecordChecksums records results in one transaction, so all artifacts that share a URI get
// their results together.
func (d *DB) RecordChecksums(ctx context.Context, results []models.ChecksumResult) error {
	if err := d.Do(ctx, func(ctx context.Context) error {
		return d.ArtifactChecksums.Record(ctx, results)
	}); err != nil {
		return fmt.Errorf("record checksums: %w", err)
	}
	return nil
}
