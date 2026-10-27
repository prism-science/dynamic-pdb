package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/services/cdn"
)

const ourFilesBaseURL = "https://files.example.com/"

// fakeQueue hands out its claims once each, like artifact_checksums with nothing else due.
type fakeQueue struct {
	mu        sync.Mutex
	pending   []models.ChecksumClaim
	locations map[uuid.UUID]models.ArtifactLocation
	results   map[uuid.UUID]models.ChecksumResult
	// missing is the artifacts without a row, in id order; AddMissingAfter pages through them.
	missing    []uuid.UUID
	fillAfters []uuid.UUID
	fillErr    error
	// onFillPage runs inside every AddMissingAfter call, so a test can move its clock.
	onFillPage  func()
	claimLimits []int
}

func newFakeQueue(claims ...models.ChecksumClaim) *fakeQueue {
	return &fakeQueue{
		pending:   claims,
		locations: map[uuid.UUID]models.ArtifactLocation{},
		results:   map[uuid.UUID]models.ChecksumResult{},
	}
}

func (q *fakeQueue) AddMissingAfter(_ context.Context, after uuid.UUID, limit int) (models.MissingChecksumsPage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fillAfters = append(q.fillAfters, after)
	if q.onFillPage != nil {
		q.onFillPage()
	}
	if q.fillErr != nil {
		return models.MissingChecksumsPage{}, q.fillErr
	}
	page := models.MissingChecksumsPage{}
	for _, id := range q.missing {
		if page.Scanned == limit {
			break
		}
		if id.String() > after.String() {
			page.Scanned++
			page.Added++
			page.LastArtifactID = id
		}
	}
	return page, nil
}

// ClaimDue takes up to limit URIs and every pending claim that shares one of them.
func (q *fakeQueue) ClaimDue(_ context.Context, _ time.Time, limit int, _ time.Duration) ([]models.ChecksumClaim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claimLimits = append(q.claimLimits, limit)
	taken := make([]models.ChecksumClaim, 0)
	kept := make([]models.ChecksumClaim, 0)
	takenURIs := map[string]bool{}
	for _, claim := range q.pending {
		uri := ""
		if claim.URI != nil {
			uri = *claim.URI
		}
		if takenURIs[uri] || len(takenURIs) < limit {
			takenURIs[uri] = true
			taken = append(taken, claim)
			continue
		}
		kept = append(kept, claim)
	}
	q.pending = kept
	return taken, nil
}

func (q *fakeQueue) Locations(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]models.ArtifactLocation, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	found := map[uuid.UUID]models.ArtifactLocation{}
	for _, id := range ids {
		if location, ok := q.locations[id]; ok {
			found[id] = location
		}
	}
	return found, nil
}

func (q *fakeQueue) Record(_ context.Context, results []models.ChecksumResult) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, result := range results {
		q.results[result.ArtifactID] = result
	}
	return nil
}

func (q *fakeQueue) result(t *testing.T, artifactID uuid.UUID) models.ChecksumResult {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	result, ok := q.results[artifactID]
	require.True(t, ok, "no result recorded for %s", artifactID)
	return result
}

// fakeFiles is our bucket: URIs under ourFilesBaseURL are stored files.
type fakeFiles struct {
	mu          sync.Mutex
	objects     map[string]string
	writers     []*fakeWriter
	completeErr error
	abortErr    error
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{objects: map[string]string{}}
}

func (f *fakeFiles) StoredKey(uri string) (string, bool) {
	if !strings.HasPrefix(uri, ourFilesBaseURL) {
		return "", false
	}
	return strings.TrimPrefix(uri, ourFilesBaseURL), true
}

func (f *fakeFiles) Open(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	content, ok := f.objects[key]
	if !ok {
		return nil, cdn.ErrObjectNotFound
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (f *fakeFiles) CopyKey(file cdn.FileUpload) (string, error) {
	if file.ModelID == "" {
		return file.EntryID + "/artifacts/" + file.ArtifactID + "/" + file.OriginalFilename, nil
	}
	return file.EntryID + "/models/" + file.ModelID + "/artifacts/" + file.ArtifactID + "/" + file.OriginalFilename, nil
}

func (f *fakeFiles) NewWriter(_ context.Context, key string) (cdn.ObjectWriter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	writer := &fakeWriter{files: f, key: key, completeErr: f.completeErr, abortErr: f.abortErr}
	f.writers = append(f.writers, writer)
	return writer, nil
}

func (f *fakeFiles) URL(key string) (string, error) {
	return ourFilesBaseURL + key, nil
}

type fakeWriter struct {
	files       *fakeFiles
	key         string
	buffer      bytes.Buffer
	completed   bool
	aborted     bool
	completeErr error
	abortErr    error
}

func (w *fakeWriter) Write(data []byte) (int, error) {
	return w.buffer.Write(data)
}

func (w *fakeWriter) Complete() error {
	if w.completeErr != nil {
		return w.completeErr
	}
	w.completed = true
	w.files.mu.Lock()
	defer w.files.mu.Unlock()
	w.files.objects[w.key] = w.buffer.String()
	return nil
}

func (w *fakeWriter) Abort() error {
	w.aborted = true
	return w.abortErr
}

// rcsbServer serves files by path and counts requests per path.
type rcsbServer struct {
	server   *httptest.Server
	files    map[string]string
	statuses map[string]int
	requests sync.Map
}

func newRCSBServer(t *testing.T) *rcsbServer {
	t.Helper()
	rcsb := &rcsbServer{files: map[string]string{}, statuses: map[string]int{}}
	rcsb.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter, _ := rcsb.requests.LoadOrStore(r.URL.Path, new(atomic.Int32))
		counter.(*atomic.Int32).Add(1)
		if status, ok := rcsb.statuses[r.URL.Path]; ok {
			w.WriteHeader(status)
			return
		}
		content, ok := rcsb.files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(content))
		assert.NoError(t, err)
	}))
	t.Cleanup(rcsb.server.Close)
	return rcsb
}

func (r *rcsbServer) uri(path string) string {
	return r.server.URL + path
}

func (r *rcsbServer) requestCount(path string) int32 {
	counter, ok := r.requests.Load(path)
	if !ok {
		return 0
	}
	return counter.(*atomic.Int32).Load()
}

var jobTestNow = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func testJobConfig() Config {
	return Config{
		Concurrency:        2,
		PerHostConcurrency: 2,
		BatchSize:          10,
		ClaimFor:           50 * time.Minute,
		FileTimeout:        time.Minute,
		CopyExternal:       true,
		CopyLimitBytes:     1 << 20,
		FillBatchSize:      100,
	}
}

func newTestJob(t *testing.T, queue *fakeQueue, files *fakeFiles, rcsb *rcsbServer, cfg Config) *ChecksumJob {
	t.Helper()
	var client *http.Client
	if rcsb != nil {
		client = rcsb.server.Client()
	} else {
		client = http.DefaultClient
	}
	job, err := NewChecksumJob(queue, files, NewHTTPSource(client), cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	job.now = func() time.Time { return jobTestNow }
	job.sleep = func(context.Context, time.Duration) error { return nil }
	return job
}

// sortedIDs is count new ids in the order PostgreSQL sorts uuids: by their lowercase text.
func sortedIDs(count int) []uuid.UUID {
	ids := make([]uuid.UUID, 0, count)
	for range count {
		ids = append(ids, uuid.New())
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return ids
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func claimFor(uri string, sha256 *string, sizeBytes *int64, attempts int) models.ChecksumClaim {
	return models.ChecksumClaim{
		ArtifactID: uuid.New(),
		Name:       "file-" + uuid.NewString()[:8] + ".cif",
		URI:        &uri,
		SHA256:     sha256,
		SizeBytes:  sizeBytes,
		Attempts:   attempts,
	}
}

func ptr[T any](value T) *T {
	return &value
}

func Test_should_record_verified_when_stored_file_matches_declared_hash_and_size(t *testing.T) {
	// given
	files := newFakeFiles()
	files.objects["entry-1/artifacts/a1/model.cif"] = "data_model\n"
	claim := claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/model.cif", ptr(strings.ToUpper(sha256Hex("data_model\n"))), ptr(int64(11)), 1)
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, files, nil, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusVerified, result.Status)
	assert.Equal(t, ptr(sha256Hex("data_model\n")), result.SHA256)
	assert.Equal(t, ptr(int64(11)), result.SizeBytes)
	assert.Nil(t, result.StoredURI)
	assert.Equal(t, jobTestNow, result.CheckedAt)
	assert.Empty(t, files.writers)
	assert.Equal(t, []uuid.UUID{uuid.Nil}, queue.fillAfters)
}

func Test_should_add_missing_rows_page_by_page_and_pause_as_long_as_each_page_took_when_table_is_larger_than_a_page(t *testing.T) {
	// given
	queue := newFakeQueue()
	queue.missing = sortedIDs(5)
	cfg := testJobConfig()
	cfg.FillBatchSize = 2
	job := newTestJob(t, queue, newFakeFiles(), nil, cfg)
	var logs bytes.Buffer
	job.logger = slog.New(slog.NewTextHandler(&logs, nil))
	clock := jobTestNow
	job.now = func() time.Time { return clock }
	queue.onFillPage = func() { clock = clock.Add(3 * time.Second) }
	var pauses []time.Duration
	job.sleep = func(_ context.Context, pause time.Duration) error {
		pauses = append(pauses, pause)
		return nil
	}

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{uuid.Nil, queue.missing[1], queue.missing[3]}, queue.fillAfters)
	assert.Equal(t, []time.Duration{3 * time.Second, 3 * time.Second}, pauses)
	assert.Contains(t, logs.String(), "added_rows=5")
}

func Test_should_stop_the_run_when_adding_missing_rows_fails(t *testing.T) {
	// given
	files := newFakeFiles()
	files.objects["entry-1/artifacts/a1/model.cif"] = "data"
	queue := newFakeQueue(claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/model.cif", nil, nil, 1))
	queue.fillErr = errors.New("canceling statement due to statement timeout")
	job := newTestJob(t, queue, files, nil, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.ErrorIs(t, err, queue.fillErr)
	assert.Empty(t, queue.claimLimits)
	assert.Empty(t, queue.results)
}

func Test_should_record_computed_values_as_verified_when_nothing_was_declared(t *testing.T) {
	// given
	files := newFakeFiles()
	files.objects["entry-1/artifacts/a1/model.cif"] = "abc"
	claim := claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/model.cif", nil, nil, 1)
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, files, nil, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusVerified, result.Status)
	assert.Equal(t, ptr(sha256Hex("abc")), result.SHA256)
	assert.Equal(t, ptr(int64(3)), result.SizeBytes)
}

func Test_should_record_mismatch_with_computed_values_when_declared_hash_differs(t *testing.T) {
	// given
	files := newFakeFiles()
	files.objects["entry-1/artifacts/a1/model.cif"] = "abc"
	claim := claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/model.cif", ptr(sha256Hex("other bytes")), nil, 1)
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, files, nil, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusMismatch, result.Status)
	assert.Equal(t, ptr(sha256Hex("abc")), result.SHA256)
	assert.Equal(t, ptr(int64(3)), result.SizeBytes)
}

func Test_should_record_mismatch_when_declared_size_differs(t *testing.T) {
	// given
	files := newFakeFiles()
	files.objects["entry-1/artifacts/a1/model.cif"] = "abc"
	claim := claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/model.cif", ptr(sha256Hex("abc")), ptr(int64(4)), 1)
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, files, nil, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, models.ChecksumStatusMismatch, queue.result(t, claim.ArtifactID).Status)
}

func Test_should_log_declared_and_computed_values_when_bytes_differ(t *testing.T) {
	cases := map[string]struct {
		declaredSHA256    *string
		declaredSizeBytes *int64
		wantInLog         []string
	}{
		"hash and size declared": {
			declaredSHA256:    ptr(sha256Hex("other bytes")),
			declaredSizeBytes: ptr(int64(99)),
			wantInLog: []string{
				"declared_sha256=" + sha256Hex("other bytes"),
				"computed_sha256=" + sha256Hex("abc"),
				"declared_size_bytes=99",
				"computed_size_bytes=3",
			},
		},
		"only size declared": {
			declaredSizeBytes: ptr(int64(4)),
			wantInLog:         []string{"declared_sha256=none", "declared_size_bytes=4", "computed_size_bytes=3"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			files := newFakeFiles()
			files.objects["entry-1/artifacts/a1/model.cif"] = "abc"
			claim := claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/model.cif", tc.declaredSHA256, tc.declaredSizeBytes, 1)
			job := newTestJob(t, newFakeQueue(claim), files, nil, testJobConfig())
			var logs bytes.Buffer
			job.logger = slog.New(slog.NewTextHandler(&logs, nil))

			// when
			err := job.Run(context.Background())

			// then
			require.NoError(t, err)
			for _, want := range tc.wantInLog {
				assert.Contains(t, logs.String(), want)
			}
			assert.NotContains(t, logs.String(), "=0x", "a pointer address was logged instead of a value")
		})
	}
}

func Test_should_fail_at_once_when_stored_file_is_missing(t *testing.T) {
	// given
	claim := claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/missing.cif", nil, nil, 1)
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, newFakeFiles(), nil, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusFailed, result.Status)
	require.NotNil(t, result.Error)
	assert.Contains(t, *result.Error, "not found")
}

func Test_should_fail_at_once_when_external_file_is_gone(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	claim := claimFor(rcsb.uri("/download/gone.cif"), nil, nil, 1)
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, newFakeFiles(), rcsb, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusFailed, result.Status)
	assert.Equal(t, ptr("open external file: http status 404: not retryable"), result.Error)
}

func Test_should_fail_when_artifact_has_no_uri(t *testing.T) {
	// given
	claim := models.ChecksumClaim{ArtifactID: uuid.New(), Name: "no-uri.cif", Attempts: 1}
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, newFakeFiles(), nil, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusFailed, result.Status)
	assert.Equal(t, ptr("artifact has no uri: not retryable"), result.Error)
}

func Test_should_retry_later_with_growing_delay_when_server_errors(t *testing.T) {
	cases := map[int]time.Duration{1: time.Hour, 2: 2 * time.Hour, 3: 4 * time.Hour, 4: 8 * time.Hour}
	for attempts, delay := range cases {
		t.Run(delay.String(), func(t *testing.T) {
			// given
			rcsb := newRCSBServer(t)
			rcsb.statuses["/download/busy.cif"] = http.StatusServiceUnavailable
			claim := claimFor(rcsb.uri("/download/busy.cif"), nil, nil, attempts)
			queue := newFakeQueue(claim)
			job := newTestJob(t, queue, newFakeFiles(), rcsb, testJobConfig())

			// when
			err := job.Run(context.Background())

			// then
			require.NoError(t, err)
			result := queue.result(t, claim.ArtifactID)
			assert.Equal(t, models.ChecksumStatusPending, result.Status)
			assert.Equal(t, ptr(jobTestNow.Add(delay)), result.NextAttemptAt)
			assert.Equal(t, ptr("open external file: http status 503"), result.Error)
		})
	}
}

func Test_should_fail_when_fifth_attempt_errors(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.statuses["/download/busy.cif"] = http.StatusInternalServerError
	claim := claimFor(rcsb.uri("/download/busy.cif"), nil, nil, 5)
	queue := newFakeQueue(claim)
	job := newTestJob(t, queue, newFakeFiles(), rcsb, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusFailed, result.Status)
	assert.Equal(t, ptr("open external file: http status 500"), result.Error)
}

func Test_should_copy_external_file_into_artifact_folder_when_hash_verified(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.files["/download/1ABC-sf.cif"] = "data_1abc_sf\n"
	claim := claimFor(rcsb.uri("/download/1ABC-sf.cif"), ptr(sha256Hex("data_1abc_sf\n")), nil, 1)
	claim.Name = "1abc-sf.cif"
	queue := newFakeQueue(claim)
	queue.locations[claim.ArtifactID] = models.ArtifactLocation{EntryID: "entry-1", ModelID: ptr("model-1")}
	files := newFakeFiles()
	job := newTestJob(t, queue, files, rcsb, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	copyKey := "entry-1/models/model-1/artifacts/" + claim.ArtifactID.String() + "/1abc-sf.cif"
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusVerified, result.Status)
	assert.Equal(t, ptr(ourFilesBaseURL+copyKey), result.StoredURI)
	assert.Equal(t, "data_1abc_sf\n", files.objects[copyKey])
}

func Test_should_discard_copy_when_external_file_does_not_match_declared_hash(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.files["/download/1ABC-sf.cif"] = "remediated\n"
	claim := claimFor(rcsb.uri("/download/1ABC-sf.cif"), ptr(sha256Hex("original\n")), nil, 1)
	queue := newFakeQueue(claim)
	queue.locations[claim.ArtifactID] = models.ArtifactLocation{EntryID: "entry-1"}
	files := newFakeFiles()
	job := newTestJob(t, queue, files, rcsb, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusMismatch, result.Status)
	assert.Equal(t, ptr(sha256Hex("remediated\n")), result.SHA256)
	assert.Nil(t, result.StoredURI)
	require.Len(t, files.writers, 1)
	assert.True(t, files.writers[0].aborted)
	assert.False(t, files.writers[0].completed)
}

func Test_should_keep_link_only_when_external_file_is_over_copy_limit(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.files["/download/huge.cif"] = strings.Repeat("x", 100)
	claim := claimFor(rcsb.uri("/download/huge.cif"), nil, nil, 1)
	queue := newFakeQueue(claim)
	queue.locations[claim.ArtifactID] = models.ArtifactLocation{EntryID: "entry-1"}
	files := newFakeFiles()
	cfg := testJobConfig()
	cfg.CopyLimitBytes = 10
	job := newTestJob(t, queue, files, rcsb, cfg)

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusVerified, result.Status)
	assert.Equal(t, ptr(sha256Hex(strings.Repeat("x", 100))), result.SHA256)
	assert.Nil(t, result.StoredURI)
	require.Len(t, files.writers, 1)
	assert.True(t, files.writers[0].aborted)
}

func Test_should_read_shared_uri_once_and_record_every_artifact_when_rows_share_a_uri(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.files["/download/1ABC-sf.cif"] = "data_1abc_sf\n"
	uri := rcsb.uri("/download/1ABC-sf.cif")
	first := claimFor(uri, ptr(sha256Hex("data_1abc_sf\n")), nil, 1)
	second := claimFor(uri, nil, nil, 1)
	third := claimFor(uri, ptr(sha256Hex("something else")), nil, 1)
	queue := newFakeQueue(first, second, third)
	for _, claim := range []models.ChecksumClaim{first, second, third} {
		queue.locations[claim.ArtifactID] = models.ArtifactLocation{EntryID: "entry-1"}
	}
	files := newFakeFiles()
	job := newTestJob(t, queue, files, rcsb, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, int32(1), rcsb.requestCount("/download/1ABC-sf.cif"))
	require.Len(t, files.writers, 1)
	storedURI := queue.result(t, first.ArtifactID).StoredURI
	require.NotNil(t, storedURI)
	assert.Equal(t, storedURI, queue.result(t, second.ArtifactID).StoredURI)
	assert.Equal(t, models.ChecksumStatusMismatch, queue.result(t, third.ArtifactID).Status)
	assert.Nil(t, queue.result(t, third.ArtifactID).StoredURI)
}

func Test_should_not_copy_when_copying_external_files_is_off(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.files["/download/1ABC.cif"] = "data_1abc\n"
	claim := claimFor(rcsb.uri("/download/1ABC.cif"), nil, nil, 1)
	queue := newFakeQueue(claim)
	queue.locations[claim.ArtifactID] = models.ArtifactLocation{EntryID: "entry-1"}
	files := newFakeFiles()
	cfg := testJobConfig()
	cfg.CopyExternal = false
	job := newTestJob(t, queue, files, rcsb, cfg)

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, models.ChecksumStatusVerified, queue.result(t, claim.ArtifactID).Status)
	assert.Empty(t, files.writers)
}

func Test_should_stop_claiming_when_max_files_per_run_reached(t *testing.T) {
	// given
	files := newFakeFiles()
	claims := make([]models.ChecksumClaim, 0, 5)
	for index := range 5 {
		key := "entry-1/artifacts/a" + string(rune('0'+index)) + "/model.cif"
		files.objects[key] = "data"
		claims = append(claims, claimFor(ourFilesBaseURL+key, nil, nil, 1))
	}
	queue := newFakeQueue(claims...)
	cfg := testJobConfig()
	cfg.BatchSize = 2
	cfg.MaxFilesPerRun = 3
	job := newTestJob(t, queue, files, nil, cfg)

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Len(t, queue.results, 3)
	assert.Equal(t, []int{2, 1}, queue.claimLimits)
}

func Test_should_claim_nothing_when_claiming_time_is_over(t *testing.T) {
	// given
	files := newFakeFiles()
	files.objects["entry-1/artifacts/a1/model.cif"] = "data"
	queue := newFakeQueue(claimFor(ourFilesBaseURL+"entry-1/artifacts/a1/model.cif", nil, nil, 1))
	job := newTestJob(t, queue, files, nil, testJobConfig())
	calls := 0
	job.now = func() time.Time {
		calls++
		if calls == 1 {
			return jobTestNow
		}
		return jobTestNow.Add(51 * time.Minute)
	}

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Empty(t, queue.results)
	assert.Empty(t, queue.claimLimits)
}

func Test_should_record_nothing_when_run_is_cancelled_during_a_read(t *testing.T) {
	// given
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cancel()
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	claim := claimFor(server.URL+"/slow.cif", nil, nil, 1)
	queue := newFakeQueue(claim)
	job, err := NewChecksumJob(queue, newFakeFiles(), NewHTTPSource(server.Client()), testJobConfig(), slog.New(slog.DiscardHandler))
	require.NoError(t, err)

	// when
	err = job.Run(ctx)

	// then
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, queue.results)
}

func Test_should_reject_config_when_values_are_out_of_range(t *testing.T) {
	cases := map[string]func(*Config){
		"concurrency":          func(cfg *Config) { cfg.Concurrency = 0 },
		"per host concurrency": func(cfg *Config) { cfg.PerHostConcurrency = 0 },
		"batch size":           func(cfg *Config) { cfg.BatchSize = 0 },
		"max files per run":    func(cfg *Config) { cfg.MaxFilesPerRun = -1 },
		"claim for":            func(cfg *Config) { cfg.ClaimFor = 0 },
		"file timeout":         func(cfg *Config) { cfg.FileTimeout = 0 },
		"copy limit":           func(cfg *Config) { cfg.CopyLimitBytes = 0 },
		"fill batch size":      func(cfg *Config) { cfg.FillBatchSize = 0 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			cfg := testJobConfig()
			change(&cfg)

			// when
			_, err := NewChecksumJob(newFakeQueue(), newFakeFiles(), NewHTTPSource(http.DefaultClient), cfg, nil)

			// then
			require.Error(t, err)
			assert.False(t, errors.Is(err, context.Canceled))
		})
	}
}

func Test_should_keep_verified_hash_without_copy_when_storing_the_copy_fails(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.files["/download/1ABC-sf.cif"] = "data_1abc_sf\n"
	claim := claimFor(rcsb.uri("/download/1ABC-sf.cif"), ptr(sha256Hex("data_1abc_sf\n")), nil, 1)
	queue := newFakeQueue(claim)
	queue.locations[claim.ArtifactID] = models.ArtifactLocation{EntryID: "entry-1"}
	files := newFakeFiles()
	files.completeErr = errors.New("bucket refused the upload")
	job := newTestJob(t, queue, files, rcsb, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusVerified, result.Status)
	assert.Equal(t, ptr(sha256Hex("data_1abc_sf\n")), result.SHA256)
	assert.Nil(t, result.StoredURI)
	assert.Nil(t, result.NextAttemptAt)
}

func Test_should_record_mismatch_when_discarding_the_copy_fails(t *testing.T) {
	// given
	rcsb := newRCSBServer(t)
	rcsb.files["/download/1ABC-sf.cif"] = "remediated\n"
	claim := claimFor(rcsb.uri("/download/1ABC-sf.cif"), ptr(sha256Hex("original\n")), nil, 1)
	queue := newFakeQueue(claim)
	queue.locations[claim.ArtifactID] = models.ArtifactLocation{EntryID: "entry-1"}
	files := newFakeFiles()
	files.abortErr = errors.New("abort timed out")
	job := newTestJob(t, queue, files, rcsb, testJobConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	result := queue.result(t, claim.ArtifactID)
	assert.Equal(t, models.ChecksumStatusMismatch, result.Status)
	assert.Equal(t, ptr(sha256Hex("remediated\n")), result.SHA256)
}

func Test_should_not_count_waiting_for_a_host_slot_against_file_timeout_when_host_is_busy(t *testing.T) {
	// given
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, err := w.Write([]byte(r.URL.Path))
		assert.NoError(t, err)
	}))
	t.Cleanup(slow.Close)
	first := claimFor(slow.URL+"/first.cif", nil, nil, 1)
	second := claimFor(slow.URL+"/second.cif", nil, nil, 1)
	queue := newFakeQueue(first, second)
	cfg := testJobConfig()
	cfg.Concurrency = 2
	cfg.PerHostConcurrency = 1
	cfg.FileTimeout = 300 * time.Millisecond
	cfg.CopyExternal = false
	job, err := NewChecksumJob(queue, newFakeFiles(), NewHTTPSource(slow.Client()), cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	job.now = func() time.Time { return jobTestNow }

	// when
	err = job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, models.ChecksumStatusVerified, queue.result(t, first.ArtifactID).Status)
	assert.Equal(t, models.ChecksumStatusVerified, queue.result(t, second.ArtifactID).Status)
}
