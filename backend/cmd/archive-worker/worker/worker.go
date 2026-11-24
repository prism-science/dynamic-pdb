// Package worker builds bulk-download archives in two lanes. The planning lane claims a queued
// download job, walks the frozen selection's pinned files and records the archive's preview, or
// blocks it (over its size limit, before any file is read). The building lane claims a planned job
// whose archive was asked for, plans it again and writes the zip and its manifest into the data
// bucket.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/downloadfiles"
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
	// MaxAttempts is how many failed attempts each phase of a job (planning, building) gets
	// before the job fails.
	MaxAttempts int `mapstructure:"max_attempts"`
	// PageSize is how many selection models one database read takes.
	PageSize int `mapstructure:"page_size"`
	// ScratchDir holds one file at a time while it is checked, and the manifest; empty is the
	// system's temporary directory.
	ScratchDir string `mapstructure:"scratch_dir"`
	// ArchivePrefix is the bucket folder of the archives; the lifecycle rule expires it.
	ArchivePrefix string `mapstructure:"archive_prefix"`
	// FileReadAttempts is how many times reading one file from the bucket is tried. A file still
	// unreadable after them is listed as read_failed, and a retry may fetch it.
	FileReadAttempts int `mapstructure:"file_read_attempts"`
	// MaxConsecutiveReadFailures is how many files in a row may fail to read before the worker
	// takes the bucket for down and tries the whole attempt again later.
	MaxConsecutiveReadFailures int `mapstructure:"max_consecutive_read_failures"`
	// FileTimeout bounds one read of one file from the bucket, so a stalled read is tried again
	// instead of holding the job while its heartbeat keeps the lease.
	FileTimeout time.Duration `mapstructure:"file_timeout"`
	// JobTimeout bounds one attempt at one archive; past it the attempt fails and is tried again.
	JobTimeout time.Duration `mapstructure:"job_timeout"`
	// ExitWhenIdle runs both lanes in turn in one loop and stops the first time neither finds a
	// job waiting, for tests and one-off runs. The Deployment leaves it off: each lane then runs
	// on its own, so a long build never holds up a preview.
	ExitWhenIdle bool `mapstructure:"exit_when_idle"`
	// ArchiveTTL is how long a ready archive is offered.
	ArchiveTTL time.Duration `mapstructure:"-"`
	// CC0Depositors are Prism's accounts: files they added are CC0 (downloads::cc0_depositors,
	// researchers' Q7). Files Dynamic PDB's own system account added are CC0 as well.
	CC0Depositors []uuid.UUID `mapstructure:"-"`
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
	case c.MaxConsecutiveReadFailures <= 0:
		return errors.New("max_consecutive_read_failures must be positive")
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
	Claim(ctx context.Context, lane db.DownloadLane, now, leaseUntil time.Time) (*models.DownloadJob, error)
	Heartbeat(ctx context.Context, id uuid.UUID, attempt int, leaseUntil time.Time) error
	FinishPlanning(ctx context.Context, id uuid.UUID, attempt int, plannedBytes int64, preview models.DownloadPreview) error
	UpdatePlan(ctx context.Context, id uuid.UUID, attempt int, plannedBytes int64, preview models.DownloadPreview) error
	// RetryFileIDs returns the files a retry of the job asks for again.
	RetryFileIDs(ctx context.Context, jobID uuid.UUID) (map[uuid.UUID]bool, error)
	Block(ctx context.Context, id uuid.UUID, attempt int, plannedBytes int64, preview models.DownloadPreview,
		reason string, now time.Time) error
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

// errStorageUnreadable ends an attempt whose files failed to read MaxConsecutiveReadFailures times in
// a row: the bucket is taken for down, and the whole attempt is tried again later.
var errStorageUnreadable = errors.New("the archive's files could not be read from Dynamic PDB's storage")

// releaseTimeout bounds putting a job back in the queue after the worker was told to stop.
const releaseTimeout = 10 * time.Second

// lanes are the worker's loops, in the order a one-off run takes them.
var lanes = []db.DownloadLane{db.DownloadLanePlanning, db.DownloadLaneBuilding}

// Worker plans and builds archives, one job at a time in each lane.
type Worker struct {
	jobs       JobStore
	selections SelectionReader
	bucket     Bucket
	cfg        Config
	logger     *slog.Logger
	now        func() time.Time
	// firstReadPause is the pause before retrying a failed read from the bucket.
	firstReadPause time.Duration
	// newScratch creates the scratch file an attempt stages each file in.
	newScratch func(dir string) (*scratchFile, error)
	// cc0Depositors is Config.CC0Depositors as a set.
	cc0Depositors map[uuid.UUID]bool
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
		newScratch:     newScratchFile,
		cc0Depositors:  depositorSet(cfg.CC0Depositors),
	}, nil
}

// Run plans and builds jobs until ctx ends. Errors of one job are recorded on the job; errors
// reaching the queue are logged and tried again after the poll interval.
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("archive worker started", "exit_when_idle", w.cfg.ExitWhenIdle)
	if w.cfg.ExitWhenIdle {
		w.poll(ctx, "both lanes", w.RunOnce, true)
	} else {
		var group sync.WaitGroup
		for _, lane := range lanes {
			group.Go(func() {
				w.poll(ctx, string(lane), func(ctx context.Context) (bool, error) {
					return w.runLaneOnce(ctx, lane)
				}, false)
			})
		}
		group.Wait()
	}
	w.logger.Info("archive worker stopped")
	return nil
}

// poll runs once until ctx ends, straight on after a job and after the poll interval otherwise;
// with stopWhenIdle it returns the first time once finds no job.
func (w *Worker) poll(ctx context.Context, name string, once func(context.Context) (bool, error), stopWhenIdle bool) {
	for ctx.Err() == nil {
		worked, err := once(ctx)
		if err != nil && ctx.Err() == nil {
			w.logger.Error("archive worker could not take a job", "lane", name, "err", err)
		}
		if worked && err == nil {
			continue
		}
		if !worked && err == nil && stopWhenIdle {
			return
		}
		if err := sleepContext(ctx, w.cfg.PollInterval); err != nil {
			return
		}
	}
}

// RunOnce plans one waiting job, then builds one. It reports whether either lane had a job.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	worked := false
	for _, lane := range lanes {
		if ctx.Err() != nil {
			break
		}
		laneWorked, err := w.runLaneOnce(ctx, lane)
		worked = worked || laneWorked
		if err != nil {
			return worked, err
		}
	}
	return worked, nil
}

// runLaneOnce claims one job for the lane and plans or builds it. It reports whether there was a
// job.
func (w *Worker) runLaneOnce(ctx context.Context, lane db.DownloadLane) (bool, error) {
	verb, err := laneVerb(lane)
	if err != nil {
		return false, err
	}
	now := w.now()
	job, err := w.jobs.Claim(ctx, lane, now, now.Add(w.cfg.Lease))
	if err != nil {
		return false, fmt.Errorf("claim for %s: %w", lane, err)
	}
	if job == nil {
		return false, nil
	}
	logger := w.logger.With("download_id", job.ID, "selection_id", job.SelectionID,
		"file_choice", job.FileChoice, "lane", lane, "attempt", job.Attempts, "failed_attempts", job.FailedAttempts)
	if job.FailedAttempts >= w.cfg.MaxAttempts {
		message := fmt.Sprintf("the archive could not be %s in %d attempts", verb, w.cfg.MaxAttempts)
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
	w.runJob(ctx, lane, *job, logger)
	return true, nil
}

// laneVerb is what a lane does to an archive, for messages.
func laneVerb(lane db.DownloadLane) (string, error) {
	switch lane {
	case db.DownloadLanePlanning:
		return "planned", nil
	case db.DownloadLaneBuilding:
		return "built", nil
	default:
		return "", fmt.Errorf("unknown download lane: %s", lane)
	}
}

// runJob plans or builds a claimed job while a heartbeat holds its lease, then records how it
// ended when the work itself did not: put back in its lane's queue on shutdown, requeued or failed
// on an error.
func (w *Worker) runJob(ctx context.Context, lane db.DownloadLane, job models.DownloadJob, logger *slog.Logger) {
	jobCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	buildCtx, cancelBuild := context.WithTimeoutCause(jobCtx, w.cfg.JobTimeout, errJobTimedOut)
	defer cancelBuild()
	stopHeartbeat := w.startHeartbeat(jobCtx, cancel, job, logger)
	err := w.work(buildCtx, lane, job, logger)
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
	case job.FailedAttempts+1 >= w.cfg.MaxAttempts:
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
	case errors.Is(err, errStorageUnreadable):
		return errStorageUnreadable.Error()
	case errors.Is(err, errScratchDisk):
		return errScratchDisk.Error()
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

// work does the lane's part of the job.
func (w *Worker) work(ctx context.Context, lane db.DownloadLane, job models.DownloadJob, logger *slog.Logger) error {
	switch lane {
	case db.DownloadLanePlanning:
		return w.planJob(ctx, job, logger)
	case db.DownloadLaneBuilding:
		return w.buildJob(ctx, job, logger)
	default:
		return fmt.Errorf("unknown download lane: %s", lane)
	}
}

// planJob plans the job and records its preview, or blocks it when its files add up to more than
// its limit.
func (w *Worker) planJob(ctx context.Context, job models.DownloadJob, logger *slog.Logger) error {
	only, err := w.retryFiles(ctx, job)
	if err != nil {
		return err
	}
	plan, err := w.plan(ctx, job, only)
	if err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	if plan.overLimit() {
		return w.block(ctx, job, plan, logger)
	}
	if err := w.jobs.FinishPlanning(ctx, job.ID, job.Attempts, plan.bytes, plan.preview); err != nil {
		return fmt.Errorf("finish planning: %w", err)
	}
	logger.Info("archive planned", "planned_bytes", plan.bytes, "files", plan.preview.Files,
		"prepare_requested", job.PrepareRequested)
	return nil
}

// block finishes a job whose files add up to more than its limit, before any file is read.
func (w *Worker) block(ctx context.Context, job models.DownloadJob, plan archivePlan, logger *slog.Logger) error {
	reason := fmt.Sprintf("the files to include add up to %s, more than the %s one archive may hold",
		formatBytes(plan.bytes), formatBytes(job.MaxArchiveBytes))
	if err := w.jobs.Block(ctx, job.ID, job.Attempts, plan.bytes, plan.preview, reason, w.now()); err != nil {
		return fmt.Errorf("block: %w", err)
	}
	logger.Info("archive blocked: over its size limit", "planned_bytes", plan.bytes,
		"max_archive_bytes", job.MaxArchiveBytes)
	return nil
}

// buildJob plans the job again, records the new plan when it differs from the preview shown (a
// file was copied in between), and writes and finishes the archive, or blocks it when it now
// passes its limit.
func (w *Worker) buildJob(ctx context.Context, job models.DownloadJob, logger *slog.Logger) error {
	selection, err := w.selections.Get(ctx, job.SelectionID)
	if err != nil {
		return fmt.Errorf("read selection: %w", err)
	}
	only, err := w.retryFiles(ctx, job)
	if err != nil {
		return err
	}
	plan, err := w.plan(ctx, job, only)
	if err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	if plan.overLimit() {
		return w.block(ctx, job, plan, logger)
	}
	same, err := samePlan(job, plan)
	if err != nil {
		return err
	}
	if !same {
		if err := w.jobs.UpdatePlan(ctx, job.ID, job.Attempts, plan.bytes, plan.preview); err != nil {
			return fmt.Errorf("update plan: %w", err)
		}
		logger.Info("archive plan changed since its preview", "planned_bytes", plan.bytes)
	}
	header := manifestHeader{
		SelectionID: job.SelectionID,
		CatalogDate: selection.CreatedAt,
		FileChoice:  job.FileChoice,
		PreparedAt:  w.now(),
		RetryOf:     job.RetryOf,
	}
	result, err := w.write(ctx, job, header, plan, only, logger)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	finishedAt := w.now()
	if err := w.jobs.Finish(ctx, job.ID, job.Attempts, result, finishedAt, finishedAt.Add(w.cfg.ArchiveTTL)); err != nil {
		return fmt.Errorf("finish: %w", err)
	}
	logger.Info("archive ready", "archive_bytes", result.ArchiveBytes, "planned_bytes", plan.bytes,
		"included", result.Counts.Included, "linked", result.Counts.Linked,
		"unavailable", result.Counts.Unavailable,
		"failures", len(result.Failures))
	return nil
}

// retryFiles returns the files a retry job asks for again, or nil for a job of the whole
// selection.
func (w *Worker) retryFiles(ctx context.Context, job models.DownloadJob) (map[uuid.UUID]bool, error) {
	if job.RetryOf == nil {
		return nil, nil
	}
	files, err := w.jobs.RetryFileIDs(ctx, *job.RetryOf)
	if err != nil {
		return nil, fmt.Errorf("read the files to retry: %w", err)
	}
	return files, nil
}

// samePlan reports whether a new plan equals the one the job recorded when it was planned.
func samePlan(job models.DownloadJob, plan archivePlan) (bool, error) {
	if job.Preview == nil || job.PlannedBytes == nil || *job.PlannedBytes != plan.bytes {
		return false, nil
	}
	recorded, err := json.Marshal(job.Preview)
	if err != nil {
		return false, fmt.Errorf("encode recorded preview: %w", err)
	}
	planned, err := json.Marshal(plan.preview)
	if err != nil {
		return false, fmt.Errorf("encode new preview: %w", err)
	}
	return bytes.Equal(recorded, planned), nil
}

// archivePlan is what the planning walk over the selection decided: which files the zip includes
// (by ID, with their size), their total, and the preview. The writing walk includes exactly these
// files, so the archive never passes the size limit it was checked against.
type archivePlan struct {
	include         map[uuid.UUID]int64
	bytes           int64
	maxArchiveBytes int64
	preview         models.DownloadPreview
	// modelFolders are every selected model's folder, reserved before the writing walk names any
	// file.
	modelFolders []string
}

func (plan archivePlan) overLimit() bool {
	return plan.bytes > plan.maxArchiveBytes
}

// plan walks all of the selection's pinned files (only those of a retry), as the writing walk will,
// and adds up the bytes of the files the zip can include and the preview.
func (w *Worker) plan(ctx context.Context, job models.DownloadJob, only map[uuid.UUID]bool) (archivePlan, error) {
	collector, err := newPreviewCollector(job, w.bucket, w.cc0Depositors, only)
	if err != nil {
		return archivePlan{}, err
	}
	if err := w.eachModel(ctx, job.SelectionID, collector.addModel); err != nil {
		return archivePlan{}, err
	}
	return collector.plan()
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

// fileOutcome is how a file is listed: in the zip at path, or left out with a reason code.
type fileOutcome struct {
	availability availability
	verification verification
	verifiedAt   *time.Time
	reasonCode   models.DownloadReasonCode
	path         string
}

// stateOutcome is how a file the archive does not try to include is listed: as the checksum job
// left it.
func stateOutcome(state fileState) fileOutcome {
	return fileOutcome{
		availability: state.availability,
		verification: state.verification,
		verifiedAt:   state.verifiedAt,
		reasonCode:   state.reasonCode,
	}
}

// archiveWrite is the state of the second walk.
type archiveWrite struct {
	job      models.DownloadJob
	header   manifestHeader
	plan     archivePlan
	zip      *zipArchive
	manifest *manifestWriter
	scratch  *scratchFile
	layout   *archiveLayout
	outcomes map[uuid.UUID]fileOutcome
	statuses map[uuid.UUID]availability
	walk     *modelWalk
	logger   *slog.Logger
	// failures are the files listed as unavailable or changed-at-source, in the order they first
	// appear.
	failures []models.DownloadFailure
	// readFailuresInARow counts the files that failed to read since the last one that did not.
	readFailuresInARow int
}

// write streams the zip into the bucket: README.txt, every included file at its path, then
// manifest.tsv, which also goes into the bucket on its own.
func (w *Worker) write(
	ctx context.Context,
	job models.DownloadJob,
	header manifestHeader,
	plan archivePlan,
	only map[uuid.UUID]bool,
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
	scratch, err := w.newScratch(w.cfg.ScratchDir)
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

	layout := newArchiveLayout(archiveRoot(job))
	for _, folder := range plan.modelFolders {
		layout.reserveFolder(folder)
	}
	state := &archiveWrite{
		job: job, header: header, plan: plan,
		zip:      newZipArchive(upload, header.CatalogDate),
		manifest: manifest,
		scratch:  scratch,
		layout:   layout,
		outcomes: map[uuid.UUID]fileOutcome{},
		statuses: map[uuid.UUID]availability{},
		walk:     newModelWalk(job.FileChoice, only),
		logger:   logger,
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
		Failures:      state.failures,
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
	return modelRows(state.walk, model, details, func(row selectedFile) (manifestRow, error) {
		outcome, err := w.outcomeOf(ctx, state, model, row)
		if err != nil {
			return nil, err
		}
		if _, listed := state.statuses[row.file.ID]; !listed {
			if err := state.recordFailure(model, row.file, outcome); err != nil {
				return nil, err
			}
		}
		state.statuses[row.file.ID] = outcome.availability
		return fileRow(model, details, row, outcome, w.bucket, w.cc0Depositors)
	}, state.manifest.write)
}

// recordFailure keeps a file the archive lists as unavailable or changed-at-source, the first
// time it appears; included and linked files are not failures.
func (state *archiveWrite) recordFailure(model models.SelectionModel, file models.SelectionFile, outcome fileOutcome) error {
	switch outcome.availability {
	case availabilityIncluded, availabilityLinked:
		return nil
	case availabilityUnavailable:
	default:
		return fmt.Errorf("file %s: unknown availability: %s", file.ID, outcome.availability)
	}
	retryable, err := outcome.reasonCode.Retryable()
	if err != nil {
		return fmt.Errorf("file %s: %w", file.ID, err)
	}
	state.failures = append(state.failures, models.DownloadFailure{
		JobID: state.job.ID, FileID: file.ID, Ordinal: model.Ordinal, EntryID: model.EntryID, ModelID: model.ModelID,
		FileName: file.Name, ReasonCode: outcome.reasonCode, Retryable: retryable,
	})
	return nil
}

// modelRows lists one model in the manifest: a row per file of the file choice (fileRowOf builds
// it), a row saying why when the model has none, and its entry's related datasets for the first
// model of an entry. The writing walk and the planning walk both list the selection with it, so the
// preview counts what the manifest holds.
func modelRows(
	walk *modelWalk,
	model models.SelectionModel,
	details models.DownloadSelectionModel,
	fileRowOf func(selectedFile) (manifestRow, error),
	write func(manifestRow) error,
) error {
	rows, matched, err := walk.filesOf(model, details)
	if err != nil {
		return err
	}
	for _, selected := range rows {
		row, err := fileRowOf(selected)
		if err != nil {
			return err
		}
		if err := write(row); err != nil {
			return err
		}
	}
	if walk.only != nil {
		// A retry lists only the files it asks for again.
		return nil
	}
	if !matched {
		reason, err := noMatchingFileReason(walk.choice)
		if err != nil {
			return err
		}
		if reason != "" {
			row := merge(recordValues(model, details, true), manifestRow{
				"record_type": string(recordNoMatchingFile),
				"reason":      reason,
			})
			if err := write(row); err != nil {
				return err
			}
		}
	}
	if walk.firstOfEntry(model) {
		for _, dataset := range details.RelatedDatasets {
			if err := write(merge(recordValues(model, details, false), relatedDatasetRow(dataset))); err != nil {
				return err
			}
		}
	}
	return nil
}

// outcomeOf is how the archive lists a file: a planned file is staged and added to the zip the
// first time it appears, and later rows point to the same path; any other file is listed as the
// checksum job left it.
func (w *Worker) outcomeOf(
	ctx context.Context,
	state *archiveWrite,
	model models.SelectionModel,
	row selectedFile,
) (fileOutcome, error) {
	fileState, err := stateOf(row.file, w.bucket)
	if err != nil {
		return fileOutcome{}, err
	}
	if _, planned := state.plan.include[row.file.ID]; planned {
		if done, seen := state.outcomes[row.file.ID]; seen {
			return done, nil
		}
		done, err := w.includeFile(ctx, state, model, row.file, fileState, row.entryFile)
		if err != nil {
			return fileOutcome{}, err
		}
		state.outcomes[row.file.ID] = done
		return done, nil
	}
	if fileState.canInclude {
		// The checksum job copied the file after the archive was planned: the plan, and so the size
		// limit, did not count it.
		return fileOutcome{
			availability: availabilityLinked,
			verification: verificationVerified,
			verifiedAt:   row.file.Checksum.CheckedAt,
			reasonCode:   models.DownloadReasonCopiedAfterPlanning,
		}, nil
	}
	return stateOutcome(fileState), nil
}

// fileRow is a file's manifest row for the model it is listed with.
func fileRow(
	model models.SelectionModel,
	details models.DownloadSelectionModel,
	selected selectedFile,
	outcome fileOutcome,
	bucket keyLocator,
	cc0Depositors map[uuid.UUID]bool,
) (manifestRow, error) {
	values, err := fileRowValues(selected.file, sourceOf(selected.file, bucket), cc0Depositors)
	if err != nil {
		return nil, err
	}
	reason, err := reasonText(outcome.reasonCode)
	if err != nil {
		return nil, err
	}
	row := merge(recordValues(model, details, !selected.entryFile), values)
	row["status"] = string(outcome.availability)
	row["verification"] = string(outcome.verification)
	row["reason"] = reason
	row["reason_code"] = string(outcome.reasonCode)
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
		state.readFailuresInARow = 0
		state.logger.Error("archive left out a file: our copy is missing", "file_id", file.ID, "key", fileState.key)
		return fileOutcome{
			availability: availabilityUnavailable,
			verification: verificationNotVerified,
			reasonCode:   models.DownloadReasonCopyMissing,
		}, nil
	}
	if errors.Is(err, errScratchDisk) {
		// The worker's own disk failed, not our copy: the attempt fails and is tried again, and a
		// file the bucket served is never listed as read_failed.
		return fileOutcome{}, err
	}
	if err != nil {
		if ctx.Err() != nil {
			// The worker is stopping, lost its lease or ran out of time: not this file's fault.
			return fileOutcome{}, err
		}
		state.readFailuresInARow++
		if state.readFailuresInARow >= w.cfg.MaxConsecutiveReadFailures {
			return fileOutcome{}, fmt.Errorf("%w: %d files in a row: %w", errStorageUnreadable, state.readFailuresInARow, err)
		}
		state.logger.Error("archive left out a file: our copy could not be read", "file_id", file.ID,
			"key", fileState.key, "err", err)
		return fileOutcome{
			availability: availabilityUnavailable,
			verification: verificationNotVerified,
			reasonCode:   models.DownloadReasonReadFailed,
		}, nil
	}
	state.readFailuresInARow = 0
	recorded := strings.ToLower(strings.TrimSpace(stringValue(file.RecordedSHA256)))
	if staged.sha256 != recorded || staged.size != fileState.sizeBytes {
		state.logger.Error("archive left out a file: our copy differs from its record",
			"file_id", file.ID, "key", fileState.key,
			"recorded_sha256", recorded, "read_sha256", staged.sha256,
			"recorded_size", fileState.sizeBytes, "read_size", staged.size)
		return fileOutcome{
			availability: availabilityUnavailable,
			verification: verificationMismatch,
			reasonCode:   models.DownloadReasonCopyDiffers,
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

// depositorSet is a list of accounts as a set.
func depositorSet(accounts []uuid.UUID) map[uuid.UUID]bool {
	return downloadfiles.DepositorSet(accounts)
}
