package cif

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_warn_about_the_row_limit_when_struct_conn_has_more_rows_than_it_allows(t *testing.T) {
	// given
	text := "data_X\nloop_\n_struct_conn.id\n" + repeatToSize("a ", hostileFileBytes)

	// when
	_, streamed, streamedErr := ExtractText(text, false)
	parsed, parsedErr := Extract(Parse(text))

	// then
	require.NoError(t, streamedErr)
	require.NoError(t, parsedErr)
	assert.Empty(t, streamed.Connections)
	assert.LessOrEqual(t, len(parsed.Connections), maxTableRows)
	assert.NotEmpty(t, parsed.Connections)
	assert.Contains(t, strings.Join(parsed.Warnings, "\n"), "parser limit: table _struct_conn has more than")
}

func Test_should_keep_every_metalc_link_when_struct_conn_is_streamed(t *testing.T) {
	// given
	text := "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n_atom_site.auth_seq_id\n_atom_site.label_atom_id\n_atom_site.pdbx_PDB_model_num\n" +
		"HETATM ZN B 101 ZN 1\nHETATM ZN B 102 ZN 1\n#\n" +
		"loop_\n_struct_conn.id\n_struct_conn.conn_type_id\n_struct_conn.ptnr1_label_asym_id\n_struct_conn.ptnr1_label_comp_id\n_struct_conn.ptnr1_auth_seq_id\n" +
		"_struct_conn.ptnr2_label_asym_id\n_struct_conn.ptnr2_label_comp_id\n_struct_conn.ptnr2_auth_seq_id\n" +
		"c1 metalc B ZN 101 A HIS 5\nc2 covale B ZN 102 A HIS 6\n#\n"

	// when
	_, result, err := ExtractText(text, false)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"ZN"}, idsOf(result.Cofactors))
	require.Len(t, result.Ligands, 1)
	assert.Equal(t, "ZN", result.Ligands[0].ID)
	assert.True(t, result.Ligands[0].NeedsReview)
}

func Test_should_skip_the_rows_and_warn_when_a_loop_is_wider_than_the_limit(t *testing.T) {
	// given
	text := "data_X\nloop_\n" + repeatToSize("_entity.a\n", 20_000) + "1 2 3\n"

	// when
	_, result, err := ExtractText(text, false)

	// then
	require.NoError(t, err)
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "has more than 1000 columns")
}

func Test_should_ignore_further_scalar_items_and_warn_when_the_file_has_more_than_the_limit(t *testing.T) {
	// given
	var builder strings.Builder
	builder.WriteString("data_X\n")
	for index := range maxScalarItems + 10 {
		fmt.Fprintf(&builder, "_a.t%d %d\n", index, index)
	}

	// when
	document, result, err := ExtractText(builder.String(), false)

	// then
	require.NoError(t, err)
	assert.Len(t, document.Scalars(), maxScalarItems)
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "more than 20000 non-looped items")
}

func Test_should_report_cut_off_coordinates_when_atom_site_has_too_many_residues(t *testing.T) {
	// given
	var builder strings.Builder
	builder.WriteString("data_X\nloop_\n_atom_site.group_PDB\n_atom_site.auth_seq_id\n")
	for index := range maxDistinctResidues + 5 {
		fmt.Fprintf(&builder, "ATOM %d\n", index)
	}

	// when
	_, result, err := ExtractText(builder.String(), false)

	// then
	require.NoError(t, err)
	assert.Nil(t, result.Atoms)
	assert.True(t, result.AtomsCutOff)
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "more distinct residues than the parser limit allows")
}

func Test_should_report_cut_off_coordinates_when_the_head_ends_before_atom_site_starts(t *testing.T) {
	// given
	whole := readFixture(t, "4HHB.cif")
	head := whole[:strings.Index(whole, "_atom_site.group_PDB")-200]

	// when
	_, result, err := ExtractText(head, true)

	// then
	require.NoError(t, err)
	assert.Nil(t, result.Atoms)
	assert.True(t, result.AtomsCutOff)
	assert.Contains(t, strings.Join(result.Warnings, "\n"), "ended before any atom_site coordinates")
}

func Test_should_report_no_cut_off_when_a_complete_file_has_no_atom_site(t *testing.T) {
	// given
	text := "data_X\n_entry.id X\n"

	// when
	_, result, err := ExtractText(text, false)

	// then
	require.NoError(t, err)
	assert.False(t, result.AtomsCutOff)
	assert.NotContains(t, strings.Join(result.Warnings, "\n"), "atom_site")
}

func Test_should_flag_unknown_atom_count_when_mmcif_head_is_cut_after_one_atom_of_a_ligand(t *testing.T) {
	// given
	header := "data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n_atom_site.auth_seq_id\n_atom_site.label_atom_id\n_atom_site.pdbx_PDB_model_num\n"
	text := header + "ATOM ALA A 1 CA 1\nHETATM LIG B 101 C1 1\nHETATM LIG B 101 C2 1\nHETATM XYZ B 102 C1 1\nHETATM XYZ B 102 C"

	// when
	_, result, err := ExtractText(text, true)

	// then
	require.NoError(t, err)
	byID := map[string]Compound{}
	for _, compound := range result.Ligands {
		byID[compound.ID] = compound
	}
	assert.False(t, byID["LIG"].NeedsReview, "a ligand with two atoms read is not a single-atom compound")
	assert.True(t, byID["XYZ"].NeedsReview)
	assert.Contains(t, byID["XYZ"].ReviewReason, "atom count unknown")
	assert.NotContains(t, byID["XYZ"].ReviewReason, "single-atom compound is not")
}

func Test_should_not_flag_single_atom_when_pdb_het_record_declares_more_atoms_and_head_is_cut_after_one(t *testing.T) {
	// given
	whole := "REMARK   3   PROGRAM     : PHENIX (1.20)\n" +
		"HET    LIG  B 101      43\n" +
		strings.Repeat("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n", 3) +
		"HETATM 9000  C1  LIG B 101      16.104  13.207   9.447  1.00 20.00           C\n" +
		"HETATM 9001  C2  LIG B 101      16.104  13.207   9.447  1.00 20.00           C\n"
	head := whole[:strings.LastIndex(whole, "HETATM 9001")+10]

	// when
	extraction, err := PDBExtraction(head, true)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"LIG"}, extraction.Fields["ligands"])
	assert.Empty(t, extraction.Review)
}

func Test_should_flag_unknown_atom_count_when_pdb_head_is_cut_after_one_atom_of_a_ligand_without_het_record(t *testing.T) {
	// given
	head := strings.Repeat("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n", 3) +
		"HETATM 9000  C1  LIG B 101      16.104  13.207   9.447  1.00 20.00           C\n"

	// when
	extraction, err := PDBExtraction(head, true)

	// then
	require.NoError(t, err)
	require.Len(t, extraction.Review, 1)
	assert.Contains(t, extraction.Review[0].Reason, "atom count unknown")
}

func Test_should_flag_single_atom_when_pdb_file_is_complete_and_ligand_has_one_atom(t *testing.T) {
	// given
	text := "ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n" +
		"HETATM 9000  C1  LIG B 101      16.104  13.207   9.447  1.00 20.00           C\nEND\n"

	// when
	extraction, err := PDBExtraction(text, false)

	// then
	require.NoError(t, err)
	require.Len(t, extraction.Review, 1)
	assert.Contains(t, extraction.Review[0].Reason, "single-atom compound")
}

func Test_should_warn_and_cap_the_list_when_a_file_has_more_distinct_compounds_than_the_limit(t *testing.T) {
	// given
	var builder strings.Builder
	builder.WriteString("data_X\nloop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.auth_seq_id\n")
	for index := range maxCompoundsPerList + 50 {
		fmt.Fprintf(&builder, "HETATM L%d %d\n", index, index)
	}

	// when
	_, result, err := ExtractText(builder.String(), false)

	// then
	require.NoError(t, err)
	assert.Len(t, result.Ligands, maxCompoundsPerList)
	assert.Contains(t, result.Warnings, tooManyCompoundsWarning)
}
