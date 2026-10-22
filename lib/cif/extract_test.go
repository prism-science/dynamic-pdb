package cif

import (
	"os"
	"path/filepath"
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

func Test_should_keep_heme_as_cofactor_and_drop_buffer_when_file_has_several_entities(t *testing.T) {
	// given
	text := readFixture(t, "4HHB.cif")

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	require.NotNil(t, result.Atoms)
	assert.Equal(t, 420, result.Atoms.AtomCount)
	assert.Equal(t, 32, result.Atoms.ModeledResidues)
	assert.Equal(t, 4, result.Atoms.ProteinChains)
	assert.Empty(t, result.Ligands)
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

func Test_should_not_count_water_coordination_as_link_when_metal_only_binds_water(t *testing.T) {
	// given
	text := "data_X\n" +
		"loop_\n_struct_conn.id\n_struct_conn.conn_type_id\n" +
		"_struct_conn.ptnr1_label_comp_id\n_struct_conn.ptnr2_label_comp_id\n" +
		"metalc1 metalc ZN HOH\nmetalc2 metalc MG ASP\n" +
		"loop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.name\n_pdbx_entity_nonpoly.comp_id\n" +
		"2 'zinc ion' ZN\n3 'magnesium ion' MG\n4 water HOH\n"

	// when
	result, err := Extract(Parse(text))

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"MG"}, idsOf(result.Cofactors))
	require.Len(t, result.Ligands, 1)
	assert.Equal(t, "ZN", result.Ligands[0].ID)
	assert.True(t, result.Ligands[0].NeedsReview)
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
