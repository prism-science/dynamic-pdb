package cif

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_sort_known_compounds_when_reference_is_loaded(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// when / then
	assert.Equal(t, kindSolvent, reference.kind("hoh"))
	assert.Equal(t, kindDraftSolvent, reference.kind("GOL"))
	assert.Equal(t, kindMetalIon, reference.kind("ZN"))
	assert.Equal(t, kindCofactor, reference.kind("hem"))
	for _, id := range []string{"HEM", "HEC", "NAD", "NAP", "NDP", "FAD", "FMN", "PLP", "SAM", "COA", "TPP"} {
		assert.Equal(t, kindCofactor, reference.kind(id), id)
	}
	assert.Equal(t, kindLigand, reference.kind("ATP"))
}

func Test_should_carry_version_and_pending_approval_when_reference_is_draft(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// then
	assert.NotEmpty(t, reference.Version)
	assert.False(t, reference.Approved)
}

func Test_should_not_list_a_compound_twice_when_reference_is_loaded(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// when
	seen := map[string]string{}
	for id := range reference.cofactors {
		seen[id] += "cofactor "
	}
	for id := range reference.metals {
		seen[id] += "metal "
	}
	for id := range reference.solvents {
		seen[id] += "solvent "
	}

	// then
	for id, lists := range seen {
		assert.Len(t, strings.Fields(lists), 1, "%s is in several lists: %s", id, lists)
	}
}

func Test_should_reject_reference_without_version_when_parsing(t *testing.T) {
	// when
	_, err := parseReference([]byte(`{"cofactors": []}`), []byte(`{"version": "1", "solvents": []}`))

	// then
	require.Error(t, err)
}

func Test_should_reject_solvent_list_without_version_when_parsing(t *testing.T) {
	// when
	_, err := parseReference([]byte(`{"version": "1", "cofactors": []}`), []byte(`{"solvents": []}`))

	// then
	require.Error(t, err)
}

func Test_should_report_both_list_versions_when_reference_is_loaded(t *testing.T) {
	// given
	reference, err := DefaultReference()
	require.NoError(t, err)

	// then
	assert.NotEmpty(t, reference.CofactorVersion)
	assert.NotEmpty(t, reference.SolventVersion)
	assert.Contains(t, reference.Version, reference.CofactorVersion)
	assert.Contains(t, reference.Version, reference.SolventVersion)
}

// approvedReference is the embedded reference data with both approval flags set,
// as it will be once researchers sign off.
func approvedReference(t *testing.T) *Reference {
	t.Helper()
	approve := func(data []byte) []byte {
		approved := strings.Replace(string(data), `"approved": false`, `"approved": true`, 1)
		require.NotEqual(t, string(data), approved, "embedded list has no approved flag to flip")
		return []byte(approved)
	}
	reference, err := parseReference(approve(referenceJSON), approve(solventsJSON))
	require.NoError(t, err)
	require.True(t, reference.Approved)
	return reference
}

func Test_should_drop_every_listed_solvent_when_both_lists_are_approved(t *testing.T) {
	// given
	reference := approvedReference(t)

	// when / then
	for _, id := range []string{"HOH", "GOL", "SO4", "PO4", "CIT", "CL"} {
		assert.Equal(t, kindSolvent, reference.kind(id), id)
	}
}

func Test_should_drop_only_water_when_lists_are_not_approved(t *testing.T) {
	// given
	reference, err := parseReference(
		[]byte(`{"version": "1", "approved": true, "cofactors": []}`),
		[]byte(`{"version": "1", "approved": false, "solvents": ["GOL", "SO4"]}`),
	)
	require.NoError(t, err)

	// when / then
	assert.False(t, reference.Approved)
	for _, id := range []string{"HOH", "DOD", "WAT", "H2O", "hoh"} {
		assert.Equal(t, kindSolvent, reference.kind(id), id)
	}
	assert.Equal(t, kindDraftSolvent, reference.kind("SO4"))
	assert.Equal(t, kindDraftSolvent, reference.kind("gol"))
	assert.Equal(t, kindLigand, reference.kind("ATP"))
}

func Test_should_keep_real_ligand_and_flag_it_when_solvent_list_is_not_approved(t *testing.T) {
	// given
	text := "data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 SO4\n3 HOH\n4 ATP\n"
	draft, err := DefaultReference()
	require.NoError(t, err)

	// when
	draftResult, draftErr := ExtractWithReference(Parse(text), draft)
	approvedResult, approvedErr := ExtractWithReference(Parse(text), approvedReference(t))

	// then
	require.NoError(t, draftErr)
	require.NoError(t, approvedErr)
	assert.Equal(t, []string{"ATP", "SO4"}, idsOf(draftResult.Ligands))
	assert.False(t, draftResult.Ligands[0].NeedsReview)
	assert.True(t, draftResult.Ligands[1].NeedsReview)
	assert.Contains(t, draftResult.Ligands[1].ReviewReason, "draft solvent list")
	assert.Contains(t, draftResult.Warnings, DraftWarning)
	assert.Equal(t, []string{"ATP"}, idsOf(approvedResult.Ligands))
	assert.NotContains(t, approvedResult.Warnings, DraftWarning)
}

func Test_should_carry_draft_flag_into_review_items_when_building_field_map(t *testing.T) {
	// given
	text := "data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 SO4\n4 ATP\n"

	// when
	extraction, err := MMCIFExtraction(text, false)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"ATP", "SO4"}, extraction.Fields["ligands"])
	require.Len(t, extraction.Review, 1)
	assert.Equal(t, ReviewItem{List: ListLigands, ID: "SO4", Reason: draftSolventReason}, extraction.Review[0])
	assert.Equal(t, extraction.Review, extraction.Fields[FieldCompoundReview])
	assert.Contains(t, extraction.Warnings, DraftWarning)
}
