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

func Test_should_detect_entry_compound_record_change_when_records_differ(t *testing.T) {
	// given
	revision := EntryRevision{
		CompoundSources: &CompoundSources{
			Ligands:   map[string]CompoundSource{"NAG": {Rule: CompoundRuleBranched, ListVersion: "2026.10.0"}},
			Cofactors: map[string]CompoundSource{},
			Excluded:  map[string]CompoundSource{},
		},
		GlycanDescriptors: &[]Glycan{{EntityID: "2", CompoundIDs: []string{"NAG"}}},
	}
	withOtherRecord := revision
	withOtherRecord.CompoundSources = &CompoundSources{
		Ligands:   map[string]CompoundSource{"NAG": {Rule: CompoundRuleDepositor}},
		Cofactors: map[string]CompoundSource{},
		Excluded:  map[string]CompoundSource{},
	}
	withoutGlycans := revision
	withoutGlycans.GlycanDescriptors = nil

	// when
	sameRecord := revision.HasSameData(revision)
	otherRecord := revision.HasSameData(withOtherRecord)
	noGlycans := revision.HasSameData(withoutGlycans)

	// then
	assert.True(t, sameRecord)
	assert.False(t, otherRecord)
	assert.False(t, noGlycans)
}

func Test_should_leave_out_empty_list_version_and_review_reason_when_compound_record_is_encoded(t *testing.T) {
	// given
	record := CompoundSources{
		Ligands: map[string]CompoundSource{
			"SO4": {Rule: CompoundRuleList, ListVersion: "2026.10.0", ReviewReason: "often from the buffer"},
			"LIG": {Rule: CompoundRuleDepositor},
		},
		Cofactors: map[string]CompoundSource{"ZN": {Rule: CompoundRuleMetalc, ListVersion: "2026.10.0"}},
		Excluded:  map[string]CompoundSource{},
	}

	// when
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	var decoded CompoundSources
	decodeErr := json.Unmarshal(encoded, &decoded)

	// then
	require.NoError(t, decodeErr)
	assert.JSONEq(t, `{
		"ligands": {
			"SO4": {"rule": "list", "list_version": "2026.10.0", "review_reason": "often from the buffer"},
			"LIG": {"rule": "depositor"}
		},
		"cofactors": {"ZN": {"rule": "metalc", "list_version": "2026.10.0"}},
		"excluded": {}
	}`, string(encoded))
	assert.Equal(t, record, decoded)
}
