package cif

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const memberCountHeader = `data_TEST
_entry.id TEST
#
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.auth_asym_id
_atom_site.occupancy
_atom_site.pdbx_PDB_model_num
`

// atomRows lists one alpha carbon per residue and altloc letter, in the given model.
func atomRows(residues int, letters []string, model int) string {
	var rows strings.Builder
	for residue := 1; residue <= residues; residue++ {
		for _, letter := range letters {
			fmt.Fprintf(&rows, "ATOM %d C CA %s GLY A %d %d A 0.5 %d\n", residue, letter, residue, residue, model)
		}
	}
	return rows.String()
}

func extractedMemberCount(t *testing.T, atomSite string) *int {
	t.Helper()
	_, result, err := ExtractText(memberCountHeader+atomSite, false)
	require.NoError(t, err)
	require.NotNil(t, result.Atoms)
	return result.Atoms.MemberCount
}

func Test_should_count_models_when_the_file_lists_several(t *testing.T) {
	// given
	atomSite := atomRows(2, []string{"."}, 1) + atomRows(2, []string{"."}, 2) + atomRows(2, []string{"."}, 3)

	// when
	count := extractedMemberCount(t, atomSite)

	// then
	assert.Equal(t, new(3), count)
}

func Test_should_count_altloc_letters_when_every_polymer_residue_has_them(t *testing.T) {
	// given
	atomSite := atomRows(3, []string{"A", "B", "C", "D", "E", "F"}, 1)

	// when
	count := extractedMemberCount(t, atomSite)

	// then
	assert.Equal(t, new(6), count)
}

func Test_should_ignore_letters_on_waters_when_counting_altloc_letters(t *testing.T) {
	// given
	atomSite := atomRows(2, []string{"A", "B"}, 1) + "HETATM 9 O O c HOH B . 101 B 0.13 1\n"

	// when
	count := extractedMemberCount(t, atomSite)

	// then
	assert.Equal(t, new(2), count)
}

func Test_should_have_no_count_when_some_polymer_residue_lacks_the_letters(t *testing.T) {
	// given
	atomSite := atomRows(2, []string{"A", "B"}, 1) + "ATOM 9 C CA . GLY A 3 3 A 1.00 1\n"

	// when
	count := extractedMemberCount(t, atomSite)

	// then
	assert.Nil(t, count)
}

func Test_should_have_no_count_when_the_file_is_a_single_conformer(t *testing.T) {
	// given
	atomSite := atomRows(2, []string{"."}, 1)

	// when
	count := extractedMemberCount(t, atomSite)

	// then
	assert.Nil(t, count)
}
