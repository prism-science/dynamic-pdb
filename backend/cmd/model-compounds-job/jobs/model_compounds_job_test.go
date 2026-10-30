package jobs

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

const bucketPrefix = "https://bucket.example.org/"

// mmcifWithGlycerol is a small mmCIF file modeling one glycerol.
const mmcifWithGlycerol = "data_TEST\nloop_\n_entity.id\n_entity.type\n1 polymer\n2 non-polymer\n#\n" +
	"loop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n" +
	"_atom_site.label_entity_id\n_atom_site.label_seq_id\n_atom_site.auth_seq_id\n_atom_site.auth_asym_id\n" +
	"ATOM ALA A 1 1 1 A\nHETATM GOL B 2 . 301 A\n#\n"

// pdbWithSulfate is a small PDB-format file modeling one sulfate.
const pdbWithSulfate = "SEQRES   1 A    1  ALA\n" +
	"ATOM      1  CA  ALA A   1       1.000   2.000   3.000  1.00 20.00           C\n" +
	"TER       2      ALA A   1\n" +
	"HETATM    3  S   SO4 A 101       1.000   2.000   3.000  1.00 20.00           S\n" +
	"END\n"

type failure struct {
	artifactID uuid.UUID
	message    string
	retryAt    *time.Time
}

type fakeQueue struct {
	claims   []models.ModeledCompoundClaim
	saved    map[uuid.UUID]cif.ModeledCompounds
	saveErr  error
	failures []failure
	// recentSince is the time AddMissingSince was asked about.
	recentSince time.Time
	// pages are the historical fill's pages, served in order; pageAfter records the
	// artifact each request started after.
	pages       []models.MissingSetsPage
	pageAfter   []uuid.UUID
	maxAttempts int
}

func (q *fakeQueue) AddMissingSince(_ context.Context, since time.Time) (int64, error) {
	q.recentSince = since
	return 0, nil
}

func (q *fakeQueue) AddMissingAfter(_ context.Context, after uuid.UUID, _ int) (models.MissingSetsPage, error) {
	q.pageAfter = append(q.pageAfter, after)
	if len(q.pages) == 0 {
		return models.MissingSetsPage{}, nil
	}
	page := q.pages[0]
	q.pages = q.pages[1:]
	return page, nil
}

func (q *fakeQueue) ClaimDue(_ context.Context, _ time.Time, limit int, _ time.Duration, maxAttempts int) ([]models.ModeledCompoundClaim, error) {
	q.maxAttempts = maxAttempts
	taken := q.claims[:min(limit, len(q.claims))]
	q.claims = q.claims[len(taken):]
	return taken, nil
}

func (q *fakeQueue) RecordFailure(_ context.Context, artifactID uuid.UUID, message string, retryAt *time.Time) error {
	q.failures = append(q.failures, failure{artifactID: artifactID, message: message, retryAt: retryAt})
	return nil
}

func (q *fakeQueue) Save(_ context.Context, artifactID uuid.UUID, result cif.ModeledCompounds, _ time.Time) (int, error) {
	if q.saveErr != nil {
		return 0, q.saveErr
	}
	q.saved[artifactID] = result
	return 1, nil
}

type fakeFiles struct {
	contents map[string][]byte
	openErr  error
}

func (f *fakeFiles) StoredKey(uri string) (string, bool) {
	key, ok := strings.CutPrefix(uri, bucketPrefix)
	return key, ok
}

func (f *fakeFiles) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.openErr != nil {
		return nil, f.openErr
	}
	contents, ok := f.contents[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return io.NopCloser(bytes.NewReader(contents)), nil
}

var testNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func newTestJob(t *testing.T, queue *fakeQueue, files *fakeFiles, cfg Config) *ModelCompoundsJob {
	t.Helper()
	if queue.saved == nil {
		queue.saved = map[uuid.UUID]cif.ModeledCompounds{}
	}
	job, err := NewModelCompoundsJob(queue, files, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	job.now = func() time.Time { return testNow }
	job.sleep = func(context.Context, time.Duration) error { return nil }
	return job
}

func testConfig() Config {
	return Config{ClaimFor: 4 * time.Minute, FileTimeout: 10 * time.Minute, RecentWindow: 48 * time.Hour, FillBatchSize: 2}
}

func storedClaim(key string, format *string, attempts int) models.ModeledCompoundClaim {
	uri := bucketPrefix + key
	return models.ModeledCompoundClaim{ArtifactID: uuid.New(), Name: key, URI: &uri, Format: format, Attempts: attempts}
}

func ptr[T any](value T) *T {
	return &value
}

func gzipped(t *testing.T, text string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, err := writer.Write([]byte(text))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func copyIDs(result cif.ModeledCompounds) []string {
	ids := []string{}
	for _, copied := range result.Copies {
		ids = append(ids, copied.CCDID+" "+copied.AuthAsymID+" "+copied.AuthSeqID)
	}
	return ids
}

func Test_should_save_the_compounds_when_stored_files_are_read(t *testing.T) {
	cases := map[string]struct {
		contents []byte
		format   *string
		want     []string
	}{
		"mmCIF declared as mmcif":      {contents: []byte(mmcifWithGlycerol), format: ptr("mmcif"), want: []string{"GOL A 301"}},
		"mmCIF declared as CIF":        {contents: []byte(mmcifWithGlycerol), format: ptr(" CIF "), want: []string{"GOL A 301"}},
		"PDB format declared as pdb":   {contents: []byte(pdbWithSulfate), format: ptr("pdb"), want: []string{"SO4 A 101"}},
		"gzipped mmCIF, no format":     {contents: gzipped(t, mmcifWithGlycerol), want: []string{"GOL A 301"}},
		"gzipped PDB, declared as pdb": {contents: gzipped(t, pdbWithSulfate), format: ptr("pdb"), want: []string{"SO4 A 101"}},
		"PDB format, no format":        {contents: []byte(pdbWithSulfate), want: []string{"SO4 A 101"}},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			claim := storedClaim("e1/models/m1/artifacts/a1/model", testCase.format, 1)
			queue := &fakeQueue{claims: []models.ModeledCompoundClaim{claim}}
			files := &fakeFiles{contents: map[string][]byte{"e1/models/m1/artifacts/a1/model": testCase.contents}}
			job := newTestJob(t, queue, files, testConfig())

			// when
			err := job.Run(context.Background())

			// then
			require.NoError(t, err)
			assert.Empty(t, queue.failures)
			require.Contains(t, queue.saved, claim.ArtifactID)
			assert.Equal(t, testCase.want, copyIDs(queue.saved[claim.ArtifactID]))
		})
	}
}

func Test_should_fail_for_good_when_no_retry_can_change_the_outcome(t *testing.T) {
	cases := map[string]struct {
		claim    models.ModeledCompoundClaim
		contents []byte
		message  string
	}{
		"not a structure format": {
			claim: storedClaim("k", ptr("mtz"), 1), contents: []byte(mmcifWithGlycerol),
			message: `open the structure: declared format: unknown model file format "mtz"`,
		},
		"not in our bucket": {
			claim:   models.ModeledCompoundClaim{ArtifactID: uuid.New(), URI: ptr("https://files.rcsb.org/download/1ABC.cif"), Attempts: 1},
			message: "the file is not in our bucket",
		},
		"no uri": {
			claim:   models.ModeledCompoundClaim{ArtifactID: uuid.New(), Attempts: 1},
			message: "artifact has no uri",
		},
		"neither mmCIF nor PDB": {
			claim: storedClaim("k", nil, 1), contents: []byte("hello, this is a map file\n"),
			message: "open the structure: first bytes: the file starts like neither mmCIF nor PDB format",
		},
		"empty file": {
			claim: storedClaim("k", nil, 1), contents: []byte("\n\n"),
			message: "open the structure: first bytes: the file is empty",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			queue := &fakeQueue{claims: []models.ModeledCompoundClaim{testCase.claim}}
			files := &fakeFiles{contents: map[string][]byte{"k": testCase.contents}}
			job := newTestJob(t, queue, files, testConfig())

			// when
			err := job.Run(context.Background())

			// then
			require.NoError(t, err)
			assert.Equal(t, []failure{{artifactID: testCase.claim.ArtifactID, message: testCase.message}}, queue.failures)
			assert.Empty(t, queue.saved)
		})
	}
}

func Test_should_retry_after_a_doubling_delay_and_then_fail_when_the_file_cannot_be_opened(t *testing.T) {
	// given: the same broken file at its first, second and third attempt.
	claims := []models.ModeledCompoundClaim{
		storedClaim("k", ptr("mmcif"), 1),
		storedClaim("k", ptr("mmcif"), 2),
		storedClaim("k", ptr("mmcif"), 3),
	}
	queue := &fakeQueue{claims: claims}
	files := &fakeFiles{openErr: errors.New("connection reset")}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	require.Len(t, queue.failures, 3)
	assert.Equal(t, ptr(testNow.Add(10*time.Minute)), queue.failures[0].retryAt)
	assert.Equal(t, ptr(testNow.Add(20*time.Minute)), queue.failures[1].retryAt)
	assert.Nil(t, queue.failures[2].retryAt)
	assert.Equal(t, "open the file: connection reset", queue.failures[2].message)
}

func Test_should_stop_claiming_when_the_run_reaches_its_file_cap(t *testing.T) {
	// given
	queue := &fakeQueue{claims: []models.ModeledCompoundClaim{
		storedClaim("k", ptr("mmcif"), 1),
		storedClaim("k", ptr("mmcif"), 1),
		storedClaim("k", ptr("mmcif"), 1),
	}}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithGlycerol)}}
	cfg := testConfig()
	cfg.MaxFilesPerRun = 2
	job := newTestJob(t, queue, files, cfg)

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Len(t, queue.saved, 2)
	assert.Len(t, queue.claims, 1)
}

func Test_should_record_nothing_when_the_run_is_stopped_during_a_read(t *testing.T) {
	// given: the run's context ends before the file is opened.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	queue := &fakeQueue{claims: []models.ModeledCompoundClaim{storedClaim("k", ptr("mmcif"), 1)}}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithGlycerol)}}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(ctx)

	// then: the claim lapses with its lease and the file is read again.
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, queue.failures)
	assert.Empty(t, queue.saved)
}

func Test_should_refuse_a_config_when_a_setting_is_out_of_range(t *testing.T) {
	// given
	valid := testConfig()
	withChange := func(change func(*Config)) Config {
		cfg := valid
		change(&cfg)
		return cfg
	}
	cases := map[string]Config{
		"negative file cap":   withChange(func(cfg *Config) { cfg.MaxFilesPerRun = -1 }),
		"no claiming time":    withChange(func(cfg *Config) { cfg.ClaimFor = 0 }),
		"no file timeout":     withChange(func(cfg *Config) { cfg.FileTimeout = 0 }),
		"negative claim time": withChange(func(cfg *Config) { cfg.ClaimFor = -time.Minute }),
		"no recent window":    withChange(func(cfg *Config) { cfg.RecentWindow = 0 }),
		"no fill batch":       withChange(func(cfg *Config) { cfg.FillBatchSize = 0 }),
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, err := NewModelCompoundsJob(&fakeQueue{}, &fakeFiles{}, cfg, nil)

			// then
			require.Error(t, err)
		})
	}
}

func Test_should_give_recent_uploads_sets_and_leave_older_files_alone_when_the_historical_fill_is_off(t *testing.T) {
	// given
	queue := &fakeQueue{}
	job := newTestJob(t, queue, &fakeFiles{}, testConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(-48*time.Hour), queue.recentSince)
	assert.Empty(t, queue.pageAfter, "no historical fill")
	assert.Equal(t, maxAttempts, queue.maxAttempts)
}

func Test_should_page_through_every_artifact_and_pause_between_pages_when_the_historical_fill_is_on(t *testing.T) {
	// given: two full pages of two artifacts, then a short one.
	first, second := uuid.New(), uuid.New()
	queue := &fakeQueue{pages: []models.MissingSetsPage{
		{Scanned: 2, Added: 1, LastArtifactID: first},
		{Scanned: 2, Added: 2, LastArtifactID: second},
		{Scanned: 1, Added: 0, LastArtifactID: uuid.New()},
	}}
	cfg := testConfig()
	cfg.HistoricalFill = true
	job := newTestJob(t, queue, &fakeFiles{}, cfg)
	pauses := 0
	job.sleep = func(context.Context, time.Duration) error {
		pauses++
		return nil
	}

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{uuid.Nil, first, second}, queue.pageAfter)
	assert.Equal(t, 2, pauses, "a pause after each full page")
}

func Test_should_count_a_refused_save_as_a_failed_attempt_when_the_database_rejects_the_copies(t *testing.T) {
	// given
	claim := storedClaim("k", ptr("mmcif"), 1)
	queue := &fakeQueue{claims: []models.ModeledCompoundClaim{claim}, saveErr: errors.New("invalid byte sequence")}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithGlycerol)}}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(context.Background())

	// then: the run goes on, and the set is retried later.
	require.NoError(t, err)
	require.Len(t, queue.failures, 1)
	assert.Equal(t, "save the compounds: invalid byte sequence", queue.failures[0].message)
	assert.Equal(t, ptr(testNow.Add(10*time.Minute)), queue.failures[0].retryAt)
}
