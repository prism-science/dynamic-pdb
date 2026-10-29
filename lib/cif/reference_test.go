package cif

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approvedReference is the embedded reference data, which researchers approved.
func approvedReference(t *testing.T) *Reference {
	t.Helper()
	reference, err := DefaultReference()
	require.NoError(t, err)
	require.True(t, reference.Approved)
	return reference
}

// draftReference is the embedded reference data with the solvent list set back
// to draft, as a future list version is until researchers approve it.
func draftReference(t *testing.T) *Reference {
	t.Helper()
	files := embeddedReferenceFiles()
	draft := strings.Replace(string(files.solvents), `"approved": true`, `"approved": false`, 1)
	require.NotEqual(t, string(files.solvents), draft, "embedded solvent list has no approved flag to flip")
	files.solvents = []byte(draft)
	reference, err := parseReference(files)
	require.NoError(t, err)
	require.False(t, reference.Approved)
	return reference
}

func Test_should_sort_known_compounds_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)

	// when / then
	assert.Equal(t, kindSolvent, reference.kind("hoh"))
	assert.Equal(t, kindSolvent, reference.kind("GOL"))
	assert.Equal(t, kindSolvent, reference.kind("BTB"))
	assert.Equal(t, kindSolventUnlessMetalLinked, reference.kind("IMD"))
	assert.Equal(t, kindAmbiguous, reference.kind("so4"))
	assert.Equal(t, kindMetalIon, reference.kind("ZN"))
	assert.Equal(t, kindCofactor, reference.kind("hem"))
	for _, id := range []string{"HEM", "HEC", "NAD", "NAP", "NDP", "FAD", "FMN", "PLP", "SAM", "COA", "TPP", "UQ5", "UQ6", "THG"} {
		assert.Equal(t, kindCofactor, reference.kind(id), id)
	}
	assert.Equal(t, kindLigand, reference.kind("ATP"))
	assert.Equal(t, kindLigand, reference.kind("TDP"), "TDP is not in the dictionary and was removed")
}

func Test_should_name_every_file_version_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)

	// then
	assert.Equal(t,
		"cofactors:2026.10.0+metals:2026.10.0+solvents:2026.10.0+covalent:2026.10.0+ccd-types:2026.10.0",
		reference.Version)
}

func Test_should_put_every_compound_in_one_group_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)

	// when
	seen := map[string][]string{}
	for id := range reference.cofactors {
		seen[id] = append(seen[id], "cofactors")
	}
	for id := range reference.metals {
		seen[id] = append(seen[id], "metal_ions")
	}
	for id := range reference.solvents {
		seen[id] = append(seen[id], "solvents")
	}
	for id := range reference.ambiguous {
		seen[id] = append(seen[id], "ambiguous")
	}
	for residue := range reference.covalentCofactors {
		seen[residue] = append(seen[residue], "covalent residue")
	}

	// then
	for id, groups := range seen {
		assert.Len(t, groups, 1, "%s is in several groups: %v", id, groups)
	}
}

func Test_should_give_paired_ids_the_same_decision_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)

	// when / then
	for _, pair := range [][2]string{{"CIT", "FLC"}, {"ACT", "ACY"}, {"MPD", "MRD"}} {
		assert.Equal(t, reference.kind(pair[0]), reference.kind(pair[1]), "%s and %s", pair[0], pair[1])
	}
}

func Test_should_hold_the_reviewed_lists_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)
	reviewedMetals := []string{
		"AG", "AL", "AU", "AU3", "BA", "CA", "CD", "CO", "3CO", "CR", "CS", "CU", "CU1", "CU3",
		"EU", "EU3", "FE", "FE2", "GA", "GD", "GD3", "HG", "K", "LA", "LI", "MG", "MN", "MN3",
		"MO", "NA", "NI", "3NI", "PB", "PT", "RB", "SR", "TB", "TL", "V", "W", "YB", "ZN",
		"PD", "RU", "RH", "IR", "OS", "SM", "LU", "HO", "DY", "ND", "CE", "PR", "Y1", "ZR",
		"IN", "4TI", "SB", "BS3",
	}

	// then
	assert.Len(t, reference.cofactors, 57)
	assert.Len(t, reference.solvents, 40, "36 drops and 4 water IDs")
	assert.ElementsMatch(t, []string{"SO4", "PO4", "CIT", "FLC", "F", "CL", "BR", "IOD"}, keys(reference.ambiguous))
	assert.ElementsMatch(t, []string{"IMD"}, keys(reference.keepWhenMetalLinked))
	assert.Len(t, reviewedMetals, 60)
	for _, id := range reviewedMetals {
		assert.Contains(t, reference.metals, id, "reviewed metal %s must stay on the generated list", id)
	}
	assert.Equal(t, map[string]string{"LLP": "PLP", "LYR": "RET"}, reference.covalentCofactors)
}

func Test_should_not_list_quinone_lookalikes_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)

	// when / then: IDs are never added by pattern (MQ6 is harringtonine).
	for _, id := range []string{"UQ3", "MQ4", "MQ6"} {
		assert.Equal(t, kindLigand, reference.kind(id), id)
	}
}

func Test_should_sort_element_symbols_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)

	// when / then
	for _, symbol := range []string{"ZN", "Zn", "SB", "Na", "U"} {
		assert.Equal(t, elementMetal, reference.elementClass(symbol), symbol)
	}
	for _, symbol := range []string{"CL", "Br", "XE", "O", "D", "SE"} {
		assert.Equal(t, elementNonmetal, reference.elementClass(symbol), symbol)
	}
	for _, symbol := range []string{"", "X", "QQ"} {
		assert.Equal(t, elementUnknown, reference.elementClass(symbol), symbol)
	}
	for symbol := range reference.metalElements {
		assert.NotContains(t, reference.nonmetalElements, symbol)
	}
}

func Test_should_know_ccd_categories_and_covalent_cofactors_when_reference_is_loaded(t *testing.T) {
	// given
	reference := approvedReference(t)

	// when / then
	for id, want := range map[string]ccdCategory{"MSE": ccdPeptide, "LLP": ccdPeptide, "NAG": ccdSaccharide, "DA": ccdNucleotide, "U": ccdNucleotide} {
		got, ok := reference.ccdCategoryOf(id)
		assert.True(t, ok, id)
		assert.Equal(t, want, got, id)
	}
	_, ok := reference.ccdCategoryOf("ATP")
	assert.False(t, ok, "ATP is a non-polymer, sorted by the lists")
	cofactor, ok := reference.covalentCofactor("llp")
	assert.True(t, ok)
	assert.Equal(t, "PLP", cofactor)
}

func Test_should_reject_a_file_without_version_when_parsing(t *testing.T) {
	cases := map[string]func(*referenceFiles){
		"cofactors": func(files *referenceFiles) { files.cofactors = []byte(`{"cofactors": []}`) },
		"metal ions": func(files *referenceFiles) {
			files.metalIons = []byte(`{"metal_ions": []}`)
		},
		"solvents": func(files *referenceFiles) { files.solvents = []byte(`{"solvents": []}`) },
		"covalent": func(files *referenceFiles) {
			files.covalentCofactors = []byte(`{"covalent_cofactors": []}`)
		},
		"ccd types": func(files *referenceFiles) { files.ccdTypes = []byte(`{"peptide": []}`) },
	}
	for name, breakFile := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			files := embeddedReferenceFiles()
			breakFile(&files)

			// when
			_, err := parseReference(files)

			// then
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no version")
		})
	}
}

func Test_should_reject_covalent_map_to_a_compound_that_is_not_a_cofactor_when_parsing(t *testing.T) {
	// given
	files := embeddedReferenceFiles()
	files.covalentCofactors = []byte(`{"version": "1", "approved": true, "covalent_cofactors": [{"residue": "LLP", "cofactor": "ATP"}]}`)

	// when
	_, err := parseReference(files)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ATP, which is not on the cofactor list")
}

func Test_should_reject_metal_linked_exception_missing_from_solvent_list_when_parsing(t *testing.T) {
	// given
	files := embeddedReferenceFiles()
	files.solvents = []byte(`{"version": "1", "approved": true, "solvents": ["GOL"], "keep_when_metal_linked": ["IMD"]}`)

	// when
	_, err := parseReference(files)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IMD is kept when metal-linked but is not on the solvent list")
}

func Test_should_drop_only_water_when_lists_are_not_approved(t *testing.T) {
	// given
	reference := draftReference(t)

	// when / then
	for _, id := range []string{"HOH", "DOD", "WAT", "H2O", "hoh"} {
		assert.Equal(t, kindSolvent, reference.kind(id), id)
	}
	assert.Equal(t, kindDraftSolvent, reference.kind("gol"))
	assert.Equal(t, kindDraftSolvent, reference.kind("IMD"))
	assert.Equal(t, kindAmbiguous, reference.kind("SO4"), "ambiguous compounds are kept and flagged either way")
	assert.Equal(t, kindLigand, reference.kind("ATP"))
}

func Test_should_keep_buffer_and_flag_it_only_while_solvent_list_is_draft(t *testing.T) {
	// given
	text := "data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 GOL\n3 HOH\n4 ATP\n5 SO4\n"

	// when
	draftResult, draftErr := ExtractWithReference(Parse(text), draftReference(t))
	approvedResult, approvedErr := ExtractWithReference(Parse(text), approvedReference(t))

	// then
	require.NoError(t, draftErr)
	require.NoError(t, approvedErr)
	assert.Equal(t, []Compound{
		{ID: "ATP"},
		{ID: "GOL", NeedsReview: true, ReviewReason: draftSolventReason},
		{ID: "SO4", NeedsReview: true, ReviewReason: ambiguousReason},
	}, draftResult.Ligands)
	assert.Contains(t, draftResult.Warnings, DraftWarning)
	assert.Equal(t, []Compound{
		{ID: "ATP"},
		{ID: "SO4", NeedsReview: true, ReviewReason: ambiguousReason},
	}, approvedResult.Ligands)
	assert.NotContains(t, approvedResult.Warnings, DraftWarning)
}

func Test_should_carry_review_flags_into_review_items_when_building_field_map(t *testing.T) {
	// given
	text := "data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 SO4\n3 GOL\n4 ATP\n"

	// when
	approved, approvedErr := MMCIFExtraction(text, false)
	draft, draftErr := mmcifExtraction(context.Background(), text, false, draftReference(t))

	// then
	require.NoError(t, approvedErr)
	assert.Equal(t, []string{"ATP", "SO4"}, approved.Fields["ligands"])
	assert.Equal(t, []ReviewItem{{List: ListLigands, ID: "SO4", Reason: ambiguousReason}}, approved.Review)
	assert.Equal(t, approved.Review, approved.Fields[FieldCompoundReview])
	assert.NotContains(t, approved.Warnings, DraftWarning)
	require.NoError(t, draftErr)
	assert.Equal(t, []string{"ATP", "GOL", "SO4"}, draft.Fields["ligands"])
	assert.Contains(t, draft.Review, ReviewItem{List: ListLigands, ID: "GOL", Reason: draftSolventReason})
	assert.Contains(t, draft.Warnings, DraftWarning)
}

func keys(set map[string]struct{}) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	return values
}
