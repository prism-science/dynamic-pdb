package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_comma_joined_mutations_when_stored_metadata_predates_the_list(t *testing.T) {
	// given
	stored := `{"label_entity_id":"1","mutations":"C54T, C97A"}`

	// when
	var metadata PolymerEntityMetadata
	err := json.Unmarshal([]byte(stored), &metadata)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"C54T", "C97A"}, metadata.Mutations)
	assert.Equal(t, "1", *metadata.LabelEntityID)
}

func Test_should_round_trip_mutations_when_metadata_stores_a_list(t *testing.T) {
	// given
	metadata := PolymerEntityMetadata{Mutations: []string{"C54T", "C97A"}}

	// when
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	var decoded PolymerEntityMetadata
	err = json.Unmarshal(encoded, &decoded)

	// then
	require.NoError(t, err)
	assert.JSONEq(t, `{"mutations":["C54T","C97A"]}`, string(encoded))
	assert.Equal(t, metadata, decoded)
}

func Test_should_leave_mutations_empty_when_metadata_has_none(t *testing.T) {
	// given
	stored := `{"description":"lysozyme","mutations":null}`

	// when
	var metadata PolymerEntityMetadata
	err := json.Unmarshal([]byte(stored), &metadata)

	// then
	require.NoError(t, err)
	assert.Empty(t, metadata.Mutations)
}
