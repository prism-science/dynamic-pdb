// Package worker builds bulk-download archives: it claims a queued download job, plans the
// archive from the frozen selection's pinned files, and either blocks it (over its size limit,
// before any file is read) or writes the zip and its manifest into the data bucket.
package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/cdn"
)

// Config holds the worker's timing and paths. ArchiveTTL comes from downloads::archive_ttl.
type Config struct {
	// Lease is how long a claim holds a job without a heartbeat; a worker that dies frees its job
	// once the lease runs out.
	Lease time.Duration `mapstructure:"lease"`
	// Heartbeat is how often the worker extends its lease while it works on a job.
	Heartbeat time.Duration `mapstructure:"heartbeat"`
	// PollInterval is the pause after finding no job.
	PollInterval time.Duration `mapstructure:"poll_interval"`
	// MaxAttempts is how many claims a job gets before it fails.
	MaxAttempts int `mapstructure:"max_attempts"`
	// PageSize is how many selection models one database read takes.
	PageSize int `mapstructure:"page_size"`
	// ScratchDir holds one file at a time while it is checked, and the manifest; empty is the
	// system's temporary directory.
	ScratchDir string `mapstructure:"scratch_dir"`
	// ArchivePrefix is the bucket folder of the archives; the lifecycle rule expires it.
	ArchivePrefix string `mapstructure:"archive_prefix"`
	// FileReadAttempts is how many times reading one file from the bucket is tried.
	FileReadAttempts int `mapstructure:"file_read_attempts"`
	// FileTimeout bounds one read of one file from the bucket, so a stalled read is tried again
	// instead of holding the job while its heartbeat keeps the lease.
	FileTimeout time.Duration `mapstructure:"file_timeout"`
	// JobTimeout bounds one attempt at one archive; past it the attempt fails and is tried again.
	JobTimeout time.Duration `mapstructure:"job_timeout"`
	// ExitWhenIdle stops the worker the first time it finds no job waiting, for tests and one-off
	// runs. The Deployment leaves it off.
	ExitWhenIdle bool `mapstructure:"exit_when_idle"`
	// ArchiveTTL is how long a ready archive is offered.
	ArchiveTTL time.Duration `mapstructure:"-"`
}

func (c Config) validate() error {
	switch {
	case c.Lease <= 0:
		return errors.New("lease must be positive")
	case c.Heartbeat <= 0 || c.Heartbeat >= c.Lease:
		return errors.New("heartbeat must be positive and shorter than the lease")
	case c.PollInterval <= 0:
		return errors.New("poll_interval must be positive")
	case c.MaxAttempts <= 0:
		return errors.New("max_attempts must be positive")
	case c.PageSize <= 0:
		return errors.New("page_size must be positive")
	case strings.TrimSpace(c.ArchivePrefix) == "" || !strings.HasSuffix(c.ArchivePrefix, "/"):
		return errors.New("archive_prefix must be a folder ending in /")
	case c.FileReadAttempts <= 0:
		return errors.New("file_read_attempts must be positive")
	case c.FileTimeout <= 0 || c.JobTimeout <= c.FileTimeout:
		return errors.New("file_timeout must be positive and shorter than job_timeout")
	case c.ArchiveTTL <= 0:
		return errors.New("archive_ttl must be positive")
	}
	return nil
}

// JobStore is the download_jobs table as the worker uses it. Every write after the claim names
// the claimed attempt and fails with db.ErrDownloadLeaseLost when the job moved on.
type JobStore interface {
	Claim(ctx context.Context, now, leaseUntil time.Time) (*models.DownloadJob, error)
	Heartbeat(ctx context.Context, id uuid.UUID, attempt int, leaseUntil time.Time) error
	StartPreparing(ctx context.Context, id uuid.UUID, attempt int, plannedBytes int64) error
	Block(ctx context.Context, id uuid.UUID, attempt int, plannedBytes int64, reason string, now time.Time) error
	Finish(ctx context.Context, id uuid.UUID, attempt int, result db.DownloadResult, now, expiresAt time.Time) error
	Fail(ctx context.Context, id uuid.UUID, attempt int, message string, now time.Time) error
	Requeue(ctx context.Context, id uuid.UUID, attempt int, message string) error
	Release(ctx context.Context, id uuid.UUID, attempt int) error
}

// SelectionReader reads a frozen selection's pinned models, files and manifest details.
type SelectionReader interface {
	Get(ctx context.Context, id uuid.UUID) (*models.Selection, error)
	ListModels(ctx context.Context, selectionID uuid.UUID, afterOrdinal, limit int) ([]models.SelectionModel, error)
	ModelDetails(ctx context.Context, selectionID uuid.UUID, firstOrdinal, lastOrdinal int) (map[int]models.DownloadSelectionModel, error)
}

// Bucket is the data bucket: files are read by key, archives written by key, and URLs built for
// the public files host.
type Bucket interface {
	StoredKey(uri string) (string, bool)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	NewWriter(ctx context.Context, key string) (cdn.ObjectWriter, error)
	URL(key string) (string, error)
}

// errLeaseLost cancels a job whose lease another attempt took.
var errLeaseLost = errors.New("the job's lease was lost")

// errJobTimedOut ends an attempt that ran past JobTimeout.
var errJobTimedOut = errors.New("the archive took longer than one attempt may take")

// errStopPaging ends a walk over a selection early.
var errStopPaging = errors.New("stop paging")

// releaseTimeout bounds putting a job back in the queue after the worker was told to stop.
const releaseTimeout = 10 * time.Second

// Worker builds archives one job at a time.
type Worker struct {
	jobs       JobStore
	selections SelectionReader
	bucket     Bucket
	cfg        Config
	logger     *slog.Logger
	now        func() time.Time
	// firstReadPause is the pause before retrying a failed read from the bucket.
	firstReadPause time.Duration
}

func New(jobs JobStore, selections SelectionReader, bucket Bucket, cfg Config, logger *slog.Logger) (*Worker, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("archive worker config: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		jobs:           jobs,
		selections:     selections,
		bucket:         bucket,
		cfg:            cfg,
		logger:         logger,
		now:            func() time.Time { return time.Now().UTC() },
		firstReadPause: time.Second,
	}, nil
}

// Run claims and builds jobs until ctx ends. Errors of one job are recorded on the job; errors
// reaching the queue are logged and tried again after the poll interval.
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("archive worker started")
	for ctx.Err() == nil {
		worked, err := w.RunOnce(ctx)
		if err != nil && ctx.Err() == nil {
			w.logger.Error("archive worker could not take a job", "err", err)
		}
		if worked && err == nil {
			continue
		}
		if !worked && err == nil && w.cfg.ExitWhenIdle {
			break
		}
		if err := sleepContext(ctx, w.cfg.PollInterval); err != nil {
			break
		}
	}
	w.logger.Info("archive worker stopped")
	return nil
}

// RunOnce claims one job and builds it. It reports whether there was a job.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	now := w.now()
	job, err := w.jobs.Claim(ctx, now, now.Add(w.cfg.Lease))
	if err != nil {
		return false, fmt.Errorf("claim: %w", err)
	}
	if job == nil {
		return false, nil
	}
	logger := w.logger.With("download_id", job.ID, "selection_id", job.SelectionID,
		"file_choice", job.FileChoice, "attempt", job.Attempts)
	if job.Attempts > w.cfg.MaxAttempts {
		message := fmt.Sprintf("the archive could not be built in %d attempts", w.cfg.MaxAttempts)
		if job.Error != nil {
			message += "; the last attempt failed: " + *job.Error
		}
		logger.Error("archive failed: no attempts left", "last_error", job.Error)
		if err := w.jobs.Fail(ctx, job.ID, job.Attempts, message, w.now()); err != nil {
			return true, fmt.Errorf("fail download %s: %w", job.ID, err)
		}
		return true, nil
	}
	logger.Info("archive claimed")
	w.runJob(ctx, *job, logger)
	return true, nil
}

// runJob builds a claimed job while a heartbeat holds its lease, then records how it ended when
// the build itself did not: put back in the queue on shutdown, requeued or failed on an error.
func (w *Worker) runJob(ctx context.Context, job models.DownloadJob, logger *slog.Logger) {
	jobCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	buildCtx, cancelBuild := context.WithTimeoutCause(jobCtx, w.cfg.JobTimeout, errJobTimedOut)
	defer cancelBuild()
	stopHeartbeat := w.startHeartbeat(jobCtx, cancel, job, logger)
	err := w.build(buildCtx, job, logger)
	stopHeartbeat()
	if err == nil {
		return
	}
	if errors.Is(context.Cause(buildCtx), errJobTimedOut) {
		err = fmt.Errorf("%w: %w", errJobTimedOut, err)
	}
	switch {
	case errors.Is(context.Cause(jobCtx), errLeaseLost) || errors.Is(err, db.ErrDownloadLeaseLost):
		logger.Warn("archive abandoned: another attempt holds the job", "err", err)
	case ctx.Err() != nil:
		releaseCtx, cancelRelease := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
		defer cancelRelease()
		if releaseErr := w.jobs.Release(releaseCtx, job.ID, job.Attempts); releaseErr != nil {
			logger.Error("archive not put back in the queue; its lease frees it later", "err", releaseErr)
			return
		}
		logger.Info("archive put back in the queue: the worker is stopping")
	case job.Attempts >= w.cfg.MaxAttempts:
		logger.Error("archive failed", "err", err)
		if failErr := w.jobs.Fail(ctx, job.ID, job.Attempts, publicError(err), w.now()); failErr != nil {
			logger.Error("record failed archive", "err", failErr)
		}
	default:
		logger.Warn("archive attempt failed; it will be tried again", "err", err)
		if requeueErr := w.jobs.Requeue(ctx, job.ID, job.Attempts, publicError(err)); requeueErr != nil {
			logger.Error("requeue archive; its lease frees it later", "err", requeueErr)
		}
	}
}

// publicError is what a failed attempt shows the person who asked for the archive: no bucket keys
// or internal detail.
func publicError(err error) string {
	switch {
	case errors.Is(err, errJobTimedOut):
		return errJobTimedOut.Error()
	case errors.Is(err, db.ErrSelectionNotFound):
		return "the selection could not be read"
	default:
		return "the archive could not be written to storage"
	}
}

// startHeartbeat extends the job's lease every Heartbeat until the returned stop is called. A lost
// lease cancels the job with errLeaseLost.
func (w *Worker) startHeartbeat(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	job models.DownloadJob,
	logger *slog.Logger,
) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(w.cfg.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				err := w.jobs.Heartbeat(ctx, job.ID, job.Attempts, w.now().Add(w.cfg.Lease))
				switch {
				case errors.Is(err, db.ErrDownloadLeaseLost):
					cancel(errLeaseLost)
					return
				case err != nil && ctx.Err() == nil:
					logger.Warn("extend archive lease failed; trying again at the next heartbeat", "err", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// build plans the job and blocks it, or writes and finishes it.
func (w *Worker) build(ctx context.Context, job models.DownloadJob, logger *slog.Logger) error {
	selection, err := w.selections.Get(ctx, job.SelectionID)
	if err != nil {
		return fmt.Errorf("read selection: %w", err)
	}
	plan, err := w.plan(ctx, job)
	if err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	if plan.overLimit {
		reason := fmt.Sprintf("the files to include add up to at least %s, more than the %s one archive may hold",
			formatBytes(plan.bytes), formatBytes(job.MaxArchiveBytes))
		if err := w.jobs.Block(ctx, job.ID, job.Attempts, plan.bytes, reason, w.now()); err != nil {
			return fmt.Errorf("block: %w", err)
		}
		logger.Info("archive blocked: over its size limit", "planned_bytes", plan.bytes,
			"max_archive_bytes", job.MaxArchiveBytes)
		return nil
	}
	if err := w.jobs.StartPreparing(ctx, job.ID, job.Attempts, plan.bytes); err != nil {
		return fmt.Errorf("start preparing: %w", err)
	}
	header := manifestHeader{
		SelectionID: job.SelectionID,
		CatalogDate: selection.CreatedAt,
		FileChoice:  job.FileChoice,
		PreparedAt:  w.now(),
	}
	result, err := w.write(ctx, job, header, plan, logger)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	finishedAt := w.now()
	if err := w.jobs.Finish(ctx, job.ID, job.Attempts, result, finishedAt, finishedAt.Add(w.cfg.ArchiveTTL)); err != nil {
		return fmt.Errorf("finish: %w", err)
	}
	logger.Info("archive ready", "archive_bytes", result.ArchiveBytes, "planned_bytes", plan.bytes,
		"included", result.Counts.Included, "linked", result.Counts.Linked,
		"unavailable", result.Counts.Unavailable, "changed_at_source", result.Counts.ChangedAtSource)
	return nil
}

// archivePlan is what the first walk over the selection decided: which files the zip includes
// (by ID, with their size) and their total. The second walk includes exactly these files, so the
// archive never passes the size limit it was checked against.
type archivePlan struct {
	include   map[uuid.UUID]int64
	bytes     int64
	overLimit bool
}

// plan walks the selection's pinned files and adds up the bytes of the files the zip can include.
// It stops as soon as the total passes the job's limit.
func (w *Worker) plan(ctx context.Context, job models.DownloadJob) (archivePlan, error) {
	plan := archivePlan{include: map[uuid.UUID]int64{}}
	err := w.eachModel(ctx, job.SelectionID, func(model models.SelectionModel, details models.DownloadSelectionModel) error {
		for _, file := range model.Files {
			selected, err := selectsFile(job.FileChoice, file, details)
			if err != nil {
				return err
			}
			if _, planned := plan.include[file.ID]; !selected || planned {
				continue
			}
			state, err := stateOf(file, w.bucket)
			if err != nil {
				return err
			}
			if !state.canInclude {
				continue
			}
			plan.include[file.ID] = state.sizeBytes
			plan.bytes += state.sizeBytes
			if plan.bytes > job.MaxArchiveBytes {
				plan.overLimit = true
				return errStopPaging
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopPaging) {
		return archivePlan{}, err
	}
	return plan, nil
}

// eachModel calls visit for every model of the selection in order, with its manifest details,
// one page of short reads at a time.
func (w *Worker) eachModel(
	ctx context.Context,
	selectionID uuid.UUID,
	visit func(models.SelectionModel, models.DownloadSelectionModel) error,
) error {
	after := 0
	for {
		page, err := w.selections.ListModels(ctx, selectionID, after, w.cfg.PageSize)
		if err != nil {
			return fmt.Errorf("read selection models after %d: %w", after, err)
		}
		if len(page) == 0 {
			return nil
		}
		first, last := page[0].Ordinal, page[len(page)-1].Ordinal
		details, err := w.selections.ModelDetails(ctx, selectionID, first, last)
		if err != nil {
			return fmt.Errorf("read selection model details %d-%d: %w", first, last, err)
		}
		for _, model := range page {
			modelDetails, found := details[model.Ordinal]
			if !found {
				return fmt.Errorf("selection model %d has no details", model.Ordinal)
			}
			if err := visit(model, modelDetails); err != nil {
				return err
			}
		}
		if len(page) < w.cfg.PageSize {
			return nil
		}
		after = last
	}
}

// fileOutcome is how an included file ended up: in the zip at path, or left out after staging.
type fileOutcome struct {
	availability availability
	verification verification
	verifiedAt   *time.Time
	reason       string
	path         string
}

// archiveWrite is the state of the second walk.
type archiveWrite struct {
	job        models.DownloadJob
	header     manifestHeader
	plan       archivePlan
	zip        *zipArchive
	manifest   *manifestWriter
	scratch    *scratchFile
	layout     *archiveLayout
	outcomes   map[uuid.UUID]fileOutcome
	statuses   map[uuid.UUID]availability
	entryFiles map[string]bool
	entries    map[string]bool
	logger     *slog.Logger
}

// write streams the zip into the bucket: README.txt, every included file at its path, then
// manifest.tsv, which also goes into the bucket on its own.
func (w *Worker) write(
	ctx context.Context,
	job models.DownloadJob,
	header manifestHeader,
	plan archivePlan,
	logger *slog.Logger,
) (db.DownloadResult, error) {
	// Each attempt writes under a folder of its own: an attempt that lost its lease but still
	// reaches the bucket can never overwrite the objects a later attempt finished with.
	folder := fmt.Sprintf("%s%s/%d/", w.cfg.ArchivePrefix, job.ID, job.Attempts)
	archiveKey := folder + fmt.Sprintf("dynamic-pdb-%s-%s.zip", job.SelectionID, job.FileChoice)
	manifestKey := folder + "manifest.tsv"

	manifestFile, err := os.CreateTemp(w.cfg.ScratchDir, "manifest-*.tsv")
	if err != nil {
		return db.DownloadResult{}, fmt.Errorf("create manifest scratch file: %w", err)
	}
	defer func() {
		if cleanupErr := errors.Join(manifestFile.Close(), os.Remove(manifestFile.Name())); cleanupErr != nil {
			logger.Warn("remove manifest scratch file", "err", cleanupErr)
		}
	}()
	manifest, err := newManifestWriter(manifestFile, header)
	if err != nil {
		return db.DownloadResult{}, err
	}
	scratch, err := newScratchFile(w.cfg.ScratchDir)
	if err != nil {
		return db.DownloadResult{}, err
	}
	defer func() {
		if closeErr := scratch.close(); closeErr != nil {
			logger.Warn("remove scratch file", "err", closeErr)
		}
	}()

	upload, err := w.bucket.NewWriter(ctx, archiveKey)
	if err != nil {
		return db.DownloadResult{}, fmt.Errorf("start archive upload: %w", err)
	}
	uploaded := false
	defer func() {
		if !uploaded {
			if abortErr := upload.Abort(); abortErr != nil {
				logger.Warn("abort archive upload; the bucket's rule removes it", "err", abortErr)
			}
		}
	}()

	layout := newArchiveLayout(job.SelectionID)
	state := &archiveWrite{
		job: job, header: header, plan: plan,
		zip:        newZipArchive(upload, header.CatalogDate),
		manifest:   manifest,
		scratch:    scratch,
		layout:     layout,
		outcomes:   map[uuid.UUID]fileOutcome{},
		statuses:   map[uuid.UUID]availability{},
		entryFiles: map[string]bool{},
		entries:    map[string]bool{},
		logger:     logger,
	}
	if err := state.zip.add(layout.root+"/README.txt", strings.NewReader(readmeText(header))); err != nil {
		return db.DownloadResult{}, err
	}
	if err := w.eachModel(ctx, job.SelectionID, func(model models.SelectionModel, details models.DownloadSelectionModel) error {
		return w.writeModel(ctx, state, model, details)
	}); err != nil {
		return db.DownloadResult{}, err
	}
	if err := manifest.flush(); err != nil {
		return db.DownloadResult{}, err
	}
	if _, err := manifestFile.Seek(0, io.SeekStart); err != nil {
		return db.DownloadResult{}, fmt.Errorf("rewind manifest: %w", err)
	}
	if err := state.zip.add(layout.root+"/manifest.tsv", manifestFile); err != nil {
		return db.DownloadResult{}, err
	}
	archiveBytes, archiveSHA256, err := state.zip.close()
	if err != nil {
		return db.DownloadResult{}, err
	}
	if err := upload.Complete(); err != nil {
		uploaded = true // Complete aborts a multipart upload it cannot finish.
		return db.DownloadResult{}, fmt.Errorf("complete archive upload: %w", err)
	}
	uploaded = true
	if err := w.uploadManifest(ctx, manifestFile, manifestKey); err != nil {
		return db.DownloadResult{}, err
	}
	archiveURL, err := w.bucket.URL(archiveKey)
	if err != nil {
		return db.DownloadResult{}, fmt.Errorf("archive url: %w", err)
	}
	manifestURL, err := w.bucket.URL(manifestKey)
	if err != nil {
		return db.DownloadResult{}, fmt.Errorf("manifest url: %w", err)
	}
	counts, err := countStatuses(state.statuses)
	if err != nil {
		return db.DownloadResult{}, err
	}
	return db.DownloadResult{
		ArchiveKey:    archiveKey,
		ArchiveURL:    archiveURL,
		ArchiveBytes:  archiveBytes,
		ArchiveSHA256: archiveSHA256,
		ManifestKey:   manifestKey,
		ManifestURL:   manifestURL,
		Counts:        counts,
	}, nil
}

// uploadManifest stores manifest.tsv on its own next to the zip.
func (w *Worker) uploadManifest(ctx context.Context, manifestFile *os.File, key string) error {
	if _, err := manifestFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind manifest: %w", err)
	}
	upload, err := w.bucket.NewWriter(ctx, key)
	if err != nil {
		return fmt.Errorf("start manifest upload: %w", err)
	}
	if _, err := io.Copy(upload, manifestFile); err != nil {
		return errors.Join(fmt.Errorf("write manifest upload: %w", err), upload.Abort())
	}
	if err := upload.Complete(); err != nil {
		return fmt.Errorf("complete manifest upload: %w", err)
	}
	return nil
}

// writeModel writes one model's manifest rows, includes its planned files the first time they
// appear, and lists its entry's related datasets once per entry.
func (w *Worker) writeModel(
	ctx context.Context,
	state *archiveWrite,
	model models.SelectionModel,
	details models.DownloadSelectionModel,
) error {
	matched := false
	for _, file := range model.Files {
		selected, err := selectsFile(state.job.FileChoice, file, details)
		if err != nil {
			return err
		}
		if !selected {
			continue
		}
		matched = true
		entryFile := isEntryFile(file)
		if entryFile {
			key := model.EntryID + "/" + file.ID.String()
			if state.entryFiles[key] {
				continue
			}
			state.entryFiles[key] = true
		}
		row, err := w.fileRow(ctx, state, model, details, file, entryFile)
		if err != nil {
			return err
		}
		if err := state.manifest.write(row); err != nil {
			return err
		}
	}
	if !matched {
		reason, err := noMatchingFileReason(state.job.FileChoice)
		if err != nil {
			return err
		}
		if reason != "" {
			row := merge(recordValues(model, details, true), manifestRow{
				"record_type": string(recordNoMatchingFile),
				"reason":      reason,
			})
			if err := state.manifest.write(row); err != nil {
				return err
			}
		}
	}
	if !state.entries[model.EntryID] {
		state.entries[model.EntryID] = true
		for _, dataset := range details.RelatedDatasets {
			row := merge(recordValues(model, details, false), relatedDatasetRow(dataset))
			if err := state.manifest.write(row); err != nil {
				return err
			}
		}
	}
	return nil
}

// fileRow is a file's manifest row for the model it is listed with. A planned file is staged and
// added to the zip the first time it appears; later rows point to the same path.
func (w *Worker) fileRow(
	ctx context.Context,
	state *archiveWrite,
	model models.SelectionModel,
	details models.DownloadSelectionModel,
	file models.SelectionFile,
	entryFile bool,
) (manifestRow, error) {
	values, err := fileRowValues(file, sourceOf(file, w.bucket))
	if err != nil {
		return nil, err
	}
	row := merge(recordValues(model, details, !entryFile), values)
	fileState, err := stateOf(file, w.bucket)
	if err != nil {
		return nil, err
	}
	var outcome fileOutcome
	if _, planned := state.plan.include[file.ID]; planned {
		done, seen := state.outcomes[file.ID]
		if !seen {
			done, err = w.includeFile(ctx, state, model, file, fileState, entryFile)
			if err != nil {
				return nil, err
			}
			state.outcomes[file.ID] = done
		}
		outcome = done
	} else {
		outcome = fileOutcome{
			availability: fileState.availability,
			verification: fileState.verification,
			verifiedAt:   fileState.verifiedAt,
			reason:       fileState.reason,
		}
		if fileState.canInclude {
			// The checksum job copied the file after the archive was planned: the plan, and so the
			// size limit, did not count it.
			outcome = fileOutcome{
				availability: availabilityLinked,
				verification: verificationVerified,
				verifiedAt:   file.Checksum.CheckedAt,
				reason:       "Dynamic PDB copied this file after the archive was planned; ask again to include it",
			}
		}
	}
	state.statuses[file.ID] = outcome.availability
	row["status"] = string(outcome.availability)
	row["verification"] = string(outcome.verification)
	row["reason"] = outcome.reason
	row["path_in_archive"] = outcome.path
	if outcome.verifiedAt != nil {
		row["verified_at"] = outcome.verifiedAt.UTC().Format(time.RFC3339)
	}
	return row, nil
}

// includeFile stages a planned file from the bucket, checks it against its recorded SHA-256 and
// size, and adds it to the zip only when both match. A missing or differing copy is left out and
// reported, never replaced.
func (w *Worker) includeFile(
	ctx context.Context,
	state *archiveWrite,
	model models.SelectionModel,
	file models.SelectionFile,
	fileState fileState,
	entryFile bool,
) (fileOutcome, error) {
	staged, err := state.scratch.stage(ctx, w.bucket, fileState.key, w.cfg.FileReadAttempts, w.cfg.FileTimeout,
		w.firstReadPause)
	if errors.Is(err, errStoredCopyMissing) {
		state.logger.Error("archive left out a file: our copy is missing", "file_id", file.ID, "key", fileState.key)
		return fileOutcome{
			availability: availabilityUnavailable,
			verification: verificationNotVerified,
			reason:       "Dynamic PDB's copy of the file is missing",
		}, nil
	}
	if err != nil {
		return fileOutcome{}, err
	}
	recorded := strings.ToLower(strings.TrimSpace(stringValue(file.RecordedSHA256)))
	if staged.sha256 != recorded || staged.size != fileState.sizeBytes {
		state.logger.Error("archive left out a file: our copy differs from its record",
			"file_id", file.ID, "key", fileState.key,
			"recorded_sha256", recorded, "read_sha256", staged.sha256,
			"recorded_size", fileState.sizeBytes, "read_size", staged.size)
		return fileOutcome{
			availability: availabilityUnavailable,
			verification: verificationMismatch,
			reason:       "Dynamic PDB's copy differs from the recorded SHA-256",
		}, nil
	}
	path := state.layout.path(file, model, entryFile)
	if err := state.zip.add(path, state.scratch.reader()); err != nil {
		return fileOutcome{}, err
	}
	verifiedAt := state.header.PreparedAt
	return fileOutcome{
		availability: availabilityIncluded,
		verification: verificationVerified,
		verifiedAt:   &verifiedAt,
		path:         path,
	}, nil
}

// countStatuses counts distinct files by availability.
func countStatuses(statuses map[uuid.UUID]availability) (models.DownloadCounts, error) {
	var counts models.DownloadCounts
	for fileID, status := range statuses {
		switch status {
		case availabilityIncluded:
			counts.Included++
		case availabilityLinked:
			counts.Linked++
		case availabilityUnavailable:
			counts.Unavailable++
		case availabilityChangedAtSource:
			counts.ChangedAtSource++
		default:
			return models.DownloadCounts{}, fmt.Errorf("file %s: unknown availability: %s", fileID, status)
		}
	}
	return counts, nil
}

// formatBytes writes a byte count in decimal gigabytes or megabytes, as the 10 GB limit is.
func formatBytes(value int64) string {
	switch {
	case value >= 1_000_000_000:
		return fmt.Sprintf("%.1f GB (%d bytes)", float64(value)/1e9, value)
	case value >= 1_000_000:
		return fmt.Sprintf("%.1f MB (%d bytes)", float64(value)/1e6, value)
	default:
		return fmt.Sprintf("%d bytes", value)
	}
}
