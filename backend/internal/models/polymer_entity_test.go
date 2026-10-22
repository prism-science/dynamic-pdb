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

func Test_should_parse_only_plain_substitutions_when_rcsb_mutation_text_is_read(t *testing.T) {
	// given
	cases := []struct {
		name     string
		raw      string
		mutation []string
		status   MutationsStatus
	}{
		{"substitutions", "C54T, C97A", []string{"C54T", "C97A"}, MutationsStatusParsed},
		{"semicolon and spaces", " C54T ;K7R ", []string{"C54T", "K7R"}, MutationsStatusParsed},
		{"free text", "ENGINEERED MUTATION", nil, MutationsStatusNeedsReview},
		{"mixed", "C54T;DELETION,A(-3)G, K7R", []string{"C54T", "K7R"}, MutationsStatusNeedsReview},
		{"empty", "", nil, MutationsStatusNone},
		{"separators only", " , ;", nil, MutationsStatusNone},
		{"position zero", "C0T", nil, MutationsStatusNeedsReview},
	}

	for _, testCase := range cases {
		// when
		mutations, status := ParseMutations(testCase.raw)

		// then
		assert.Equal(t, testCase.mutation, mutations, testCase.name)
		assert.Equal(t, testCase.status, status, testCase.name)
	}
}

func Test_should_read_empty_mutation_list_as_none_when_stored_metadata_has_empty_array(t *testing.T) {
	// given
	stored := `{"mutations":[],"mutations_raw":"ENGINEERED MUTATION","mutations_status":"needs_review"}`

	// when
	var metadata PolymerEntityMetadata
	err := json.Unmarshal([]byte(stored), &metadata)

	// then
	require.NoError(t, err)
	assert.Nil(t, metadata.Mutations, "[] and omitted read the same, so a re-sync compares equal")
	assert.Equal(t, "ENGINEERED MUTATION", *metadata.MutationsRaw)
	assert.Equal(t, MutationsStatusNeedsReview, *metadata.MutationsStatus)
}
