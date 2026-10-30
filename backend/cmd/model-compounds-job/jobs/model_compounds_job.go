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
	ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]models.ModeledCompoundClaim, error)
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
}

func (c Config) validate() error {
	switch {
	case c.MaxFilesPerRun < 0:
		return errors.New("max_files_per_run must not be negative")
	case c.ClaimFor <= 0:
		return errors.New("claim_for must be positive")
	case c.FileTimeout <= 0:
		return errors.New("file_timeout must be positive")
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
	}, nil
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

// Run claims and reads due files until nothing is due, the run's claiming time is over, or
// MaxFilesPerRun is reached. It stops with an error only when the database fails or ctx ends.
func (j *ModelCompoundsJob) Run(ctx context.Context) error {
	started := j.now()
	claimUntil := started.Add(j.cfg.ClaimFor)
	summary := runSummary{outcomes: map[outcome]int{}}
	for (j.cfg.MaxFilesPerRun == 0 || summary.files < j.cfg.MaxFilesPerRun) && j.now().Before(claimUntil) {
		claims, err := j.queue.ClaimDue(ctx, j.now(), 1, j.cfg.FileTimeout+leaseMargin)
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
	result, readErr := j.read(ctx, claim)
	if ctx.Err() != nil {
		return outcomeStopped, nil
	}
	if readErr != nil {
		return j.recordFailure(ctx, claim, readErr)
	}
	reindexed, err := j.queue.Save(ctx, claim.ArtifactID, result, j.now())
	if err != nil {
		return outcomeDone, fmt.Errorf("save the compounds: %w", err)
	}
	review := 0
	for _, copied := range result.Copies {
		if copied.NeedsReview {
			review++
		}
	}
	j.logger.Info("model file read",
		"artifact_id", claim.ArtifactID,
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

// read opens a claimed file in our bucket and reads the compounds it models.
func (j *ModelCompoundsJob) read(ctx context.Context, claim models.ModeledCompoundClaim) (cif.ModeledCompounds, error) {
	if claim.URI == nil || strings.TrimSpace(*claim.URI) == "" {
		return cif.ModeledCompounds{}, permanent("artifact has no uri")
	}
	key, stored := j.files.StoredKey(*claim.URI)
	if !stored {
		return cif.ModeledCompounds{}, permanent("the file is not in our bucket")
	}
	fileCtx, cancel := context.WithTimeout(ctx, j.cfg.FileTimeout)
	defer cancel()
	file, err := j.files.Open(fileCtx, key)
	if err != nil {
		return cif.ModeledCompounds{}, fmt.Errorf("open the file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			j.logger.Warn("close model file failed", "artifact_id", claim.ArtifactID, "err", closeErr)
		}
	}()
	body, format, err := openStructure(file, claim.Format)
	if err != nil {
		return cif.ModeledCompounds{}, err
	}
	result, err := cif.ReadModeledCompounds(fileCtx, body, format)
	if err != nil {
		return cif.ModeledCompounds{}, fmt.Errorf("read the file: %w", err)
	}
	return result, nil
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
			return nil, 0, err
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
		return nil, 0, err
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
