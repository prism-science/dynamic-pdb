package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_add_cofactors_and_review_flags_when_metadata_reads_ligands_from_coordinates_only(t *testing.T) {
	// given
	metadata := ModelMetadata{
		"ligands":    coordinateField("ligands", map[string]struct{}{".cif": {}, ".pdb": {}}),
		"atom_count": coordinateField("atom_count", map[string]struct{}{".cif": {}}),
	}

	// when
	upgraded, changed := metadata.WithCoordinateCofactors()

	// then
	require.True(t, changed)
	require.Len(t, upgraded["cofactors"], 1)
	assert.Equal(t, "cofactors", upgraded["cofactors"][0].Extract.MMCIF.Field)
	assert.Equal(t, "cofactors", upgraded["cofactors"][0].Extract.PDB.Field)
	assert.Equal(t, "compound_review", upgraded["extensions.compound_review"][0].Extract.MMCIF.Field)
	assert.NotContains(t, metadata, "cofactors", "the input is not modified")
}

func Test_should_leave_metadata_alone_when_it_already_has_cofactors_or_reads_ligands_from_rcsb(t *testing.T) {
	cases := map[string]ModelMetadata{
		"cofactors present": {
			"ligands":   coordinateField("ligands", map[string]struct{}{".cif": {}}),
			"cofactors": coordinateField("cofactors", map[string]struct{}{".cif": {}}),
		},
		"rcsb ligands": {"ligands": rcsbJSONField("rcsb_entry_info.nonpolymer_bound_components")},
		"no ligands":   {"atom_count": coordinateField("atom_count", map[string]struct{}{".cif": {}})},
	}
	for name, metadata := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			upgraded, changed := metadata.WithCoordinateCofactors()

			// then
			assert.False(t, changed)
			assert.Equal(t, metadata, upgraded)
		})
	}
}
