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
