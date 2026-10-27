package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/cdn"
)

const (
	// lease is how long a claim keeps a row from other runs; a run is killed after 55 minutes.
	lease = time.Hour
	// maxAttempts is when a file that keeps failing to read is marked failed.
	maxAttempts = 5
	// firstRetryDelay doubles after each failed attempt: 1 h, 2 h, 4 h, 8 h.
	firstRetryDelay = time.Hour
	readBufferSize  = 256 * 1024
)

var errNoURI = errors.New("artifact has no uri")

// ChecksumQueue is the artifact_checksums table as the job uses it.
type ChecksumQueue interface {
	AddMissingAfter(ctx context.Context, after uuid.UUID, limit int) (models.MissingChecksumsPage, error)
	ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]models.ChecksumClaim, error)
	Locations(ctx context.Context, artifactIDs []uuid.UUID) (map[uuid.UUID]models.ArtifactLocation, error)
	// Record stores the results of one URI's artifacts together.
	Record(ctx context.Context, results []models.ChecksumResult) error
}

// ArtifactFiles is our bucket: stored files are read by key, and copies of external files go there.
type ArtifactFiles interface {
	StoredKey(uri string) (string, bool)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	CopyKey(file cdn.FileUpload) (string, error)
	NewWriter(ctx context.Context, key string) (cdn.ObjectWriter, error)
	URL(key string) (string, error)
}

// ExternalFiles reads files that live outside our bucket.
type ExternalFiles interface {
	Open(ctx context.Context, uri string) (io.ReadCloser, error)
}

type Config struct {
	Concurrency int `mapstructure:"concurrency"`
	// PerHostConcurrency caps reads from one external host, such as files.rcsb.org.
	PerHostConcurrency int `mapstructure:"per_host_concurrency"`
	// BatchSize is how many URIs one claim takes.
	BatchSize int `mapstructure:"batch_size"`
	// MaxFilesPerRun caps the URIs one run checks; 0 means no cap.
	MaxFilesPerRun int `mapstructure:"max_files_per_run"`
	// ClaimFor is how long after its start a run keeps claiming work.
	ClaimFor time.Duration `mapstructure:"claim_for"`
	// FileTimeout counts from when a file is opened, not while it waits for a host slot.
	FileTimeout  time.Duration `mapstructure:"file_timeout"`
	CopyExternal bool          `mapstructure:"copy_external"`
	// CopyLimitBytes is the largest external file that is copied; larger ones keep only their link.
	CopyLimitBytes int64 `mapstructure:"copy_limit_bytes"`
	// FillBatchSize is how many artifacts one statement looks at when a run adds missing rows.
	FillBatchSize int `mapstructure:"fill_batch_size"`
}

func (c Config) validate() error {
	switch {
	case c.Concurrency <= 0:
		return errors.New("concurrency must be positive")
	case c.PerHostConcurrency <= 0:
		return errors.New("per_host_concurrency must be positive")
	case c.BatchSize <= 0:
		return errors.New("batch_size must be positive")
	case c.MaxFilesPerRun < 0:
		return errors.New("max_files_per_run must not be negative")
	case c.ClaimFor <= 0:
		return errors.New("claim_for must be positive")
	case c.FileTimeout <= 0:
		return errors.New("file_timeout must be positive")
	case c.CopyLimitBytes <= 0:
		return errors.New("copy_limit_bytes must be positive")
	case c.FillBatchSize <= 0:
		return errors.New("fill_batch_size must be positive")
	}
	return nil
}

// ChecksumJob computes the sha256 and size of every artifact's bytes, compares them with
// what the uploader declared, and keeps a copy of each verified external file.
type ChecksumJob struct {
	queue    ChecksumQueue
	files    ArtifactFiles
	external ExternalFiles
	cfg      Config
	logger   *slog.Logger
	now      func() time.Time
	sleep    func(ctx context.Context, pause time.Duration) error

	hostSlotsMu sync.Mutex
	hostSlots   map[string]chan struct{}
}

func NewChecksumJob(
	queue ChecksumQueue,
	files ArtifactFiles,
	external ExternalFiles,
	cfg Config,
	logger *slog.Logger,
) (*ChecksumJob, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("checksum job config: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ChecksumJob{
		queue:     queue,
		files:     files,
		external:  external,
		cfg:       cfg,
		logger:    logger,
		now:       func() time.Time { return time.Now().UTC() },
		sleep:     sleepContext,
		hostSlots: map[string]chan struct{}{},
	}, nil
}

// sleepContext waits for pause, or returns the context's error if it ends first.
func sleepContext(ctx context.Context, pause time.Duration) error {
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// uriGroup is the claimed artifacts that share one URI; the URI is read once for all of them.
type uriGroup struct {
	uri    *string
	claims []models.ChecksumClaim
}

type runSummary struct {
	files    int
	statuses map[models.ChecksumStatus]int
}

// Run adds missing rows, then claims and checks due files until nothing is due, the run's
// claiming time is over, or MaxFilesPerRun is reached.
func (j *ChecksumJob) Run(ctx context.Context) error {
	started := j.now()
	claimUntil := started.Add(j.cfg.ClaimFor)
	added, err := j.addMissing(ctx)
	if err != nil {
		return fmt.Errorf("add missing checksum rows: %w", err)
	}
	j.logger.Info("checksum job started", "added_rows", added)

	summary := runSummary{statuses: map[models.ChecksumStatus]int{}}
	for {
		limit := j.cfg.BatchSize
		if j.cfg.MaxFilesPerRun > 0 {
			limit = min(limit, j.cfg.MaxFilesPerRun-summary.files)
		}
		if limit <= 0 || !j.now().Before(claimUntil) {
			break
		}
		claims, err := j.queue.ClaimDue(ctx, j.now(), limit, lease)
		if err != nil {
			return fmt.Errorf("claim due checksums: %w", err)
		}
		if len(claims) == 0 {
			break
		}
		groups := groupByURI(claims)
		if err := j.checkBatch(ctx, groups, &summary); err != nil {
			return fmt.Errorf("check claimed files: %w", err)
		}
		summary.files += len(groups)
	}
	j.logger.Info("checksum job finished",
		"files", summary.files,
		"verified", summary.statuses[models.ChecksumStatusVerified],
		"mismatch", summary.statuses[models.ChecksumStatusMismatch],
		"failed", summary.statuses[models.ChecksumStatusFailed],
		"retry_later", summary.statuses[models.ChecksumStatusPending],
		"duration", j.now().Sub(started).String())
	return nil
}

// addMissing gives every artifact without a row a pending one, FillBatchSize artifacts per
// statement, and returns how many rows it added. After each full page it pauses as long as
// that page took, so the fill keeps the database at most about half busy, however slow it
// is. Each page commits on its own, so a run that ends early keeps the rows it added.
func (j *ChecksumJob) addMissing(ctx context.Context) (int64, error) {
	var added int64
	// uuid.Nil sorts before every real artifact id, so the first page starts at the beginning.
	after := uuid.Nil
	for {
		pageStarted := j.now()
		page, err := j.queue.AddMissingAfter(ctx, after, j.cfg.FillBatchSize)
		if err != nil {
			return added, fmt.Errorf("page after artifact %s: %w", after, err)
		}
		added += page.Added
		if page.Scanned < j.cfg.FillBatchSize {
			return added, nil
		}
		after = page.LastArtifactID
		if err := j.sleep(ctx, j.now().Sub(pageStarted)); err != nil {
			return added, fmt.Errorf("pause after artifact %s: %w", after, err)
		}
	}
}

func groupByURI(claims []models.ChecksumClaim) []uriGroup {
	groups := make([]uriGroup, 0, len(claims))
	byURI := map[string]int{}
	for _, claim := range claims {
		if claim.URI == nil {
			groups = append(groups, uriGroup{claims: []models.ChecksumClaim{claim}})
			continue
		}
		if index, ok := byURI[*claim.URI]; ok {
			groups[index].claims = append(groups[index].claims, claim)
			continue
		}
		byURI[*claim.URI] = len(groups)
		groups = append(groups, uriGroup{uri: claim.URI, claims: []models.ChecksumClaim{claim}})
	}
	return groups
}

// checkBatch checks the groups Concurrency at a time and records each group's results as soon
// as they are known. After all groups it returns the errors of recording results, or the run's
// cancellation.
func (j *ChecksumJob) checkBatch(ctx context.Context, groups []uriGroup, summary *runSummary) error {
	locations, err := j.copyLocations(ctx, groups)
	if err != nil {
		return fmt.Errorf("find copy folders: %w", err)
	}
	var (
		mu         sync.Mutex
		recordErrs error
		wait       sync.WaitGroup
	)
	slots := make(chan struct{}, j.cfg.Concurrency)
	for _, group := range groups {
		slots <- struct{}{}
		wait.Add(1)
		go func() {
			defer wait.Done()
			defer func() { <-slots }()
			results := j.checkGroup(ctx, group, locations)
			if results == nil {
				return
			}
			recordErr := j.queue.Record(ctx, results)
			mu.Lock()
			defer mu.Unlock()
			if recordErr != nil {
				recordErrs = errors.Join(recordErrs, fmt.Errorf("record checksums: %w", recordErr))
				return
			}
			for _, result := range results {
				summary.statuses[result.Status]++
			}
		}()
	}
	wait.Wait()
	if recordErrs != nil {
		return recordErrs
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checksum run stopped: %w", err)
	}
	return nil
}

// copyLocations reads where each external file's artifacts live, so a copy can go into the
// folder an upload of that artifact would use.
func (j *ChecksumJob) copyLocations(ctx context.Context, groups []uriGroup) (map[uuid.UUID]models.ArtifactLocation, error) {
	if !j.cfg.CopyExternal {
		return map[uuid.UUID]models.ArtifactLocation{}, nil
	}
	ids := make([]uuid.UUID, 0)
	for _, group := range groups {
		if group.uri == nil {
			continue
		}
		if _, stored := j.files.StoredKey(*group.uri); stored {
			continue
		}
		for _, claim := range group.claims {
			ids = append(ids, claim.ArtifactID)
		}
	}
	locations, err := j.queue.Locations(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read artifact locations: %w", err)
	}
	return locations, nil
}

// checkGroup reads one URI and returns the results of its artifacts. It returns nil when the
// run was cancelled, so the rows stay claimed and are checked again after the lease.
func (j *ChecksumJob) checkGroup(
	ctx context.Context,
	group uriGroup,
	locations map[uuid.UUID]models.ArtifactLocation,
) []models.ChecksumResult {
	digest, err := j.readGroup(ctx, group, locations)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return j.unreadResults(group, err)
	}
	return j.readResults(group, digest)
}

// fileDigest is what one read of a URI found.
type fileDigest struct {
	sha256    string
	sizeBytes int64
	storedURI *string
}

func (j *ChecksumJob) readGroup(
	ctx context.Context,
	group uriGroup,
	locations map[uuid.UUID]models.ArtifactLocation,
) (fileDigest, error) {
	if group.uri == nil {
		return fileDigest{}, fmt.Errorf("%w: %w", errNoURI, errNotRetryable)
	}
	uri := *group.uri
	if key, stored := j.files.StoredKey(uri); stored {
		return j.timedRead(ctx, func(fileCtx context.Context) (fileDigest, error) {
			reader, err := j.files.Open(fileCtx, key)
			if errors.Is(err, cdn.ErrObjectNotFound) {
				return fileDigest{}, fmt.Errorf("%w: %w", err, errNotRetryable)
			}
			if err != nil {
				return fileDigest{}, fmt.Errorf("open stored file: %w", err)
			}
			return j.hashAndCopy(group, reader, nil)
		})
	}

	release, err := j.acquireHost(ctx, uri)
	if err != nil {
		return fileDigest{}, fmt.Errorf("read external file: %w", err)
	}
	defer release()
	return j.timedRead(ctx, func(fileCtx context.Context) (fileDigest, error) {
		reader, err := j.external.Open(fileCtx, uri)
		if err != nil {
			return fileDigest{}, fmt.Errorf("open external file: %w", err)
		}
		return j.hashAndCopy(group, reader, j.startCopy(fileCtx, group, locations))
	})
}

// timedRead gives read FileTimeout, counted from when the file is opened, and says so in the
// error when the time ran out.
func (j *ChecksumJob) timedRead(
	ctx context.Context,
	read func(fileCtx context.Context) (fileDigest, error),
) (fileDigest, error) {
	fileCtx, cancel := context.WithTimeout(ctx, j.cfg.FileTimeout)
	defer cancel()
	digest, err := read(fileCtx)
	if err != nil && ctx.Err() == nil && errors.Is(fileCtx.Err(), context.DeadlineExceeded) {
		return fileDigest{}, fmt.Errorf("read took longer than %s: %w", j.cfg.FileTimeout, err)
	}
	return digest, err
}

// fileCopy is an external file being stored in our bucket while it is hashed.
type fileCopy struct {
	key    string
	writer cdn.ObjectWriter
}

// startCopy opens the copy of an external file in the folder of the group's first artifact
// that has a location. It returns nil, and the file is only hashed, when copies are off or
// no copy can be started.
func (j *ChecksumJob) startCopy(
	ctx context.Context,
	group uriGroup,
	locations map[uuid.UUID]models.ArtifactLocation,
) *fileCopy {
	if !j.cfg.CopyExternal {
		return nil
	}
	claims := slices.Clone(group.claims)
	slices.SortFunc(claims, func(a, b models.ChecksumClaim) int {
		return strings.Compare(a.ArtifactID.String(), b.ArtifactID.String())
	})
	for _, claim := range claims {
		location, ok := locations[claim.ArtifactID]
		if !ok {
			continue
		}
		modelID := ""
		if location.ModelID != nil {
			modelID = *location.ModelID
		}
		key, err := j.files.CopyKey(cdn.FileUpload{
			EntryID:          location.EntryID,
			ModelID:          modelID,
			ArtifactID:       claim.ArtifactID.String(),
			OriginalFilename: claim.Name,
		})
		if err != nil {
			j.logger.Warn("external file not copied: no copy key", "artifact_id", claim.ArtifactID, "err", err)
			return nil
		}
		writer, err := j.files.NewWriter(ctx, key)
		if err != nil {
			j.logger.Warn("external file not copied: upload did not start", "artifact_id", claim.ArtifactID, "err", err)
			return nil
		}
		return &fileCopy{key: key, writer: writer}
	}
	j.logger.Warn("external file not copied: none of its artifacts belongs to an entry", "uri", *group.uri)
	return nil
}

// hashAndCopy streams the file through sha256 and, while it stays within CopyLimitBytes, into
// the copy. Only a failed read is an error; a copy that cannot be kept leaves storedURI empty.
func (j *ChecksumJob) hashAndCopy(group uriGroup, reader io.ReadCloser, target *fileCopy) (fileDigest, error) {
	hasher := sha256.New()
	var size int64
	buffer := make([]byte, readBufferSize)
	readErr := func() error {
		for {
			count, err := reader.Read(buffer)
			if count > 0 {
				if _, hashErr := hasher.Write(buffer[:count]); hashErr != nil {
					return fmt.Errorf("hash file: %w", hashErr)
				}
				size += int64(count)
				if target != nil {
					target = j.writeCopy(target, buffer[:count], size, *group.uri)
				}
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
		}
	}()
	readErr = errors.Join(readErr, reader.Close())
	if readErr != nil {
		if target != nil {
			if abortErr := target.writer.Abort(); abortErr != nil {
				j.logger.Warn("discard copy of unread file", "uri", *group.uri, "err", abortErr)
			}
		}
		return fileDigest{}, readErr
	}

	digest := fileDigest{sha256: hex.EncodeToString(hasher.Sum(nil)), sizeBytes: size}
	if target != nil {
		digest.storedURI = j.finishCopy(target, anyMatches(group.claims, digest), *group.uri)
	}
	return digest, nil
}

// finishCopy stores the copy when keep is set and returns its URL. It returns nil, and the
// copy is discarded, when keep is not set or storing fails.
func (j *ChecksumJob) finishCopy(target *fileCopy, keep bool, uri string) *string {
	if !keep {
		if err := target.writer.Abort(); err != nil {
			j.logger.Warn("discard copy of mismatched file", "uri", uri, "err", err)
		}
		return nil
	}
	if err := target.writer.Complete(); err != nil {
		j.logger.Warn("copy of external file not stored, keeping only its link", "uri", uri, "err", err)
		return nil
	}
	storedURI, err := j.files.URL(target.key)
	if err != nil {
		j.logger.Warn("copy stored but its url cannot be built", "uri", uri, "key", target.key, "err", err)
		return nil
	}
	return &storedURI
}

// writeCopy adds data to the copy and returns the copy, or nil once the copy was given up:
// the file passed CopyLimitBytes or the bucket refused the bytes. The hash is unaffected.
func (j *ChecksumJob) writeCopy(target *fileCopy, data []byte, sizeSoFar int64, uri string) *fileCopy {
	if sizeSoFar > j.cfg.CopyLimitBytes {
		if err := target.writer.Abort(); err != nil {
			j.logger.Warn("abort copy over the copy limit", "uri", uri, "err", err)
		}
		j.logger.Info("external file over the copy limit keeps only its link", "uri", uri, "limit_bytes", j.cfg.CopyLimitBytes)
		return nil
	}
	if _, err := target.writer.Write(data); err != nil {
		j.logger.Warn("copy of external file failed, keeping only its link this time", "uri", uri, "err", err)
		if abortErr := target.writer.Abort(); abortErr != nil {
			j.logger.Warn("abort failed copy", "uri", uri, "err", abortErr)
		}
		return nil
	}
	return target
}

func anyMatches(claims []models.ChecksumClaim, digest fileDigest) bool {
	for _, claim := range claims {
		if matchesDeclared(claim, digest) {
			return true
		}
	}
	return false
}

// matchesDeclared is true when every value the uploader declared equals the bytes. A claim
// that declared nothing matches.
func matchesDeclared(claim models.ChecksumClaim, digest fileDigest) bool {
	if claim.SHA256 != nil && strings.ToLower(strings.TrimSpace(*claim.SHA256)) != digest.sha256 {
		return false
	}
	if claim.SizeBytes != nil && *claim.SizeBytes != digest.sizeBytes {
		return false
	}
	return true
}

// declaredValue is what a log line shows for an optional declared value: the value, or
// "none" when the uploader declared nothing. Logging the pointer itself prints its address.
func declaredValue[T any](value *T) any {
	if value == nil {
		return "none"
	}
	return *value
}

func (j *ChecksumJob) readResults(group uriGroup, digest fileDigest) []models.ChecksumResult {
	checkedAt := j.now()
	results := make([]models.ChecksumResult, 0, len(group.claims))
	for _, claim := range group.claims {
		result := models.ChecksumResult{
			ArtifactID: claim.ArtifactID,
			Status:     models.ChecksumStatusVerified,
			SHA256:     &digest.sha256,
			SizeBytes:  &digest.sizeBytes,
			StoredURI:  digest.storedURI,
			CheckedAt:  checkedAt,
		}
		if !matchesDeclared(claim, digest) {
			result.Status = models.ChecksumStatusMismatch
			result.StoredURI = nil
			j.logger.Warn("artifact bytes differ from the declared values",
				"artifact_id", claim.ArtifactID,
				"uri", *group.uri,
				"declared_sha256", declaredValue(claim.SHA256),
				"computed_sha256", digest.sha256,
				"declared_size_bytes", declaredValue(claim.SizeBytes),
				"computed_size_bytes", digest.sizeBytes)
		}
		results = append(results, result)
	}
	return results
}

// unreadResults marks a file that could not be read: failed at once when it will not change,
// otherwise retried later until the last attempt.
func (j *ChecksumJob) unreadResults(group uriGroup, readErr error) []models.ChecksumResult {
	now := j.now()
	message := readErr.Error()
	results := make([]models.ChecksumResult, 0, len(group.claims))
	for _, claim := range group.claims {
		result := models.ChecksumResult{ArtifactID: claim.ArtifactID, Error: &message, CheckedAt: now}
		if errors.Is(readErr, errNotRetryable) || claim.Attempts >= maxAttempts {
			result.Status = models.ChecksumStatusFailed
			j.logger.Info("artifact bytes cannot be read", "artifact_id", claim.ArtifactID, "attempts", claim.Attempts, "err", message)
		} else {
			result.Status = models.ChecksumStatusPending
			nextAttemptAt := now.Add(retryDelay(claim.Attempts))
			result.NextAttemptAt = &nextAttemptAt
		}
		results = append(results, result)
	}
	return results
}

func retryDelay(attempts int) time.Duration {
	delay := firstRetryDelay
	for range max(attempts-1, 0) {
		delay *= 2
	}
	return delay
}

// acquireHost waits for a free read slot of the URI's host.
func (j *ChecksumJob) acquireHost(ctx context.Context, uri string) (func(), error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parse uri: %w: %w", err, errNotRetryable)
	}
	j.hostSlotsMu.Lock()
	slots, ok := j.hostSlots[parsed.Host]
	if !ok {
		slots = make(chan struct{}, j.cfg.PerHostConcurrency)
		j.hostSlots[parsed.Host] = slots
	}
	j.hostSlotsMu.Unlock()
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for a read slot of %s: %w", parsed.Host, ctx.Err())
	}
}
