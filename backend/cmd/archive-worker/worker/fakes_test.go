package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/cdn"
)

// bucketBase is the fake bucket's public URL; a URI under it is in our bucket.
const bucketBase = "https://files.example.org/"

// fakeBucket is the data bucket in memory.
type fakeBucket struct {
	mu       sync.Mutex
	objects  map[string][]byte
	writers  []string
	aborted  []string
	openErrs map[string]error
	// stalled keys answer, then send no byte until the read's context ends.
	stalled map[string]bool
}

func newFakeBucket() *fakeBucket {
	return &fakeBucket{objects: map[string][]byte{}, openErrs: map[string]error{}, stalled: map[string]bool{}}
}

func (b *fakeBucket) StoredKey(uri string) (string, bool) {
	key, found := strings.CutPrefix(uri, bucketBase)
	return key, found
}

func (b *fakeBucket) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.openErrs[key]; err != nil {
		return nil, err
	}
	if b.stalled[key] {
		return io.NopCloser(stallingReader{ctx: ctx}), nil
	}
	content, found := b.objects[key]
	if !found {
		return nil, fmt.Errorf("open %s: %w", key, cdn.ErrObjectNotFound)
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (b *fakeBucket) NewWriter(_ context.Context, key string) (cdn.ObjectWriter, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writers = append(b.writers, key)
	return &fakeObjectWriter{bucket: b, key: key}, nil
}

func (b *fakeBucket) URL(key string) (string, error) {
	return bucketBase + key, nil
}

func (b *fakeBucket) put(key string, content []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = content
}

func (b *fakeBucket) object(key string) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	content, found := b.objects[key]
	return content, found
}

// stallingReader is a response body that sends nothing until its request's context ends.
type stallingReader struct {
	ctx context.Context
}

func (r stallingReader) Read([]byte) (int, error) {
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

type fakeObjectWriter struct {
	bucket   *fakeBucket
	key      string
	buffer   bytes.Buffer
	finished bool
}

func (w *fakeObjectWriter) Write(data []byte) (int, error) {
	if w.finished {
		return 0, errors.New("write to finished object")
	}
	return w.buffer.Write(data)
}

func (w *fakeObjectWriter) Complete() error {
	if w.finished {
		return errors.New("complete finished object")
	}
	w.finished = true
	w.bucket.put(w.key, w.buffer.Bytes())
	return nil
}

func (w *fakeObjectWriter) Abort() error {
	w.finished = true
	w.bucket.mu.Lock()
	defer w.bucket.mu.Unlock()
	w.bucket.aborted = append(w.bucket.aborted, w.key)
	return nil
}

// fakeStore is one download job and its selection in memory. It moves the job through the lanes
// as the database does and records how each step ended.
type fakeStore struct {
	mu  sync.Mutex
	job models.DownloadJob
	// claimed set means no job is waiting in either lane.
	claimed   bool
	selection models.Selection
	models    []models.SelectionModel
	details   map[int]models.DownloadSelectionModel
	// listCalls counts ListModels calls; beforeList may change the models or fail a call.
	listCalls  int
	beforeList func(ctx context.Context, call int, store *fakeStore) error
	heartbeat  func() error

	plannedBytes  int64
	preview       *models.DownloadPreview
	planned       bool
	planUpdated   bool
	preparing     bool
	blockedReason string
	result        *db.DownloadResult
	expiresAt     time.Time
	failed        string
	requeued      string
	released      bool
}

func (s *fakeStore) Claim(_ context.Context, lane db.DownloadLane, now, leaseUntil time.Time) (*models.DownloadJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed {
		return nil, nil
	}
	switch lane {
	case db.DownloadLanePlanning:
		if s.job.Status != models.DownloadStatusQueued {
			return nil, nil
		}
		s.job.Status = models.DownloadStatusPlanning
	case db.DownloadLaneBuilding:
		if s.job.Status != models.DownloadStatusPlanned || !s.job.PrepareRequested {
			return nil, nil
		}
		s.job.Status = models.DownloadStatusPreparing
		s.preparing = true
	default:
		return nil, fmt.Errorf("unknown download lane: %s", lane)
	}
	s.job.Attempts++
	s.job.StartedAt = &now
	s.job.LeaseUntil = &leaseUntil
	job := s.job
	return &job, nil
}

func (s *fakeStore) Heartbeat(context.Context, uuid.UUID, int, time.Time) error {
	if s.heartbeat != nil {
		return s.heartbeat()
	}
	return nil
}

func (s *fakeStore) FinishPlanning(_ context.Context, _ uuid.UUID, _ int, plannedBytes int64, preview models.DownloadPreview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plannedBytes = plannedBytes
	s.preview = &preview
	s.planned = true
	s.job.Status = models.DownloadStatusPlanned
	s.job.PlannedBytes = &plannedBytes
	s.job.Preview = &preview
	s.job.FailedAttempts = 0
	s.job.LeaseUntil = nil
	return nil
}

func (s *fakeStore) UpdatePlan(_ context.Context, _ uuid.UUID, _ int, plannedBytes int64, preview models.DownloadPreview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plannedBytes = plannedBytes
	s.preview = &preview
	s.planUpdated = true
	s.job.PlannedBytes = &plannedBytes
	s.job.Preview = &preview
	return nil
}

func (s *fakeStore) Block(
	_ context.Context,
	_ uuid.UUID,
	_ int,
	plannedBytes int64,
	preview models.DownloadPreview,
	reason string,
	_ time.Time,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plannedBytes = plannedBytes
	s.preview = &preview
	s.blockedReason = reason
	s.job.Status = models.DownloadStatusBlocked
	return nil
}

func (s *fakeStore) Finish(_ context.Context, _ uuid.UUID, _ int, result db.DownloadResult, _, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = &result
	s.expiresAt = expiresAt
	s.job.Status = models.DownloadStatusReady
	return nil
}

func (s *fakeStore) Fail(_ context.Context, _ uuid.UUID, _ int, message string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed = message
	s.job.Status = models.DownloadStatusFailed
	return nil
}

// backToWaiting is where a job held by a worker goes back to: queued for planning, planned for
// building.
func (s *fakeStore) backToWaiting() {
	if s.job.Status == models.DownloadStatusPlanning {
		s.job.Status = models.DownloadStatusQueued
		return
	}
	s.job.Status = models.DownloadStatusPlanned
}

func (s *fakeStore) Requeue(_ context.Context, _ uuid.UUID, _ int, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requeued = message
	s.job.FailedAttempts++
	s.backToWaiting()
	return nil
}

func (s *fakeStore) Release(context.Context, uuid.UUID, int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = true
	s.backToWaiting()
	return nil
}

func (s *fakeStore) Get(_ context.Context, id uuid.UUID) (*models.Selection, error) {
	if id != s.selection.ID {
		return nil, db.ErrSelectionNotFound
	}
	selection := s.selection
	return &selection, nil
}

func (s *fakeStore) ListModels(ctx context.Context, _ uuid.UUID, afterOrdinal, limit int) ([]models.SelectionModel, error) {
	s.mu.Lock()
	s.listCalls++
	call := s.listCalls
	hook := s.beforeList
	s.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, call, s); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	page := []models.SelectionModel{}
	for _, model := range s.models {
		if model.Ordinal > afterOrdinal && len(page) < limit {
			page = append(page, model)
		}
	}
	return page, nil
}

func (s *fakeStore) ModelDetails(_ context.Context, _ uuid.UUID, first, last int) (map[int]models.DownloadSelectionModel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	details := map[int]models.DownloadSelectionModel{}
	for ordinal, model := range s.details {
		if ordinal >= first && ordinal <= last {
			details[ordinal] = model
		}
	}
	return details, nil
}

// setChecksum replaces a file's checksum result in every model that lists it.
func (s *fakeStore) setChecksum(fileID uuid.UUID, checksum models.ArtifactChecksum) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for modelIndex := range s.models {
		for fileIndex := range s.models[modelIndex].Files {
			if s.models[modelIndex].Files[fileIndex].ID == fileID {
				s.models[modelIndex].Files[fileIndex].Checksum = checksum
			}
		}
	}
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func ptr[T any](value T) *T {
	return &value
}

// updateFile changes a pinned file in every model that lists it.
func (s *fakeStore) updateFile(fileID uuid.UUID, change func(*models.SelectionFile)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for modelIndex := range s.models {
		for fileIndex := range s.models[modelIndex].Files {
			if s.models[modelIndex].Files[fileIndex].ID == fileID {
				change(&s.models[modelIndex].Files[fileIndex])
			}
		}
	}
}
