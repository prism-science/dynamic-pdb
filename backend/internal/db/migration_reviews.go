package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

// MigrationReviewsRepository writes schema_migration_review: what a migration or a job could not
// settle on its own, one row per object and issue.
type MigrationReviewsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewMigrationReviewsRepository(database *sqlx.DB, queriers *QuerierProvider) *MigrationReviewsRepository {
	return &MigrationReviewsRepository{
		db:       database,
		queriers: queriers,
	}
}

// Open records review for a person to look at. A row for the same object and issue takes the new
// details and is open again, even when a job resolved it before; it keeps when it was first made.
func (r *MigrationReviewsRepository) Open(ctx context.Context, review models.MigrationReview) error {
	details, err := json.Marshal(review.Details)
	if err != nil {
		return fmt.Errorf("encode %s review details of %s %s: %w", review.Issue, review.ObjectType, review.ObjectID, err)
	}
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx,
		`insert into schema_migration_review (object_type, object_id, issue, details)
		 values ($1, $2, $3, $4)
		 on conflict (object_type, object_id, issue)
		 do update set details = excluded.details, resolved_at = null, resolved_by = null`,
		review.ObjectType, review.ObjectID, review.Issue, details,
	); err != nil {
		return fmt.Errorf("open %s review row of %s %s: %w", review.Issue, review.ObjectType, review.ObjectID, err)
	}
	return nil
}

// Resolve marks the open schema_migration_review rows resolution names as resolved by
// resolution.ResolvedBy. Rows that are resolved already keep when and by whom.
func (r *MigrationReviewsRepository) Resolve(ctx context.Context, resolution models.MigrationReviewResolution) error {
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
