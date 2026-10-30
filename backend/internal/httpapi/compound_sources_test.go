package httpapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_return_null_compound_record_and_glycans_when_revision_has_none(t *testing.T) {
	// given
	revision := domainmodels.EntryRevision{}

	// when
	record, err := compoundSourcesFromModel(revision.CompoundSources)
	glycans := glycanDescriptorsFromModel(revision.GlycanDescriptors)

	// then
	require.NoError(t, err)
	assert.Nil(t, record)
	assert.Nil(t, glycans)
}

func Test_should_map_every_group_and_leave_out_empty_values_when_compound_record_is_returned(t *testing.T) {
	// given
	record := &domainmodels.CompoundSources{
		Ligands: map[string]domainmodels.CompoundSource{
			"SO4": {Rule: domainmodels.CompoundRuleList, ListVersion: "2026.10.0", ReviewReason: "often from the buffer"},
			"LIG": {Rule: domainmodels.CompoundRuleDepositor},
		},
		Cofactors: map[string]domainmodels.CompoundSource{"ZN": {Rule: domainmodels.CompoundRuleMetalc, ListVersion: "2026.10.0"}},
	}

	// when
	response, err := compoundSourcesFromModel(record)

	// then
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, CompoundSources{
		Ligands: map[string]CompoundSource{
			"SO4": {Rule: CompoundSourceRuleList, ListVersion: new("2026.10.0"), ReviewReason: new("often from the buffer")},
			"LIG": {Rule: CompoundSourceRuleDepositor},
		},
		Cofactors: map[string]CompoundSource{"ZN": {Rule: CompoundSourceRuleMetalc, ListVersion: new("2026.10.0")}},
		Excluded:  map[string]CompoundSource{},
	}, *response)
}

func Test_should_map_every_compound_rule_when_compound_record_is_returned(t *testing.T) {
	// given
	rules := map[domainmodels.CompoundRule]CompoundSourceRule{
		domainmodels.CompoundRuleList:          CompoundSourceRuleList,
		domainmodels.CompoundRuleMetalc:        CompoundSourceRuleMetalc,
		domainmodels.CompoundRuleBranched:      CompoundSourceRuleBranched,
		domainmodels.CompoundRuleMacrolide:     CompoundSourceRuleMacrolide,
		domainmodels.CompoundRuleCCDType:       CompoundSourceRuleCcdType,
		domainmodels.CompoundRuleDepositorFlag: CompoundSourceRuleDepositorFlag,
		domainmodels.CompoundRuleCovalentMap:   CompoundSourceRuleCovalentMap,
		domainmodels.CompoundRuleIHMProbe:      CompoundSourceRuleIhmProbe,
		domainmodels.CompoundRuleDepositor:     CompoundSourceRuleDepositor,
	}

	for rule, expected := range rules {
		// when
		mapped, err := compoundRuleFromModel(rule)

		// then
		require.NoError(t, err, rule)
		assert.Equal(t, expected, mapped, rule)
	}
}

func Test_should_fail_when_compound_rule_is_unknown(t *testing.T) {
	// given
	record := &domainmodels.CompoundSources{
		Excluded: map[string]domainmodels.CompoundSource{"GOL": {Rule: "guess"}},
	}

	// when
	_, err := compoundSourcesFromModel(record)

	// then
	require.ErrorContains(t, err, "excluded: compound GOL: unknown compound rule: guess")
}

func Test_should_return_glycans_with_descriptors_when_revision_has_them(t *testing.T) {
	// given
	glycans := &[]domainmodels.Glycan{
		{
			EntityID:    "2",
			CompoundIDs: []string{"BMA", "NAG"},
			Descriptors: []domainmodels.GlycanDescriptor{{Descriptor: "WURCS=2.0/...", Type: "WURCS", Program: "PDB2Glycan", ProgramVersion: "1.1.0"}},
		},
		{EntityID: "3", Descriptors: []domainmodels.GlycanDescriptor{{Descriptor: "WURCS=2.0/...", Type: "WURCS"}}},
	}

	// when
	response := glycanDescriptorsFromModel(glycans)

	// then
	require.NotNil(t, response)
	assert.Equal(t, GlycanDescriptors{
		{
			EntityId:    "2",
			CompoundIds: []string{"BMA", "NAG"},
			Descriptors: []GlycanDescriptor{{Descriptor: "WURCS=2.0/...", Type: "WURCS", Program: new("PDB2Glycan"), ProgramVersion: new("1.1.0")}},
		},
		{EntityId: "3", CompoundIds: []string{}, Descriptors: []GlycanDescriptor{{Descriptor: "WURCS=2.0/...", Type: "WURCS"}}},
	}, *response)
}
