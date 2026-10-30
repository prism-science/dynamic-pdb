package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// fixtureResult is what the parser reads from one of its test files, read whole.
func fixtureResult(t *testing.T, name string) cif.Result {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "lib", "cif", "testdata", name))
	require.NoError(t, err)
	_, result, err := cif.ExtractText(string(contents), false)
	require.NoError(t, err)
	return result
}

func Test_should_leave_polymer_residue_out_of_lists_when_composition_is_derived(t *testing.T) {
	// given
	result := fixtureResult(t, "1A62.cif")

	// when
	composition, err := compositionFromResult(result)

	// then
	require.NoError(t, err)
	assert.NotContains(t, composition.ligands, "MSE")
	assert.NotContains(t, composition.sources.Ligands, "MSE")
	assert.NotContains(t, composition.sources.Excluded, "MSE")
}

func Test_should_list_glycan_sugars_with_rule_branched_and_keep_wurcs_when_composition_is_derived(t *testing.T) {
	// given
	result := fixtureResult(t, "2VSD.cif")

	// when
	composition, err := compositionFromResult(result)

	// then
	require.NoError(t, err)
	assert.Contains(t, composition.ligands, "NAG")
	assert.Equal(t, models.CompoundRuleBranched, composition.sources.Ligands["NAG"].Rule)
	assert.NotEmpty(t, composition.sources.Ligands["NAG"].ListVersion)
	require.NotEmpty(t, composition.glycans)
	assert.Contains(t, composition.glycans[0].CompoundIDs, "NAG")
	require.NotEmpty(t, composition.glycans[0].Descriptors)
	assert.Equal(t, "WURCS", composition.glycans[0].Descriptors[0].Type)
}

func Test_should_place_metal_linked_by_metalc_as_cofactor_when_composition_is_derived(t *testing.T) {
	// given
	result := fixtureResult(t, "3CLN.cif")

	// when
	composition, err := compositionFromResult(result)

	// then
	require.NoError(t, err)
	assert.Contains(t, composition.cofactors, "CA")
	assert.Equal(t, models.CompoundSource{Rule: models.CompoundRuleMetalc, ListVersion: composition.sources.Cofactors["CA"].ListVersion},
		composition.sources.Cofactors["CA"])
}

func Test_should_keep_review_reason_in_record_when_parser_flags_compound(t *testing.T) {
	// given
	result := fixtureResult(t, filepath.Join("parity", "no_metalc.cif"))

	// when
	composition, err := compositionFromResult(result)

	// then
	require.NoError(t, err)
	require.NotEmpty(t, composition.cofactors)
	for _, id := range composition.cofactors {
		assert.NotEmpty(t, composition.sources.Cofactors[id].ReviewReason, id)
	}
	assert.Contains(t, composition.sources.Excluded, "GOL")
	assert.Empty(t, composition.sources.Excluded["GOL"].ReviewReason)
}

func Test_should_record_ihm_probe_as_excluded_when_composition_is_derived(t *testing.T) {
	// given
	result := fixtureResult(t, filepath.Join("parity", "ihm_probe.cif"))

	// when
	composition, err := compositionFromResult(result)

	// then
	require.NoError(t, err)
	var probes []string
	for id, source := range composition.sources.Excluded {
		if source.Rule == models.CompoundRuleIHMProbe {
			probes = append(probes, id)
		}
	}
	require.NotEmpty(t, probes)
	for _, probe := range probes {
		assert.NotContains(t, composition.ligands, probe)
		assert.NotContains(t, composition.cofactors, probe)
	}
	assert.Contains(t, composition.ligands, "ATP")
}

func Test_should_list_each_compound_once_when_parser_lists_it_twice(t *testing.T) {
	// given
	result := cif.Result{
		Ligands: []cif.Compound{{ID: "SO4"}, {ID: "SO4", NeedsReview: true, ReviewReason: "often from the buffer"}},
		CompoundSources: cif.CompoundSources{
			Ligands: map[string]cif.CompoundSource{"SO4": {Rule: "list", ListVersion: "2026.10.0"}},
		},
	}

	// when
	composition, err := compositionFromResult(result)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"SO4"}, composition.ligands)
	assert.Empty(t, composition.cofactors)
	assert.NotNil(t, composition.cofactors)
	assert.Equal(t, models.CompoundSources{
		Ligands:   map[string]models.CompoundSource{"SO4": {Rule: models.CompoundRuleList, ListVersion: "2026.10.0", ReviewReason: "often from the buffer"}},
		Cofactors: map[string]models.CompoundSource{},
		Excluded:  map[string]models.CompoundSource{},
	}, composition.sources)
	assert.Equal(t, []models.Glycan{}, composition.glycans)
}

func Test_should_fail_when_parser_gives_rule_backend_does_not_know(t *testing.T) {
	// given
	unknown := cif.Result{CompoundSources: cif.CompoundSources{
		Excluded: map[string]cif.CompoundSource{"GOL": {Rule: "guess"}},
	}}
	depositor := cif.Result{
		Ligands:         []cif.Compound{{ID: "LIG"}},
		CompoundSources: cif.CompoundSources{Ligands: map[string]cif.CompoundSource{"LIG": {Rule: "depositor"}}},
	}

	// when
	_, unknownErr := compositionFromResult(unknown)
	_, depositorErr := compositionFromResult(depositor)

	// then
	require.ErrorContains(t, unknownErr, "excluded compounds: compound GOL: unknown compound rule from the parser: guess")
	require.ErrorContains(t, depositorErr, "ligands: convert the record: compound LIG: the parser gave rule depositor")
}

func Test_should_fail_when_listed_compound_has_no_record(t *testing.T) {
	// given
	result := cif.Result{Cofactors: []cif.Compound{{ID: "ZN"}}}

	// when
	_, err := compositionFromResult(result)

	// then
	require.ErrorContains(t, err, "cofactors: compound ZN has no record")
}

func Test_should_keep_sulfate_for_review_and_leave_glycerol_out_when_composition_is_derived(t *testing.T) {
	// given
	result := fixtureResult(t, "3CEB.cif")

	// when
	composition, err := compositionFromResult(result)

	// then
	require.NoError(t, err)
	assert.Contains(t, composition.ligands, "SO4")
	assert.Equal(t, models.CompoundRuleList, composition.sources.Ligands["SO4"].Rule)
	assert.NotEmpty(t, composition.sources.Ligands["SO4"].ReviewReason)
	assert.Contains(t, composition.sources.Excluded, "GOL")
	assert.NotContains(t, composition.ligands, "GOL")
	assert.NotContains(t, composition.ligands, "MSE")
}
