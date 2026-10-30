package cif

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(contents)
}

func extractFixture(t *testing.T, name string) Result {
	t.Helper()
	result, err := Extract(Parse(readFixture(t, name)))
	require.NoError(t, err)
	return result
}

func idsOf(compounds []Compound) []string {
	ids := make([]string, 0, len(compounds))
	for _, compound := range compounds {
		ids = append(ids, compound.ID)
	}
	return ids
}

func Test_should_keep_heme_as_cofactor_and_flag_phosphate_when_file_has_several_entities(t *testing.T) {
	// given
	text := readFixture(t, "4HHB.cif")

	// when
	result, err := ExtractWithReference(Parse(text), approvedReference(t))

	// then
	require.NoError(t, err)
	require.NotNil(t, result.Atoms)
	assert.Equal(t, 420, result.Atoms.AtomCount)
	assert.Equal(t, 32, result.Atoms.ModeledResidues)
	assert.Equal(t, 4, result.Atoms.ProteinChains)
	assert.Equal(t, []Compound{{ID: "PO4", NeedsReview: true, ReviewReason: ambiguousReason}}, result.Ligands)
	assert.Equal(t, []string{"HEM"}, idsOf(result.Cofactors))
	assert.False(t, result.Cofactors[0].NeedsReview)
	assert.Len(t, result.Connections, 4)
	assert.Equal(t, "metalc", result.Connections[0].ConnTypeID)
	assert.Equal(t, "HIS", result.Connections[0].Partner1.LabelCompID)
	assert.Equal(t, "NE2", result.Connections[0].Partner1.LabelAtomID)
	assert.Equal(t, "HEM", result.Connections[0].Partner2.LabelCompID)
	assert.Equal(t, "FE", result.Connections[0].Partner2.LabelAtomID)
	assert.Equal(t, "142", result.Connections[0].Partner2.AuthSeqID)
	assert.Equal(t, "metalc1", result.Connections[0].ID)
}

func Test_should_keep_metal_as_cofactor_when_metalc_row_links_it_to_protein(t *testing.T) {
	// given
	text := readFixture(t, "3CLN.cif")

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"CA"}, idsOf(result.Cofactors))
	assert.False(t, result.Cofactors[0].NeedsReview)
	assert.Empty(t, result.Ligands)
}

func Test_should_flag_metal_for_review_when_file_has_no_metalc_rows(t *testing.T) {
	// given
	lines := strings.Split(readFixture(t, "3CLN.cif"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, "metalc") {
			kept = append(kept, line)
		}
	}

	// when
	result, err := Extract(Parse(strings.Join(kept, "\n")))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"CA"}, idsOf(result.Cofactors))
	assert.True(t, result.Cofactors[0].NeedsReview)
	assert.NotEmpty(t, result.Cofactors[0].ReviewReason)
	assert.Empty(t, result.Ligands)
}

// metalcText is a file of compounds without coordinates and metalc rows
// between the given partner pairs.
func metalcText(compounds []string, pairs [][2]string) string {
	var builder strings.Builder
	builder.WriteString("data_X\nloop_\n_struct_conn.id\n_struct_conn.conn_type_id\n" +
		"_struct_conn.ptnr1_label_comp_id\n_struct_conn.ptnr2_label_comp_id\n")
	for index, pair := range pairs {
		builder.WriteString("metalc" + strconv.Itoa(index+1) + " metalc " + pair[0] + " " + pair[1] + "\n")
	}
	builder.WriteString("loop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n")
	for index, compound := range compounds {
		builder.WriteString(strconv.Itoa(index+2) + " " + compound + "\n")
	}
	return builder.String()
}

func Test_should_flag_metal_as_cofactor_when_metal_only_binds_water(t *testing.T) {
	// given
	text := metalcText([]string{"ZN", "MG", "HOH"}, [][2]string{{"ZN", "HOH"}, {"MG", "ASP"}})

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []Compound{
		{ID: "MG"},
		{ID: "ZN", NeedsReview: true, ReviewReason: waterOnlyMetalReason},
	}, result.Cofactors)
	assert.Empty(t, result.Ligands)
}

func Test_should_keep_metal_unflagged_when_it_binds_water_and_protein(t *testing.T) {
	// given
	text := metalcText([]string{"ZN", "HOH"}, [][2]string{{"ZN", "HOH"}, {"CYS", "ZN"}})

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []Compound{{ID: "ZN"}}, result.Cofactors)
}

func Test_should_keep_imidazole_and_link_its_metal_when_metalc_joins_them(t *testing.T) {
	// given
	text := metalcText([]string{"NI", "IMD"}, [][2]string{{"NI", "IMD"}})

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []Compound{{ID: "IMD"}}, result.Ligands)
	assert.Equal(t, []Compound{{ID: "NI"}}, result.Cofactors)
}

func Test_should_drop_imidazole_when_no_metalc_row_binds_it(t *testing.T) {
	// given
	text := metalcText([]string{"NI", "IMD"}, [][2]string{{"NI", "HIS"}})

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Empty(t, result.Ligands)
	assert.Equal(t, []Compound{{ID: "NI"}}, result.Cofactors)
}

func Test_should_not_count_a_dropped_buffer_as_metal_link_when_metal_binds_only_glycerol(t *testing.T) {
	// given
	text := metalcText([]string{"ZN", "GOL"}, [][2]string{{"ZN", "GOL"}})

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []Compound{{ID: "ZN", NeedsReview: true, ReviewReason: unlinkedMetalReason}}, result.Ligands)
	assert.Empty(t, result.Cofactors)
}

func Test_should_keep_ambiguous_compounds_flagged_when_lists_are_approved(t *testing.T) {
	// given
	text := metalcText([]string{"SO4", "PO4", "CIT", "FLC", "F", "CL", "BR", "IOD"}, nil)

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Empty(t, result.Cofactors)
	assert.Equal(t, []string{"BR", "CIT", "CL", "F", "FLC", "IOD", "PO4", "SO4"}, idsOf(result.Ligands))
	for _, ligand := range result.Ligands {
		assert.Equal(t, ambiguousReason, ligand.ReviewReason, ligand.ID)
	}
}

// singleAtomText is a file with one single-atom instance of each compound, with
// the given element.
func singleAtomText(atoms map[string]string) string {
	var builder strings.Builder
	builder.WriteString("data_X\nloop_\n_atom_site.group_PDB\n_atom_site.type_symbol\n_atom_site.label_atom_id\n" +
		"_atom_site.label_comp_id\n_atom_site.label_asym_id\n_atom_site.auth_seq_id\n")
	builder.WriteString("ATOM C CA ALA A 1\n")
	for index, compound := range slices.Sorted(maps.Keys(atoms)) {
		element := atoms[compound]
		fmt.Fprintf(&builder, "HETATM %s %s %s B %d\n", element, element, compound, 100+index)
	}
	return builder.String()
}

func Test_should_flag_single_atom_only_when_its_element_is_a_metal_or_unknown(t *testing.T) {
	// given: no list names XE, ZZ1, UNX or ZZ2.
	text := singleAtomText(map[string]string{"XE": "XE", "ZZ1": "PT", "UNX": "X", "ZZ2": "."})

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []Compound{
		{ID: "UNX", NeedsReview: true, ReviewReason: singleAtomReason},
		{ID: "XE"},
		{ID: "ZZ1", NeedsReview: true, ReviewReason: singleAtomReason},
		{ID: "ZZ2", NeedsReview: true, ReviewReason: singleAtomReason},
	}, result.Ligands)
}

func Test_should_flag_halide_as_ambiguous_not_as_possible_metal_when_lists_are_approved(t *testing.T) {
	// given
	text := singleAtomText(map[string]string{"CL": "CL"})

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []Compound{{ID: "CL", NeedsReview: true, ReviewReason: ambiguousReason}}, result.Ligands)
}

func Test_should_sort_cofactors_ligands_and_covalent_inhibitors_when_file_has_gdp_and_magnesium(t *testing.T) {
	// given
	text := readFixture(t, "6OIM.cif")

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"GDP", "MOV"}, idsOf(result.Ligands))
	assert.Equal(t, []string{"MG"}, idsOf(result.Cofactors))
	assert.Len(t, result.Connections, 7)
}

func Test_should_map_engineered_mutations_to_uniprot_numbering_when_dif_rows_exist(t *testing.T) {
	// given
	text := readFixture(t, "6OIM.cif")

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	require.Len(t, result.Mutations, 1)
	mutations := result.Mutations[0]
	assert.Equal(t, "1", mutations.EntityID)
	assert.Equal(t, "P01116", mutations.Accession)
	assert.Equal(t, []string{"C51S", "C80L", "C118S"}, mutations.Mutations)
	assert.Equal(t, "C51S,C80L,C118S", mutations.Original)
	assert.False(t, mutations.NeedsReview)
	require.Len(t, result.SequenceDifferences, 18)
	assert.Equal(t, "initiating methionine", result.SequenceDifferences[0].Details)
	assert.Equal(t, "variant", result.SequenceDifferences[14].Details)
	assert.Equal(t, "engineered mutation", result.SequenceDifferences[15].Details)
}

func Test_should_keep_original_text_for_review_when_entity_mutation_has_no_dif_rows(t *testing.T) {
	// given
	text := "data_X\nloop_\n_entity.id\n_entity.type\n_entity.pdbx_mutation\n1 polymer 'K12R, L45A'\n2 polymer ?\n"

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	require.Len(t, result.Mutations, 1)
	assert.Equal(t, "1", result.Mutations[0].EntityID)
	assert.Equal(t, "K12R, L45A", result.Mutations[0].Original)
	assert.Empty(t, result.Mutations[0].Mutations)
	assert.True(t, result.Mutations[0].NeedsReview)
}

func Test_should_flag_review_when_dif_row_is_not_uniprot_or_residue_is_unknown(t *testing.T) {
	// given
	text := "data_X\n" +
		"loop_\n_entity.id\n_entity.type\n_entity.pdbx_mutation\n1 polymer 'A1B'\n" +
		"_struct_ref.id 1\n_struct_ref.entity_id 1\n" +
		"_struct_ref_seq.align_id 1\n_struct_ref_seq.ref_id 1\n" +
		"loop_\n_struct_ref_seq_dif.align_id\n_struct_ref_seq_dif.mon_id\n_struct_ref_seq_dif.pdbx_seq_db_name\n" +
		"_struct_ref_seq_dif.pdbx_seq_db_accession_code\n_struct_ref_seq_dif.db_mon_id\n" +
		"_struct_ref_seq_dif.pdbx_seq_db_seq_num\n_struct_ref_seq_dif.details\n" +
		"1 ALA GB Q12345 GLY 10 'engineered mutation'\n" +
		"1 XXX UNP P11111 GLY 11 'engineered mutation'\n" +
		"1 ALA UNP P11111 GLY 12 'expression tag'\n"

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	require.Len(t, result.Mutations, 1)
	assert.True(t, result.Mutations[0].NeedsReview)
	assert.Empty(t, result.Mutations[0].Mutations)
	assert.Equal(t, "A1B", result.Mutations[0].Original)
	assert.Len(t, result.Mutations[0].Unmapped, 2)
}

func Test_should_list_each_mutation_once_when_every_chain_of_the_entity_has_the_row(t *testing.T) {
	// given
	text := "data_X\nloop_\n_entity.id\n_entity.type\n_entity.pdbx_mutation\n1 polymer 'K127R'\n" +
		"_struct_ref.id 1\n_struct_ref.entity_id 1\n" +
		"loop_\n_struct_ref_seq.align_id\n_struct_ref_seq.ref_id\n_struct_ref_seq.pdbx_strand_id\n1 1 A\n2 1 B\n" +
		"loop_\n_struct_ref_seq_dif.align_id\n_struct_ref_seq_dif.mon_id\n_struct_ref_seq_dif.pdbx_seq_db_name\n" +
		"_struct_ref_seq_dif.pdbx_seq_db_accession_code\n_struct_ref_seq_dif.db_mon_id\n" +
		"_struct_ref_seq_dif.pdbx_seq_db_seq_num\n_struct_ref_seq_dif.details\n" +
		"1 ARG UNP B0Y813 LYS 127 'engineered mutation'\n" +
		"1 MSE UNP B0Y813 MET 130 'engineered mutation'\n" +
		"2 ARG UNP B0Y813 LYS 127 'engineered mutation'\n" +
		"2 MSE UNP B0Y813 MET 130 'engineered mutation'\n"

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	require.Len(t, result.Mutations, 2)
	unmapped, mapped := result.Mutations[0], result.Mutations[1]
	assert.Equal(t, "B0Y813", mapped.Accession)
	assert.Equal(t, []string{"K127R"}, mapped.Mutations)
	assert.Empty(t, unmapped.Accession)
	assert.Len(t, unmapped.Unmapped, 1)
	assert.True(t, unmapped.NeedsReview)
}

func Test_should_count_altloc_residues_when_second_position_has_enough_occupancy(t *testing.T) {
	// given
	text := "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.type_symbol\n_atom_site.label_comp_id\n" +
		"_atom_site.auth_asym_id\n_atom_site.auth_seq_id\n_atom_site.label_alt_id\n_atom_site.occupancy\n" +
		"ATOM N ALA A 1 . 1.00\n" +
		"ATOM C ALA A 2 A 0.60\nATOM C ALA A 2 B 0.40\n" +
		"ATOM C ALA A 3 A 0.95\nATOM C ALA A 3 B 0.05\n" +
		"ATOM C ALA A 4 A 1.00\n" +
		"#\n"

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	require.NotNil(t, result.Atoms)
	assert.Equal(t, 4, result.Atoms.ModeledResidues)
	assert.Equal(t, 0.25, result.Atoms.AltlocFraction)
}

func Test_should_count_residues_not_atoms_when_altloc_residue_has_many_atoms(t *testing.T) {
	// given
	text := readFixture(t, "193L.cif")

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	require.NotNil(t, result.Atoms)
	assert.Equal(t, 11, result.Atoms.ModeledResidues)
	assert.InDelta(t, 4.0/11.0, result.Atoms.AltlocFraction, 1e-9)
	assert.Equal(t, []string{"NA"}, idsOf(result.Cofactors))
}

func Test_should_read_text_blocks_when_file_has_multiline_values(t *testing.T) {
	// given
	text := readFixture(t, "1AKE.cif")

	// when
	document := Parse(text)
	result, err := Extract(document)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"AP5"}, idsOf(result.Ligands))
	require.NotNil(t, result.Atoms)
	assert.Equal(t, 2, result.Atoms.ProteinChains)
	assert.InDelta(t, 1.0/17.0, result.Atoms.AltlocFraction, 1e-9)
	assert.NotEmpty(t, document.Table("_struct_keywords"))
}

func Test_should_skip_atom_statistics_when_atom_site_loop_is_truncated(t *testing.T) {
	// given
	text := readFixture(t, "6OIM.cif")
	head := text[:strings.Index(text, "_atom_site.group_PDB")+2000]

	// when
	result, err := Extract(ParseTruncated(head))

	// then
	require.NoError(t, err)
	assert.Nil(t, result.Atoms)
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "atom_site")
	require.Len(t, result.Mutations, 1)
	assert.Equal(t, []string{"C51S", "C80L", "C118S"}, result.Mutations[0].Mutations)
}

func Test_should_report_program_when_software_loop_names_refinement(t *testing.T) {
	// given
	text := "data_X\nloop_\n_software.name\n_software.classification\n_software.version\nREFMAC refinement 5.2.0005\n"

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, "REFMAC", result.Program.Name)
	assert.Equal(t, "5.2.0005", result.Program.Version)
}

func Test_should_report_reference_version_when_extracting(t *testing.T) {
	// given
	document := Parse("data_X\n")

	// when
	result, err := Extract(document)

	// then
	require.NoError(t, err)
	assert.NotEmpty(t, result.ReferenceVersion)
}

func withoutMetalcRowsOfAsym(text string, asymID string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "metalc") && slices.Contains(strings.Fields(line), asymID) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func Test_should_flag_unlinked_metal_instance_when_same_compound_has_linked_instances(t *testing.T) {
	// given
	text := withoutMetalcRowsOfAsym(readFixture(t, "3CLN.cif"), "E")

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"CA"}, idsOf(result.Cofactors))
	assert.False(t, result.Cofactors[0].NeedsReview)
	require.Len(t, result.Ligands, 1)
	assert.Equal(t, "CA", result.Ligands[0].ID)
	assert.True(t, result.Ligands[0].NeedsReview)
	assert.NotEmpty(t, result.Ligands[0].ReviewReason)
}

func Test_should_flag_metal_missing_from_reference_when_file_has_metalc_rows_for_other_metals(t *testing.T) {
	// given
	text := "data_X\n" +
		"loop_\n_struct_conn.id\n_struct_conn.conn_type_id\n" +
		"_struct_conn.ptnr1_label_asym_id\n_struct_conn.ptnr1_label_comp_id\n_struct_conn.ptnr1_auth_seq_id\n" +
		"_struct_conn.ptnr2_label_asym_id\n_struct_conn.ptnr2_label_comp_id\n_struct_conn.ptnr2_auth_seq_id\n" +
		"metalc1 metalc A ASP 20 B CA 149\n" +
		"loop_\n_atom_site.group_PDB\n_atom_site.type_symbol\n_atom_site.label_comp_id\n" +
		"_atom_site.label_asym_id\n_atom_site.auth_seq_id\n" +
		"HETATM CA CA B 149\nHETATM PD PD Z 300\n"

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"CA"}, idsOf(result.Cofactors))
	require.NotEmpty(t, result.Ligands)
	for _, ligand := range result.Ligands {
		assert.True(t, ligand.NeedsReview, ligand.ID)
	}
}

func Test_should_keep_metal_instances_in_cofactors_when_every_instance_has_metalc_link(t *testing.T) {
	// given
	text := readFixture(t, "3CLN.cif")

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"CA"}, idsOf(result.Cofactors))
	assert.Empty(t, result.Ligands)
}

func Test_should_flag_metal_instance_unlinked_when_its_only_metalc_partner_is_water(t *testing.T) {
	// given
	text := withoutMetalcRowsOfAsym(readFixture(t, "3CLN.cif"), "E")
	var waterOnly []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "metalc") && strings.Contains(line, " HOH ") {
			continue
		}
		waterOnly = append(waterOnly, line)
	}

	// when
	result, err := Extract(Parse(strings.Join(waterOnly, "\n")))

	// then
	require.NoError(t, err)
	require.NotEmpty(t, result.Ligands)
	assert.True(t, result.Ligands[0].NeedsReview)
}
