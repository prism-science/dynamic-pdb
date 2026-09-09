package artifact

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_extract_field_from_artifact_payload(t *testing.T) {
	// given
	extractor := NewFieldExtractor(map[string]extractors.Artifact{
		"coordinates": {
			Filename: "model.cif",
			Contents: []byte("_refine.ls_R_factor_R_free 0.243\n"),
		},
	})

	// when
	value, ok, err := extractor.Extract(context.Background(), "5AMF",
		manifest.Source{Artifact: "coordinates"},
		manifest.Extract{MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"}},
	)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "0.243", value)
}

func Test_should_build_field_map_from_pdb_contents(t *testing.T) {
	// given
	text := "REMARK   3   PROGRAM     : PHENIX (2.0_5824: ???)\n" +
		"REMARK   3   R VALUE     (WORKING SET) : 0.191\n" +
		"REMARK   3   FREE R VALUE                     : 0.243\n" +
		"ATOM      1  N   ALA A   1      11.104  13.207   9.447  1.00 20.00           N\n" +
		"ATOM      2  CA AALA A   1      12.104  13.207   9.447  1.00 20.00           C\n" +
		"ATOM      3  N   GLY A   2      13.104  13.207   9.447  1.00 20.00           N\n" +
		"HETATM    4  C1  ATP B 101      14.104  13.207   9.447  1.00 20.00           C\n" +
		"HETATM    5  O   HOH B 201      15.104  13.207   9.447  1.00 20.00           O\n" +
		"HETATM    6  H1  ATP B 101      16.104  13.207   9.447  1.00 20.00           H\n"

	// when
	fields := pdbFields(text)

	// then
	assert.Equal(t, "PHENIX (2.0_5824: ???)", fields["REMARK 3 PROGRAM"])
	assert.Equal(t, "PHENIX", fields["program.name"])
	assert.Equal(t, "2.0_5824", fields["program.version"])
	assert.Equal(t, "0.191", fields["REMARK 3 R VALUE (WORKING SET)"])
	assert.Equal(t, "0.191", fields["remark 3 r value working set"])
	assert.Equal(t, "0.243", fields["REMARK 3 FREE R VALUE"])
	assert.Equal(t, 4, fields["atom_count"])
	assert.Equal(t, 2, fields["modeled_residues"])
	assert.Equal(t, 1, fields["unique_protein_chains"])
	assert.Equal(t, 0.5, fields["altloc_fraction"])
	assert.Equal(t, []string{"ATP"}, fields["ligands"])
}

func Test_should_parse_phenix_component_program_version_from_pdb_contents(t *testing.T) {
	// given
	text := "REMARK   3   PROGRAM     : PHENIX (phenix.ensemble_refinement: 1.21.2_5419)\n"

	// when
	fields := pdbFields(text)

	// then
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
	fields := mmcifFields(text)

	// then
	assert.Equal(t, "0.243", fields["_refine.ls_R_factor_R_free"])
	assert.Equal(t, "0.191", fields["_refine.ls_R_factor_R_work"])
	assert.Equal(t, "REFMAC", fields["program.name"])
	assert.Equal(t, "5.2.0005", fields["program.version"])
	assert.Equal(t, 4, fields["atom_count"])
	assert.Equal(t, 2, fields["modeled_residues"])
	assert.Equal(t, 1, fields["unique_protein_chains"])
	assert.Equal(t, 0.5, fields["altloc_fraction"])
	assert.Equal(t, []string{"ATP"}, fields["ligands"])
}
