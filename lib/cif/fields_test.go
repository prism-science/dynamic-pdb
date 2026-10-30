package cif

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_build_field_map_from_pdb_contents(t *testing.T) {
	// given
	text := "REMARK   3   PROGRAM     : PHENIX (2.0_5824: ???)\n" +
		"REMARK   3   R VALUE     (WORKING SET) : 0.191\n" +
		"REMARK   3   FREE R VALUE                     : 0.243\n" +
		"ATOM      1  N   ALA A   1      11.104  13.207   9.447  1.00 20.00           N\n" +
		"ATOM      2  CA AALA A   1      12.104  13.207   9.447  0.50 20.00           C\n" +
		"ATOM      3  CA BALA A   1      12.204  13.207   9.447  0.50 20.00           C\n" +
		"ATOM      4  N   GLY A   2      13.104  13.207   9.447  1.00 20.00           N\n" +
		"HETATM    5  C1  ATP B 101      14.104  13.207   9.447  1.00 20.00           C\n" +
		"HETATM    6  O   HOH B 201      15.104  13.207   9.447  1.00 20.00           O\n" +
		"HETATM    7  H1  ATP B 101      16.104  13.207   9.447  1.00 20.00           H\n" +
		"HETATM    8 ZN    ZN B 102      16.104  13.207   9.447  1.00 20.00          ZN\n" +
		"HETATM    9  S   SO4 B 103      16.104  13.207   9.447  1.00 20.00           S\n"

	// when
	extraction, err := pdbExtraction(context.Background(), text, false, approvedReference(t))
	fields := extraction.Fields

	// then
	require.NoError(t, err)
	assert.Equal(t, "PHENIX (2.0_5824: ???)", fields["REMARK 3 PROGRAM"])
	assert.Equal(t, "PHENIX", fields["program.name"])
	assert.Equal(t, "2.0_5824", fields["program.version"])
	assert.Equal(t, "0.191", fields["REMARK 3 R VALUE (WORKING SET)"])
	assert.Equal(t, "0.191", fields["remark 3 r value working set"])
	assert.Equal(t, "0.243", fields["REMARK 3 FREE R VALUE"])
	assert.Equal(t, 7, fields["atom_count"])
	assert.Equal(t, 2, fields["modeled_residues"])
	assert.Equal(t, 1, fields["unique_protein_chains"])
	assert.Equal(t, 0.5, fields["altloc_fraction"])
	assert.Equal(t, []string{"ATP", "SO4"}, fields["ligands"])
	assert.Equal(t, []string{"ZN"}, fields["cofactors"])
	assert.Contains(t, extraction.Review, ReviewItem{List: ListLigands, ID: "SO4", Reason: ambiguousReason})
}

func Test_should_parse_phenix_component_program_version_from_pdb_contents(t *testing.T) {
	// given
	text := "REMARK   3   PROGRAM     : PHENIX (phenix.ensemble_refinement: 1.21.2_5419)\n"

	// when
	fields, err := PDBFields(text)

	// then
	require.NoError(t, err)
	assert.Equal(t, "PHENIX (phenix.ensemble_refinement: 1.21.2_5419)", fields["REMARK 3 PROGRAM"])
	assert.Equal(t, "PHENIX", fields["program.name"])
	assert.Equal(t, "1.21.2_5419", fields["program.version"])
}

func Test_should_build_field_map_from_mmcif_contents(t *testing.T) {
	// given
	text := `data_model
_refine.ls_R_factor_R_free 0.243
_refine.ls_R_factor_R_work 0.191
loop_
_atom_site.group_PDB
_atom_site.type_symbol
_atom_site.label_comp_id
_atom_site.auth_asym_id
_atom_site.auth_seq_id
_atom_site.label_alt_id
ATOM N ALA A 1 .
ATOM C ALA A 1 A
ATOM C ALA A 1 B
ATOM N GLY A 2 .
HETATM C ATP B 101 .
HETATM O HOH B 201 .
HETATM H ATP B 101 .
#
loop_
_software.name
_software.classification
_software.version
REFMAC refinement 5.2.0005
#
`

	// when
	fields, err := MMCIFFields(text)

	// then
	require.NoError(t, err)
	assert.Equal(t, "0.243", fields["_refine.ls_R_factor_R_free"])
	assert.Equal(t, "0.191", fields["_refine.ls_R_factor_R_work"])
	assert.Equal(t, "REFMAC", fields["program.name"])
	assert.Equal(t, "5.2.0005", fields["program.version"])
	assert.Equal(t, 5, fields["atom_count"])
	assert.Equal(t, 2, fields["modeled_residues"])
	assert.Equal(t, 1, fields["unique_protein_chains"])
	assert.Equal(t, 0.5, fields["altloc_fraction"])
	assert.Equal(t, []string{"ATP"}, fields["ligands"])
}

func Test_should_find_field_by_normalized_key_when_exact_key_is_missing(t *testing.T) {
	// given
	fields := map[string]any{"remark 3 free r value": "0.243"}

	// when
	value, ok := FieldValue(fields, "REMARK 3 FREE R VALUE")

	// then
	require.True(t, ok)
	assert.Equal(t, "0.243", value)
}

func Test_should_report_empty_cofactors_when_coordinates_were_read_and_hold_none(t *testing.T) {
	// given
	text := readFixture(t, "1AKE.cif")

	// when
	fields, err := MMCIFFields(text)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{}, fields["cofactors"])
}

func Test_should_omit_cofactors_when_coordinates_are_unknown(t *testing.T) {
	// given
	text := "data_X\n_entry.id X\n"

	// when
	fields, err := MMCIFFields(text)

	// then
	require.NoError(t, err)
	assert.NotContains(t, fields, "cofactors")
}

func Test_should_keep_heme_and_metal_in_cofactors_not_ligands_when_building_field_map(t *testing.T) {
	// given
	heme := readFixture(t, "4HHB.cif")
	calcium := readFixture(t, "3CLN.cif")

	reference := approvedReference(t)

	// when
	hemeExtraction, hemeErr := mmcifExtraction(context.Background(), heme, false, reference)
	calciumExtraction, calciumErr := mmcifExtraction(context.Background(), calcium, false, reference)
	hemeFields, calciumFields := hemeExtraction.Fields, calciumExtraction.Fields

	// then
	require.NoError(t, hemeErr)
	require.NoError(t, calciumErr)
	assert.Equal(t, []string{"HEM"}, hemeFields["cofactors"])
	assert.Equal(t, []string{"PO4"}, hemeFields["ligands"], "phosphate is ambiguous: kept as a ligand, flagged")
	assert.Equal(t, []ReviewItem{{List: ListLigands, ID: "PO4", Reason: ambiguousReason}}, hemeExtraction.Review)
	assert.Equal(t, []string{"CA"}, calciumFields["cofactors"])
	assert.NotContains(t, calciumFields, "ligands")
}

func Test_should_omit_atom_counts_when_cut_falls_exactly_on_a_row_boundary(t *testing.T) {
	// given
	text := readFixture(t, "3CLN.cif")
	start := strings.Index(text, "\nHETATM 1127")
	require.Positive(t, start)
	rowEnd := start + 1 + strings.Index(text[start+1:], "\n") + 1
	head := text[:rowEnd]
	require.True(t, strings.HasSuffix(head, "\n"))

	// when
	parsedAsWhole, wholeErr := MMCIFFields(head)
	parsedAsTruncated, truncatedErr := MMCIFFieldsTruncated(head)

	// then
	require.NoError(t, wholeErr)
	require.NoError(t, truncatedErr)
	assert.Contains(t, parsedAsWhole, "atom_count")
	assert.NotContains(t, parsedAsTruncated, "atom_count")
	assert.NotContains(t, parsedAsTruncated, "modeled_residues")
	assert.NotContains(t, parsedAsTruncated, "altloc_fraction")
}
