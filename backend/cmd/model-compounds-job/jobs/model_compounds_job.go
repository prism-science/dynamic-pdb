package jobs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

const (
	// maxAttempts is when a file that keeps failing to read is marked failed.
	maxAttempts = 3
	// firstRetryDelay doubles after each failed attempt: 10 min, then 20 min.
	firstRetryDelay = 10 * time.Minute
	// leaseMargin is how long a claim outlives the file timeout, so a file still being read
	// is never claimed by the next run.
	leaseMargin = 5 * time.Minute
	// sniffBytes is how much of a file without a declared format is looked at to tell
	// mmCIF from PDB format.
	sniffBytes     = 4096
	readBufferSize = 64 * 1024
)

// Queue is the modeled_compound_sets table as the job uses it.
type Queue interface {
	// AddMissingSince and AddMissingAfter give pending sets to model files in our bucket that
	// have none: those created since a time, or the next page of all artifacts in id order.
	// With archiveFiles, AddMissingSince also takes recent external model files the checksum
	// job copied into our bucket.
	AddMissingSince(ctx context.Context, since time.Time, archiveFiles bool) (int64, error)
	AddMissingAfter(ctx context.Context, after uuid.UUID, limit int) (models.MissingSetsPage, error)
	// AddCopiedAfter gives pending sets to at most room copied external model files without
	// one in the next page of artifacts in id order.
	AddCopiedAfter(ctx context.Context, after uuid.UUID, limit int, room int64) (models.MissingSetsPage, error)
	// PendingCount is how many sets are pending.
	PendingCount(ctx context.Context) (int64, error)
	// ClaimDue fails, instead of claiming, a set already claimed maxAttempts times.
	ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration, maxAttempts int) ([]models.ModeledCompoundClaim, error)
	// RecordFailure keeps the set pending until retryAt, or fails it for good when retryAt is nil.
	RecordFailure(ctx context.Context, artifactID uuid.UUID, message string, retryAt *time.Time) error
	// Save stores the copies, finishes the set and re-indexes the revisions with the file; it
	// returns how many revisions it re-indexed.
	Save(ctx context.Context, artifactID uuid.UUID, result cif.ModeledCompounds, readAt time.Time) (int, error)
}

// StoredFiles is our bucket: stored files are read by key.
type StoredFiles interface {
	StoredKey(uri string) (string, bool)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
}

type Config struct {
	// MaxFilesPerRun caps the files one run reads; 0 means no cap.
	MaxFilesPerRun int `mapstructure:"max_files_per_run"`
	// ClaimFor is how long after its start a run keeps claiming files.
	ClaimFor time.Duration `mapstructure:"claim_for"`
	// FileTimeout bounds the read of one file, from opening it to its last line.
	FileTimeout time.Duration `mapstructure:"file_timeout"`
	// RecentWindow is how far back each run looks for new model files without a set: an
	// upload an older backend stored while a deploy was rolling out.
	RecentWindow time.Duration `mapstructure:"recent_window"`
	// HistoricalFill turns on the paced fill over every artifact, which gives the model
	// files stored before this job existed their sets. Off until the rollout is watched.
	HistoricalFill bool `mapstructure:"historical_fill"`
	// FillBatchSize is how many artifacts one statement of a fill looks at.
	FillBatchSize int `mapstructure:"fill_batch_size"`
	// ArchiveFiles lets the recent window take archive model files (on RCSB) once the checksum
	// job has copied them into our bucket; the job reads the copy.
	ArchiveFiles bool `mapstructure:"archive_files"`
	// ArchiveFill gives the archive model files copied before ArchiveFiles was on their sets: each
	// run tops the queue up to MaxFilesPerRun with them. On only until a run adds none.
	ArchiveFill bool `mapstructure:"archive_fill"`
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
	case c.ArchiveFill && !c.ArchiveFiles:
		return errors.New("archive_fill needs archive_files: without it, archive files copied after the fill would get no set")
	case c.ArchiveFill && c.MaxFilesPerRun == 0:
		return errors.New("archive_fill needs a max_files_per_run: it tops the queue up to that many files")
	}
	return nil
}

// ModelCompoundsJob reads every model file in our bucket that has a pending set and stores the
// compounds it models (cif.ReadModeledCompounds), one file at a time.
type ModelCompoundsJob struct {
	queue  Queue
	files  StoredFiles
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
	sleep  func(ctx context.Context, pause time.Duration) error
}

func NewModelCompoundsJob(queue Queue, files StoredFiles, cfg Config, logger *slog.Logger) (*ModelCompoundsJob, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("model compounds job config: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ModelCompoundsJob{
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

// Run gives pending sets to the model files that lack one, then claims and reads due files
// until nothing is due, the run's claiming time is over, or MaxFilesPerRun is reached. It
// stops with an error only when the database fails or ctx ends.
func (j *ModelCompoundsJob) Run(ctx context.Context) error {
	started := j.now()
	claimUntil := started.Add(j.cfg.ClaimFor)
	recent, err := j.queue.AddMissingSince(ctx, started.Add(-j.cfg.RecentWindow), j.cfg.ArchiveFiles)
	if err != nil {
		return fmt.Errorf("add sets for recent model files: %w", err)
	}
	historical := int64(0)
	if j.cfg.HistoricalFill {
		historical, err = j.fillHistorical(ctx)
		if err != nil {
			return fmt.Errorf("add sets for stored model files: %w", err)
		}
	}
	archive := int64(0)
	if j.cfg.ArchiveFill {
		archive, err = j.fillArchive(ctx)
		if err != nil {
			return fmt.Errorf("add sets for copied archive model files: %w", err)
		}
	}
	j.logger.Info("model compounds job started",
		"recent_sets_added", recent,
		"historical_sets_added", historical,
		"archive_sets_added", archive)
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
	j.logger.Info("model compounds job finished",
		"files", summary.files,
		"done", summary.outcomes[outcomeDone],
		"retry_later", summary.outcomes[outcomeRetryLater],
		"failed", summary.outcomes[outcomeFailed],
		"duration", j.now().Sub(started).String())
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("model compounds run stopped: %w", err)
	}
	return nil
}

// fillHistorical gives a pending set to every model file in our bucket that has none and
// returns how many it added.
func (j *ModelCompoundsJob) fillHistorical(ctx context.Context) (int64, error) {
	addPage := func(after uuid.UUID, _ int64) (models.MissingSetsPage, error) {
		return j.queue.AddMissingAfter(ctx, after, j.cfg.FillBatchSize)
	}
	return j.fillPages(ctx, addPage, noLimit)
}

// fillArchive tops the queue up to MaxFilesPerRun with archive model files the checksum job
// copied into our bucket and that have no set, and returns how many it added. The queue
// never holds much more than one run's reading, so an upload saved during the fill waits
// about one run at most, and an archive page shows "reading" for about as long. Each run
// starts again from the first artifact; pages already filled add nothing.
func (j *ModelCompoundsJob) fillArchive(ctx context.Context) (int64, error) {
	pending, err := j.queue.PendingCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("count pending sets: %w", err)
	}
	room := int64(j.cfg.MaxFilesPerRun) - pending
	if room <= 0 {
		return 0, nil
	}
	addPage := func(after uuid.UUID, roomLeft int64) (models.MissingSetsPage, error) {
		return j.queue.AddCopiedAfter(ctx, after, j.cfg.FillBatchSize, roomLeft)
	}
	return j.fillPages(ctx, addPage, room)
}

// noLimit is the room of a fill that adds every set it finds.
const noLimit int64 = -1

// fillPages calls addPage on pages of FillBatchSize artifacts in id order, from the first,
// until a page is short or room sets were added (noLimit: never), and returns how many sets
// it added. addPage gets the room left, which noLimit fills ignore. After each full page it
// pauses as long as that page took, so a fill keeps the database at most about half busy, as
// the checksum job does. Each page commits on its own.
func (j *ModelCompoundsJob) fillPages(
	ctx context.Context,
	addPage func(after uuid.UUID, roomLeft int64) (models.MissingSetsPage, error),
	room int64,
) (int64, error) {
	var added int64
	// uuid.Nil sorts before every real artifact id, so the first page starts at the beginning.
	after := uuid.Nil
	for {
		pageStarted := j.now()
		page, err := addPage(after, room-added)
		if err != nil {
			return added, fmt.Errorf("page after artifact %s: %w", after, err)
		}
		added += page.Added
		if page.Scanned < j.cfg.FillBatchSize || (room != noLimit && added >= room) {
			return added, nil
		}
		after = page.LastArtifactID
		if err := j.sleep(ctx, j.now().Sub(pageStarted)); err != nil {
			return added, fmt.Errorf("pause after artifact %s: %w", after, err)
		}
	}
}

// permanentError is a reason no retry can change: the file is not ours, or not a structure file.
type permanentError struct {
	reason string
}

func (e *permanentError) Error() string {
	return e.reason
}

func permanent(format string, arguments ...any) error {
	return &permanentError{reason: fmt.Sprintf(format, arguments...)}
}

// readClaim reads one claimed file and records the outcome. It returns an error only when
// recording fails.
func (j *ModelCompoundsJob) readClaim(ctx context.Context, claim models.ModeledCompoundClaim) (outcome, error) {
	started := j.now()
	result, from, readErr := j.read(ctx, claim)
	if ctx.Err() != nil {
		return outcomeStopped, nil
	}
	if readErr != nil {
		return j.recordFailure(ctx, claim, readErr)
	}
	reindexed, err := j.queue.Save(ctx, claim.ArtifactID, result, j.now())
	if err != nil {
		if ctx.Err() != nil {
			return outcomeStopped, nil
		}
		// A save the database refuses counts as a failed attempt, so it is retried and then
		// failed rather than claimed forever; only a failure to record that stops the run.
		return j.recordFailure(ctx, claim, fmt.Errorf("save the compounds: %w", err))
	}
	review := 0
	for _, copied := range result.Copies {
		if copied.NeedsReview {
			review++
		}
	}
	j.logger.Info("model file read",
		"artifact_id", claim.ArtifactID,
		"read_from", from,
		"copies", len(result.Copies),
		"needs_review", review,
		"complete", result.Complete,
		"warnings", len(result.Warnings),
		"reindexed_revisions", reindexed,
		"duration", j.now().Sub(started).String())
	return outcomeDone, nil
}

func (j *ModelCompoundsJob) recordFailure(ctx context.Context, claim models.ModeledCompoundClaim, readErr error) (outcome, error) {
	var final *permanentError
	var retryAt *time.Time
	result := outcomeFailed
	if !errors.As(readErr, &final) && claim.Attempts < maxAttempts {
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

// readFrom names the object a file was read from: the artifact's own file in our bucket, or
// the checksum job's copy of an external file (an archive model file on RCSB).
type readFrom string

const (
	readFromOwnFile      readFrom = "own_file"
	readFromChecksumCopy readFrom = "checksum_copy"
)

// storedObject returns the bucket key a claimed file is read from: the artifact's own file
// when it is in our bucket, otherwise the checksum job's copy of the external file.
func (j *ModelCompoundsJob) storedObject(claim models.ModeledCompoundClaim) (string, readFrom, error) {
	if claim.URI == nil || strings.TrimSpace(*claim.URI) == "" {
		return "", "", permanent("artifact has no uri")
	}
	if key, stored := j.files.StoredKey(*claim.URI); stored {
		return key, readFromOwnFile, nil
	}
	if claim.CopyStatus == nil {
		return "", "", permanent("the file is not in our bucket, and the checksum job has not copied it")
	}
	switch *claim.CopyStatus {
	case models.CopyStatusCopied:
		if claim.StoredURI == nil {
			return "", "", permanent("the checksum job copied the file but recorded no uri for the copy")
		}
		key, stored := j.files.StoredKey(*claim.StoredURI)
		if !stored {
			return "", "", permanent("the checksum job's copy of the file is not in our bucket")
		}
		return key, readFromChecksumCopy, nil
	case models.CopyStatusOwnStorage,
		models.CopyStatusOverLimit,
		models.CopyStatusMismatch,
		models.CopyStatusCopyFailed,
		models.CopyStatusCopyOff:
		return "", "", permanent("the file is not in our bucket, and the checksum job kept no copy of it (copy status %s)",
			*claim.CopyStatus)
	default:
		return "", "", fmt.Errorf("unknown copy status %q", *claim.CopyStatus)
	}
}

// read opens a claimed file in our bucket, or our copy of it, and reads the compounds it
// models.
func (j *ModelCompoundsJob) read(ctx context.Context, claim models.ModeledCompoundClaim) (cif.ModeledCompounds, readFrom, error) {
	key, from, err := j.storedObject(claim)
	if err != nil {
		return cif.ModeledCompounds{}, "", fmt.Errorf("locate the file: %w", err)
	}
	fileCtx, cancel := context.WithTimeout(ctx, j.cfg.FileTimeout)
	defer cancel()
	file, err := j.files.Open(fileCtx, key)
	if err != nil {
		return cif.ModeledCompounds{}, from, fmt.Errorf("open the file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			j.logger.Warn("close model file failed", "artifact_id", claim.ArtifactID, "err", closeErr)
		}
	}()
	body, format, err := openStructure(file, claim.Format)
	if err != nil {
		return cif.ModeledCompounds{}, from, fmt.Errorf("open the structure: %w", err)
	}
	result, err := cif.ReadModeledCompounds(fileCtx, body, format)
	if err != nil {
		return cif.ModeledCompounds{}, from, fmt.Errorf("read the file: %w", err)
	}
	return result, from, nil
}

// openStructure returns the structure text of a file, gunzipped when its first bytes are a
// gzip header, and its format: the declared one, or for a file without one, what its first
// bytes show. A declared format that is not a structure format fails for good.
func openStructure(file io.Reader, declared *string) (io.Reader, cif.StructureFormat, error) {
	buffered := bufio.NewReaderSize(file, readBufferSize)
	body := io.Reader(buffered)
	if magic, err := buffered.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		unzipped, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, 0, permanent("the file has a gzip header but cannot be unzipped: %v", err)
		}
		body = bufio.NewReaderSize(unzipped, readBufferSize)
	}
	if declared != nil && strings.TrimSpace(*declared) != "" {
		format, err := declaredFormat(*declared)
		if err != nil {
			return nil, 0, fmt.Errorf("declared format: %w", err)
		}
		return body, format, nil
	}
	peeker, ok := body.(*bufio.Reader)
	if !ok {
		return nil, 0, errors.New("the file cannot be looked at before it is read")
	}
	head, err := peeker.Peek(sniffBytes)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, 0, fmt.Errorf("look at the start of the file: %w", err)
	}
	format, err := sniffFormat(head)
	if err != nil {
		return nil, 0, fmt.Errorf("first bytes: %w", err)
	}
	return body, format, nil
}

// declaredFormat reads an artifact's format as the website and the CLI write it.
func declaredFormat(value string) (cif.StructureFormat, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cif", "mmcif":
		return cif.FormatMMCIF, nil
	case "pdb", "ent":
		return cif.FormatPDB, nil
	default:
		return 0, permanent("unknown model file format %q", value)
	}
}

// pdbRecords are record names a PDB-format file starts with.
var pdbRecords = []string{"HEADER", "TITLE ", "COMPND", "REMARK", "CRYST1", "ATOM  ", "HETATM", "MODEL ", "SEQRES", "EXPDTA", "AUTHOR"}

// sniffFormat tells mmCIF (a data_ block header first) from PDB format (a known record name
// first), skipping blank lines and, in mmCIF, comment lines.
func sniffFormat(head []byte) (cif.StructureFormat, error) {
	for _, line := range bytes.Split(head, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		if len(trimmed) >= 5 && strings.EqualFold(string(trimmed[:5]), "data_") {
			return cif.FormatMMCIF, nil
		}
		for _, record := range pdbRecords {
			if bytes.HasPrefix(line, []byte(record)) {
				return cif.FormatPDB, nil
			}
		}
		return 0, permanent("the file starts like neither mmCIF nor PDB format")
	}
	return 0, permanent("the file is empty")
}
