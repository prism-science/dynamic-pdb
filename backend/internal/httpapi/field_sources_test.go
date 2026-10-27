package httpapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_return_null_field_sources_when_revision_predates_sources(t *testing.T) {
	// given
	var sources domainmodels.FieldSources

	// when
	response, err := fieldSourcesFromModel(sources)

	// then
	require.NoError(t, err)
	assert.Nil(t, response)
}

func Test_should_key_field_sources_by_pointer_when_revision_has_sources(t *testing.T) {
	// given
	retrievedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	sources := domainmodels.FieldSources{
		domainmodels.EntryFieldTitle: {Source: domainmodels.FieldSourceDepositor},
		domainmodels.EntryFieldResolution: {
			Source:      domainmodels.FieldSourceRCSB,
			Accession:   new("1ABC"),
			RetrievedAt: &retrievedAt,
		},
	}

	// when
	response, err := fieldSourcesFromModel(sources)

	// then
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, FieldSources{
		"/title":               {Source: FieldSourceSourceDepositor},
		"/metadata/resolution": {Source: FieldSourceSourceRcsb, Accession: new("1ABC"), RetrievedAt: &retrievedAt},
	}, *response)
}

func Test_should_fail_when_field_source_kind_is_unknown(t *testing.T) {
	// given
	sources := domainmodels.FieldSources{domainmodels.EntryFieldTitle: {Source: "guess"}}

	// when
	_, err := fieldSourcesFromModel(sources)

	// then
	require.ErrorContains(t, err, "unknown field source: guess")
}

func Test_should_return_release_when_archive_source_names_one(t *testing.T) {
	// given
	sources := domainmodels.FieldSources{
		domainmodels.EntryFieldTitle: {Source: domainmodels.FieldSourceRCSB, Accession: new("1ABC"), Release: new("4.3")},
	}

	// when
	response, err := fieldSourcesFromModel(sources)

	// then
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, new("4.3"), (*response)["/title"].Release)
}

func Test_should_link_successors_to_registry_entries_when_entry_is_obsolete(t *testing.T) {
	// given
	obsoletedAt := time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC)
	status := &domainmodels.EntryArchiveStatus{
		State:        domainmodels.EntryArchiveStateObsolete,
		ObsoletedAt:  &obsoletedAt,
		SupersededBy: []string{"8R76", "8R78"},
	}
	registryEntries := map[string]string{"8R76": "dpdb_abcdefgh"}

	// when
	response, err := entryArchiveStatusFromModel(status, registryEntries)

	// then
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, EntryArchiveStatus{
		State:       EntryArchiveStatusStateObsolete,
		ObsoletedAt: &obsoletedAt,
		SupersededBy: []EntrySuccessor{
			{PdbId: "8R76", EntryId: new("dpdb_abcdefgh")},
			{PdbId: "8R78"},
		},
	}, *response)
}

func Test_should_omit_archive_status_when_archive_still_serves_the_entry(t *testing.T) {
	// given
	var status *domainmodels.EntryArchiveStatus

	// when
	response, err := entryArchiveStatusFromModel(status, nil)

	// then
	require.NoError(t, err)
	assert.Nil(t, response)
}

func Test_should_fail_when_archive_state_is_unknown(t *testing.T) {
	// given
	status := &domainmodels.EntryArchiveStatus{State: "gone"}

	// when
	_, err := entryArchiveStatusFromModel(status, nil)

	// then
	require.ErrorContains(t, err, "unknown entry archive state: gone")
}
