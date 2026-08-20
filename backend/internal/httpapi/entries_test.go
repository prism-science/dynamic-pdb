package httpapi

import (
	"encoding/json"
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
	assert.Equal(t, defaultEntryListLimit, *got.Limit)
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

func Test_should_apply_default_limit_when_list_models_across_entries_limit_is_missing(t *testing.T) {
	// given
	// when
	got, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.Limit)
	assert.Equal(t, defaultEntryListLimit, *got.Limit)
}

func Test_should_reject_negative_offset_when_list_models_across_entries_offset_is_provided(t *testing.T) {
	// given
	offset := -1

	// when
	_, err := modelFiltersFromParams(ListModelsAcrossEntriesParams{Offset: &offset})

	// then
	require.Error(t, err)
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
