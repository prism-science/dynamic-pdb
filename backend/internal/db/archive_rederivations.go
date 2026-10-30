package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

// ArchiveRederivationsRepository is the archive re-derivation job's log: one row per active
// archive entry revision the job looked at, in archive_rederivations.
type ArchiveRederivationsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewArchiveRederivationsRepository(database *sqlx.DB, queriers *QuerierProvider) *ArchiveRederivationsRepository {
	return &ArchiveRederivationsRepository{
		db:       database,
		queriers: queriers,
	}
}

// ListCandidates returns, in id order after the given id, at most limit active revisions of
// active entries with a PDB ID that the job has not finished. Finished means a real run logged
// any outcome other than skipped_in_review, or failed maxAttempts times. Rows a dry run wrote
// do not count.
func (r *ArchiveRederivationsRepository) ListCandidates(
	ctx context.Context,
	after uuid.UUID,
	limit int,
	maxAttempts int,
) ([]models.ArchiveRederivationCandidate, error) {
	if limit <= 0 {
		return nil, errors.New("candidate limit must be positive")
	}
	if maxAttempts <= 0 {
		return nil, errors.New("max attempts must be positive")
	}
	query := `select revision.id, revision.entry_id,
			         upper(btrim(revision.metadata -> 'external_refs' ->> 'pdb')) as pdb_id
			  from entry_revisions revision
			  join entries on entries.id = revision.entry_id
			  where revision.state = 'active'
			    and revision.entry_state = 'active'
			    and entries.state = 'active'
			    and coalesce(btrim(revision.metadata -> 'external_refs' ->> 'pdb'), '') <> ''
			    and revision.id > $1
			    and not exists (
			      select 1 from archive_rederivations logged
			      where logged.entry_revision_id = revision.id
			        and not logged.dry_run
			        and (logged.outcome not in ('failed', 'skipped_in_review')
			             or (logged.outcome = 'failed' and logged.attempts >= $3))
			    )
			  order by revision.id
			  limit $2`
	var rows []struct {
		EntryRevisionID uuid.UUID `db:"id"`
		EntryID         string    `db:"entry_id"`
		PDBID           string    `db:"pdb_id"`
	}
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows, query, after, limit, maxAttempts); err != nil {
		return nil, fmt.Errorf("list archive re-derivation candidates: %w", err)
	}
	candidates := make([]models.ArchiveRederivationCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, models.ArchiveRederivationCandidate{
			EntryRevisionID: row.EntryRevisionID,
			EntryID:         row.EntryID,
			PDBID:           row.PDBID,
		})
	}
	return candidates, nil
}

// Record logs what the job did to one revision. A real run replaces the revision's row and
// counts the attempt; a dry run writes a row only for a revision that has none.
func (r *ArchiveRederivationsRepository) Record(ctx context.Context, row models.ArchiveRederivation) error {
	before, err := optionalJSONToSQL(row.Before)
	if err != nil {
		return fmt.Errorf("prepare archive re-derivation before values: %w", err)
	}
	after, err := optionalJSONToSQL(row.After)
	if err != nil {
		return fmt.Errorf("prepare archive re-derivation after values: %w", err)
	}
	query := `insert into archive_rederivations(
			    entry_revision_id, entry_id, pdb_id, outcome, dry_run, reference_version, before, after, error
			  ) values ($1, $2, $3, $4, $5, $6, cast($7 as jsonb), cast($8 as jsonb), $9)
			  on conflict (entry_revision_id) do update set
			    outcome = excluded.outcome,
			    dry_run = excluded.dry_run,
			    reference_version = excluded.reference_version,
			    before = excluded.before,
			    after = excluded.after,
			    error = excluded.error,
			    attempts = case when archive_rederivations.dry_run then 1 else archive_rederivations.attempts + 1 end,
			    updated_at = now()
			  where not excluded.dry_run`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query,
		row.EntryRevisionID, row.EntryID, row.PDBID, string(row.Outcome), row.DryRun,
		row.ReferenceVersion, before, after, row.Error,
	); err != nil {
		return fmt.Errorf("record archive re-derivation of %s: %w", row.EntryRevisionID, err)
	}
	return nil
}

// ResolveMigrationReviews marks the open schema_migration_review rows resolution names as resolved
// by resolution.ResolvedBy. Rows that are resolved already keep when and by whom.
func (r *ArchiveRederivationsRepository) ResolveMigrationReviews(
	ctx context.Context,
	resolution models.MigrationReviewResolution,
) error {
	if len(resolution.ObjectIDs) == 0 {
		return nil
	}
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx,
		`update schema_migration_review
		 set resolved_at = now(), resolved_by = $4
		 where object_type = $1 and issue = $2 and object_id = any($3) and resolved_at is null`,
		resolution.ObjectType, resolution.Issue, pq.Array(resolution.ObjectIDs), resolution.ResolvedBy,
	); err != nil {
		return fmt.Errorf("resolve %s review rows: %w", resolution.Issue, err)
	}
	return nil
}
