package jobs

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
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

// mmcifWithTwoChains has protein chain A (entity 1, 3 residues in _entity_poly_seq, 2 modeled)
// and protein chain B (entity 2, not in _entity_poly_seq, 1 modeled), and a ligand.
const mmcifWithTwoChains = "data_TEST\nloop_\n_entity_poly_seq.entity_id\n_entity_poly_seq.num\n_entity_poly_seq.mon_id\n" +
	"1 1 ALA\n1 2 GLY\n1 3 LYS\n#\n" +
	"loop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n" +
	"_atom_site.label_entity_id\n_atom_site.label_seq_id\n_atom_site.auth_seq_id\n_atom_site.auth_asym_id\n" +
	"ATOM ALA A 1 1 1 A\nATOM GLY A 1 2 2 A\nATOM SER B 2 1 1 B\nHETATM GOL C 3 . 301 A\n#\n"

// pdbWithSeqres is a PDB-format file with chain A of 4 SEQRES residues, 2 modeled.
const pdbWithSeqres = "SEQRES   1 A    4  ALA GLY LYS SER\n" +
	"ATOM      1  CA  ALA A   1       1.000   2.000   3.000  1.00 20.00           C\n" +
	"ATOM      2  CA  GLY A   2       1.000   2.000   3.000  1.00 20.00           C\n" +
	"TER       3      GLY A   2\n" +
	"END\n"

type failure struct {
	artifactID uuid.UUID
	message    string
	retryAt    *time.Time
}

type savedIndex struct {
	index  SavedIndex
	readAt time.Time
}

type fakeQueue struct {
	claims   []models.CoordinateIndexClaim
	fasta    map[uuid.UUID]models.ModelFileFASTA
	fastaErr error
	saved    map[uuid.UUID]savedIndex
	saveErr  error
	failures []failure
	// recentSince and recentArchive are what AddMissingSince was asked; requeueSince and
	// requeueNow what RequeueForNewEntries was asked.
	recentSince   time.Time
	recentArchive bool
	requeueSince  time.Time
	requeueNow    time.Time
	// pages are the fill's pages, served in order; pageAfter and pageArchive record each request.
	pages       []models.MissingSetsPage
	pageAfter   []uuid.UUID
	pageArchive []bool
	// pendingFill is what HasPendingFill answers.
	pendingFill bool
	maxAttempts int
}

func (q *fakeQueue) AddMissingSince(_ context.Context, since time.Time, archiveFiles bool) (int64, error) {
	q.recentSince = since
	q.recentArchive = archiveFiles
	return 0, nil
}

func (q *fakeQueue) AddFillAfter(_ context.Context, after uuid.UUID, _ int, archiveFiles bool) (models.MissingSetsPage, error) {
	q.pageAfter = append(q.pageAfter, after)
	q.pageArchive = append(q.pageArchive, archiveFiles)
	if len(q.pages) == 0 {
		return models.MissingSetsPage{}, nil
	}
	page := q.pages[0]
	q.pages = q.pages[1:]
	return page, nil
}

func (q *fakeQueue) HasPendingFill(context.Context) (bool, error) {
	return q.pendingFill, nil
}

func (q *fakeQueue) RequeueForNewEntries(_ context.Context, since time.Time, now time.Time) (int64, error) {
	q.requeueSince = since
	q.requeueNow = now
	return 0, nil
}

func (q *fakeQueue) ClaimDue(_ context.Context, _ time.Time, limit int, _ time.Duration, maxAttempts int) ([]models.CoordinateIndexClaim, error) {
	q.maxAttempts = maxAttempts
	taken := q.claims[:min(limit, len(q.claims))]
	q.claims = q.claims[len(taken):]
	return taken, nil
}

func (q *fakeQueue) RecordFailure(_ context.Context, artifactID uuid.UUID, message string, retryAt *time.Time) error {
	q.failures = append(q.failures, failure{artifactID: artifactID, message: message, retryAt: retryAt})
	return nil
}

// Save resolves the read against the file's FASTA records as DatabaseQueue.Save does inside its
// transaction.
func (q *fakeQueue) Save(_ context.Context, artifactID uuid.UUID, read cif.CoordinateIndex, readAt time.Time) (SavedIndex, error) {
	if q.saveErr != nil {
		return SavedIndex{}, q.saveErr
	}
	if q.fastaErr != nil {
		return SavedIndex{}, q.fastaErr
	}
	index, err := resolveIndex(read, q.fasta[artifactID])
	if err != nil {
		return SavedIndex{}, err
	}
	q.saved[artifactID] = savedIndex{index: index, readAt: readAt}
	return index, nil
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

var testNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func newTestJob(t *testing.T, queue *fakeQueue, files *fakeFiles, cfg Config) *CoordinateIndexJob {
	t.Helper()
	if queue.saved == nil {
		queue.saved = map[uuid.UUID]savedIndex{}
	}
	job, err := NewCoordinateIndexJob(queue, files, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	job.now = func() time.Time { return testNow }
	job.sleep = func(context.Context, time.Duration) error { return nil }
	return job
}

func testConfig() Config {
	return Config{ClaimFor: 4 * time.Minute, FileTimeout: 10 * time.Minute, RecentWindow: 48 * time.Hour, FillBatchSize: 2}
}

func storedClaim(key string, format *string, attempts int) models.CoordinateIndexClaim {
	uri := bucketPrefix + key
	return models.CoordinateIndexClaim{ArtifactID: uuid.New(), Name: key, URI: &uri, Format: format, Attempts: attempts}
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

// chainSummaries writes each chain as "chain length/source residues protein".
func chainSummaries(chains []models.IndexedChain) []string {
	summaries := make([]string, 0, len(chains))
	for _, chain := range chains {
		length, source := 0, models.ChainLengthSource("")
		if chain.DepositedLength != nil {
			length = *chain.DepositedLength
		}
		if chain.LengthSource != nil {
			source = *chain.LengthSource
		}
		summaries = append(summaries, fmt.Sprintf("%s %d/%s %d %t", chain.AuthAsymID, length, source, chain.ModeledResidues, chain.Protein))
	}
	return summaries
}

func Test_should_save_the_chains_when_stored_files_are_read(t *testing.T) {
	cases := map[string]struct {
		contents []byte
		format   *string
		want     []string
	}{
		"mmCIF declared as mmcif": {
			contents: []byte(mmcifWithTwoChains), format: ptr("mmcif"),
			want: []string{"A 3/entity_poly_seq 2 true", "B 0/ 1 true"},
		},
		"gzipped mmCIF, no format": {
			contents: gzipped(t, mmcifWithTwoChains),
			want:     []string{"A 3/entity_poly_seq 2 true", "B 0/ 1 true"},
		},
		"PDB format declared as pdb": {
			contents: []byte(pdbWithSeqres), format: ptr("pdb"),
			want: []string{"A 4/seqres 2 true"},
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			claim := storedClaim("e1/models/m1/artifacts/a1/model", testCase.format, 1)
			queue := &fakeQueue{claims: []models.CoordinateIndexClaim{claim}}
			files := &fakeFiles{contents: map[string][]byte{"e1/models/m1/artifacts/a1/model": testCase.contents}}
			job := newTestJob(t, queue, files, testConfig())

			// when
			err := job.Run(context.Background())

			// then
			require.NoError(t, err)
			assert.Empty(t, queue.failures)
			require.Contains(t, queue.saved, claim.ArtifactID)
			assert.Equal(t, testCase.want, chainSummaries(queue.saved[claim.ArtifactID].index.Chains))
			assert.Equal(t, testNow, queue.saved[claim.ArtifactID].readAt)
			assert.Equal(t, 0, queue.saved[claim.ArtifactID].index.Entries)
		})
	}
}

func Test_should_take_a_missing_length_from_the_entry_fasta_when_the_file_states_none(t *testing.T) {
	// given: chain A's length is in the file; chain B's only in the entry's FASTA records.
	claim := storedClaim("k", ptr("mmcif"), 1)
	queue := &fakeQueue{
		claims: []models.CoordinateIndexClaim{claim},
		fasta: map[uuid.UUID]models.ModelFileFASTA{claim.ArtifactID: {
			EntryIDs: []string{"entry-1"},
			Records: []models.FASTARecordLength{
				{EntryID: "entry-1", Header: "1ABC_1|Chain A|first", Length: 99},
				{EntryID: "entry-1", Header: "1ABC_2|Chains B, C|second", Length: 120},
			},
		}},
	}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithTwoChains)}}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(context.Background())

	// then: the file's own length wins for chain A.
	require.NoError(t, err)
	saved := queue.saved[claim.ArtifactID].index
	assert.Equal(t, []string{"A 3/entity_poly_seq 2 true", "B 120/fasta_auth_chain 1 true"}, chainSummaries(saved.Chains))
	assert.Equal(t, 1, saved.Entries)
	assert.Empty(t, saved.Warnings)
}

func Test_should_fail_for_good_when_no_retry_can_change_the_outcome(t *testing.T) {
	cases := map[string]struct {
		claim    models.CoordinateIndexClaim
		contents []byte
		want     string
	}{
		"no uri": {
			claim: models.CoordinateIndexClaim{ArtifactID: uuid.New(), Attempts: 1},
			want:  "locate the file: artifact has no uri",
		},
		"unknown declared format": {
			claim:    storedClaim("k", ptr("xyz"), 1),
			contents: []byte(mmcifWithTwoChains),
			want:     `open the structure: declared format: unknown model file format "xyz"`,
		},
		"neither mmCIF nor PDB": {
			claim:    storedClaim("k", nil, 1),
			contents: []byte("hello\n"),
			want:     "open the structure: first bytes: the file starts like neither mmCIF nor PDB format",
		},
		"archive file not copied": {
			claim: models.CoordinateIndexClaim{
				ArtifactID: uuid.New(), URI: ptr("https://files.rcsb.org/download/1ABC.cif"),
				CopyStatus: ptr(models.CopyStatusOverLimit), Attempts: 1,
			},
			want: "locate the file: the file is not in our bucket, and the checksum job kept no copy of it (copy status over_limit)",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			queue := &fakeQueue{claims: []models.CoordinateIndexClaim{testCase.claim}}
			files := &fakeFiles{contents: map[string][]byte{"k": testCase.contents}}
			job := newTestJob(t, queue, files, testConfig())

			// when
			err := job.Run(context.Background())

			// then
			require.NoError(t, err)
			require.Len(t, queue.failures, 1)
			assert.Equal(t, testCase.want, queue.failures[0].message)
			assert.Nil(t, queue.failures[0].retryAt)
		})
	}
}

func Test_should_read_the_checksum_copy_when_an_archive_file_was_copied_into_our_bucket(t *testing.T) {
	// given
	claim := models.CoordinateIndexClaim{
		ArtifactID: uuid.New(), URI: ptr("https://files.rcsb.org/download/1ABC.cif"), Format: ptr("cif"),
		CopyStatus: ptr(models.CopyStatusCopied), StoredURI: ptr(bucketPrefix + "copies/1ABC.cif"), Attempts: 1,
	}
	queue := &fakeQueue{claims: []models.CoordinateIndexClaim{claim}}
	files := &fakeFiles{contents: map[string][]byte{"copies/1ABC.cif": []byte(mmcifWithTwoChains)}}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Empty(t, queue.failures)
	assert.Len(t, queue.saved[claim.ArtifactID].index.Chains, 2)
}

func Test_should_retry_after_a_doubling_delay_and_then_fail_when_the_file_cannot_be_opened(t *testing.T) {
	// given: the same broken file at its first, second and third attempt.
	queue := &fakeQueue{claims: []models.CoordinateIndexClaim{
		storedClaim("k", ptr("mmcif"), 1),
		storedClaim("k", ptr("mmcif"), 2),
		storedClaim("k", ptr("mmcif"), 3),
	}}
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
	assert.Equal(t, maxAttempts, queue.maxAttempts)
}

func Test_should_retry_when_the_entry_fasta_records_cannot_be_read(t *testing.T) {
	// given
	queue := &fakeQueue{claims: []models.CoordinateIndexClaim{storedClaim("k", ptr("mmcif"), 1)}, fastaErr: errors.New("timeout")}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithTwoChains)}}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	require.Len(t, queue.failures, 1)
	assert.Equal(t, "save the chains: timeout", queue.failures[0].message)
	assert.NotNil(t, queue.failures[0].retryAt)
	assert.Empty(t, queue.saved)
}

func Test_should_fail_when_the_coordinates_are_cut_off_after_the_last_retry(t *testing.T) {
	// given: the file ends inside a row, at its last attempt.
	queue := &fakeQueue{claims: []models.CoordinateIndexClaim{storedClaim("k", ptr("mmcif"), maxAttempts)}}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(strings.TrimSuffix(mmcifWithTwoChains, "301 A\n#\n"))}}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	require.Len(t, queue.failures, 1)
	assert.Contains(t, queue.failures[0].message, "ended inside a row")
	assert.Nil(t, queue.failures[0].retryAt)
}

func Test_should_count_a_refused_save_as_a_failed_attempt_when_the_database_rejects_the_chains(t *testing.T) {
	// given
	queue := &fakeQueue{claims: []models.CoordinateIndexClaim{storedClaim("k", ptr("mmcif"), 1)}, saveErr: errors.New("invalid byte sequence")}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithTwoChains)}}
	job := newTestJob(t, queue, files, testConfig())

	// when
	err := job.Run(context.Background())

	// then: the run goes on, and the set is retried later.
	require.NoError(t, err)
	require.Len(t, queue.failures, 1)
	assert.Equal(t, "save the chains: invalid byte sequence", queue.failures[0].message)
	assert.Equal(t, ptr(testNow.Add(10*time.Minute)), queue.failures[0].retryAt)
}

func Test_should_stop_claiming_when_the_run_reaches_its_file_cap(t *testing.T) {
	// given
	queue := &fakeQueue{claims: []models.CoordinateIndexClaim{
		storedClaim("k", ptr("mmcif"), 1),
		storedClaim("k", ptr("mmcif"), 1),
		storedClaim("k", ptr("mmcif"), 1),
	}}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithTwoChains)}}
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
	queue := &fakeQueue{claims: []models.CoordinateIndexClaim{storedClaim("k", ptr("mmcif"), 1)}}
	files := &fakeFiles{contents: map[string][]byte{"k": []byte(mmcifWithTwoChains)}}
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
		"negative file cap":      withChange(func(cfg *Config) { cfg.MaxFilesPerRun = -1 }),
		"no claiming time":       withChange(func(cfg *Config) { cfg.ClaimFor = 0 }),
		"no file timeout":        withChange(func(cfg *Config) { cfg.FileTimeout = 0 }),
		"no recent window":       withChange(func(cfg *Config) { cfg.RecentWindow = 0 }),
		"no fill batch size":     withChange(func(cfg *Config) { cfg.FillBatchSize = 0 }),
		"negative batch size":    withChange(func(cfg *Config) { cfg.FillBatchSize = -5 }),
		"negative claiming time": withChange(func(cfg *Config) { cfg.ClaimFor = -time.Minute }),
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, err := NewCoordinateIndexJob(&fakeQueue{}, &fakeFiles{}, cfg, nil)

			// then
			require.Error(t, err)
		})
	}
}

func Test_should_queue_recent_files_and_new_fasta_over_the_recent_window_when_a_run_starts(t *testing.T) {
	// given
	queue := &fakeQueue{}
	cfg := testConfig()
	cfg.ArchiveFiles = true
	job := newTestJob(t, queue, &fakeFiles{}, cfg)

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(-48*time.Hour), queue.recentSince)
	assert.True(t, queue.recentArchive)
	assert.Equal(t, testNow.Add(-48*time.Hour), queue.requeueSince, "model revisions of the window")
	assert.Equal(t, testNow, queue.requeueNow)
	assert.Empty(t, queue.pageAfter, "the fill is off")
}

func Test_should_page_through_every_artifact_and_pause_between_pages_when_the_fill_is_on(t *testing.T) {
	// given: two full pages of two artifacts, then a short one.
	first, second := uuid.New(), uuid.New()
	queue := &fakeQueue{pages: []models.MissingSetsPage{
		{Scanned: 2, Added: 1, LastArtifactID: first},
		{Scanned: 2, Added: 2, LastArtifactID: second},
		{Scanned: 1, Added: 0, LastArtifactID: uuid.New()},
	}}
	cfg := testConfig()
	cfg.Fill = true
	cfg.ArchiveFiles = true
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
	assert.Equal(t, []bool{true, true, true}, queue.pageArchive)
	assert.Equal(t, 2, pauses, "a pause after each full page")
}

func Test_should_skip_the_fill_scan_when_sets_it_queued_are_still_pending(t *testing.T) {
	// given
	queue := &fakeQueue{pendingFill: true, pages: []models.MissingSetsPage{{Scanned: 1, Added: 1}}}
	cfg := testConfig()
	cfg.Fill = true
	job := newTestJob(t, queue, &fakeFiles{}, cfg)

	// when
	err := job.Run(context.Background())

	// then
	require.NoError(t, err)
	assert.Empty(t, queue.pageAfter)
}
