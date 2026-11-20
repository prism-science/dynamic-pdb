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

// DatabaseQueue is Queue on the coordinate_index_sets table.
type DatabaseQueue struct {
	database *db.DB
}

func NewDatabaseQueue(database *db.DB) *DatabaseQueue {
	return &DatabaseQueue{database: database}
}

func (q *DatabaseQueue) AddMissingSince(ctx context.Context, since time.Time, archiveFiles bool) (int64, error) {
	added, err := q.database.CoordinateIndex.AddMissingSince(ctx, since, archiveFiles)
	if err != nil {
		return 0, fmt.Errorf("coordinate index queue: %w", err)
	}
	return added, nil
}

func (q *DatabaseQueue) AddFillAfter(ctx context.Context, after uuid.UUID, limit int, archiveFiles bool) (models.MissingSetsPage, error) {
	page, err := q.database.CoordinateIndex.AddFillAfter(ctx, after, limit, archiveFiles)
	if err != nil {
		return models.MissingSetsPage{}, fmt.Errorf("coordinate index queue: %w", err)
	}
	return page, nil
}

func (q *DatabaseQueue) HasPendingFill(ctx context.Context) (bool, error) {
	pending, err := q.database.CoordinateIndex.HasPendingFill(ctx)
	if err != nil {
		return false, fmt.Errorf("coordinate index queue: %w", err)
	}
	return pending, nil
}

func (q *DatabaseQueue) RequeueForNewEntries(ctx context.Context, since time.Time, now time.Time) (int64, error) {
	queued, err := q.database.CoordinateIndex.RequeueForNewEntries(ctx, since, now)
	if err != nil {
		return 0, fmt.Errorf("coordinate index queue: %w", err)
	}
	return queued, nil
}

func (q *DatabaseQueue) ClaimDue(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
	maxAttempts int,
) ([]models.CoordinateIndexClaim, error) {
	claims, err := q.database.CoordinateIndex.ClaimDue(ctx, now, limit, lease, maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("coordinate index queue: %w", err)
	}
	return claims, nil
}

func (q *DatabaseQueue) RecordFailure(ctx context.Context, artifactID uuid.UUID, message string, retryAt *time.Time) error {
	if err := q.database.CoordinateIndex.RecordFailure(ctx, artifactID, message, retryAt); err != nil {
		return fmt.Errorf("coordinate index queue: %w", err)
	}
	return nil
}

func (q *DatabaseQueue) Save(ctx context.Context, artifactID uuid.UUID, read cif.CoordinateIndex, readAt time.Time) (SavedIndex, error) {
	var saved SavedIndex
	err := q.database.SaveCoordinateIndex(ctx, db.SaveCoordinateIndexParams{
		ArtifactID:    artifactID,
		ReaderVersion: cif.CoordinateIndexVersion,
		ReadAt:        readAt,
		Resolve: func(fasta models.ModelFileFASTA) (db.ResolvedIndex, error) {
			resolved, err := resolveIndex(read, fasta)
			if err != nil {
				return db.ResolvedIndex{}, err
			}
			saved = resolved
			return db.ResolvedIndex{Chains: resolved.Chains, Warnings: resolved.Warnings}, nil
		},
	})
	if err != nil {
		return SavedIndex{}, fmt.Errorf("coordinate index queue: %w", err)
	}
	return saved, nil
}
