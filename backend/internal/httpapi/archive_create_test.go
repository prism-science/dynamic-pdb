package httpapi

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
)

func archiveResultForCreate() archive.Result {
	return archive.Result{
		PDBID: "7L0B",
		Entry: archive.EntryMapping{
			Title:         new("Lysozyme at room temperature "),
			Details:       new("Collected at 293 K."),
			Resolution:    new(1.77),
			Method:        new(archive.MethodXRayCrystallography),
			SpaceGroup:    new("P 43 21 2"),
			LatestVersion: new("1.2"),
			Crystallography: &archive.Crystallography{Crystals: []archive.Crystal{
				{
					ID:           "1",
					Growth:       &archive.CrystalGrowth{PH: new(4.5)},
					Diffractions: []archive.Diffraction{{ID: "1", TemperatureKelvin: new(100.0)}},
				},
				{ID: "2"},
			}},
		},
	}
}

func Test_should_return_archive_values_with_an_rcsb_source_when_lookup_finds_the_entry(t *testing.T) {
	// given
	server := (&Server{}).WithArchiveLookup(&fakeArchiveLookup{result: archiveResultForCreate()})
	retrievedAt := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	// when
	values, source, found := server.archiveEntryValues(t.Context(), "7L0B", retrievedAt)

	// then
	require.True(t, found)
	assert.Equal(t, domainmodels.FieldSource{
		Source:      domainmodels.FieldSourceRCSB,
		Accession:   new("7L0B"),
		Release:     new("1.2"),
		RetrievedAt: &retrievedAt,
	}, source)
	require.NotNil(t, values.Title)
	assert.Equal(t, "Lysozyme at room temperature", *values.Title)
	assert.Equal(t, new(1.77), values.Metadata.Resolution)
}

func Test_should_find_no_archive_values_when_lookup_fails(t *testing.T) {
	// given
	server := (&Server{}).WithArchiveLookup(&fakeArchiveLookup{err: errors.New("rcsb is down")})

	// when
	_, _, found := server.archiveEntryValues(t.Context(), "7L0B", time.Now())

	// then
	assert.False(t, found)
}

func Test_should_find_no_archive_values_when_entry_has_no_pdb_id_or_lookup_is_off(t *testing.T) {
	// given
	lookup := &fakeArchiveLookup{result: archiveResultForCreate()}
	withLookup := (&Server{}).WithArchiveLookup(lookup)
	withoutLookup := &Server{}

	// when
	_, _, foundWithoutPDB := withLookup.archiveEntryValues(t.Context(), "", time.Now())
	_, _, foundWithoutLookup := withoutLookup.archiveEntryValues(t.Context(), "7L0B", time.Now())

	// then
	assert.False(t, foundWithoutPDB)
	assert.False(t, foundWithoutLookup)
	assert.Empty(t, lookup.requestedID, "no PDB ID means no lookup")
}

// The form copies the lookup's values into the create request. Every one of them must
// compare equal after the request is decoded, or the entry would freeze it as the
// depositor's.
func Test_should_mark_every_value_copied_from_the_lookup_rcsb_when_entry_is_created(t *testing.T) {
	// given
	server := (&Server{}).WithArchiveLookup(&fakeArchiveLookup{result: archiveResultForCreate()})
	proposal := decodeArchiveDocument(t, archiveRequest(t, server, "7L0B")).Data.Attributes.Entry
	body, err := json.Marshal(map[string]any{"entry": map[string]any{
		"title":           proposal.Title,
		"details":         proposal.Details,
		"resolution":      proposal.Resolution,
		"method":          proposal.Method,
		"space_group":     proposal.SpaceGroup,
		"crystallography": proposal.Crystallography,
		"external_refs":   map[string]string{"pdb": "7L0B"},
	}})
	require.NoError(t, err)
	var request CreateEntryRequest
	require.NoError(t, json.Unmarshal(body, &request))
	metadata, err := entryMetadataFromAPIFields(request.Entry)
	require.NoError(t, err)
	revision := domainmodels.EntryRevision{Title: trimmedStringPtr(&request.Entry.Title), Metadata: metadata}

	// when
	values, source, found := server.archiveEntryValues(t.Context(), "7L0B", time.Now())
	sources := domainmodels.ArchiveEntryFieldSources(
		domainmodels.EntryRevisionFieldSources(nil, revision), revision, values, source)

	// then
	require.True(t, found)
	for _, pointer := range archiveLookupFields() {
		assert.Equal(t, domainmodels.FieldSourceRCSB, sources[pointer].Source, pointer)
	}
}

// archiveLookupFields are the archive fields the lookup proposes. It proposes no composition
// lists: the entry sync derives those from the entry's mmCIF.
func archiveLookupFields() []domainmodels.FieldPointer {
	return slices.DeleteFunc(slices.Clone(domainmodels.EntryArchiveFields), func(pointer domainmodels.FieldPointer) bool {
		return pointer == domainmodels.EntryFieldLigands || pointer == domainmodels.EntryFieldCofactors
	})
}
