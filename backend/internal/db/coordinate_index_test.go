package db_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

// indexChain is one chain row of a model file: author chain, deposited length (0 unknown) and
// whether it is a protein chain.
type indexChain struct {
	chain   string
	length  int
	protein bool
}

// indexChains gives a model's coordinate file a coordinate index set with status and chains.
func (f *modelSearchTestDB) indexChains(t *testing.T, model searchModel, status string, chains ...indexChain) {
	t.Helper()
	if status == "done" {
		f.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at)
		           values ($1, 'done', '1.0.0', now())`, model.coordinateFile)
	} else {
		f.exec(t, `insert into coordinate_index_sets (artifact_id, status) values ($1, $2)`, model.coordinateFile, status)
	}
	for _, chain := range chains {
		var length, source any
		if chain.length > 0 {
			length, source = chain.length, "fasta_auth_chain"
		}
		f.exec(t, `insert into coordinate_index_chains (artifact_id, auth_asym_id, deposited_length, length_source,
		                                                 modeled_residues, protein, protein_source)
		           values ($1, $2, $3, $4, 10, $5, 'residue_majority')`, model.coordinateFile, chain.chain, length, source, chain.protein)
	}
}

// longChains returns count protein chains of the given length, named A, B, C…
func longChains(count int, length int) []indexChain {
	chains := make([]indexChain, 0, count)
	for index := range count {
		chains = append(chains, indexChain{chain: string(rune('A' + index)), length: length, protein: true})
	}
	return chains
}

// storedModelArtifact makes a model artifact in our bucket (a URI in its own folder) created at
// createdAt, without the sets the API's Create adds.
func (f *modelSearchTestDB) storedModelArtifact(t *testing.T, createdAt time.Time) uuid.UUID {
	t.Helper()
	artifactID := uuid.New()
	uri := fmt.Sprintf("https://bucket.example.org/e1/models/m1/artifacts/%s/model.cif", artifactID)
	f.exec(t, `insert into artifacts (id, name, level, type, uri, created_by, created_at)
	           values ($1, 'model.cif', 'L2', 'model', $2, $3, $4)`, artifactID, uri, f.userID, createdAt)
	return artifactID
}

// copiedArchiveArtifact makes an archive model artifact on RCSB, created at createdAt, that the
// checksum job copied at copiedAt.
func (f *modelSearchTestDB) copiedArchiveArtifact(t *testing.T, createdAt time.Time, copiedAt time.Time) uuid.UUID {
	t.Helper()
	artifactID := uuid.New()
	f.exec(t, `insert into artifacts (id, name, level, type, uri, created_by, created_at)
	           values ($1, '1abc.cif', 'L2', 'model', 'https://files.rcsb.org/download/1ABC.cif', $2, $3)`,
		artifactID, f.userID, createdAt)
	f.exec(t, `insert into artifact_checksums (artifact_id, status, sha256, size_bytes, checked_at, copy_status, stored_uri)
	           values ($1, 'verified', $2, 100, $3, 'copied', 'https://bucket.example.org/copies/1ABC.cif')`,
		artifactID, strings.Repeat("a", 64), copiedAt)
	return artifactID
}

// saveChains saves a read of a model file whose chains are already resolved.
func (f *modelSearchTestDB) saveChains(t *testing.T, artifactID uuid.UUID, readAt time.Time, warnings []string, chains ...models.IndexedChain) error {
	t.Helper()
	return f.database.SaveCoordinateIndex(context.Background(), db.SaveCoordinateIndexParams{
		ArtifactID: artifactID, ReaderVersion: "1.0.0", ReadAt: readAt,
		Resolve: func(models.ModelFileFASTA) (db.ResolvedIndex, error) {
			return db.ResolvedIndex{Chains: chains, Warnings: warnings}, nil
		},
	})
}

func (f *modelSearchTestDB) coordinateIndexSet(t *testing.T, artifactID uuid.UUID) *models.CoordinateIndexSet {
	t.Helper()
	set, err := f.database.CoordinateIndex.Get(context.Background(), artifactID)
	require.NoError(t, err)
	return set
}

func (f *modelSearchTestDB) hasCoordinateIndexSet(t *testing.T, artifactID uuid.UUID) bool {
	t.Helper()
	_, err := f.database.CoordinateIndex.Get(context.Background(), artifactID)
	if err == nil {
		return true
	}
	require.ErrorIs(t, err, db.ErrCoordinateIndexSetNotFound)
	return false
}

// fastaRecord stores one FASTA record in an entry revision.
func (f *modelSearchTestDB) fastaRecord(t *testing.T, entryRevisionID uuid.UUID, header string, length int, createdAt time.Time) {
	t.Helper()
	fasta := f.artifact(t, models.ArtifactLevelL0, models.ArtifactTypeFASTA)
	sequence := ""
	for range length {
		sequence += "M"
	}
	f.exec(t, `insert into protein_sequences (id, entry_revision_id, source_artifact_id, record_index, header, sequence, created_at)
	           values ($1, $2, $3, 0, $4, $5, $6)`, uuid.New(), entryRevisionID, fasta, header, sequence, createdAt)
}

func Test_should_queue_a_model_file_in_our_bucket_when_its_artifact_is_created(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	create := func(artifactType models.ArtifactType, uri func(uuid.UUID) string) uuid.UUID {
		id := uuid.New()
		address := uri(id)
		_, err := fixture.database.Artifacts.Create(ctx, models.Artifact{
			ID: id, Name: "file", Level: models.ArtifactLevelL2, Type: artifactType, URI: &address,
			Metadata: map[string]any{}, CreatedBy: fixture.userID, CreatedAt: time.Now().UTC(),
		})
		require.NoError(t, err)
		return id
	}
	inBucket := func(id uuid.UUID) string {
		return fmt.Sprintf("https://bucket.example.org/e1/models/m1/artifacts/%s/model.cif", id)
	}

	// when
	upload := create(models.ArtifactTypeModel, inBucket)
	archive := create(models.ArtifactTypeModel, func(uuid.UUID) string { return "https://files.rcsb.org/download/1ABC.cif" })
	notModel := create(models.ArtifactTypeOther, inBucket)

	// then
	set := fixture.coordinateIndexSet(t, upload)
	assert.Equal(t, models.CoordinateIndexStatusPending, set.Status)
	assert.False(t, set.Fill)
	assert.Empty(t, set.Chains)
	assert.False(t, fixture.hasCoordinateIndexSet(t, archive))
	assert.False(t, fixture.hasCoordinateIndexSet(t, notModel))
}

func Test_should_claim_uploads_before_fill_rows_when_both_are_due(t *testing.T) {
	// given: a fill row due an hour ago, an upload due a minute ago.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	fillFile, upload := fixture.storedModelArtifact(t, now), fixture.storedModelArtifact(t, now)
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, fill, next_attempt_at) values ($1, true, $2)`, fillFile, now.Add(-time.Hour))
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, next_attempt_at) values ($1, $2)`, upload, now.Add(-time.Minute))

	// when
	first, err := fixture.database.CoordinateIndex.ClaimDue(ctx, now, 1, 15*time.Minute, 3)
	require.NoError(t, err)
	second, err := fixture.database.CoordinateIndex.ClaimDue(ctx, now, 1, 15*time.Minute, 3)
	require.NoError(t, err)
	third, err := fixture.database.CoordinateIndex.ClaimDue(ctx, now, 1, 15*time.Minute, 3)
	require.NoError(t, err)

	// then: each is leased once claimed.
	require.Len(t, first, 1)
	assert.Equal(t, upload, first[0].ArtifactID)
	assert.Equal(t, 1, first[0].Attempts)
	require.NotNil(t, first[0].URI)
	require.Len(t, second, 1)
	assert.Equal(t, fillFile, second[0].ArtifactID)
	assert.Empty(t, third)
}

func Test_should_claim_the_checksum_copy_with_an_archive_file_when_it_was_copied(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	now := time.Now().UTC()
	archive := fixture.copiedArchiveArtifact(t, now, now)
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, archive)

	// when
	claims, err := fixture.database.CoordinateIndex.ClaimDue(context.Background(), now.Add(time.Second), 5, time.Minute, 3)

	// then
	require.NoError(t, err)
	require.Len(t, claims, 1)
	assert.Equal(t, ptr(models.CopyStatusCopied), claims[0].CopyStatus)
	assert.Equal(t, ptr("https://bucket.example.org/copies/1ABC.cif"), claims[0].StoredURI)
}

func Test_should_fail_a_coordinate_index_set_instead_of_claiming_it_when_its_reads_never_came_back(t *testing.T) {
	// given: claimed three times, and due again.
	fixture := newModelSearchTestDB(t)
	now := time.Now().UTC()
	artifactID := fixture.storedModelArtifact(t, now)
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, attempts, next_attempt_at) values ($1, 3, $2)`, artifactID, now.Add(-time.Minute))

	// when
	claims, err := fixture.database.CoordinateIndex.ClaimDue(context.Background(), now, 5, time.Minute, 3)

	// then
	require.NoError(t, err)
	assert.Empty(t, claims)
	set := fixture.coordinateIndexSet(t, artifactID)
	assert.Equal(t, models.CoordinateIndexStatusFailed, set.Status)
	require.NotNil(t, set.LastError)
	assert.Contains(t, *set.LastError, "3 times")
}

func Test_should_give_recent_model_files_a_set_when_they_have_none(t *testing.T) {
	// given: an upload from an hour ago and one from three days ago; an archive file created a
	// month ago and copied an hour ago, and one copied three days ago.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	recent := fixture.storedModelArtifact(t, now.Add(-time.Hour))
	old := fixture.storedModelArtifact(t, now.Add(-72*time.Hour))
	copiedLate := fixture.copiedArchiveArtifact(t, now.Add(-30*24*time.Hour), now.Add(-time.Hour))
	copiedLongAgo := fixture.copiedArchiveArtifact(t, now.Add(-30*24*time.Hour), now.Add(-72*time.Hour))

	// when: without archive files, then with them, then again.
	withoutArchive, err := fixture.database.CoordinateIndex.AddMissingSince(ctx, now.Add(-48*time.Hour), false)
	require.NoError(t, err)
	withArchive, err := fixture.database.CoordinateIndex.AddMissingSince(ctx, now.Add(-48*time.Hour), true)
	require.NoError(t, err)
	again, err := fixture.database.CoordinateIndex.AddMissingSince(ctx, now.Add(-48*time.Hour), true)
	require.NoError(t, err)

	// then: archive files are found by when their copy was recorded.
	assert.Equal(t, []int64{1, 1, 0}, []int64{withoutArchive, withArchive, again})
	assert.True(t, fixture.hasCoordinateIndexSet(t, recent))
	assert.True(t, fixture.hasCoordinateIndexSet(t, copiedLate))
	assert.False(t, fixture.hasCoordinateIndexSet(t, old))
	assert.False(t, fixture.hasCoordinateIndexSet(t, copiedLongAgo))
	assert.False(t, fixture.coordinateIndexSet(t, recent).Fill)
}

func Test_should_queue_fill_rows_page_by_page_when_the_fill_runs(t *testing.T) {
	// given: three uploads without a set, one with a done set, a copied archive file and a FASTA
	// file, all from long ago.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-30 * 24 * time.Hour)
	uploads := []uuid.UUID{fixture.storedModelArtifact(t, old), fixture.storedModelArtifact(t, old), fixture.storedModelArtifact(t, old)}
	done := fixture.storedModelArtifact(t, old)
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at) values ($1, 'done', '1.0.0', now())`, done)
	archive := fixture.copiedArchiveArtifact(t, old, old)
	fixture.artifact(t, models.ArtifactLevelL0, models.ArtifactTypeFASTA)

	// when: pages of 2 artifacts, without archive files.
	added := int64(0)
	after := uuid.Nil
	for {
		page, err := fixture.database.CoordinateIndex.AddFillAfter(ctx, after, 2, false)
		require.NoError(t, err)
		added += page.Added
		if page.Scanned < 2 {
			break
		}
		after = page.LastArtifactID
	}
	pending, err := fixture.database.CoordinateIndex.HasPendingFill(ctx)
	require.NoError(t, err)

	// then
	assert.Equal(t, int64(3), added)
	for _, upload := range uploads {
		set := fixture.coordinateIndexSet(t, upload)
		assert.True(t, set.Fill)
		assert.Equal(t, models.CoordinateIndexStatusPending, set.Status)
	}
	assert.Equal(t, models.CoordinateIndexStatusDone, fixture.coordinateIndexSet(t, done).Status)
	assert.False(t, fixture.hasCoordinateIndexSet(t, archive))
	assert.True(t, pending)

	// and with archive files, the archive file is added too.
	page, err := fixture.database.CoordinateIndex.AddFillAfter(ctx, uuid.Nil, 100, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Added)
	assert.True(t, fixture.coordinateIndexSet(t, archive).Fill)
}

func Test_should_report_no_pending_fill_when_only_uploads_are_pending(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	upload := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, upload)

	// when
	pending, err := fixture.database.CoordinateIndex.HasPendingFill(context.Background())

	// then
	require.NoError(t, err)
	assert.False(t, pending)
}

func Test_should_keep_a_set_pending_until_its_retry_or_fail_it_when_a_read_fails(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	retried, failed := fixture.storedModelArtifact(t, now), fixture.storedModelArtifact(t, now)
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1), ($2)`, retried, failed)
	retryAt := now.Add(10 * time.Minute)

	// when
	require.NoError(t, fixture.database.CoordinateIndex.RecordFailure(ctx, retried, "connection reset", &retryAt))
	require.NoError(t, fixture.database.CoordinateIndex.RecordFailure(ctx, failed, "not a structure file", nil))
	missing := fixture.database.CoordinateIndex.RecordFailure(ctx, uuid.New(), "gone", nil)

	// then
	retriedSet := fixture.coordinateIndexSet(t, retried)
	assert.Equal(t, models.CoordinateIndexStatusPending, retriedSet.Status)
	assert.Equal(t, ptr("connection reset"), retriedSet.LastError)
	failedSet := fixture.coordinateIndexSet(t, failed)
	assert.Equal(t, models.CoordinateIndexStatusFailed, failedSet.Status)
	assert.Equal(t, ptr("not a structure file"), failedSet.LastError)
	require.ErrorIs(t, missing, db.ErrCoordinateIndexSetNotFound)
}

func Test_should_replace_the_chains_and_finish_the_set_when_a_read_is_saved(t *testing.T) {
	// given: a set read once, then queued again.
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)
	readAt := time.Now().UTC().Truncate(time.Microsecond)
	length := 141
	source := models.ChainLengthSourceEntityPolySeq
	require.NoError(t, fixture.saveChains(t, artifactID, readAt, []string{"a warning"},
		models.IndexedChain{AuthAsymID: "A", EntityID: "1", DepositedLength: &length, LengthSource: &source, ModeledResidues: 140,
			Protein: true, ProteinSource: models.ProteinSourceEntityPolyType},
		models.IndexedChain{AuthAsymID: "B", ModeledResidues: 12, ProteinSource: models.ProteinSourceResidueMajority},
	))
	fixture.exec(t, `update coordinate_index_sets set status = 'pending' where artifact_id = $1`, artifactID)

	// when
	second := models.IndexedChain{AuthAsymID: "C", ModeledResidues: 5, Protein: true, ProteinSource: models.ProteinSourceResidueMajority}
	require.NoError(t, fixture.saveChains(t, artifactID, readAt.Add(time.Minute), nil, second))
	again := fixture.saveChains(t, artifactID, readAt.Add(time.Minute), nil, second)

	// then: the second read's chains only; a set no longer pending is not saved twice.
	set := fixture.coordinateIndexSet(t, artifactID)
	assert.Equal(t, models.CoordinateIndexStatusDone, set.Status)
	assert.Equal(t, ptr("1.0.0"), set.ReaderVersion)
	assert.Empty(t, set.Warnings)
	assert.Empty(t, set.FASTAEntryIDs, "no model uses the file")
	assert.Equal(t, []models.IndexedChain{second}, set.Chains)
	require.ErrorIs(t, again, db.ErrCoordinateIndexSetNotFound)
}

func Test_should_store_each_chain_field_when_a_read_is_saved(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)
	length := 230
	source := models.ChainLengthSourceFASTALabelChain
	chain := models.IndexedChain{AuthAsymID: "A", EntityID: "2", DepositedLength: &length, LengthSource: &source, ModeledResidues: 228,
		Protein: true, ProteinSource: models.ProteinSourceEntityPolyType}

	// when
	err := fixture.saveChains(t, artifactID, time.Now().UTC(), nil, chain)

	// then
	require.NoError(t, err)
	assert.Equal(t, []models.IndexedChain{chain}, fixture.coordinateIndexSet(t, artifactID).Chains)
}

func Test_should_resolve_the_chains_with_the_entry_fasta_records_when_a_read_is_saved(t *testing.T) {
	// given: the file's model belongs to an entry with one active FASTA record.
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	model := fixture.model(t, entry, map[string]any{})
	created := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	fixture.fastaRecord(t, entry.revisionID, "1ABC_1|Chain A|alpha", 141, created)
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, model.coordinateFile)
	var seen models.ModelFileFASTA

	// when
	err := fixture.database.SaveCoordinateIndex(context.Background(), db.SaveCoordinateIndexParams{
		ArtifactID: model.coordinateFile, ReaderVersion: "1.0.0", ReadAt: time.Now().UTC(),
		Resolve: func(fasta models.ModelFileFASTA) (db.ResolvedIndex, error) {
			seen = fasta
			return db.ResolvedIndex{Chains: []models.IndexedChain{{
				AuthAsymID: "A", ModeledResidues: 3, Protein: true, ProteinSource: models.ProteinSourceResidueMajority,
			}}}, nil
		},
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{entry.id}, seen.EntryIDs)
	assert.Equal(t, []models.FASTARecordLength{{EntryID: entry.id, Header: "1ABC_1|Chain A|alpha", Length: 141}}, seen.Records)
	assert.Equal(t, []string{entry.id}, fixture.coordinateIndexSet(t, model.coordinateFile).FASTAEntryIDs)
}

func Test_should_store_nothing_when_resolving_the_chains_fails(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)

	// when
	err := fixture.database.SaveCoordinateIndex(context.Background(), db.SaveCoordinateIndexParams{
		ArtifactID: artifactID, ReaderVersion: "1.0.0", ReadAt: time.Now().UTC(),
		Resolve: func(models.ModelFileFASTA) (db.ResolvedIndex, error) {
			return db.ResolvedIndex{}, fmt.Errorf("unknown chain length source")
		},
	})

	// then
	require.ErrorContains(t, err, "unknown chain length source")
	assert.Equal(t, models.CoordinateIndexStatusPending, fixture.coordinateIndexSet(t, artifactID).Status)
}

func Test_should_list_the_active_fasta_records_of_the_entry_when_a_model_uses_the_file(t *testing.T) {
	// given: the active revision has two records; an archived revision has another.
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	model := fixture.model(t, entry, map[string]any{})
	older, newer := time.Now().UTC().Add(-2*time.Hour).Truncate(time.Microsecond), time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	fixture.fastaRecord(t, entry.revisionID, "1ABC_1|Chain A|alpha", 141, older)
	fixture.fastaRecord(t, entry.revisionID, "1ABC_2|Chain B|beta", 146, newer)
	archivedID := uuid.New()
	fixture.entryRevision(t, entry, archivedID, 1, "archived", map[string]any{})
	fixture.fastaRecord(t, archivedID, "1ABC_1|Chain A|old alpha", 99, newer.Add(time.Minute))

	// when
	fasta, err := fixture.database.CoordinateIndex.ModelFileFASTA(context.Background(), model.coordinateFile)
	unused, unusedErr := fixture.database.CoordinateIndex.ModelFileFASTA(context.Background(), uuid.New())

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{entry.id}, fasta.EntryIDs)
	assert.ElementsMatch(t, []models.FASTARecordLength{
		{EntryID: entry.id, Header: "1ABC_1|Chain A|alpha", Length: 141},
		{EntryID: entry.id, Header: "1ABC_2|Chain B|beta", Length: 146},
	}, fasta.Records)
	require.NoError(t, unusedErr)
	assert.Empty(t, unused.EntryIDs)
}

func Test_should_queue_a_done_set_again_when_the_entries_of_its_models_changed_since_the_read(t *testing.T) {
	// given: files read an hour ago, with the entries their models had then.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	markDone := func(artifactID uuid.UUID, entryIDs ...string) {
		fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, fill, fasta_entry_ids)
		                 values ($1, 'done', '1.0.0', $2, true, $3)`, artifactID, now.Add(-time.Hour), pq.Array(append([]string{}, entryIDs...)))
	}
	// readBeforeModel: no model used the file when it was read.
	readBeforeModel := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{})
	markDone(readBeforeModel.coordinateFile)
	// unchanged: a new revision of its model keeps the file and the entry.
	unchangedEntry := fixture.entry(t, map[string]any{})
	unchanged := fixture.model(t, unchangedEntry, map[string]any{})
	markDone(unchanged.coordinateFile, unchangedEntry.id)
	// sharedEntry: read with its first model; a new model of another entry links the same file.
	firstEntry := fixture.entry(t, map[string]any{})
	first := fixture.model(t, firstEntry, map[string]any{})
	markDone(first.coordinateFile, firstEntry.id)
	second := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{})
	fixture.exec(t, `update model_revisions set primary_artifact_id = $2 where id = $1`, second.revisionID, first.coordinateFile)
	// switched: an existing model's new revision switched to a file read with no model.
	switchedEntry := fixture.entry(t, map[string]any{})
	switched := fixture.model(t, switchedEntry, map[string]any{})
	secondaryFile := fixture.artifact(t, models.ArtifactLevelL2, models.ArtifactTypeModel)
	markDone(secondaryFile)
	fixture.exec(t, `update model_revisions set primary_artifact_id = $2 where id = $1`, switched.revisionID, secondaryFile)
	// oldRevision: no model used the file at the read, but the revision is older than the window.
	oldRevision := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{})
	fixture.exec(t, `update model_revisions set created_at = $2 where id = $1`, oldRevision.revisionID, now.Add(-72*time.Hour))
	markDone(oldRevision.coordinateFile)

	// when
	queued, err := fixture.database.CoordinateIndex.RequeueForNewEntries(ctx, now.Add(-48*time.Hour), now)
	require.NoError(t, err)
	again, err := fixture.database.CoordinateIndex.RequeueForNewEntries(ctx, now.Add(-48*time.Hour), now)
	require.NoError(t, err)

	// then: queued as uploads, not fill rows; a pending set is not queued twice.
	assert.Equal(t, int64(3), queued)
	assert.Equal(t, int64(0), again)
	for _, artifactID := range []uuid.UUID{readBeforeModel.coordinateFile, first.coordinateFile, secondaryFile} {
		set := fixture.coordinateIndexSet(t, artifactID)
		assert.Equal(t, models.CoordinateIndexStatusPending, set.Status)
		assert.False(t, set.Fill)
		assert.Equal(t, 0, set.Attempts)
	}
	for _, model := range []searchModel{unchanged, oldRevision} {
		assert.Equal(t, models.CoordinateIndexStatusDone, fixture.coordinateIndexSet(t, model.coordinateFile).Status, model.id)
	}
}

func Test_should_queue_every_read_file_of_the_entry_again_when_its_fasta_records_change(t *testing.T) {
	// given: two models of the entry, read or being read; a failed one; another entry's model.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	entry := fixture.entry(t, map[string]any{})
	done := fixture.model(t, entry, map[string]any{})
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, fill, attempts)
	                 values ($1, 'done', '1.0.0', now(), true, 1)`, done.coordinateFile)
	reading := fixture.model(t, entry, map[string]any{})
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, attempts, next_attempt_at) values ($1, 2, now() + interval '15 minutes')`,
		reading.coordinateFile)
	failed := fixture.model(t, entry, map[string]any{})
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, last_error) values ($1, 'failed', 'not a structure file')`,
		failed.coordinateFile)
	other := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{})
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at) values ($1, 'done', '1.0.0', now())`,
		other.coordinateFile)

	// when
	queued, err := fixture.database.CoordinateIndex.RequeueEntry(ctx, entry.id)

	// then: due now, ahead of the fill; the failed set stays failed.
	require.NoError(t, err)
	assert.Equal(t, int64(2), queued)
	for _, model := range []searchModel{done, reading} {
		set := fixture.coordinateIndexSet(t, model.coordinateFile)
		assert.Equal(t, models.CoordinateIndexStatusPending, set.Status, model.id)
		assert.False(t, set.Fill, model.id)
		assert.Equal(t, 0, set.Attempts, model.id)
	}
	claims, err := fixture.database.CoordinateIndex.ClaimDue(ctx, time.Now().UTC().Add(time.Second), 5, time.Minute, 3)
	require.NoError(t, err)
	assert.Len(t, claims, 2)
	assert.Equal(t, models.CoordinateIndexStatusFailed, fixture.coordinateIndexSet(t, failed.coordinateFile).Status)
	assert.Equal(t, models.CoordinateIndexStatusDone, fixture.coordinateIndexSet(t, other.coordinateFile).Status)
}

func Test_should_match_models_by_their_own_protein_chains_when_models_are_searched_by_chains(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	// Example 2's chain part: more than four protein chains longer than 300.
	fiveLong := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, fiveLong, "done", longChains(5, 400)...)
	// Five long chains and two short ones.
	fiveLongTwoShort := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, fiveLongTwoShort, "done", append(longChains(5, 400),
		indexChain{chain: "F", length: 120, protein: true}, indexChain{chain: "G", length: 80, protein: true})...)
	// Four long chains and a short one.
	fourLong := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, fourLong, "done", append(longChains(4, 400), indexChain{chain: "E", length: 300, protein: true})...)
	// Five long chains, one of them DNA.
	withDNA := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, withDNA, "done", append(longChains(4, 400), indexChain{chain: "E", length: 400})...)
	// Five protein chains of unknown length.
	unknownLength := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, unknownLength, "done", longChains(5, 0)...)
	// Three long chains and one of unknown length.
	oneUnknown := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, oneUnknown, "done", append(longChains(3, 400), indexChain{chain: "D", protein: true})...)
	// A file the job has not read, and one it is reading again after an earlier read.
	notRead := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, notRead, "pending")
	readingAgain := fixture.model(t, entry, map[string]any{})
	fixture.indexChains(t, readingAgain, "pending", longChains(5, 400)...)
	// Another entry's model with two long chains.
	twoLong := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{})
	fixture.indexChains(t, twoLong, "done", longChains(2, 350)...)

	atLeast := func(minChains int, minLength, maxLength *int) db.ModelSearch {
		return db.ModelSearch{ProteinChains: &db.ProteinChainFilter{
			Mode: db.ProteinChainsAtLeast, MinChains: minChains, MinLength: minLength, MaxLength: maxLength,
		}}
	}
	every := func(minChains int, minLength, maxLength *int) db.ModelSearch {
		return db.ModelSearch{ProteinChains: &db.ProteinChainFilter{
			Mode: db.ProteinChainsEvery, MinChains: minChains, MinLength: minLength, MaxLength: maxLength,
		}}
	}
	withCompound := atLeast(2, ptr(301), nil)
	withCompound.Compounds = []string{"ADP"}
	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "example 2's chain part: shorter chains may exist, four are not enough", search: atLeast(5, ptr(301), nil),
			want: []string{fiveLong.id, fiveLongTwoShort.id, readingAgain.id}},
		{name: "without a length every protein chain counts", search: atLeast(5, nil, nil),
			want: []string{fiveLong.id, fiveLongTwoShort.id, fourLong.id, unknownLength.id, readingAgain.id}},
		{name: "length bounds are inclusive", search: atLeast(1, ptr(300), ptr(300)),
			want: []string{fourLong.id}},
		{name: "a maximum alone", search: atLeast(2, nil, ptr(350)),
			want: []string{fiveLongTwoShort.id, twoLong.id}},
		{name: "chains of different models never add up", search: atLeast(8, nil, nil),
			want: nil},
		{name: "with another filter", search: withCompound,
			want: nil},
		{name: "every protein chain longer than 300, at least five", search: every(5, ptr(301), nil),
			want: []string{fiveLong.id, readingAgain.id}},
		{name: "every leaves out non-protein chains and rules out unknown lengths", search: every(2, ptr(301), nil),
			want: []string{fiveLong.id, withDNA.id, readingAgain.id, twoLong.id}},
		{name: "every with a maximum", search: every(1, nil, ptr(350)),
			want: []string{twoLong.id}},
		{name: "a maximum past the integer range", search: atLeast(5, nil, ptr(math.MaxInt32+1)),
			want: []string{fiveLong.id, fiveLongTwoShort.id, fourLong.id, readingAgain.id}},
		{name: "every with bounds past the integer range", search: every(5, ptr(301), ptr(math.MaxInt64)),
			want: []string{fiveLong.id, readingAgain.id}},
	})
}

func Test_should_refuse_a_protein_chain_filter_when_its_values_are_out_of_range(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	cases := map[string]db.ProteinChainFilter{
		"no chains":                {Mode: db.ProteinChainsAtLeast, MinChains: 0},
		"zero length":              {Mode: db.ProteinChainsAtLeast, MinChains: 1, MinLength: ptr(0)},
		"empty range":              {Mode: db.ProteinChainsAtLeast, MinChains: 1, MinLength: ptr(300), MaxLength: ptr(200)},
		"negative cap":             {Mode: db.ProteinChainsAtLeast, MinChains: 1, MaxLength: ptr(-3)},
		"every without a bound":    {Mode: db.ProteinChainsEvery, MinChains: 2},
		"unknown mode":             {Mode: "most", MinChains: 1, MinLength: ptr(10)},
		"mode left out by mistake": {MinChains: 1, MinLength: ptr(10)},
	}
	for name, filter := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, err := fixture.database.Models.List(context.Background(), activeSearchFilters(db.ModelSearch{ProteinChains: &filter}))

			// then
			require.ErrorIs(t, err, db.ErrInvalidModelSearch)
		})
	}
}
