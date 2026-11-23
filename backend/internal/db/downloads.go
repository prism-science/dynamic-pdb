package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/models"
)

var (
	// ErrDownloadNotFound means no download job has the ID.
	ErrDownloadNotFound = errors.New("db: download not found")
	// ErrDownloadSelectionNotFound means the download names no saved selection.
	ErrDownloadSelectionNotFound = errors.New("db: download names no saved selection")
	// ErrDownloadSelectionBusy means the selection already has as many unfinished archives as one
	// selection may have.
	ErrDownloadSelectionBusy = errors.New("db: the selection has too many unfinished archives")
	// ErrDownloadsBusy means all selections together have as many unfinished archives as the
	// worker takes.
	ErrDownloadsBusy = errors.New("db: too many unfinished archives")
	// ErrDownloadLeaseLost means the job is no longer held by the worker's attempt: its lease ran
	// out and another attempt claimed it, or it already finished.
	ErrDownloadLeaseLost = errors.New("db: the download is no longer held by this attempt")
)

// DownloadCreate is one request for an archive of a frozen selection.
type DownloadCreate struct {
	ID              uuid.UUID
	SelectionID     uuid.UUID
	FileChoice      models.DownloadFileChoice
	MaxArchiveBytes int64
	// MaxUnfinished caps the queued, planning and preparing archives of all selections together.
	MaxUnfinished int
	// MaxUnfinishedPerSelection caps them for one selection.
	MaxUnfinishedPerSelection int
	// Now decides whether a ready archive has expired.
	Now time.Time
}

// DownloadResult is what the worker records for a finished archive.
type DownloadResult struct {
	ArchiveKey    string
	ArchiveURL    string
	ArchiveBytes  int64
	ArchiveSHA256 string
	ManifestKey   string
	ManifestURL   string
	Counts        models.DownloadCounts
}

type DownloadsRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

func NewDownloadsRepository(database *sqlx.DB, queriers *QuerierProvider) *DownloadsRepository {
	return &DownloadsRepository{db: database, queriers: queriers}
}

// downloadJobColumns are the columns downloadJobRow reads, in a select list or a returning clause.
const downloadJobColumns = `download_jobs.id, download_jobs.selection_id, download_jobs.file_choice,
	download_jobs.status, download_jobs.max_archive_bytes, download_jobs.created_at,
	download_jobs.started_at, download_jobs.finished_at, download_jobs.expires_at,
	download_jobs.lease_until, download_jobs.attempts, download_jobs.planned_bytes,
	download_jobs.archive_key, download_jobs.archive_url, download_jobs.archive_bytes,
	download_jobs.archive_sha256, download_jobs.manifest_key, download_jobs.manifest_url,
	download_jobs.files_included, download_jobs.files_linked, download_jobs.files_unavailable,
	download_jobs.files_changed_at_source, download_jobs.blocked_reason, download_jobs.error`

// unfinishedDownloadStatuses is the SQL list of the statuses a worker has not finished.
const unfinishedDownloadStatuses = `('queued', 'planning', 'preparing')`

type downloadJobRow struct {
	ID                   uuid.UUID      `db:"id"`
	SelectionID          uuid.UUID      `db:"selection_id"`
	FileChoice           string         `db:"file_choice"`
	Status               string         `db:"status"`
	MaxArchiveBytes      int64          `db:"max_archive_bytes"`
	CreatedAt            time.Time      `db:"created_at"`
	StartedAt            sql.NullTime   `db:"started_at"`
	FinishedAt           sql.NullTime   `db:"finished_at"`
	ExpiresAt            sql.NullTime   `db:"expires_at"`
	LeaseUntil           sql.NullTime   `db:"lease_until"`
	Attempts             int            `db:"attempts"`
	PlannedBytes         sql.NullInt64  `db:"planned_bytes"`
	ArchiveKey           sql.NullString `db:"archive_key"`
	ArchiveURL           sql.NullString `db:"archive_url"`
	ArchiveBytes         sql.NullInt64  `db:"archive_bytes"`
	ArchiveSHA256        sql.NullString `db:"archive_sha256"`
	ManifestKey          sql.NullString `db:"manifest_key"`
	ManifestURL          sql.NullString `db:"manifest_url"`
	FilesIncluded        sql.NullInt64  `db:"files_included"`
	FilesLinked          sql.NullInt64  `db:"files_linked"`
	FilesUnavailable     sql.NullInt64  `db:"files_unavailable"`
	FilesChangedAtSource sql.NullInt64  `db:"files_changed_at_source"`
	BlockedReason        sql.NullString `db:"blocked_reason"`
	Error                sql.NullString `db:"error"`
}

func (row downloadJobRow) job() (*models.DownloadJob, error) {
	choice := models.DownloadFileChoice(row.FileChoice)
	if err := choice.Validate(); err != nil {
		return nil, fmt.Errorf("download %s: %w", row.ID, err)
	}
	status := models.DownloadStatus(row.Status)
	switch status {
	case models.DownloadStatusQueued, models.DownloadStatusPlanning, models.DownloadStatusPreparing,
		models.DownloadStatusReady, models.DownloadStatusBlocked, models.DownloadStatusFailed:
	case models.DownloadStatusExpired:
		return nil, fmt.Errorf("download %s: the reported-only status %s is stored", row.ID, status)
	default:
		return nil, fmt.Errorf("download %s: unknown download status: %s", row.ID, status)
	}
	job := &models.DownloadJob{
		ID:              row.ID,
		SelectionID:     row.SelectionID,
		FileChoice:      choice,
		Status:          status,
		MaxArchiveBytes: row.MaxArchiveBytes,
		CreatedAt:       row.CreatedAt,
		StartedAt:       timePtrFromSQL(row.StartedAt),
		FinishedAt:      timePtrFromSQL(row.FinishedAt),
		ExpiresAt:       timePtrFromSQL(row.ExpiresAt),
		LeaseUntil:      timePtrFromSQL(row.LeaseUntil),
		Attempts:        row.Attempts,
		PlannedBytes:    int64PtrFromSQL(row.PlannedBytes),
		ArchiveKey:      stringPtrFromSQL(row.ArchiveKey),
		ArchiveURL:      stringPtrFromSQL(row.ArchiveURL),
		ArchiveBytes:    int64PtrFromSQL(row.ArchiveBytes),
		ArchiveSHA256:   stringPtrFromSQL(row.ArchiveSHA256),
		ManifestKey:     stringPtrFromSQL(row.ManifestKey),
		ManifestURL:     stringPtrFromSQL(row.ManifestURL),
		BlockedReason:   stringPtrFromSQL(row.BlockedReason),
		Error:           stringPtrFromSQL(row.Error),
	}
	if row.FilesIncluded.Valid && row.FilesLinked.Valid && row.FilesUnavailable.Valid && row.FilesChangedAtSource.Valid {
		job.Counts = &models.DownloadCounts{
			Included:        int(row.FilesIncluded.Int64),
			Linked:          int(row.FilesLinked.Int64),
			Unavailable:     int(row.FilesUnavailable.Int64),
			ChangedAtSource: int(row.FilesChangedAtSource.Int64),
		}
	}
	return job, nil
}

// Get returns the download job with the ID, or ErrDownloadNotFound.
func (r *DownloadsRepository) Get(ctx context.Context, id uuid.UUID) (*models.DownloadJob, error) {
	var row downloadJobRow
	err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row,
		`select `+downloadJobColumns+` from download_jobs where download_jobs.id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDownloadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get download %s: %w", id, err)
	}
	return row.job()
}

// lockCreates makes the transaction the only one creating download jobs until it ends, so the
// caps and the reuse of an existing job are decided on a stable set of rows. Creates are rare
// (rate limited per client) and short.
func (r *DownloadsRepository) lockCreates(ctx context.Context) error {
	if _, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx,
		`select pg_advisory_xact_lock(hashtextextended('download_jobs_create', 0))`); err != nil {
		return fmt.Errorf("lock download creates: %w", err)
	}
	return nil
}

// reusable returns the selection's latest archive of the file choice that is unfinished, or
// ready and not expired at now; nil when there is none.
func (r *DownloadsRepository) reusable(
	ctx context.Context,
	selectionID uuid.UUID,
	choice models.DownloadFileChoice,
	now time.Time,
) (*models.DownloadJob, error) {
	var row downloadJobRow
	err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row,
		`select `+downloadJobColumns+`
		 from download_jobs
		 where download_jobs.selection_id = $1 and download_jobs.file_choice = $2
		   and (download_jobs.status in `+unfinishedDownloadStatuses+`
		        or (download_jobs.status = 'ready' and download_jobs.expires_at > $3))
		 order by download_jobs.created_at desc, download_jobs.id desc
		 limit 1`, selectionID, string(choice), now)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find reusable download: %w", err)
	}
	return row.job()
}

// Create returns the selection's existing archive of the same file choice when one is unfinished
// or ready (created false), or adds a queued job (created true). It returns
// ErrDownloadSelectionNotFound, ErrDownloadSelectionBusy or ErrDownloadsBusy when it adds
// nothing. Run it in a transaction (DB.CreateDownload).
func (r *DownloadsRepository) Create(ctx context.Context, create DownloadCreate) (*models.DownloadJob, bool, error) {
	if err := create.FileChoice.Validate(); err != nil {
		return nil, false, fmt.Errorf("create download: %w", err)
	}
	if create.MaxArchiveBytes <= 0 || create.MaxUnfinished <= 0 || create.MaxUnfinishedPerSelection <= 0 {
		return nil, false, errors.New("create download: limits must be positive")
	}
	if err := r.lockCreates(ctx); err != nil {
		return nil, false, err
	}
	var selectionFound bool
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &selectionFound,
		`select exists (select 1 from selections where id = $1)`, create.SelectionID); err != nil {
		return nil, false, fmt.Errorf("check download selection: %w", err)
	}
	if !selectionFound {
		return nil, false, ErrDownloadSelectionNotFound
	}
	existing, err := r.reusable(ctx, create.SelectionID, create.FileChoice, create.Now)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, false, nil
	}
	var unfinished struct {
		Selection int `db:"selection"`
		All       int `db:"all_selections"`
	}
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &unfinished,
		`select count(*) filter (where selection_id = $1) as selection, count(*) as all_selections
		 from download_jobs where status in `+unfinishedDownloadStatuses, create.SelectionID); err != nil {
		return nil, false, fmt.Errorf("count unfinished downloads: %w", err)
	}
	if unfinished.Selection >= create.MaxUnfinishedPerSelection {
		return nil, false, ErrDownloadSelectionBusy
	}
	if unfinished.All >= create.MaxUnfinished {
		return nil, false, ErrDownloadsBusy
	}
	var row downloadJobRow
	if err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row,
		`insert into download_jobs (id, selection_id, file_choice, max_archive_bytes)
		 values ($1, $2, $3, $4)
		 returning `+downloadJobColumns,
		create.ID, create.SelectionID, string(create.FileChoice), create.MaxArchiveBytes); err != nil {
		return nil, false, fmt.Errorf("insert download: %w", err)
	}
	job, err := row.job()
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

// Claim takes the oldest job a worker may start: a queued one, or one whose worker's lease ran
// out by now. It sets status planning, a lease until leaseUntil, and counts the attempt. It
// returns nil when no job is waiting.
func (r *DownloadsRepository) Claim(ctx context.Context, now, leaseUntil time.Time) (*models.DownloadJob, error) {
	if !leaseUntil.After(now) {
		return nil, errors.New("claim download: the lease must end after now")
	}
	var row downloadJobRow
	err := r.queriers.Querier(ctx, r.db).GetContext(ctx, &row,
		`with picked as (
		   select id from download_jobs
		   where status = 'queued'
		      or (status in ('planning', 'preparing') and lease_until <= $1)
		   order by created_at, id
		   limit 1
		   for update skip locked
		 )
		 update download_jobs
		 set status = 'planning', lease_until = $2, attempts = download_jobs.attempts + 1,
		     started_at = coalesce(download_jobs.started_at, $1), planned_bytes = null
		 from picked
		 where download_jobs.id = picked.id
		 returning `+downloadJobColumns, now, leaseUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim download: %w", err)
	}
	return row.job()
}

// held runs an update of the job held by attempt in one of the statuses. It returns
// ErrDownloadLeaseLost when the update changed no row.
func (r *DownloadsRepository) held(
	ctx context.Context,
	action string,
	query string,
	args ...any,
) error {
	result, err := r.queriers.Querier(ctx, r.db).ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: count rows: %w", action, err)
	}
	if count != 1 {
		return fmt.Errorf("%s: %w", action, ErrDownloadLeaseLost)
	}
	return nil
}

// Heartbeat extends the lease of the job held by attempt.
func (r *DownloadsRepository) Heartbeat(ctx context.Context, id uuid.UUID, attempt int, leaseUntil time.Time) error {
	return r.held(ctx, "extend download lease",
		`update download_jobs set lease_until = $3
		 where id = $1 and attempts = $2 and status in ('planning', 'preparing')`,
		id, attempt, leaseUntil)
}

// StartPreparing records the planned bytes and moves the job from planning to preparing.
func (r *DownloadsRepository) StartPreparing(ctx context.Context, id uuid.UUID, attempt int, plannedBytes int64) error {
	return r.held(ctx, "start preparing download",
		`update download_jobs set status = 'preparing', planned_bytes = $3
		 where id = $1 and attempts = $2 and status = 'planning'`,
		id, attempt, plannedBytes)
}

// Block finishes a planning job whose files add up to more than its limit.
func (r *DownloadsRepository) Block(
	ctx context.Context,
	id uuid.UUID,
	attempt int,
	plannedBytes int64,
	reason string,
	now time.Time,
) error {
	return r.held(ctx, "block download",
		`update download_jobs
		 set status = 'blocked', planned_bytes = $3, blocked_reason = $4, finished_at = $5, lease_until = null
		 where id = $1 and attempts = $2 and status = 'planning'`,
		id, attempt, plannedBytes, reason, now)
}

// Finish records a written archive and makes the job ready until expiresAt.
func (r *DownloadsRepository) Finish(
	ctx context.Context,
	id uuid.UUID,
	attempt int,
	result DownloadResult,
	now time.Time,
	expiresAt time.Time,
) error {
	return r.held(ctx, "finish download",
		`update download_jobs
		 set status = 'ready', finished_at = $3, expires_at = $4, lease_until = null,
		     archive_key = $5, archive_url = $6, archive_bytes = $7, archive_sha256 = $8,
		     manifest_key = $9, manifest_url = $10,
		     files_included = $11, files_linked = $12, files_unavailable = $13, files_changed_at_source = $14
		 where id = $1 and attempts = $2 and status = 'preparing'`,
		id, attempt, now, expiresAt,
		result.ArchiveKey, result.ArchiveURL, result.ArchiveBytes, result.ArchiveSHA256,
		result.ManifestKey, result.ManifestURL,
		result.Counts.Included, result.Counts.Linked, result.Counts.Unavailable, result.Counts.ChangedAtSource)
}

// Fail finishes the job held by attempt with the error.
func (r *DownloadsRepository) Fail(ctx context.Context, id uuid.UUID, attempt int, message string, now time.Time) error {
	return r.held(ctx, "fail download",
		`update download_jobs set status = 'failed', error = $3, finished_at = $4, lease_until = null
		 where id = $1 and attempts = $2 and status in ('planning', 'preparing')`,
		id, attempt, message, now)
}

// Requeue puts the job held by attempt back in the queue after a failed attempt, keeping the
// attempt counted and its error, so the next claim tries again until the worker's attempts run out.
func (r *DownloadsRepository) Requeue(ctx context.Context, id uuid.UUID, attempt int, message string) error {
	return r.held(ctx, "requeue download",
		`update download_jobs
		 set status = 'queued', lease_until = null, planned_bytes = null, error = $3
		 where id = $1 and attempts = $2 and status in ('planning', 'preparing')`,
		id, attempt, message)
}

// Release puts the job held by attempt back in the queue without counting the attempt, for a
// worker that stops (a deploy) before it finished.
func (r *DownloadsRepository) Release(ctx context.Context, id uuid.UUID, attempt int) error {
	return r.held(ctx, "release download",
		`update download_jobs
		 set status = 'queued', lease_until = null, planned_bytes = null, attempts = attempts - 1
		 where id = $1 and attempts = $2 and status in ('planning', 'preparing')`,
		id, attempt)
}

// SelectionModelDetails returns what an archive's manifest says about the models of a selection
// from firstOrdinal to lastOrdinal, by ordinal, read from their pinned model and entry revisions.
func (r *DownloadsRepository) SelectionModelDetails(
	ctx context.Context,
	selectionID uuid.UUID,
	firstOrdinal, lastOrdinal int,
) (map[int]models.DownloadSelectionModel, error) {
	var rows []struct {
		Ordinal         int            `db:"ordinal"`
		ModelType       sql.NullString `db:"model_type"`
		Origin          sql.NullString `db:"origin"`
		ModelPDBID      sql.NullString `db:"model_pdb_id"`
		Authors         []byte         `db:"authors"`
		Affiliation     sql.NullString `db:"affiliation"`
		EntryPDBID      sql.NullString `db:"entry_pdb_id"`
		RelatedDatasets []byte         `db:"related_datasets"`
	}
	if err := r.queriers.Querier(ctx, r.db).SelectContext(ctx, &rows,
		`select selection_models.ordinal,
		        model_revisions.metadata ->> 'model_type' as model_type,
		        model_revisions.metadata ->> 'origin' as origin,
		        model_revisions.metadata -> 'external_refs' ->> 'pdb' as model_pdb_id,
		        model_revisions.metadata -> 'authors' as authors,
		        model_revisions.metadata ->> 'affiliation' as affiliation,
		        entry_revisions.metadata -> 'external_refs' ->> 'pdb' as entry_pdb_id,
		        entry_revisions.metadata -> 'related_datasets' as related_datasets
		 from selection_models
		 join model_revisions on model_revisions.id = selection_models.model_revision_id
		 join entry_revisions on entry_revisions.id = selection_models.entry_revision_id
		 where selection_models.selection_id = $1 and selection_models.ordinal between $2 and $3
		 order by selection_models.ordinal`, selectionID, firstOrdinal, lastOrdinal); err != nil {
		return nil, fmt.Errorf("read selection model details: %w", err)
	}
	details := make(map[int]models.DownloadSelectionModel, len(rows))
	for _, row := range rows {
		model := models.DownloadSelectionModel{
			Ordinal:     row.Ordinal,
			ModelType:   stringPtrFromSQL(row.ModelType),
			Origin:      stringPtrFromSQL(row.Origin),
			ModelPDBID:  stringPtrFromSQL(row.ModelPDBID),
			Affiliation: stringPtrFromSQL(row.Affiliation),
			EntryPDBID:  stringPtrFromSQL(row.EntryPDBID),
		}
		if err := decodeOptionalJSON(row.Authors, &model.Authors); err != nil {
			return nil, fmt.Errorf("selection %s model %d authors: %w", selectionID, row.Ordinal, err)
		}
		if err := decodeOptionalJSON(row.RelatedDatasets, &model.RelatedDatasets); err != nil {
			return nil, fmt.Errorf("selection %s model %d related datasets: %w", selectionID, row.Ordinal, err)
		}
		details[row.Ordinal] = model
	}
	return details, nil
}

// decodeOptionalJSON decodes a JSON value into target; a missing value or JSON null leaves it as is.
func decodeOptionalJSON(raw []byte, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// CreateDownload runs DownloadsRepository.Create in a transaction.
func (d *DB) CreateDownload(ctx context.Context, create DownloadCreate) (*models.DownloadJob, bool, error) {
	var (
		job     *models.DownloadJob
		created bool
	)
	if err := d.Do(ctx, func(ctx context.Context) error {
		var err error
		job, created, err = d.Downloads.Create(ctx, create)
		return err
	}); err != nil {
		return nil, false, fmt.Errorf("create download: %w", err)
	}
	return job, created, nil
}
