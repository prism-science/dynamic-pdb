package httpapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_apply_default_limit_when_list_entries_limit_is_missing(t *testing.T) {
	// given
	activeState := models.RevisionStateActive

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{}, activeState)

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultEntryListLimit, *got.Limit)
}

func Test_should_keep_explicit_limit_when_list_entries_limit_is_provided(t *testing.T) {
	// given
	activeState := models.RevisionStateActive
	limit := 7

	// when
	got, err := entryFiltersFromParams(ListEntriesParams{Limit: &limit}, activeState)

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, limit, *got.Limit)
}
