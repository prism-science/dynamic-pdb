package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
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

// ListOpenForModelRevisions lists, for each of the model revisions, the open review flags its
// search result shows (researcher question 7): flags on its model, on the revision itself, on its
// entry, and on the polymer entities of the entry's active revision. openReviewFlagCondition, the
// model search's flagged filter, uses the same objects. Flags come oldest first.
func (r *MigrationReviewsRepository) ListOpenForModelRevisions(
	ctx context.Context,
	revisionIDs []uuid.UUID,
) (map[uuid.UUID][]models.ReviewFlag, error) {
	flags := make(map[uuid.UUID][]models.ReviewFlag, len(revisionIDs))
	if len(revisionIDs) == 0 {
		return flags, nil
	}
	rows := make([]reviewFlagRow, 0)
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows,
		`with page as (
		   select model_revisions.id as model_revision_id, model_revisions.model_id, models.entry_id
		   from model_revisions
		   join models on models.id = model_revisions.model_id
		   where model_revisions.id = any($1)
		 ), flag_objects as (
		   select model_revision_id, 'model' as object_type, model_id as object_id from page
		   union all
		   select model_revision_id, 'model_revision', cast(model_revision_id as text) from page
		   union all
		   select model_revision_id, 'entry', entry_id from page
		   union all
		   select page.model_revision_id, 'polymer_entity', cast(entity_link.polymer_entity_id as text)
		   from page
		   join entry_revisions on entry_revisions.entry_id = page.entry_id and entry_revisions.state = 'active'
		   join entry_revision_polymer_entities entity_link on entity_link.entry_revision_id = entry_revisions.id
		 )
		 select flag_objects.model_revision_id, review.object_type, review.object_id, review.issue, review.created_at
		 from flag_objects
		 join schema_migration_review review
		   on review.object_type = flag_objects.object_type and review.object_id = flag_objects.object_id
		 where review.resolved_at is null
		 order by flag_objects.model_revision_id, review.created_at, review.id`,
		pq.Array(revisionIDs),
	); err != nil {
		return nil, fmt.Errorf("list open review flags of model revisions: %w", err)
	}
	for _, row := range rows {
		flags[row.ModelRevisionID] = append(flags[row.ModelRevisionID], models.ReviewFlag{
			ObjectType: row.ObjectType,
			ObjectID:   row.ObjectID,
			Issue:      row.Issue,
			FlaggedAt:  row.CreatedAt,
		})
	}
	return flags, nil
}

type reviewFlagRow struct {
	ModelRevisionID uuid.UUID `db:"model_revision_id"`
	ObjectType      string    `db:"object_type"`
	ObjectID        string    `db:"object_id"`
	Issue           string    `db:"issue"`
	CreatedAt       time.Time `db:"created_at"`
}
