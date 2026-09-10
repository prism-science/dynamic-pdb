package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

type DataSyncJobsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type dataSyncJobRow struct {
	EntryID     string         `db:"entry_id"`
	ModelID     sql.NullString `db:"model_id"`
	ScheduledAt time.Time      `db:"scheduled_at"`
}

func NewDataSyncJobsRepository(database *sqlx.DB, queriers *QuerierProvider) *DataSyncJobsRepository {
	return &DataSyncJobsRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *DataSyncJobsRepository) Schedule(ctx context.Context, job models.DataSyncJob) error {
	query := `insert into data_sync_jobs(entry_id, model_id, scheduled_at)
		      values ($1, $2, $3)
		      on conflict on constraint data_sync_jobs_entry_model_key
		      do update set scheduled_at = excluded.scheduled_at`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx,
		query,
		job.EntryID,
		job.ModelID,
		job.ScheduledAt,
	); err != nil {
		return fmt.Errorf("schedule data sync job: %w", err)
	}
	return nil
}

func (r *DataSyncJobsRepository) Delete(ctx context.Context, job models.DataSyncJob) error {
	query := `delete from data_sync_jobs
	          where entry_id = $1
	            and model_id is not distinct from $2`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, job.EntryID, job.ModelID); err != nil {
		return fmt.Errorf("delete data sync job: %w", err)
	}
	return nil
}

func (r *DataSyncJobsRepository) GetNextScheduled(
	ctx context.Context,
	scheduledBefore time.Time,
) (*models.DataSyncJob, error) {
	query := `select entry_id, model_id, scheduled_at
		      from data_sync_jobs
		      where scheduled_at <= $1
		      order by scheduled_at asc, entry_id asc, model_id asc nulls first
		      limit 1`

	var row dataSyncJobRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, scheduledBefore); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get next scheduled data sync job: %w", err)
	}

	return &models.DataSyncJob{
		EntryID:     row.EntryID,
		ModelID:     stringPtrFromSQL(row.ModelID),
		ScheduledAt: row.ScheduledAt,
	}, nil
}
