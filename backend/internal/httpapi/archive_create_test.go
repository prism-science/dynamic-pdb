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
	"dynamic-pdb/backend/internal/validation"
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

func Test_should_let_a_create_keep_the_archive_method_when_it_is_off_the_depositor_list(t *testing.T) {
	// given
	result := archiveResultForCreate()
	result.Entry.Method = new("NEUTRON DIFFRACTION")
	server := (&Server{}).WithArchiveLookup(&fakeArchiveLookup{result: result})
	entry := CreateEntryData{
		Title:        "Lysozyme",
		Method:       "NEUTRON DIFFRACTION",
		ExternalRefs: &map[string]string{"pdb": " 7l0b "},
	}

	// when
	archive := server.lookUpNewEntryArchive(t.Context(), entry, time.Now())
	keptErrors := createEntryRequestErrors(CreateEntryRequest{Entry: entry}, archive.method())
	entry.Method = "Neutron diffraction"
	typedErrors := createEntryRequestErrors(CreateEntryRequest{Entry: entry}, archive.method())

	// then
	require.True(t, archive.found)
	require.NotNil(t, archive.method())
	assert.Equal(t, domainmodels.StructureMethod("NEUTRON DIFFRACTION"), *archive.method())
	assert.Empty(t, keptErrors)
	assert.Equal(t, []validation.Pointer{"/entry/method"}, pointersOf(typedErrors))
}

func Test_should_give_no_archive_method_when_the_create_names_no_pdb_id_or_the_lookup_fails(t *testing.T) {
	// given
	lookup := &fakeArchiveLookup{result: archiveResultForCreate()}
	withLookup := (&Server{}).WithArchiveLookup(lookup)
	failing := (&Server{}).WithArchiveLookup(&fakeArchiveLookup{err: errors.New("rcsb is down")})
	withPDB := CreateEntryData{Title: "Lysozyme", Method: "CryoEM", ExternalRefs: &map[string]string{"pdb": "7L0B"}}

	// when
	withoutPDB := withLookup.lookUpNewEntryArchive(t.Context(), CreateEntryData{Title: "Lysozyme"}, time.Now())
	lookupFailed := failing.lookUpNewEntryArchive(t.Context(), withPDB, time.Now())

	// then
	assert.False(t, withoutPDB.found)
	assert.Nil(t, withoutPDB.method())
	assert.Empty(t, lookup.requestedID, "no PDB ID means no lookup")
	assert.False(t, lookupFailed.found)
	assert.Nil(t, lookupFailed.method())
}

func Test_should_require_the_depositor_list_when_an_entry_revision_sends_a_method(t *testing.T) {
	// given
	method := "NEUTRON DIFFRACTION"
	sendsMethod := createEntryRevisionPayload{Request: CreateEntryRevisionRequest{Entry: EntryRevisionChange{Method: &method}}}
	sendsTitleOnly := createEntryRevisionPayload{Request: CreateEntryRevisionRequest{Entry: EntryRevisionChange{Title: new("new title")}}}

	// when
	methodErrors := entryRevisionChangeErrors(sendsMethod)
	titleErrors := entryRevisionChangeErrors(sendsTitleOnly)

	// then
	assert.Equal(t, []validation.Pointer{"/entry/method"}, pointersOf(methodErrors))
	assert.Empty(t, titleErrors)
}
