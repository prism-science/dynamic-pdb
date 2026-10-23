package cif

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pdbAtomLine = "ATOM  %5d  CA  ALA A%4d      11.104  13.207   9.447  1.00 20.00           C\n"

func pdbModel(number int, residues int) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "MODEL     %4d\n", number)
	for residue := 1; residue <= residues; residue++ {
		fmt.Fprintf(&builder, pdbAtomLine, residue, residue)
	}
	builder.WriteString("HETATM 9000 ZN    ZN B 101      16.104  13.207   9.447  1.00 20.00          ZN\n")
	builder.WriteString("ENDMDL\n")
	return builder.String()
}

func Test_should_count_only_first_model_when_pdb_file_is_an_nmr_ensemble(t *testing.T) {
	// given
	text := "REMARK   3   PROGRAM     : CNS 1.1\n" +
		"HET     ZN  B 101       1\n" +
		pdbModel(1, 5) + pdbModel(2, 5) + pdbModel(3, 5) + "END\n"

	// when
	extraction, err := PDBExtraction(text, false)

	// then
	require.NoError(t, err)
	assert.Equal(t, 6, extraction.Fields["atom_count"])
	assert.Equal(t, 5, extraction.Fields["modeled_residues"])
	assert.Equal(t, 1, extraction.Fields["unique_protein_chains"])
	assert.Equal(t, []string{"ZN"}, extraction.Fields["cofactors"])
}

func Test_should_count_first_model_when_endmdl_record_is_missing(t *testing.T) {
	// given
	text := "MODEL        1\n" + strings.Repeat("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n", 3) +
		"MODEL        2\n" + strings.Repeat("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n", 3)

	// when
	extraction, err := PDBExtraction(text, false)

	// then
	require.NoError(t, err)
	assert.Equal(t, 3, extraction.Fields["atom_count"])
}

func Test_should_omit_counts_when_pdb_head_is_cut_inside_the_coordinates(t *testing.T) {
	// given
	whole := "REMARK   3   PROGRAM     : PHENIX (1.20)\n" +
		"HET     ZN  B 101       1\n" +
		strings.Repeat("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n", 10) +
		"HETATM 9000 ZN    ZN B 101      16.104  13.207   9.447  1.00 20.00          ZN\n" +
		"MASTER        0    0    0    0    0    0    0    0   11    0    0    0\nEND\n"
	cut := whole[:strings.Index(whole, "MASTER")-30]

	// when
	truncated, truncatedErr := PDBExtraction(cut, true)
	complete, completeErr := PDBExtraction(whole, true)

	// then
	require.NoError(t, truncatedErr)
	require.NoError(t, completeErr)
	for _, field := range []string{"atom_count", "modeled_residues", "unique_protein_chains", "altloc_fraction"} {
		assert.NotContains(t, truncated.Fields, field)
		assert.Contains(t, complete.Fields, field)
	}
	assert.Equal(t, "PHENIX", truncated.Fields["program.name"])
	assert.Equal(t, []string{"ZN"}, truncated.Fields["cofactors"])
	assert.Contains(t, truncated.Warnings, "PDB coordinates were cut off by the read limit: atom counts, residue counts and altloc fraction not extracted")
	assert.Equal(t, 11, complete.Fields["atom_count"])
}

func Test_should_drop_cut_off_last_line_when_pdb_head_is_truncated(t *testing.T) {
	// given
	line := "ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n"
	text := line + line + "ENDMDL\n" + line[:40]

	// when
	extraction, err := PDBExtraction(text, true)

	// then
	require.NoError(t, err)
	assert.Equal(t, 2, extraction.Fields["atom_count"])
}

func Test_should_read_compounds_from_het_records_when_pdb_head_ends_before_the_hetatm_rows(t *testing.T) {
	// given
	text := "HET    ATP  A 301      31\n" +
		strings.Repeat("ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n", 4)

	// when
	extraction, err := PDBExtraction(text, true)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"ATP"}, extraction.Fields["ligands"])
	assert.NotContains(t, extraction.Fields, "atom_count")
	assert.NotContains(t, extraction.Fields, "cofactors")
}

func Test_should_flag_metal_for_review_when_pdb_has_no_connection_rows(t *testing.T) {
	// given
	text := "ATOM      1  CA  ALA A   1      11.104  13.207   9.447  1.00 20.00           C\n" +
		"HETATM    2 ZN    ZN B 101      16.104  13.207   9.447  1.00 20.00          ZN\n"

	// when
	extraction, err := PDBExtraction(text, false)

	// then
	require.NoError(t, err)
	require.Len(t, extraction.Review, 1)
	assert.Equal(t, ReviewItem{
		List:   ListCofactors,
		ID:     "ZN",
		Reason: "metal ion kept for review: the file has no metalc connection rows",
	}, extraction.Review[0])
	assert.Equal(t, extraction.Review, extraction.Fields[FieldCompoundReview])
}
