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
	EntryID                string         `db:"entry_id"`
	ModelID                sql.NullString `db:"model_id"`
	ScheduledAt            time.Time      `db:"scheduled_at"`
	ArchiveRevisionOrdinal sql.NullInt64  `db:"archive_revision_ordinal"`
	ArchiveVersion         sql.NullString `db:"archive_version"`
	ArchiveRevisionDate    sql.NullTime   `db:"archive_revision_date"`
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

// RecordArchiveVersion stores the archive version a job applied or recorded as its baseline.
func (r *DataSyncJobsRepository) RecordArchiveVersion(
	ctx context.Context,
	job models.DataSyncJob,
	version models.ArchiveVersion,
) error {
	query := `update data_sync_jobs
	          set archive_revision_ordinal = $3, archive_version = $4, archive_revision_date = $5
	          where entry_id = $1
	            and model_id is not distinct from $2`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx, query, job.EntryID, job.ModelID, version.Ordinal, version.Version, version.RevisionDate,
	); err != nil {
		return fmt.Errorf("record data sync job archive version: %w", err)
	}
	return nil
}

// Stop ends a job for good, keeping the reason; GetNextScheduled never returns it again.
func (r *DataSyncJobsRepository) Stop(
	ctx context.Context,
	job models.DataSyncJob,
	reason string,
	stoppedAt time.Time,
) error {
	query := `update data_sync_jobs
	          set stopped_at = $3, stop_reason = $4
	          where entry_id = $1
	            and model_id is not distinct from $2`
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(
		ctx, query, job.EntryID, job.ModelID, stoppedAt, reason,
	); err != nil {
		return fmt.Errorf("stop data sync job: %w", err)
	}
	return nil
}

func (r *DataSyncJobsRepository) GetNextScheduled(
	ctx context.Context,
	scheduledBefore time.Time,
) (*models.DataSyncJob, error) {
	query := `select entry_id, model_id, scheduled_at,
		             archive_revision_ordinal, archive_version, archive_revision_date
		      from data_sync_jobs
		      where scheduled_at <= $1
		        and stopped_at is null
		      order by scheduled_at asc, entry_id asc, model_id asc nulls first
		      limit 1`

	var row dataSyncJobRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row, query, scheduledBefore); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get next scheduled data sync job: %w", err)
	}

	job := &models.DataSyncJob{
		EntryID:     row.EntryID,
		ModelID:     stringPtrFromSQL(row.ModelID),
		ScheduledAt: row.ScheduledAt,
	}
	if row.ArchiveRevisionOrdinal.Valid {
		job.ArchiveVersion = &models.ArchiveVersion{
			Ordinal:      int(row.ArchiveRevisionOrdinal.Int64),
			Version:      row.ArchiveVersion.String,
			RevisionDate: timePtrFromSQL(row.ArchiveRevisionDate),
		}
	}
	return job, nil
}
