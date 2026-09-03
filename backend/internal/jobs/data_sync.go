package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

const (
	dataSyncLockName        = "data-sync"
	dataSyncInterval        = time.Hour
	dataSyncBatchTimeout    = 5 * time.Minute
	dataSyncMinimumInterval = 7 * 24 * time.Hour
	dataSyncScheduleJitter  = 7 * 24 * time.Hour
)

type DataSyncJob struct {
	database        *db.DB
	logger          *slog.Logger
	now             func() time.Time
	nextScheduledAt func(time.Time) time.Time
}

func NewDataSyncJob(
	database *db.DB,
	logger *slog.Logger,
) (*DataSyncJob, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &DataSyncJob{
		database:        database,
		logger:          logger,
		now:             func() time.Time { return time.Now().UTC() },
		nextScheduledAt: nextDataSyncScheduledAt,
	}, nil
}

func (j *DataSyncJob) Run(ctx context.Context) {
	for ctx.Err() == nil {
		ran, err := j.database.RunLocked(ctx, dataSyncLockName, j.runBatch)
		if err != nil && ctx.Err() == nil {
			j.logger.Error("data sync failed", "err", err)
		} else if !ran && ctx.Err() == nil {
			j.logger.Info("data sync iteration skipped", "reason", "lock is already held")
		}
		if !waitForDataSync(ctx, dataSyncInterval) {
			return
		}
	}
}

func (j *DataSyncJob) runBatch(ctx context.Context) error {
	batchCtx, cancel := context.WithTimeout(ctx, dataSyncBatchTimeout)
	defer cancel()
	startedAt := time.Now()
	processedJobs := 0
	j.logger.Info("data sync iteration started")
	defer func() {
		j.logger.Info(
			"data sync iteration finished",
			"processed_jobs", processedJobs,
			"duration", time.Since(startedAt),
		)
	}()

	for batchCtx.Err() == nil {
		executed, err := j.executeNext(batchCtx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && batchCtx.Err() != nil {
				return nil
			}
			return err
		}
		if !executed {
			return nil
		}
		processedJobs++
	}
	return nil
}

func (j *DataSyncJob) executeNext(ctx context.Context) (bool, error) {
	job, err := j.database.DataSyncJobs.GetNextScheduled(ctx, j.now())
	if err != nil {
		return false, fmt.Errorf("get next scheduled data sync job: %w", err)
	}
	if job == nil {
		return false, nil
	}

	j.execute(ctx, *job)
	job.ScheduledAt = j.nextScheduledAt(j.now())
	if err := j.database.DataSyncJobs.Schedule(ctx, *job); err != nil {
		return false, fmt.Errorf("reschedule data sync job: %w", err)
	}
	return true, nil
}

func (j *DataSyncJob) execute(_ context.Context, _ models.DataSyncJob) {
}

func nextDataSyncScheduledAt(now time.Time) time.Time {
	// Scheduling jitter does not require cryptographic randomness.
	//nolint:gosec
	jitter := time.Duration(rand.Int64N(int64(dataSyncScheduleJitter)))
	return now.Add(dataSyncMinimumInterval + jitter)
}

func waitForDataSync(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
