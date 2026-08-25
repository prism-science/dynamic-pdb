package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_apply_default_limit_when_list_entries_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := entryFiltersFromParams(ListEntriesParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_keep_explicit_limit_when_list_entries_limit_is_provided(t *testing.T) {
	// given
	limit := 7

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{Limit: &limit})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, limit, *got.Limit)
}

func Test_should_cap_limit_when_list_entries_limit_exceeds_maximum(t *testing.T) {
	// given
	limit := 5000

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{Limit: &limit})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, maxListLimit, *got.Limit)
}

func Test_should_truncate_search_query_when_list_entries_query_is_too_long(t *testing.T) {
	// given
	query := strings.Repeat("Ж", maxEntrySearchQueryLength+1)

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{Query: &query})

	// then
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("Ж", maxEntrySearchQueryLength), got.Query)
}

func Test_should_bound_pdb_filters_when_list_entries_has_too_many_long_values(t *testing.T) {
	// given
	pdbIDs := make([]string, maxListFilterValues+1)
	for index := range pdbIDs {
		pdbIDs[index] = strings.Repeat("A", maxEntryPDBIDLength+1)
	}

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{PdbId: &pdbIDs})

	// then
	require.NoError(t, err)
	require.Len(t, got.PDBIDs, maxListFilterValues)
	assert.Equal(t, strings.Repeat("A", maxEntryPDBIDLength), got.PDBIDs[0])
}

func Test_should_bound_levels_when_list_artifacts_has_too_many_values(t *testing.T) {
	// given
	levels := make([]ArtifactLevel, maxListFilterValues+1)
	for index := range levels {
		levels[index] = L0
	}

	// when
	got, err := artifactFiltersFromParams(ListArtifactsParams{Levels: &levels})

	// then
	require.NoError(t, err)
	require.Len(t, got.Levels, maxListFilterValues)
}

func Test_should_cap_limit_when_revision_pagination_limit_exceeds_maximum(t *testing.T) {
	// given
	limit := 5000

	// when
	err := validatePagination(&limit, nil)

	// then
	require.NoError(t, err)
	assert.Equal(t, maxListLimit, *limitOrDefault(&limit))
}

func Test_should_apply_default_limit_when_list_models_across_entries_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_reject_negative_offset_when_list_models_across_entries_offset_is_provided(t *testing.T) {
	// given
	offset := -1

	// when
	_, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{Offset: &offset})

	// then
	require.Error(t, err)
}

func Test_should_apply_default_limit_when_list_artifacts_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := artifactFiltersFromParams(ListArtifactsParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_apply_default_limit_when_list_model_artifacts_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := modelArtifactFiltersFromParams(ListModelArtifactsParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultListLimit, *got.Limit)
}

func Test_should_reject_state_field_when_entry_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"state": json.RawMessage(`"deleted"`)}

	// when
	err := validateRevisionFields(fields, entryRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}

func Test_should_reject_state_field_when_model_revision_is_created(t *testing.T) {
	// given
	fields := map[string]json.RawMessage{"state": json.RawMessage(`"deleted"`)}

	// when
	err := validateRevisionFields(fields, modelRevisionFields)

	// then
	require.ErrorIs(t, err, errInvalidRequest)
}
