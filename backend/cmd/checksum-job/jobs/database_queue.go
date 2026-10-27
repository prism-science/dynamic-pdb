package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

// DatabaseQueue is ChecksumQueue on the artifact_checksums table.
type DatabaseQueue struct {
	database *db.DB
}

func NewDatabaseQueue(database *db.DB) *DatabaseQueue {
	return &DatabaseQueue{database: database}
}

func (q *DatabaseQueue) AddMissing(ctx context.Context) (int64, error) {
	added, err := q.database.ArtifactChecksums.AddMissing(ctx)
	if err != nil {
		return 0, fmt.Errorf("checksum queue: %w", err)
	}
	return added, nil
}

func (q *DatabaseQueue) ClaimDue(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
) ([]models.ChecksumClaim, error) {
	claims, err := q.database.ArtifactChecksums.ClaimDue(ctx, now, limit, lease)
	if err != nil {
		return nil, fmt.Errorf("checksum queue: %w", err)
	}
	return claims, nil
}

func (q *DatabaseQueue) Locations(ctx context.Context, artifactIDs []uuid.UUID) (map[uuid.UUID]models.ArtifactLocation, error) {
	locations, err := q.database.ArtifactChecksums.Locations(ctx, artifactIDs)
	if err != nil {
		return nil, fmt.Errorf("checksum queue: %w", err)
	}
	return locations, nil
}

func (q *DatabaseQueue) Record(ctx context.Context, results []models.ChecksumResult) error {
	if err := q.database.RecordChecksums(ctx, results); err != nil {
		return fmt.Errorf("checksum queue: %w", err)
	}
	return nil
}
