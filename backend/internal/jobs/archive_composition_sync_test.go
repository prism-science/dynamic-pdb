package jobs

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func derivedArchiveFile() archiveFileValues {
	return archiveFileValues{
		mutations: archiveMutations{complete: true},
		composition: &archiveComposition{
			ligands:   []string{"SO4"},
			cofactors: []string{"ZN"},
			sources: models.CompoundSources{
				Ligands:   map[string]models.CompoundSource{"SO4": {Rule: models.CompoundRuleList, ListVersion: "2026.10.0", ReviewReason: "often from the buffer"}},
				Cofactors: map[string]models.CompoundSource{"ZN": {Rule: models.CompoundRuleMetalc, ListVersion: "2026.10.0"}},
				Excluded:  map[string]models.CompoundSource{},
			},
			glycans: []models.Glycan{},
		},
		referenceVersion: "2026.10.0",
	}
}

func storedComposition() models.EntryRevision {
	return models.EntryRevision{
		Metadata: models.EntryMetadata{Ligands: &[]string{"HEM"}, Cofactors: &[]string{}},
		CompoundSources: &models.CompoundSources{
			Ligands:   map[string]models.CompoundSource{"HEM": {Rule: models.CompoundRuleList, ListVersion: "2026.10.0"}},
			Cofactors: map[string]models.CompoundSource{},
			Excluded:  map[string]models.CompoundSource{},
		},
		GlycanDescriptors: &[]models.Glycan{},
	}
}

func Test_should_take_compounds_from_file_when_sync_applies_new_archive_version(t *testing.T) {
	// given
	current := storedComposition()
	desired := models.EntryRevision{}

	// when
	synced := applyArchiveComposition(current, &desired, derivedArchiveFile())
	entryChanged, _ := compareEntryUpdate(current, desired, nil, nil)

	// then
	assert.True(t, synced)
	assert.Equal(t, &[]string{"SO4"}, desired.Metadata.Ligands)
	assert.Equal(t, &[]string{"ZN"}, desired.Metadata.Cofactors)
	assert.Equal(t, &derivedArchiveFile().composition.sources, desired.CompoundSources)
	assert.Equal(t, &[]models.Glycan{}, desired.GlycanDescriptors)
	assert.True(t, entryChanged)
}

func Test_should_keep_stored_compounds_when_sync_must_not_take_them_from_file(t *testing.T) {
	// given
	depositor := storedComposition()
	depositor.FieldSources = models.FieldSources{models.EntryFieldLigands: {Source: models.FieldSourceDepositor}}
	typedWithoutRecord := models.EntryRevision{Metadata: models.EntryMetadata{Ligands: &[]string{"LIG"}}}
	incomplete := archiveFileValues{incompleteReason: "the head of the RCSB mmCIF ends before its coordinates"}
	cases := map[string]struct {
		current models.EntryRevision
		file    archiveFileValues
	}{
		"person changed the lists":   {current: depositor, file: derivedArchiveFile()},
		"lists typed without record": {current: typedWithoutRecord, file: derivedArchiveFile()},
		"incomplete file":            {current: storedComposition(), file: incomplete},
	}

	for name, testCase := range cases {
		desired := models.EntryRevision{}

		// when
		synced := applyArchiveComposition(testCase.current, &desired, testCase.file)
		entryChanged, _ := compareEntryUpdate(testCase.current, desired, nil, nil)

		// then
		assert.False(t, synced, name)
		assert.Equal(t, testCase.current.Metadata.Ligands, desired.Metadata.Ligands, name)
		assert.Equal(t, testCase.current.Metadata.Cofactors, desired.Metadata.Cofactors, name)
		assert.Equal(t, testCase.current.CompoundSources, desired.CompoundSources, name)
		assert.Equal(t, testCase.current.GlycanDescriptors, desired.GlycanDescriptors, name)
		assert.False(t, entryChanged, name)
	}
}

func Test_should_report_change_when_only_compound_record_differs(t *testing.T) {
	// given
	current := storedComposition()
	desired := storedComposition()
	desired.CompoundSources.Ligands["HEM"] = models.CompoundSource{Rule: models.CompoundRuleList, ListVersion: "2026.11.0"}
	withGlycan := storedComposition()
	withGlycan.GlycanDescriptors = &[]models.Glycan{{EntityID: "2", CompoundIDs: []string{"NAG"}}}

	// when
	recordChanged, _ := compareEntryUpdate(current, desired, nil, nil)
	glycansChanged, _ := compareEntryUpdate(current, withGlycan, nil, nil)
	sameChanged, _ := compareEntryUpdate(current, storedComposition(), nil, nil)

	// then
	assert.True(t, recordChanged)
	assert.True(t, glycansChanged)
	assert.False(t, sameChanged)
}

func Test_should_keep_list_sources_when_sync_does_not_take_lists_from_file(t *testing.T) {
	// given
	retrievedAt := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	listsSource := models.FieldSource{Source: models.FieldSourceRCSB, Accession: new("1ABC"), Release: new("1.0")}
	current := models.FieldSources{models.EntryFieldLigands: listsSource}
	source := archiveRevisionSource{pdbID: "1ABC", version: models.ArchiveVersion{Ordinal: 2, Version: "1.1"}}

	// when
	kept := syncEntryFieldSources(current, source, retrievedAt, false)
	taken := syncEntryFieldSources(current, source, retrievedAt, true)

	// then
	assert.Equal(t, listsSource, kept[models.EntryFieldLigands])
	assert.NotContains(t, kept, models.EntryFieldCofactors)
	assert.Equal(t, new("1.1"), kept[models.EntryFieldResolution].Release)
	assert.Equal(t, new("1.1"), taken[models.EntryFieldLigands].Release)
	assert.Equal(t, new("1.1"), taken[models.EntryFieldCofactors].Release)
}

func Test_should_find_no_change_when_sync_reads_the_file_the_backfill_derived_from(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	job := fixture.job(t, ArchiveRederiveConfig{})
	require.NoError(t, job.Run(context.Background()))
	current := fixture.revision(t, entry.revisionID)
	file, err := readArchiveFile(context.Background(), job.rcsbClient, job.logger, "1ABC")
	require.NoError(t, err)
	// The sync builds the other fields from RCSB's JSON; only the compounds are compared here.
	desired := current

	// when
	synced := applyArchiveComposition(current, &desired, file)
	entryChanged, _ := compareEntryUpdate(current, desired, nil, nil)

	// then
	assert.True(t, synced)
	assert.False(t, entryChanged)
}

func Test_should_refuse_sync_revision_when_parent_was_changed_in_place_after_it_was_read(t *testing.T) {
	// given: the sync read the active revision, then the backfill filled its lists in place
	fixture := newRederiveFixture(t)
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	strategy := &entrySyncStrategy{database: fixture.database, logger: slog.New(slog.DiscardHandler), now: time.Now}
	read := fixture.revision(t, entry.revisionID)
	require.NoError(t, fixture.database.Entries.UpdateComposition(context.Background(), entry.revisionID,
		models.EntryComposition{Ligands: &[]string{"SO4"}, Cofactors: &[]string{}}))

	// when
	err := strategy.saveSyncEntryRevision(context.Background(), read,
		newSyncEntryRevision(read, "RCSB version 1.1", time.Now()), nil, false)

	// then
	require.ErrorIs(t, err, db.ErrEntryRevisionConflict)
	assert.Equal(t, 1, fixture.revisionCount(t, entry.entryID))
	assert.Equal(t, &[]string{"SO4"}, fixture.revision(t, entry.revisionID).Metadata.Ligands)
}

func Test_should_activate_sync_revision_when_parent_is_unchanged_since_it_was_read(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	strategy := &entrySyncStrategy{database: fixture.database, logger: slog.New(slog.DiscardHandler), now: time.Now}
	read := fixture.revision(t, entry.revisionID)

	// when
	err := strategy.saveSyncEntryRevision(context.Background(), read,
		newSyncEntryRevision(read, "RCSB version 1.1", time.Now()), nil, false)

	// then
	require.NoError(t, err)
	assert.Equal(t, 2, fixture.revisionCount(t, entry.entryID))
}
