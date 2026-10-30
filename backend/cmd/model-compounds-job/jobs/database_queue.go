package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// systemUserID owns the parser runs, as it owns the data sync's revisions.
var systemUserID = uuid.MustParse(models.SystemUserID)

// DatabaseQueue is Queue on the modeled_compound_sets table.
type DatabaseQueue struct {
	database *db.DB
}

func NewDatabaseQueue(database *db.DB) *DatabaseQueue {
	return &DatabaseQueue{database: database}
}

func (q *DatabaseQueue) AddMissingSince(ctx context.Context, since time.Time) (int64, error) {
	added, err := q.database.ModeledCompounds.AddMissingSince(ctx, since)
	if err != nil {
		return 0, fmt.Errorf("model compounds queue: %w", err)
	}
	return added, nil
}

func (q *DatabaseQueue) AddMissingAfter(ctx context.Context, after uuid.UUID, limit int) (models.MissingSetsPage, error) {
	page, err := q.database.ModeledCompounds.AddMissingAfter(ctx, after, limit)
	if err != nil {
		return models.MissingSetsPage{}, fmt.Errorf("model compounds queue: %w", err)
	}
	return page, nil
}

func (q *DatabaseQueue) ClaimDue(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
	maxAttempts int,
) ([]models.ModeledCompoundClaim, error) {
	claims, err := q.database.ModeledCompounds.ClaimDue(ctx, now, limit, lease, maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("model compounds queue: %w", err)
	}
	return claims, nil
}

func (q *DatabaseQueue) RecordFailure(ctx context.Context, artifactID uuid.UUID, message string, retryAt *time.Time) error {
	if err := q.database.ModeledCompounds.RecordFailure(ctx, artifactID, message, retryAt); err != nil {
		return fmt.Errorf("model compounds queue: %w", err)
	}
	return nil
}

func (q *DatabaseQueue) Save(ctx context.Context, artifactID uuid.UUID, result cif.ModeledCompounds, readAt time.Time) (int, error) {
	reindexed, err := q.database.SaveModeledCompounds(ctx, db.SaveModeledCompoundsParams{
		ArtifactID: artifactID,
		CreatedBy:  systemUserID,
		Result:     result,
		ReadAt:     readAt,
	})
	if err != nil {
		return 0, fmt.Errorf("model compounds queue: %w", err)
	}
	return reindexed, nil
}
