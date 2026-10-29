package cif

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mmcifFile joins mmCIF lines into a file.
func mmcifFile(lines ...string) string {
	return "data_X\n" + strings.Join(lines, "\n") + "\n#\n"
}

// entityLoop lists entities as "id type" pairs.
func entityLoop(entities ...string) string {
	return "loop_\n_entity.id\n_entity.type\n" + strings.Join(entities, "\n") + "\n#"
}

// hetatmLoop lists coordinate rows as "group comp asym entity seq auth_seq atom element model".
func hetatmLoop(rows ...string) string {
	return "loop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n" +
		"_atom_site.label_entity_id\n_atom_site.label_seq_id\n_atom_site.auth_seq_id\n" +
		"_atom_site.label_atom_id\n_atom_site.type_symbol\n_atom_site.pdbx_PDB_model_num\n" +
		strings.Join(rows, "\n") + "\n#"
}

func extractTextResult(t *testing.T, text string, truncated bool) Result {
	t.Helper()
	_, result, err := ExtractText(text, truncated)
	require.NoError(t, err)
	return result
}

func source(rule compoundRule) CompoundSource {
	reference, _ := DefaultReference()
	return CompoundSource{Rule: string(rule), ListVersion: reference.Version}
}

func Test_should_not_list_polymer_residues_when_entity_type_is_polymer(t *testing.T) {
	// given: selenomethionine in the chain, as 1A62 has it.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer", "3 water"),
		hetatmLoop(
			"ATOM ALA A 1 1 1 CA C 1",
			"HETATM MSE A 1 2 2 SE SE 1",
			"HETATM MSE A 1 2 2 CA C 1",
			"HETATM ATP B 2 . 101 PG P 1",
			"HETATM ATP B 2 . 101 PB P 1",
			"HETATM HOH C 3 . 201 O O 1",
		),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{{ID: "ATP"}}, result.Ligands)
	assert.Equal(t, map[string]CompoundSource{"ATP": source(ruleList)}, result.CompoundSources.Ligands)
	assert.Empty(t, result.CompoundSources.Excluded, "water and polymer residues are never recorded")
}

func Test_should_place_glycans_and_macrolides_in_ligands_when_entity_type_says_so(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 polymer", "2 branched", "3 macrolide"),
		hetatmLoop(
			"HETATM NAG B 2 . 1 C1 C 1",
			"HETATM NAG B 2 . 1 C2 C 1",
			"HETATM BMA B 2 . 2 C1 C 1",
			"HETATM BMA B 2 . 2 C2 C 1",
			"HETATM ERY C 3 . 1 C1 C 1",
			"HETATM ERY C 3 . 1 C2 C 1",
		),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []string{"BMA", "ERY", "NAG"}, idsOf(result.Ligands))
	assert.Equal(t, map[string]CompoundSource{
		"BMA": source(ruleBranched), "NAG": source(ruleBranched), "ERY": source(ruleMacrolide),
	}, result.CompoundSources.Ligands)
}

func Test_should_sort_by_lists_and_flag_when_entity_type_is_unknown(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 polymer", "2 'cyclic peptide'"),
		hetatmLoop("HETATM QQQ B 2 . 1 C1 C 1", "HETATM QQQ B 2 . 1 C2 C 1"),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	reason := `unknown entity type "cyclic peptide": confirm where this compound belongs`
	assert.Equal(t, []Compound{{ID: "QQQ", NeedsReview: true, ReviewReason: reason}}, result.Ligands)
	assert.Contains(t, result.Warnings, `entity 2 has unknown type "cyclic peptide": its compounds are sorted by the reference lists and flagged for review`)
}

func Test_should_sort_by_ccd_type_and_chain_position_when_file_has_no_entity(t *testing.T) {
	// given: MSE in a chain (label_seq_id set) and a free MSE, a sugar, a ligand.
	text := mmcifFile(hetatmLoop(
		"HETATM MSE A . 2 2 SE SE 1",
		"HETATM MSE A . 2 2 CA C 1",
		"HETATM MSE B . . 301 SE SE 1",
		"HETATM MSE B . . 301 CA C 1",
		"HETATM NAG C . . 401 C1 C 1",
		"HETATM NAG C . . 401 C2 C 1",
		"HETATM ATP D . . 501 PG P 1",
		"HETATM ATP D . . 501 PB P 1",
	))

	// when
	result := extractTextResult(t, text, false)

	// then: the free MSE is not in a chain, and with no SEQRES to show the
	// polymer uses it, it is an ordinary ligand.
	assert.Equal(t, []Compound{{ID: "ATP"}, {ID: "MSE"}, {ID: "NAG"}}, result.Ligands)
	assert.Equal(t, map[string]CompoundSource{
		"ATP": source(ruleList), "MSE": source(ruleList), "NAG": source(ruleBranched),
	}, result.CompoundSources.Ligands)
}

func Test_should_use_the_files_own_chem_comp_type_when_it_has_one_and_flag_an_unknown_one(t *testing.T) {
	// given: the file calls NAG a non-polymer and QQQ something unknown.
	text := mmcifFile(
		"loop_\n_chem_comp.id\n_chem_comp.type\nNAG non-polymer\nQQQ 'mystery linking'\n#",
		hetatmLoop(
			"HETATM NAG C . . 401 C1 C 1",
			"HETATM NAG C . . 401 C2 C 1",
			"HETATM QQQ D . . 501 C1 C 1",
			"HETATM QQQ D . . 501 C2 C 1",
		),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{
		{ID: "NAG"},
		{ID: "QQQ", NeedsReview: true, ReviewReason: `unknown chemical component type "mystery linking": confirm where this compound belongs`},
	}, result.Ligands)
	assert.Equal(t, source(ruleList), result.CompoundSources.Ligands["NAG"])
	assert.Contains(t, result.Warnings, `QQQ has unknown chemical component type "mystery linking": sorted by the reference lists and flagged for review`)
}

// flagLoop lists depositor flags as "comp asym auth_seq feature".
func flagLoop(rows ...string) string {
	return "loop_\n_pdbx_entity_instance_feature.ordinal\n_pdbx_entity_instance_feature.comp_id\n" +
		"_pdbx_entity_instance_feature.asym_id\n_pdbx_entity_instance_feature.auth_seq_num\n" +
		"_pdbx_entity_instance_feature.feature_type\n" + strings.Join(rows, "\n") + "\n#"
}

func Test_should_follow_the_depositor_flag_when_it_contradicts_the_lists(t *testing.T) {
	// given: biotin (a listed cofactor) is the subject; sulfate (ambiguous) has
	// no functional role; zinc is flagged OTHER.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer", "3 non-polymer", "4 non-polymer"),
		flagLoop("1 BTN ? ? 'SUBJECT OF INVESTIGATION'", "2 SO4 ? ? 'NO FUNCTIONAL ROLE'", "3 ZN ? ? OTHER"),
		hetatmLoop(
			"HETATM BTN B 2 . 101 C1 C 1",
			"HETATM BTN B 2 . 101 C2 C 1",
			"HETATM SO4 C 3 . 201 S S 1",
			"HETATM SO4 C 3 . 201 O1 O 1",
			"HETATM ZN D 4 . 301 ZN ZN 1",
		),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{{ID: "BTN"}}, result.Ligands)
	assert.Equal(t, source(ruleDepositorFlag), result.CompoundSources.Ligands["BTN"])
	assert.Equal(t, map[string]CompoundSource{"SO4": source(ruleDepositorFlag)}, result.CompoundSources.Excluded)
	assert.Equal(t, []Compound{{ID: "ZN", NeedsReview: true, ReviewReason: noMetalcRowsReason}}, result.Cofactors)
	assert.Equal(t, source(ruleMetalc), result.CompoundSources.Cofactors["ZN"])
}

func Test_should_flag_only_that_instance_when_the_depositor_flag_has_a_location(t *testing.T) {
	// given: two glycerols; the depositor marks the one at C 202.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer"),
		flagLoop("1 GOL C 202 'SUBJECT OF INVESTIGATION'"),
		hetatmLoop(
			"HETATM GOL B 2 . 201 C1 C 1",
			"HETATM GOL B 2 . 201 C2 C 1",
			"HETATM GOL C 2 . 202 C1 C 1",
			"HETATM GOL C 2 . 202 C2 C 1",
		),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{{ID: "GOL"}}, result.Ligands)
	assert.Equal(t, source(ruleDepositorFlag), result.CompoundSources.Ligands["GOL"])
	assert.Equal(t, source(ruleList), result.CompoundSources.Excluded["GOL"])
}

func Test_should_follow_the_first_matching_depositor_flag_in_file_order(t *testing.T) {
	cases := []struct {
		name     string
		flags    []string
		ligands  map[string]CompoundSource
		excluded map[string]CompoundSource
	}{
		{
			name:     "should_exclude_ATP_when_the_flag_for_every_ATP_comes_first",
			flags:    []string{"1 ATP ? ? 'NO FUNCTIONAL ROLE'", "2 ATP B 101 'SUBJECT OF INVESTIGATION'"},
			ligands:  map[string]CompoundSource{},
			excluded: map[string]CompoundSource{"ATP": source(ruleDepositorFlag)},
		},
		{
			name:     "should_keep_ATP_when_the_flag_for_that_instance_comes_first",
			flags:    []string{"1 ATP B 101 'SUBJECT OF INVESTIGATION'", "2 ATP ? ? 'NO FUNCTIONAL ROLE'"},
			ligands:  map[string]CompoundSource{"ATP": source(ruleDepositorFlag)},
			excluded: map[string]CompoundSource{},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// given: both flags match ATP at B 101.
			text := mmcifFile(
				entityLoop("1 polymer", "2 non-polymer"),
				flagLoop(testCase.flags...),
				hetatmLoop("HETATM ATP B 2 . 101 PG P 1", "HETATM ATP B 2 . 101 PB P 1"),
			)

			// when
			result := extractTextResult(t, text, false)

			// then
			assert.Equal(t, testCase.ligands, result.CompoundSources.Ligands)
			assert.Equal(t, testCase.excluded, result.CompoundSources.Excluded)
		})
	}
}

// metalcLoop lists metalc rows as "comp1 asym1 seq1 comp2 asym2 seq2".
func metalcLoop(rows ...string) string {
	numbered := make([]string, len(rows))
	for index, row := range rows {
		numbered[index] = "metalc" + strconv.Itoa(index+1) + " metalc " + row
	}
	return "loop_\n_struct_conn.id\n_struct_conn.conn_type_id\n" +
		"_struct_conn.ptnr1_label_comp_id\n_struct_conn.ptnr1_label_asym_id\n_struct_conn.ptnr1_auth_seq_id\n" +
		"_struct_conn.ptnr2_label_comp_id\n_struct_conn.ptnr2_label_asym_id\n_struct_conn.ptnr2_auth_seq_id\n" +
		strings.Join(numbered, "\n") + "\n#"
}

func Test_should_not_count_a_metal_link_when_the_depositor_leaves_the_partner_out(t *testing.T) {
	// given: zinc's only partner is an ATP the depositor marks as having no
	// functional role.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer", "3 non-polymer"),
		flagLoop("1 ATP B 101 'NO FUNCTIONAL ROLE'"),
		metalcLoop("ZN C 201 ATP B 101"),
		hetatmLoop("HETATM ATP B 2 . 101 PG P 1", "HETATM ATP B 2 . 101 PB P 1", "HETATM ZN C 3 . 201 ZN ZN 1"),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{{ID: "ZN", NeedsReview: true, ReviewReason: unlinkedMetalReason}}, result.Ligands)
	assert.Empty(t, result.Cofactors)
	assert.Equal(t, source(ruleMetalc), result.CompoundSources.Ligands["ZN"])
	assert.Equal(t, map[string]CompoundSource{"ATP": source(ruleDepositorFlag)}, result.CompoundSources.Excluded)
}

func Test_should_count_a_metal_link_when_the_depositor_keeps_a_solvent_partner(t *testing.T) {
	// given: magnesium's only partner is a glycerol the depositor marks as the
	// subject of investigation.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer", "3 non-polymer"),
		flagLoop("1 GOL B 101 'SUBJECT OF INVESTIGATION'"),
		metalcLoop("MG C 201 GOL B 101"),
		hetatmLoop("HETATM GOL B 2 . 101 C1 C 1", "HETATM GOL B 2 . 101 C2 C 1", "HETATM MG C 3 . 201 MG MG 1"),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{{ID: "GOL"}}, result.Ligands)
	assert.Equal(t, []Compound{{ID: "MG"}}, result.Cofactors)
	assert.Equal(t, source(ruleDepositorFlag), result.CompoundSources.Ligands["GOL"])
	assert.Equal(t, source(ruleMetalc), result.CompoundSources.Cofactors["MG"])
}

func Test_should_flag_compound_and_warn_when_depositor_flag_is_unknown(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer"),
		flagLoop("1 ATP ? ? 'MAYBE IMPORTANT'"),
		hetatmLoop("HETATM ATP B 2 . 101 PG P 1", "HETATM ATP B 2 . 101 PB P 1"),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{{ID: "ATP", NeedsReview: true, ReviewReason: `unknown depositor flag "MAYBE IMPORTANT": confirm where this compound belongs`}}, result.Ligands)
	assert.Equal(t, source(ruleList), result.CompoundSources.Ligands["ATP"])
	assert.Contains(t, result.Warnings, `ATP has unknown depositor flag "MAYBE IMPORTANT": no override applied, compound flagged for review`)
}

func Test_should_add_the_bound_cofactor_when_a_covalent_cofactor_residue_is_modified(t *testing.T) {
	// given: LLP (PLP on a lysine) and MSE are modified residues of the chain.
	text := mmcifFile(
		entityLoop("1 polymer"),
		"loop_\n_pdbx_struct_mod_residue.id\n_pdbx_struct_mod_residue.label_comp_id\n_pdbx_struct_mod_residue.parent_comp_id\n1 LLP LYS\n2 MSE MET\n#",
		hetatmLoop("HETATM LLP A 1 5 5 C4A C 1", "HETATM LLP A 1 5 5 NZ N 1"),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Empty(t, result.Ligands)
	assert.Equal(t, []Compound{{ID: "PLP"}}, result.Cofactors)
	assert.Equal(t, map[string]CompoundSource{"PLP": source(ruleCovalentMap)}, result.CompoundSources.Cofactors)
}

func Test_should_keep_wurcs_descriptors_when_file_has_branched_entities(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 polymer", "2 branched", "3 branched"),
		"loop_\n_pdbx_entity_branch_list.entity_id\n_pdbx_entity_branch_list.comp_id\n_pdbx_entity_branch_list.num\n2 NAG 1\n2 NAG 2\n3 NAG 1\n3 FUC 2\n#",
		"loop_\n_pdbx_entity_branch_descriptor.ordinal\n_pdbx_entity_branch_descriptor.entity_id\n_pdbx_entity_branch_descriptor.descriptor\n"+
			"_pdbx_entity_branch_descriptor.type\n_pdbx_entity_branch_descriptor.program\n_pdbx_entity_branch_descriptor.program_version\n"+
			"1 2 'WURCS=2.0/1,2,1/[a2122h-1b_1-5_2*NCC/3=O]/1-1/a4-b1' WURCS PDB2Glycan 1.1.0\n"+
			"2 2 'DGlcpNAcb1-4DGlcpNAcb1-ROH' 'Glycam Condensed Sequence' GMML 1.0\n"+
			"3 3 '[][b-D-GlcpNAc]{}' LINUCS PDB-CARE ?\n#",
		hetatmLoop("HETATM NAG B 2 . 1 C1 C 1", "HETATM NAG B 2 . 1 C2 C 1"),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Glycan{
		{EntityID: "2", CompoundIDs: []string{"NAG"}, Descriptors: []GlycanDescriptor{{
			Descriptor: "WURCS=2.0/1,2,1/[a2122h-1b_1-5_2*NCC/3=O]/1-1/a4-b1", Type: "WURCS", Program: "PDB2Glycan", ProgramVersion: "1.1.0",
		}}},
		{EntityID: "3", CompoundIDs: []string{"FUC", "NAG"}, Descriptors: []GlycanDescriptor{}},
	}, result.Glycans)
}

func Test_should_flag_glycan_compounds_when_descriptor_type_is_unknown(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 polymer", "2 branched"),
		"loop_\n_pdbx_entity_branch_descriptor.entity_id\n_pdbx_entity_branch_descriptor.descriptor\n_pdbx_entity_branch_descriptor.type\n2 'x' GLYCOCT\n#",
		hetatmLoop("HETATM NAG B 2 . 1 C1 C 1", "HETATM NAG B 2 . 1 C2 C 1"),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, []Compound{{ID: "NAG", NeedsReview: true, ReviewReason: `unknown glycan descriptor type "GLYCOCT": confirm the glycan`}}, result.Ligands)
	assert.Contains(t, result.Warnings, `entity 2 has a glycan descriptor of unknown type "GLYCOCT": its compounds are flagged for review`)
}

func Test_should_list_compounds_of_every_model_only_when_file_is_integrative(t *testing.T) {
	// given: ATP is only in model 2.
	rows := []string{
		"ATOM ALA A 1 1 1 CA C 1",
		"ATOM ALA A 1 1 1 CA C 2",
		"HETATM ATP B 2 . 101 PG P 2",
		"HETATM ATP B 2 . 101 PB P 2",
	}
	plain := mmcifFile(entityLoop("1 polymer", "2 non-polymer"), hetatmLoop(rows...))
	integrative := mmcifFile(entityLoop("1 polymer", "2 non-polymer"), "loop_\n_ihm_model_list.model_id\n1\n2\n#", hetatmLoop(rows...))

	// when
	plainResult := extractTextResult(t, plain, false)
	integrativeResult := extractTextResult(t, integrative, false)

	// then
	assert.Empty(t, plainResult.Ligands)
	assert.Equal(t, []Compound{{ID: "ATP"}}, integrativeResult.Ligands)
	require.NotNil(t, integrativeResult.Atoms)
	assert.Equal(t, 1, integrativeResult.Atoms.AtomCount, "atoms are counted in the first model only")
}

func Test_should_skip_rules_and_warn_when_a_cut_off_read_may_have_lost_their_categories(t *testing.T) {
	// given: the head ends inside the coordinates; the flags and modified
	// residues would come after them.
	text := "data_X\n" + entityLoop("1 polymer", "2 non-polymer") + "\n" +
		hetatmLoop("HETATM ATP B 2 . 101 PG P 1", "HETATM ATP B 2 . 101 PB P 1") + "\n" +
		"loop_\n_atom_site_anisotrop.id\n1"

	// when
	result := extractTextResult(t, text, true)

	// then
	assert.Equal(t, []Compound{{ID: "ATP"}}, result.Ligands)
	assert.Contains(t, result.Warnings, "the read limit may have cut these categories, so the rules they feed were not applied: "+
		"_pdbx_entity_instance_feature (depositor flags), _pdbx_struct_mod_residue (covalently bound cofactors)")
}

func Test_should_not_apply_depositor_flags_when_the_read_limit_cut_their_category_open(t *testing.T) {
	// given: the head ends inside the flag loop, so later flags may be missing.
	text := "data_X\n" + entityLoop("1 polymer", "2 non-polymer") + "\n" +
		hetatmLoop("HETATM GOL B 2 . 101 C1 C 1", "HETATM GOL B 2 . 101 C2 C 1") + "\n" +
		"loop_\n_pdbx_entity_instance_feature.ordinal\n_pdbx_entity_instance_feature.comp_id\n" +
		"_pdbx_entity_instance_feature.feature_type\n1 GOL 'SUBJECT OF INVESTIGATION'\n2 ATP"

	// when
	result := extractTextResult(t, text, true)

	// then
	assert.Empty(t, result.Ligands, "a flag from a cut-off category is never applied")
	assert.Equal(t, source(ruleList), result.CompoundSources.Excluded["GOL"])
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "_pdbx_entity_instance_feature (depositor flags)")
}

func Test_should_not_apply_a_pair_form_depositor_flag_when_the_read_limit_cut_it_open(t *testing.T) {
	// given: the head ends inside the flag's items, so its asym_id and
	// auth_seq_num may follow in the part not read.
	text := "data_X\n" + entityLoop("1 polymer", "2 non-polymer") + "\n" +
		hetatmLoop("HETATM GOL B 2 . 101 C1 C 1", "HETATM GOL B 2 . 101 C2 C 1") + "\n" +
		"_pdbx_entity_instance_feature.comp_id GOL\n_pdbx_entity_instance_feature.feature_type 'SUBJECT OF INVESTIGATION'\n"

	// when
	result := extractTextResult(t, text, true)

	// then
	assert.Empty(t, result.Ligands, "a flag from a cut-off category is never applied")
	assert.Equal(t, source(ruleList), result.CompoundSources.Excluded["GOL"])
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "_pdbx_entity_instance_feature (depositor flags)")
}

func Test_should_apply_a_pair_form_depositor_flag_when_a_hash_line_closed_it_before_the_cut(t *testing.T) {
	// given: archive files end every category with a "#" line.
	text := "data_X\n" + entityLoop("1 polymer", "2 non-polymer") + "\n" +
		hetatmLoop("HETATM GOL B 2 . 101 C1 C 1", "HETATM GOL B 2 . 101 C2 C 1") + "\n" +
		"_pdbx_entity_instance_feature.comp_id GOL\n_pdbx_entity_instance_feature.feature_type 'SUBJECT OF INVESTIGATION'\n#\n"

	// when
	result := extractTextResult(t, text, true)

	// then
	assert.Equal(t, []Compound{{ID: "GOL"}}, result.Ligands)
	assert.Equal(t, source(ruleDepositorFlag), result.CompoundSources.Ligands["GOL"])
	assert.NotContains(t, strings.Join(result.Warnings, "\n"), "_pdbx_entity_instance_feature")
}

func Test_should_record_the_more_specific_rule_when_an_id_is_placed_twice_in_one_group(t *testing.T) {
	// given: NAG in a glycan and NAG as a lone non-polymer.
	text := mmcifFile(
		entityLoop("1 polymer", "2 branched", "3 non-polymer"),
		hetatmLoop(
			"HETATM NAG B 2 . 1 C1 C 1",
			"HETATM NAG B 2 . 1 C2 C 1",
			"HETATM NAG C 3 . 301 C1 C 1",
			"HETATM NAG C 3 . 301 C2 C 1",
		),
	)

	// when
	result := extractTextResult(t, text, false)

	// then
	assert.Equal(t, map[string]CompoundSource{"NAG": source(ruleBranched)}, result.CompoundSources.Ligands)
}

func Test_should_fail_when_asked_to_rank_an_unknown_rule(t *testing.T) {
	// when
	_, err := rulePrecedence("curator")

	// then
	require.Error(t, err)
}
