package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_compare_only_entry_revision_data_when_checked_for_equality(t *testing.T) {
	// given
	title := "entry"
	revision := EntryRevision{
		ID:         uuid.New(),
		EntryID:    "dpdb_entry",
		State:      RevisionStateActive,
		EntryState: EntryStateActive,
		Title:      &title,
		Metadata: EntryMetadata{
			ExternalRefs: map[EntrySource]string{EntrySourcePDB: "5AMF"},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	other := revision
	other.ID = uuid.New()
	other.State = RevisionStatePending
	other.CreatedAt = revision.CreatedAt.Add(time.Hour)
	other.UpdatedAt = revision.UpdatedAt.Add(time.Hour)

	// when
	equalBeforeDataChange := revision.HasSameData(other)
	changedTitle := "changed entry"
	other.Title = &changedTitle
	equalAfterDataChange := revision.HasSameData(other)

	// then
	assert.True(t, equalBeforeDataChange)
	assert.False(t, equalAfterDataChange)
}

func Test_should_detect_entry_crystallography_change(t *testing.T) {
	// given
	revision := EntryRevision{Metadata: EntryMetadata{
		Crystallography: &EntryCrystallography{Crystals: []EntryCrystal{{ID: "1"}}},
	}}
	other := revision
	other.Metadata.Crystallography = &EntryCrystallography{Crystals: []EntryCrystal{{
		ID:     "1",
		Growth: &EntryCrystalGrowth{PH: new(7.5)},
	}}}

	// when
	equal := revision.HasSameData(other)

	// then
	assert.False(t, equal)
}

func Test_should_distinguish_unknown_from_empty_lists_when_entry_metadata_round_trips(t *testing.T) {
	// given
	empty := []string{}
	known := []string{"ZN", "ATP"}
	cases := map[string]struct {
		metadata EntryMetadata
		json     string
	}{
		"unknown": {EntryMetadata{}, `{}`},
		"empty":   {EntryMetadata{Ligands: &empty, Cofactors: &empty}, `{"ligands":[],"cofactors":[]}`},
		"known":   {EntryMetadata{Ligands: &known}, `{"ligands":["ZN","ATP"]}`},
	}

	for name, testCase := range cases {
		// when
		encoded, err := json.Marshal(testCase.metadata)
		require.NoError(t, err, name)
		var decoded EntryMetadata
		decodeErr := json.Unmarshal(encoded, &decoded)

		// then
		require.NoError(t, decodeErr, name)
		assert.JSONEq(t, testCase.json, string(encoded), name)
		assert.Equal(t, testCase.metadata, decoded, name)
	}
}

func Test_should_detect_entry_composition_change_when_lists_differ(t *testing.T) {
	// given
	known := []string{"ZN"}
	revision := EntryRevision{Metadata: EntryMetadata{Ligands: &known}}
	other := revision
	other.Metadata.Ligands = nil

	// when
	equal := revision.HasSameData(other)

	// then
	assert.False(t, equal)
}
