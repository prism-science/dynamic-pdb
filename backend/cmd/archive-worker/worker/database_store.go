package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

// DatabaseStore is JobStore and SelectionReader on the database.
type DatabaseStore struct {
	database *db.DB
}

func NewDatabaseStore(database *db.DB) *DatabaseStore {
	return &DatabaseStore{database: database}
}

func (s *DatabaseStore) Claim(ctx context.Context, now, leaseUntil time.Time) (*models.DownloadJob, error) {
	return s.database.Downloads.Claim(ctx, now, leaseUntil)
}

func (s *DatabaseStore) Heartbeat(ctx context.Context, id uuid.UUID, attempt int, leaseUntil time.Time) error {
	return s.database.Downloads.Heartbeat(ctx, id, attempt, leaseUntil)
}

func (s *DatabaseStore) StartPreparing(ctx context.Context, id uuid.UUID, attempt int, plannedBytes int64) error {
	return s.database.Downloads.StartPreparing(ctx, id, attempt, plannedBytes)
}

func (s *DatabaseStore) Block(
	ctx context.Context,
	id uuid.UUID,
	attempt int,
	plannedBytes int64,
	reason string,
	now time.Time,
) error {
	return s.database.Downloads.Block(ctx, id, attempt, plannedBytes, reason, now)
}

func (s *DatabaseStore) Finish(
	ctx context.Context,
	id uuid.UUID,
	attempt int,
	result db.DownloadResult,
	now, expiresAt time.Time,
) error {
	return s.database.Downloads.Finish(ctx, id, attempt, result, now, expiresAt)
}

func (s *DatabaseStore) Fail(ctx context.Context, id uuid.UUID, attempt int, message string, now time.Time) error {
	return s.database.Downloads.Fail(ctx, id, attempt, message, now)
}

func (s *DatabaseStore) Requeue(ctx context.Context, id uuid.UUID, attempt int, message string) error {
	return s.database.Downloads.Requeue(ctx, id, attempt, message)
}

func (s *DatabaseStore) Release(ctx context.Context, id uuid.UUID, attempt int) error {
	return s.database.Downloads.Release(ctx, id, attempt)
}

func (s *DatabaseStore) Get(ctx context.Context, id uuid.UUID) (*models.Selection, error) {
	selection, err := s.database.Selections.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read selection %s: %w", id, err)
	}
	return selection, nil
}

func (s *DatabaseStore) ListModels(
	ctx context.Context,
	selectionID uuid.UUID,
	afterOrdinal, limit int,
) ([]models.SelectionModel, error) {
	return s.database.Selections.ListModels(ctx, selectionID, afterOrdinal, limit)
}

func (s *DatabaseStore) ModelDetails(
	ctx context.Context,
	selectionID uuid.UUID,
	firstOrdinal, lastOrdinal int,
) (map[int]models.DownloadSelectionModel, error) {
	return s.database.Downloads.SelectionModelDetails(ctx, selectionID, firstOrdinal, lastOrdinal)
}
