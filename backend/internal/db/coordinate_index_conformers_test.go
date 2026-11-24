package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

const testResidueCountVersion = "1.0.0"

// countedConformers is what a read of a file with two residue types stores.
func countedConformers(counts ...models.ResidueConformerCount) models.FileConformers {
	residues, multi := 0, 0
	for _, count := range counts {
		residues += count.Residues
		if count.ConformerCount >= 2 {
			multi += count.Residues
		}
	}
	return models.FileConformers{
		Version:                testResidueCountVersion,
		Scope:                  models.ConformerScopeCounted,
		Key:                    ptr(models.ResidueKeyLabel),
		Residues:               &residues,
		MultiConformerResidues: &multi,
		Counts:                 counts,
	}
}

// saveWithConformers saves a read with one chain; conformers, when set, is asked for the counts and
// records the ensemble type the save saw.
func (f *modelSearchTestDB) saveWithConformers(
	t *testing.T,
	artifactID uuid.UUID,
	conformers func(ensembleType bool) (models.FileConformers, error),
) error {
	t.Helper()
	chain := models.IndexedChain{AuthAsymID: "A", ModeledResidues: 4, Protein: true, ProteinSource: models.ProteinSourceResidueMajority}
	return f.database.SaveCoordinateIndex(context.Background(), db.SaveCoordinateIndexParams{
		ArtifactID: artifactID, ReaderVersion: "1.1.0", ReadAt: time.Now().UTC(),
		Resolve: func(models.ModelFileFASTA) (db.ResolvedIndex, error) {
			return db.ResolvedIndex{Chains: []models.IndexedChain{chain}}, nil
		},
		Conformers: conformers,
	})
}

func storeConformers(stored models.FileConformers) func(bool) (models.FileConformers, error) {
	return func(ensembleType bool) (models.FileConformers, error) {
		stored.ReadAsEnsembleType = ensembleType
		return stored, nil
	}
}

// revisionOf adds a revision of a model that uses a file, with a state, metadata and creation time.
func (f *modelSearchTestDB) revisionOf(
	t *testing.T,
	model searchModel,
	file uuid.UUID,
	state string,
	metadata map[string]any,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()
	revisionID := uuid.New()
	f.exec(t, `insert into model_revisions (id, model_id, primary_artifact_id, state, model_state, title, metadata,
	                                         created_by, created_at, updated_at)
	           values ($1, $2, $3, $4, 'new', 'conformer test revision', $5, $6, $7, $7)`,
		revisionID, model.id, file, state, jsonText(t, metadata), f.userID, createdAt)
	return revisionID
}

func Test_should_store_the_conformer_counts_with_the_chains_when_a_read_is_saved_with_counts(t *testing.T) {
	// given: a file read with counts once, then queued again.
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)
	first := countedConformers(
		models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 1, Residues: 10},
		models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 4, Residues: 2},
	)
	require.NoError(t, fixture.saveWithConformers(t, artifactID, storeConformers(first)))
	fixture.exec(t, `update coordinate_index_sets set status = 'pending' where artifact_id = $1`, artifactID)

	// when
	second := countedConformers(models.ResidueConformerCount{ResidueType: "ARG", ConformerCount: 2, Residues: 3})
	require.NoError(t, fixture.saveWithConformers(t, artifactID, storeConformers(second)))

	// then: the second read's counts and totals only, next to its chains.
	set := fixture.coordinateIndexSet(t, artifactID)
	assert.Equal(t, models.CoordinateIndexStatusDone, set.Status)
	assert.Len(t, set.Chains, 1)
	require.NotNil(t, set.Conformers)
	assert.Equal(t, testResidueCountVersion, set.Conformers.Version)
	assert.Equal(t, models.ConformerScopeCounted, set.Conformers.Scope)
	assert.Equal(t, ptr(models.ResidueKeyLabel), set.Conformers.Key)
	assert.Equal(t, ptr(3), set.Conformers.Residues)
	assert.Equal(t, ptr(3), set.Conformers.MultiConformerResidues)
	assert.False(t, set.Conformers.ReadAsEnsembleType)
	assert.Equal(t, second.Counts, set.Conformers.Counts)
}

func Test_should_keep_the_stored_counts_when_a_read_is_saved_without_counts(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)
	stored := countedConformers(models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 2, Residues: 1})
	require.NoError(t, fixture.saveWithConformers(t, artifactID, storeConformers(stored)))
	fixture.exec(t, `update coordinate_index_sets set status = 'pending' where artifact_id = $1`, artifactID)

	// when: residue counts were switched off before the next read.
	require.NoError(t, fixture.saveWithConformers(t, artifactID, nil))

	// then
	set := fixture.coordinateIndexSet(t, artifactID)
	assert.Equal(t, models.CoordinateIndexStatusDone, set.Status)
	require.NotNil(t, set.Conformers)
	assert.Equal(t, stored.Counts, set.Conformers.Counts)
}

func Test_should_store_an_ensemble_without_counts_or_totals_when_a_read_finds_one(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)

	// when
	err := fixture.saveWithConformers(t, artifactID, storeConformers(models.FileConformers{
		Version: testResidueCountVersion, Scope: models.ConformerScopeEnsembleModelFrames, Counts: []models.ResidueConformerCount{},
	}))

	// then
	require.NoError(t, err)
	set := fixture.coordinateIndexSet(t, artifactID)
	require.NotNil(t, set.Conformers)
	assert.Equal(t, models.ConformerScopeEnsembleModelFrames, set.Conformers.Scope)
	assert.Empty(t, set.Conformers.Counts)
	assert.Nil(t, set.Conformers.Residues)
	assert.Nil(t, set.Conformers.Key)
}

func Test_should_store_nothing_when_the_conformer_columns_break_the_check(t *testing.T) {
	// given: a counted read with residues but no residue key, which the table refuses.
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)
	broken := countedConformers(models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 1, Residues: 2})
	broken.Key = nil

	// when
	err := fixture.saveWithConformers(t, artifactID, storeConformers(broken))

	// then: no chains, no counts, the set still pending.
	require.ErrorContains(t, err, "coordinate_index_sets_conformers_check")
	set := fixture.coordinateIndexSet(t, artifactID)
	assert.Equal(t, models.CoordinateIndexStatusPending, set.Status)
	assert.Empty(t, set.Chains)
	assert.Nil(t, set.Conformers)
	var rows int
	require.NoError(t, fixture.sql.Get(&rows, `select count(*) from coordinate_index_residue_counts where artifact_id = $1`, artifactID))
	assert.Zero(t, rows)
}

func Test_should_store_nothing_when_a_counted_read_has_no_residue_totals(t *testing.T) {
	// given: a counted read whose totals are missing, which the check refuses even though each of
	// its conditions on them is NULL rather than false.
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)
	broken := countedConformers(models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 1, Residues: 2})
	broken.Residues = nil
	broken.MultiConformerResidues = nil

	// when
	err := fixture.saveWithConformers(t, artifactID, storeConformers(broken))

	// then
	require.ErrorContains(t, err, "coordinate_index_sets_conformers_check")
	set := fixture.coordinateIndexSet(t, artifactID)
	assert.Equal(t, models.CoordinateIndexStatusPending, set.Status)
	assert.Nil(t, set.Conformers)
}

func Test_should_refuse_conformer_columns_with_a_null_where_a_value_is_needed(t *testing.T) {
	cases := []struct {
		name     string
		set      string
		accepted bool
	}{
		{
			name:     "a counted read with every value",
			set:      `residues_version = '1.0.0', conformers = 'counted', residue_key = 'label', residues = 2, multi_conformer_residues = 1, read_as_ensemble_type = false`,
			accepted: true,
		},
		{
			name:     "a counted read with every value and mapped residues",
			set:      `residues_version = '1.1.0', conformers = 'counted', residue_key = 'mapped', residues = 2, multi_conformer_residues = 1, read_as_ensemble_type = false`,
			accepted: true,
		},
		{
			name: "a counted read without residues",
			set:  `residues_version = '1.0.0', conformers = 'counted', residue_key = 'label', residues = null, multi_conformer_residues = 0, read_as_ensemble_type = false`,
		},
		{
			name: "a counted read with mapped residues but no residue totals",
			set:  `residues_version = '1.1.0', conformers = 'counted', residue_key = 'mapped', residues = null, multi_conformer_residues = null, read_as_ensemble_type = false`,
		},
		{
			name: "a counted read without multi-conformer residues",
			set:  `residues_version = '1.0.0', conformers = 'counted', residue_key = 'label', residues = 2, multi_conformer_residues = null, read_as_ensemble_type = false`,
		},
		{
			name: "a counted read without read_as_ensemble_type",
			set:  `residues_version = '1.0.0', conformers = 'counted', residue_key = 'label', residues = 2, multi_conformer_residues = 1, read_as_ensemble_type = null`,
		},
		{
			name: "a version without a scope",
			set:  `residues_version = '1.0.0', conformers = null, residue_key = null, residues = null, multi_conformer_residues = null, read_as_ensemble_type = null`,
		},
		{
			name: "an ensemble by type without read_as_ensemble_type",
			set:  `residues_version = '1.0.0', conformers = 'ensemble_model_type', residue_key = null, residues = null, multi_conformer_residues = null, read_as_ensemble_type = null`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// given
			fixture := newModelSearchTestDB(t)
			artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
			fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)

			// when
			_, err := fixture.sql.Exec(`update coordinate_index_sets set `+tc.set+` where artifact_id = $1`, artifactID)

			// then
			if tc.accepted {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, "coordinate_index_sets_conformers_check")
		})
	}
}

func Test_should_tell_whether_a_model_uses_the_file_as_an_ensemble_when_its_latest_revision_with_the_file_says_so(t *testing.T) {
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	entry := fixture.entry(t, map[string]any{})
	now := time.Now().UTC()
	ensemble := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	memberCount := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer", "member_count": 3})
	multiconformer := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	// retyped: an archived Ensemble revision, then a newer pending Multiconformer one, same file.
	retyped := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	fixture.revisionOf(t, retyped, retyped.coordinateFile, "pending", map[string]any{"model_type": "Multiconformer"}, now.Add(time.Hour))
	// rejected: a rejected newer Ensemble revision does not count.
	rejected := fixture.model(t, entry, map[string]any{"model_type": "Deposited"})
	fixture.revisionOf(t, rejected, rejected.coordinateFile, "rejected", map[string]any{"model_type": "Ensemble"}, now.Add(time.Hour))
	// shared: a Deposited model and another model's Ensemble revision use one file.
	shared := fixture.model(t, entry, map[string]any{"model_type": "Deposited"})
	other := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{"model_type": "Deposited"})
	fixture.revisionOf(t, other, shared.coordinateFile, "pending", map[string]any{"model_type": "Ensemble"}, now.Add(time.Hour))
	unused := fixture.artifact(t, models.ArtifactLevelL2, models.ArtifactTypeModel)

	cases := map[string]struct {
		file uuid.UUID
		want bool
	}{
		"Ensemble":                     {ensemble.coordinateFile, true},
		"member_count":                 {memberCount.coordinateFile, true},
		"Multiconformer":               {multiconformer.coordinateFile, false},
		"newest revision no longer":    {retyped.coordinateFile, false},
		"rejected revision":            {rejected.coordinateFile, false},
		"another model uses it as one": {shared.coordinateFile, true},
		"no model uses the file":       {unused, false},
	}
	for name, testCase := range cases {
		// when
		got, err := fixture.database.CoordinateIndex.ModelFileEnsembleType(ctx, testCase.file)

		// then
		require.NoError(t, err, name)
		assert.Equal(t, testCase.want, got, name)
	}
}

func Test_should_give_the_save_the_ensemble_type_of_the_file_when_a_read_is_saved_with_counts(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	ensemble := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{"model_type": "Ensemble"})
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, ensemble.coordinateFile)
	var seen []bool

	// when
	err := fixture.saveWithConformers(t, ensemble.coordinateFile, func(ensembleType bool) (models.FileConformers, error) {
		seen = append(seen, ensembleType)
		return models.FileConformers{
			Version: testResidueCountVersion, Scope: models.ConformerScopeEnsembleModelType, ReadAsEnsembleType: ensembleType,
			Counts: []models.ResidueConformerCount{},
		}, nil
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, seen)
	set := fixture.coordinateIndexSet(t, ensemble.coordinateFile)
	assert.True(t, set.Conformers.ReadAsEnsembleType)
}

func Test_should_queue_done_sets_without_current_counts_as_fill_rows_when_the_residue_fill_pages_through(t *testing.T) {
	// given: five sets, ordered by artifact id.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		uuid.MustParse("00000000-0000-0000-0000-000000000003"),
		uuid.MustParse("00000000-0000-0000-0000-000000000004"),
		uuid.MustParse("00000000-0000-0000-0000-000000000005"),
	}
	for _, id := range ids {
		fixture.exec(t, `insert into artifacts (id, name, level, type, created_by) values ($1, 'model.cif', 'L2', 'model', $2)`, id, fixture.userID)
	}
	// 1: done, read before conformer counts existed.
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, attempts)
	                 values ($1, 'done', '1.1.0', now(), 2)`, ids[0])
	// 2: done with counts of an older version.
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, residues_version,
	                     conformers, residue_key, residues, multi_conformer_residues, read_as_ensemble_type)
	                 values ($1, 'done', '1.1.0', now(), '0.9.0', 'counted', 'label', 1, 0, false)`, ids[1])
	// 3: done with current counts.
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, residues_version,
	                     conformers, read_as_ensemble_type)
	                 values ($1, 'done', '1.1.0', now(), $2, 'ensemble_model_frames', false)`, ids[2], testResidueCountVersion)
	// 4: pending; 5: failed.
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, attempts) values ($1, 1)`, ids[3])
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, last_error) values ($1, 'failed', 'over a limit')`, ids[4])

	// when: pages of 2.
	firstPage, err := fixture.database.CoordinateIndex.RequeueForConformersAfter(ctx, uuid.Nil, 2, testResidueCountVersion, now)
	require.NoError(t, err)
	secondPage, err := fixture.database.CoordinateIndex.RequeueForConformersAfter(ctx, firstPage.LastArtifactID, 2, testResidueCountVersion, now)
	require.NoError(t, err)
	lastPage, err := fixture.database.CoordinateIndex.RequeueForConformersAfter(ctx, secondPage.LastArtifactID, 2, testResidueCountVersion, now)
	require.NoError(t, err)
	again, err := fixture.database.CoordinateIndex.RequeueForConformersAfter(ctx, uuid.Nil, 10, testResidueCountVersion, now)
	require.NoError(t, err)

	// then
	assert.Equal(t, models.RequeuedSetsPage{Scanned: 2, Requeued: 2, LastArtifactID: ids[1]}, firstPage)
	assert.Equal(t, models.RequeuedSetsPage{Scanned: 2, Requeued: 0, LastArtifactID: ids[3]}, secondPage)
	assert.Equal(t, models.RequeuedSetsPage{Scanned: 1, Requeued: 0, LastArtifactID: ids[4]}, lastPage)
	assert.Equal(t, int64(0), again.Requeued, "a pending set is not queued twice")
	for _, id := range ids[:2] {
		set := fixture.coordinateIndexSet(t, id)
		assert.Equal(t, models.CoordinateIndexStatusPending, set.Status)
		assert.True(t, set.Fill)
		assert.Zero(t, set.Attempts)
	}
	assert.Equal(t, models.CoordinateIndexStatusDone, fixture.coordinateIndexSet(t, ids[2]).Status)
	assert.Equal(t, 1, fixture.coordinateIndexSet(t, ids[3]).Attempts)
	assert.Equal(t, models.CoordinateIndexStatusFailed, fixture.coordinateIndexSet(t, ids[4]).Status)
	pending, err := fixture.database.CoordinateIndex.HasPendingFill(ctx)
	require.NoError(t, err)
	assert.True(t, pending)
}

func Test_should_keep_finding_a_model_by_its_chains_when_its_file_is_queued_for_conformer_counts(t *testing.T) {
	// given: a model whose file was read before conformer counts existed.
	fixture := newModelSearchTestDB(t)
	model := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{})
	fixture.indexChains(t, model, "done", longChains(5, 400)...)

	// when
	page, err := fixture.database.CoordinateIndex.RequeueForConformersAfter(context.Background(), uuid.Nil, 10, testResidueCountVersion, time.Now().UTC())

	// then: queued again, and still found by its chains until the new read replaces them.
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Requeued)
	assert.Equal(t, models.CoordinateIndexStatusPending, fixture.coordinateIndexSet(t, model.coordinateFile).Status)
	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "example 2's chain part", search: db.ModelSearch{ProteinChains: &db.ProteinChainFilter{
			Mode: db.ProteinChainsAtLeast, MinChains: 5, MinLength: ptr(301),
		}}, want: []string{model.id}},
	})
}

func Test_should_queue_a_done_set_again_when_its_models_changed_between_ensemble_and_another_type(t *testing.T) {
	// given: files read with counts an hour ago; each new revision is the latest of its model.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entry := fixture.entry(t, map[string]any{})
	markDone := func(model searchModel, readAsEnsemble *bool) {
		if readAsEnsemble == nil {
			fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, fasta_entry_ids)
			                 values ($1, 'done', '1.1.0', $2, $3)`, model.coordinateFile, now.Add(-time.Hour), "{"+entry.id+"}")
			return
		}
		scope := "counted"
		var residues, multi, key any = 3, 0, "label"
		if *readAsEnsemble {
			scope, residues, multi, key = "ensemble_model_type", nil, nil, nil
		}
		fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, fasta_entry_ids,
		                     residues_version, conformers, residue_key, residues, multi_conformer_residues, read_as_ensemble_type)
		                 values ($1, 'done', '1.1.0', $2, $3, $4, $5, $6, $7, $8, $9)`,
			model.coordinateFile, now.Add(-time.Hour), "{"+entry.id+"}", testResidueCountVersion, scope, key, residues, multi, *readAsEnsemble)
	}
	// becameEnsemble: read as Multiconformer; a new revision makes it an Ensemble.
	becameEnsemble := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	markDone(becameEnsemble, ptr(false))
	fixture.revisionOf(t, becameEnsemble, becameEnsemble.coordinateFile, "pending", map[string]any{"model_type": "Ensemble"}, fixture.tick())
	// noLongerEnsemble: read as an Ensemble; a new revision makes it Multiconformer.
	noLongerEnsemble := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	markDone(noLongerEnsemble, ptr(true))
	fixture.revisionOf(t, noLongerEnsemble, noLongerEnsemble.coordinateFile, "pending", map[string]any{"model_type": "Multiconformer"}, fixture.tick())
	// sameType: a new revision keeps the type.
	sameType := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	markDone(sameType, ptr(false))
	fixture.revisionOf(t, sameType, sameType.coordinateFile, "pending", map[string]any{"model_type": "Multiconformer", "purpose": "Refinement"}, fixture.tick())
	// neverCounted: an Ensemble now, but its file was never read with counts.
	neverCounted := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	markDone(neverCounted, nil)
	fixture.revisionOf(t, neverCounted, neverCounted.coordinateFile, "pending", map[string]any{"model_type": "Ensemble"}, fixture.tick())

	// when
	queued, err := fixture.database.CoordinateIndex.RequeueForEnsembleTypeChanges(ctx, now.Add(-48*time.Hour), now)

	// then: queued ahead of the fill.
	require.NoError(t, err)
	assert.Equal(t, int64(2), queued)
	for _, model := range []searchModel{becameEnsemble, noLongerEnsemble} {
		set := fixture.coordinateIndexSet(t, model.coordinateFile)
		assert.Equal(t, models.CoordinateIndexStatusPending, set.Status, model.id)
		assert.False(t, set.Fill, model.id)
	}
	for _, model := range []searchModel{sameType, neverCounted} {
		assert.Equal(t, models.CoordinateIndexStatusDone, fixture.coordinateIndexSet(t, model.coordinateFile).Status, model.id)
	}
}

func Test_should_queue_a_file_read_as_an_ensemble_again_when_its_ensemble_revision_was_rejected_long_after_its_creation(t *testing.T) {
	// given: three files read as an Ensemble's by its type, all revisions older than any window.
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entry := fixture.entry(t, map[string]any{})
	readAsEnsemble := func(model searchModel) {
		fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, residues_version,
		                     conformers, read_as_ensemble_type)
		                 values ($1, 'done', '1.1.0', $2, $3, 'ensemble_model_type', true)`,
			model.coordinateFile, now.Add(-30*24*time.Hour), testResidueCountVersion)
	}
	// rejected: an active Multiconformer revision, and a newer Ensemble revision rejected in review.
	rejected := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	rejectedRevision := fixture.revisionOf(t, rejected, rejected.coordinateFile, "in_review", map[string]any{"model_type": "Ensemble"}, fixture.tick())
	readAsEnsemble(rejected)
	_, err := fixture.database.Models.RejectRevision(ctx, rejectedRevision)
	require.NoError(t, err)
	// stillEnsemble: an active Ensemble.
	stillEnsemble := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	readAsEnsemble(stillEnsemble)
	// counted: a Multiconformer read with counts.
	counted := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id, status, reader_version, read_at, residues_version,
	                     conformers, residue_key, residues, multi_conformer_residues, read_as_ensemble_type)
	                 values ($1, 'done', '1.1.0', $2, $3, 'counted', 'label', 2, 1, false)`,
		counted.coordinateFile, now.Add(-30*24*time.Hour), testResidueCountVersion)
	fixture.exec(t, `update model_revisions set created_at = $1`, now.Add(-30*24*time.Hour))

	// when
	queued, err := fixture.database.CoordinateIndex.RequeueForEnsembleTypeChanges(ctx, now.Add(-48*time.Hour), now)
	require.NoError(t, err)

	// then: only the rejected Ensemble's file, ahead of the fill, although no revision is in the
	// 48-hour window.
	assert.Equal(t, int64(1), queued)
	set := fixture.coordinateIndexSet(t, rejected.coordinateFile)
	assert.Equal(t, models.CoordinateIndexStatusPending, set.Status)
	assert.False(t, set.Fill)
	for _, model := range []searchModel{stillEnsemble, counted} {
		assert.Equal(t, models.CoordinateIndexStatusDone, fixture.coordinateIndexSet(t, model.coordinateFile).Status, model.id)
	}
}

func Test_should_find_the_deposited_mmcif_of_the_entry_when_a_model_file_needs_its_residue_numbering_mapped(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	ctx := context.Background()
	entry := fixture.entry(t, map[string]any{})
	deposited := fixture.model(t, entry, map[string]any{"model_type": "Deposited"})
	fixture.exec(t, `insert into artifact_checksums (artifact_id, status, sha256, size_bytes, checked_at, copy_status, stored_uri)
	                 values ($1, 'verified', $2, 100, now(), 'copied', 'https://bucket.example.org/copies/7ABC.cif')`,
		deposited.coordinateFile, strings.Repeat("a", 64))
	qfit := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	// A file used by models of two entries, and an entry without a Deposited model.
	shared := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	other := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{"model_type": "Multiconformer"})
	fixture.revisionOf(t, other, shared.coordinateFile, "pending", map[string]any{"model_type": "Multiconformer"}, fixture.tick())
	lonely := fixture.model(t, fixture.entry(t, map[string]any{}), map[string]any{"model_type": "Multiconformer"})

	// when
	found, err := fixture.database.CoordinateIndex.DepositedFileOf(ctx, qfit.coordinateFile)
	require.NoError(t, err)
	forShared, err := fixture.database.CoordinateIndex.DepositedFileOf(ctx, shared.coordinateFile)
	require.NoError(t, err)
	forLonely, err := fixture.database.CoordinateIndex.DepositedFileOf(ctx, lonely.coordinateFile)
	require.NoError(t, err)
	forItself, err := fixture.database.CoordinateIndex.DepositedFileOf(ctx, deposited.coordinateFile)
	require.NoError(t, err)

	// then
	require.NotNil(t, found)
	assert.Equal(t, deposited.coordinateFile, found.ArtifactID)
	assert.Equal(t, entry.id, found.EntryID)
	assert.Equal(t, ptr(models.CopyStatusCopied), found.CopyStatus)
	assert.Equal(t, ptr("https://bucket.example.org/copies/7ABC.cif"), found.StoredURI)
	assert.Nil(t, forShared, "models of two entries: no single deposition")
	assert.Nil(t, forLonely, "no Deposited model in the entry")
	assert.Nil(t, forItself, "the deposited file needs no mapping of its own")
}

func Test_should_store_the_mapped_residue_key_when_a_read_mapped_its_residues(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	artifactID := fixture.storedModelArtifact(t, time.Now().UTC())
	fixture.exec(t, `insert into coordinate_index_sets (artifact_id) values ($1)`, artifactID)
	mapped := countedConformers(models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 2, Residues: 1})
	mapped.Key = ptr(models.ResidueKeyMapped)

	// when
	err := fixture.saveWithConformers(t, artifactID, storeConformers(mapped))

	// then
	require.NoError(t, err)
	assert.Equal(t, ptr(models.ResidueKeyMapped), fixture.coordinateIndexSet(t, artifactID).Conformers.Key)
}
