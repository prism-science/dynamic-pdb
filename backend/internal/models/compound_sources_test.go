package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testListVersion = "2026.10.0"

func derivedCompoundSources() *CompoundSources {
	return &CompoundSources{
		Ligands: map[string]CompoundSource{
			"NAG": {Rule: CompoundRuleBranched, ListVersion: testListVersion},
			"SO4": {Rule: CompoundRuleList, ListVersion: testListVersion, ReviewReason: "often from the buffer"},
		},
		Cofactors: map[string]CompoundSource{"ZN": {Rule: CompoundRuleMetalc, ListVersion: testListVersion}},
		Excluded:  map[string]CompoundSource{"GOL": {Rule: CompoundRuleList, ListVersion: testListVersion}},
	}
}

func Test_should_record_added_compound_as_depositor_and_drop_removed_one_when_person_edits_lists(t *testing.T) {
	// given
	record := derivedCompoundSources()
	ligands := []string{"NAG", "LIG"}
	cofactors := []string{"ZN", "GOL"}

	// when
	edited := EditedCompoundSources(record, EntryMetadata{Ligands: &ligands, Cofactors: &cofactors})

	// then
	require.NotNil(t, edited)
	assert.Equal(t, CompoundSources{
		Ligands: map[string]CompoundSource{
			"NAG": {Rule: CompoundRuleBranched, ListVersion: testListVersion},
			"LIG": {Rule: CompoundRuleDepositor},
		},
		Cofactors: map[string]CompoundSource{
			"ZN":  {Rule: CompoundRuleMetalc, ListVersion: testListVersion},
			"GOL": {Rule: CompoundRuleDepositor},
		},
		Excluded: map[string]CompoundSource{"GOL": {Rule: CompoundRuleList, ListVersion: testListVersion}},
	}, *edited)
	assert.Equal(t, derivedCompoundSources(), record, "the edited revision's record is not changed")
}

func Test_should_keep_record_when_person_leaves_lists_unchanged(t *testing.T) {
	// given
	record := derivedCompoundSources()
	ligands := []string{"SO4", "NAG"}
	cofactors := []string{"ZN"}

	// when
	edited := EditedCompoundSources(record, EntryMetadata{Ligands: &ligands, Cofactors: &cofactors})

	// then
	assert.Equal(t, record, edited)
}

func Test_should_empty_group_when_person_marks_list_unknown(t *testing.T) {
	// given
	record := derivedCompoundSources()
	cofactors := []string{"ZN"}

	// when
	edited := EditedCompoundSources(record, EntryMetadata{Cofactors: &cofactors})

	// then
	require.NotNil(t, edited)
	assert.Empty(t, edited.Ligands)
	assert.NotNil(t, edited.Ligands)
	assert.Equal(t, record.Cofactors, edited.Cofactors)
}

func Test_should_keep_record_unset_when_entry_lists_were_never_derived(t *testing.T) {
	// given
	ligands := []string{"LIG"}

	// when
	edited := EditedCompoundSources(nil, EntryMetadata{Ligands: &ligands})

	// then
	assert.Nil(t, edited)
}
