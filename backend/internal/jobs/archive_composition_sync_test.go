package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func derivedArchiveFile() archiveFile {
	return archiveFile{
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
	incomplete := archiveFile{incompleteReason: "the head of the RCSB mmCIF ends before its coordinates"}
	cases := map[string]struct {
		current models.EntryRevision
		file    archiveFile
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

func Test_should_keep_list_sources_unless_sync_took_lists_from_file(t *testing.T) {
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
