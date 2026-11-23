package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/modelfiles"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

const (
	// maxAttempts is when a file that keeps failing to read is marked failed.
	maxAttempts = 3
	// firstRetryDelay doubles after each failed attempt: 10 min, then 20 min.
	firstRetryDelay = 10 * time.Minute
	// leaseMargin is how long a claim outlives the file timeout, so a file still being read is
	// never claimed by the next run.
	leaseMargin = 5 * time.Minute
)

// SavedIndex is what a save stored for a read: its chains, with lengths from the entry FASTA
// records where the file states none, and its warnings.
type SavedIndex struct {
	Chains   []models.IndexedChain
	Warnings []string
	// Entries counts the entries of the file's models.
	Entries int
}

// Queue is the coordinate_index_sets table as the job uses it.
type Queue interface {
	// AddMissingSince gives pending sets to model files in our bucket created since a time that
	// have none; with archiveFiles, also to recent external model files the checksum job copied
	// into our bucket.
	AddMissingSince(ctx context.Context, since time.Time, archiveFiles bool) (int64, error)
	// AddFillAfter gives pending fill sets to the model files without one in the next page of
	// artifacts in id order.
	AddFillAfter(ctx context.Context, after uuid.UUID, limit int, archiveFiles bool) (models.MissingSetsPage, error)
	// HasPendingFill reports whether a set the fill queued is still pending.
	HasPendingFill(ctx context.Context) (bool, error)
	// RequeueForNewEntries queues done sets again of files whose models now belong to other entries
	// than when they were read, looking at model revisions created since a time.
	RequeueForNewEntries(ctx context.Context, since time.Time, now time.Time) (int64, error)
	// ClaimDue fails, instead of claiming, a set already claimed maxAttempts times.
	ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration, maxAttempts int) ([]models.CoordinateIndexClaim, error)
	// RecordFailure keeps the set pending until retryAt, or fails it for good when retryAt is nil.
	RecordFailure(ctx context.Context, artifactID uuid.UUID, message string, retryAt *time.Time) error
	// Save takes the lengths the file does not state from the FASTA records of the entries of its
	// models, as the save transaction sees them (resolveIndex), replaces the file's chains and
	// finishes its set.
	Save(ctx context.Context, artifactID uuid.UUID, read cif.CoordinateIndex, readAt time.Time) (SavedIndex, error)
}

type Config struct {
	// MaxFilesPerRun caps the files one run reads; 0 means no cap.
	MaxFilesPerRun int `mapstructure:"max_files_per_run"`
	// ClaimFor is how long after its start a run keeps claiming files.
	ClaimFor time.Duration `mapstructure:"claim_for"`
	// FileTimeout bounds the read of one file, from opening it to its last line.
	FileTimeout time.Duration `mapstructure:"file_timeout"`
	// RecentWindow is how far back each run looks for new model files without a set (an upload
	// an older backend stored while a deploy was rolling out, an archive file copied since), and
	// for model revisions that link a file read earlier to its entry.
	RecentWindow time.Duration `mapstructure:"recent_window"`
	// Fill turns on the paced fill over every artifact, which gives the model files stored before
	// this job existed their sets. A run adds nothing while sets the fill queued are still pending,
	// so the fill is not scanned again every run. Off once a run adds none.
	Fill bool `mapstructure:"fill"`
	// FillBatchSize is how many artifacts one statement of a fill looks at.
	FillBatchSize int `mapstructure:"fill_batch_size"`
	// ArchiveFiles lets the recent window and the fill take archive model files (on RCSB) once the
	// checksum job has copied them into our bucket; the job reads the copy.
	ArchiveFiles bool `mapstructure:"archive_files"`
}

func (c Config) validate() error {
	switch {
	case c.MaxFilesPerRun < 0:
		return errors.New("max_files_per_run must not be negative")
	case c.ClaimFor <= 0:
		return errors.New("claim_for must be positive")
	case c.FileTimeout <= 0:
		return errors.New("file_timeout must be positive")
	case c.RecentWindow <= 0:
		return errors.New("recent_window must be positive")
	case c.FillBatchSize <= 0:
		return errors.New("fill_batch_size must be positive")
	}
	return nil
}

// CoordinateIndexJob reads every model file with a pending set and stores its chains
// (cif.ReadCoordinateIndex), one file at a time. A chain whose length the file does not state
// takes it from the entry's FASTA records.
type CoordinateIndexJob struct {
	queue  Queue
	files  modelfiles.StoredFiles
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
	sleep  func(ctx context.Context, pause time.Duration) error
}

func NewCoordinateIndexJob(queue Queue, files modelfiles.StoredFiles, cfg Config, logger *slog.Logger) (*CoordinateIndexJob, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("coordinate index job config: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &CoordinateIndexJob{
		queue:  queue,
		files:  files,
		cfg:    cfg,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
		sleep:  sleepContext,
	}, nil
}

// sleepContext waits for pause, or returns the context's error if it ends first.
func sleepContext(ctx context.Context, pause time.Duration) error {
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("pause interrupted: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// outcome is what one claimed file came to.
type outcome int

const (
	outcomeDone outcome = iota
	outcomeRetryLater
	outcomeFailed
	// outcomeStopped: the run ended during the read; the claim lapses with its lease.
	outcomeStopped
)

type runSummary struct {
	files    int
	outcomes map[outcome]int
}

// Run queues the model files that lack a set and the done files whose models changed entries, adds a
// page of the fill when it is on, then claims and reads due files until nothing is due, the run's
// claiming time is over, or MaxFilesPerRun is reached. It stops with an error only when the
// database fails or ctx ends.
func (j *CoordinateIndexJob) Run(ctx context.Context) error {
	started := j.now()
	claimUntil := started.Add(j.cfg.ClaimFor)
	windowStart := started.Add(-j.cfg.RecentWindow)
	recent, err := j.queue.AddMissingSince(ctx, windowStart, j.cfg.ArchiveFiles)
	if err != nil {
		return fmt.Errorf("add sets for recent model files: %w", err)
	}
	requeued, err := j.queue.RequeueForNewEntries(ctx, windowStart, started)
	if err != nil {
		return fmt.Errorf("queue model files whose models changed entries: %w", err)
	}
	fill, fillWaiting := int64(0), false
	if j.cfg.Fill {
		fill, fillWaiting, err = j.fill(ctx)
		if err != nil {
			return fmt.Errorf("add sets for stored model files: %w", err)
		}
	}
	j.logger.Info("coordinate index job started",
		"recent_sets_added", recent,
		"new_entry_sets_requeued", requeued,
		"fill_sets_added", fill,
		"fill_waiting_for_pending", fillWaiting)
	summary := runSummary{outcomes: map[outcome]int{}}
	for (j.cfg.MaxFilesPerRun == 0 || summary.files < j.cfg.MaxFilesPerRun) && j.now().Before(claimUntil) {
		claims, err := j.queue.ClaimDue(ctx, j.now(), 1, j.cfg.FileTimeout+leaseMargin, maxAttempts)
		if err != nil {
			return fmt.Errorf("claim a due model file: %w", err)
		}
		if len(claims) == 0 {
			break
		}
		result, err := j.readClaim(ctx, claims[0])
		if err != nil {
			return fmt.Errorf("read model file of artifact %s: %w", claims[0].ArtifactID, err)
		}
		summary.files++
		summary.outcomes[result]++
		if result == outcomeStopped {
			break
		}
	}
	j.logger.Info("coordinate index job finished",
		"files", summary.files,
		"done", summary.outcomes[outcomeDone],
		"retry_later", summary.outcomes[outcomeRetryLater],
		"failed", summary.outcomes[outcomeFailed],
		"duration", j.now().Sub(started).String())
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("coordinate index run stopped: %w", err)
	}
	return nil
}

// fill gives a pending fill set to every model file that has none, and returns how many it added.
// While sets an earlier fill queued are still pending it adds nothing and reports waiting: the
// queue holds about 225,000 fill sets after the first fill run, which the job reads over a day or
// more, and scanning every artifact again each run would find none to add.
//
// It walks pages of FillBatchSize artifacts in id order, from the first, until a page is short.
// After each full page it pauses as long as that page took, so the fill keeps the database at
// most about half busy, as the checksum job does. Each page commits on its own.
func (j *CoordinateIndexJob) fill(ctx context.Context) (int64, bool, error) {
	waiting, err := j.queue.HasPendingFill(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("check pending fill sets: %w", err)
	}
	if waiting {
		return 0, true, nil
	}
	var added int64
	// uuid.Nil sorts before every real artifact id, so the first page starts at the beginning.
	after := uuid.Nil
	for {
		pageStarted := j.now()
		page, err := j.queue.AddFillAfter(ctx, after, j.cfg.FillBatchSize, j.cfg.ArchiveFiles)
		if err != nil {
			return added, false, fmt.Errorf("page after artifact %s: %w", after, err)
		}
		added += page.Added
		if page.Scanned < j.cfg.FillBatchSize {
			return added, false, nil
		}
		after = page.LastArtifactID
		if err := j.sleep(ctx, j.now().Sub(pageStarted)); err != nil {
			return added, false, fmt.Errorf("pause after artifact %s: %w", after, err)
		}
	}
}

// readClaim reads one claimed file and records the outcome. It returns an error only when
// recording fails.
func (j *CoordinateIndexJob) readClaim(ctx context.Context, claim models.CoordinateIndexClaim) (outcome, error) {
	started := j.now()
	read, from, readErr := j.read(ctx, claim)
	if ctx.Err() != nil {
		return outcomeStopped, nil
	}
	if readErr != nil {
		return j.recordFailure(ctx, claim, readErr)
	}
	saved, err := j.queue.Save(ctx, claim.ArtifactID, read, j.now())
	if err != nil {
		if ctx.Err() != nil {
			return outcomeStopped, nil
		}
		// A save the database refuses counts as a failed attempt, so it is retried and then
		// failed rather than claimed forever; only a failure to record that stops the run.
		return j.recordFailure(ctx, claim, fmt.Errorf("save the chains: %w", err))
	}
	proteinChains, withLength := 0, 0
	for _, chain := range saved.Chains {
		if chain.Protein {
			proteinChains++
		}
		if chain.DepositedLength != nil {
			withLength++
		}
	}
	j.logger.Info("model file read",
		"artifact_id", claim.ArtifactID,
		"read_from", from,
		"chains", len(saved.Chains),
		"protein_chains", proteinChains,
		"chains_with_length", withLength,
		"entries", saved.Entries,
		"warnings", len(saved.Warnings),
		"duration", j.now().Sub(started).String())
	return outcomeDone, nil
}

// recordFailure records a failed attempt: retried after a doubling delay, or failed for good after
// maxAttempts or when no retry can change the outcome.
func (j *CoordinateIndexJob) recordFailure(ctx context.Context, claim models.CoordinateIndexClaim, readErr error) (outcome, error) {
	var retryAt *time.Time
	result := outcomeFailed
	final := modelfiles.IsPermanent(readErr) || errors.Is(readErr, cif.ErrCoordinateIndexLimit)
	if !final && claim.Attempts < maxAttempts {
		next := j.now().Add(firstRetryDelay << (claim.Attempts - 1))
		retryAt = &next
		result = outcomeRetryLater
	}
	if err := j.queue.RecordFailure(ctx, claim.ArtifactID, readErr.Error(), retryAt); err != nil {
		return result, fmt.Errorf("record the failure: %w", err)
	}
	j.logger.Warn("model file not read",
		"artifact_id", claim.ArtifactID,
		"attempt", claim.Attempts,
		"retry_at", retryAt,
		"err", readErr)
	return result, nil
}

// read opens a claimed file in our bucket, or our copy of it, and reads its chains.
func (j *CoordinateIndexJob) read(ctx context.Context, claim models.CoordinateIndexClaim) (cif.CoordinateIndex, modelfiles.ReadFrom, error) {
	key, from, err := modelfiles.StoredKey(j.files, modelfiles.Location{
		URI:        claim.URI,
		CopyStatus: claim.CopyStatus,
		StoredURI:  claim.StoredURI,
	})
	if err != nil {
		return cif.CoordinateIndex{}, "", fmt.Errorf("locate the file: %w", err)
	}
	fileCtx, cancel := context.WithTimeout(ctx, j.cfg.FileTimeout)
	defer cancel()
	file, err := j.files.Open(fileCtx, key)
	if err != nil {
		return cif.CoordinateIndex{}, from, fmt.Errorf("open the file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			j.logger.Warn("close model file failed", "artifact_id", claim.ArtifactID, "err", closeErr)
		}
	}()
	body, format, err := modelfiles.OpenStructure(file, claim.Format)
	if err != nil {
		return cif.CoordinateIndex{}, from, fmt.Errorf("open the structure: %w", err)
	}
	index, err := cif.ReadCoordinateIndex(fileCtx, body, format)
	if err != nil {
		return cif.CoordinateIndex{}, from, fmt.Errorf("read the file: %w", err)
	}
	return index, from, nil
}
