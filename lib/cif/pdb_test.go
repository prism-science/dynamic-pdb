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

// pdbRecord writes one ATOM or HETATM record in PDB columns.
func pdbRecord(record string, serial int, atom string, residue string, chain string, number int, element string) string {
	return fmt.Sprintf("%-6s%5d %-4s %3s %1s%4d    %8.3f%8.3f%8.3f%6.2f%6.2f          %2s\n",
		record, serial, atom, residue, chain, number, 1.0, 2.0, 3.0, 1.0, 20.0, element)
}

func pdbSeqres(chain string, residues ...string) string {
	return fmt.Sprintf("SEQRES   1 %1s %4d  %s\n", chain, len(residues), strings.Join(residues, " "))
}

func pdbModres(residue string, chain string, number int, standard string) string {
	return fmt.Sprintf("MODRES XXXX %3s %1s %4d  %3s\n", residue, chain, number, standard)
}

func pdbLink(atom1 string, residue1 string, chain1 string, number1 int, atom2 string, residue2 string, chain2 string, number2 int) string {
	return fmt.Sprintf("LINK        %-4s %3s %1s%4d                %-4s %3s %1s%4d\n",
		atom1, residue1, chain1, number1, atom2, residue2, chain2, number2)
}

func pdbTer(serial int, residue string, chain string, number int) string {
	return fmt.Sprintf("TER   %5d      %3s %1s%4d\n", serial, residue, chain, number)
}

func pdbSources(t *testing.T, text string) (Extraction, CompoundSources) {
	t.Helper()
	extraction, err := PDBExtraction(text, false)
	require.NoError(t, err)
	return extraction, extraction.CompoundSources
}

func Test_should_treat_seqres_residue_as_polymer_when_it_comes_before_ter_and_as_ligand_after(t *testing.T) {
	// given: MSE in chain A; a free glutamate after chain A's TER, as in 1II5.
	text := pdbSeqres("A", "ALA", "MSE", "GLU") +
		pdbRecord("ATOM", 1, "CA", "ALA", "A", 1, "C") +
		pdbRecord("HETATM", 2, "SE", "MSE", "A", 2, "SE") +
		pdbRecord("HETATM", 3, "CA", "MSE", "A", 2, "C") +
		pdbRecord("ATOM", 4, "CA", "GLU", "A", 3, "C") +
		pdbTer(5, "GLU", "A", 3) +
		pdbRecord("HETATM", 6, "N", "GLU", "A", 999, "N") +
		pdbRecord("HETATM", 7, "CA", "GLU", "A", 999, "C") + "END\n"

	// when
	extraction, sources := pdbSources(t, text)

	// then
	assert.Equal(t, []string{"GLU"}, extraction.Fields["ligands"])
	assert.Equal(t, []ReviewItem{{List: ListLigands, ID: "GLU", Reason: outsideChainReason}}, extraction.Review)
	assert.Equal(t, source(ruleCCDType), sources.Ligands["GLU"])
}

func Test_should_count_residue_as_polymer_when_seqres_names_it_whatever_its_ccd_type(t *testing.T) {
	// given: HT0 is a "non-polymer" in the CCD but a residue of 7LTB's chain.
	text := pdbSeqres("A", "HT0", "PHE") +
		pdbRecord("HETATM", 1, "C1", "HT0", "A", 1, "C") +
		pdbRecord("HETATM", 2, "C2", "HT0", "A", 1, "C") +
		pdbRecord("ATOM", 3, "CA", "PHE", "A", 2, "C") +
		pdbTer(4, "PHE", "A", 2) + "END\n"

	// when
	extraction, sources := pdbSources(t, text)

	// then
	assert.NotContains(t, extraction.Fields, "ligands")
	assert.Empty(t, sources.Ligands)
}

func Test_should_add_the_bound_cofactor_when_modres_lists_a_covalent_cofactor_residue(t *testing.T) {
	// given: LLP (PLP on a lysine), as MODRES lists it in 3CEB.
	text := pdbModres("LLP", "A", 5, "LYS") +
		pdbRecord("ATOM", 1, "CA", "ALA", "A", 4, "C") +
		pdbRecord("HETATM", 2, "NZ", "LLP", "A", 5, "N") +
		pdbRecord("HETATM", 3, "C4A", "LLP", "A", 5, "C") + "END\n"

	// when
	extraction, sources := pdbSources(t, text)

	// then
	assert.NotContains(t, extraction.Fields, "ligands")
	assert.Equal(t, []string{"PLP"}, extraction.Fields["cofactors"])
	assert.Equal(t, map[string]CompoundSource{"PLP": source(ruleCovalentMap)}, sources.Cofactors)
}

func Test_should_place_residues_by_numbers_when_chain_has_neither_seqres_nor_ter(t *testing.T) {
	// given: a depositor file: MSE inside chain A's numbers, a glutamate and an
	// ATP numbered inside them too, and a glutamate far outside.
	text := pdbRecord("ATOM", 1, "CA", "ALA", "A", 1, "C") +
		pdbRecord("HETATM", 2, "SE", "MSE", "A", 2, "SE") +
		pdbRecord("HETATM", 3, "CA", "MSE", "A", 2, "C") +
		pdbRecord("ATOM", 4, "CA", "ALA", "A", 9, "C") +
		pdbRecord("HETATM", 5, "PG", "ATP", "A", 5, "P") +
		pdbRecord("HETATM", 6, "PB", "ATP", "A", 5, "P") +
		pdbRecord("HETATM", 7, "N", "GLU", "A", 900, "N") +
		pdbRecord("HETATM", 8, "CA", "GLU", "A", 900, "C") + "END\n"

	// when
	extraction, sources := pdbSources(t, text)

	// then: only a peptide counts as in the chain without SEQRES to name it,
	// and without SEQRES the glutamate outside it is an ordinary ligand.
	assert.Equal(t, []string{"ATP", "GLU"}, extraction.Fields["ligands"])
	assert.Empty(t, extraction.Review)
	assert.Equal(t, source(ruleList), sources.Ligands["ATP"])
	assert.Equal(t, source(ruleList), sources.Ligands["GLU"])
}

func Test_should_not_flag_a_nucleotide_ligand_when_no_seqres_uses_its_name(t *testing.T) {
	// given: GDP, which the CCD types as "RNA linking", bound to a protein
	// chain, as in 6OIM.
	text := pdbSeqres("A", "ALA", "GLY") +
		pdbRecord("ATOM", 1, "CA", "ALA", "A", 1, "C") +
		pdbRecord("ATOM", 2, "CA", "GLY", "A", 2, "C") +
		pdbTer(3, "GLY", "A", 2) +
		pdbRecord("HETATM", 4, "PB", "GDP", "A", 201, "P") +
		pdbRecord("HETATM", 5, "PA", "GDP", "A", 201, "P") + "END\n"

	// when
	extraction, sources := pdbSources(t, text)

	// then
	assert.Equal(t, []string{"GDP"}, extraction.Fields["ligands"])
	assert.Empty(t, extraction.Review)
	assert.Equal(t, source(ruleList), sources.Ligands["GDP"])
}

func Test_should_link_metals_when_link_records_join_them_to_residues(t *testing.T) {
	// given: zinc bound to a cysteine, magnesium only to water, calcium to nothing.
	text := pdbLink("SG", "CYS", "A", 1, "ZN", "ZN", "B", 101) +
		pdbLink("MG", "MG", "B", 102, "O", "HOH", "C", 201) +
		pdbRecord("ATOM", 1, "CA", "CYS", "A", 1, "C") +
		pdbRecord("HETATM", 2, "ZN", "ZN", "B", 101, "ZN") +
		pdbRecord("HETATM", 3, "MG", "MG", "B", 102, "MG") +
		pdbRecord("HETATM", 4, "CA", "CA", "B", 103, "CA") +
		pdbRecord("HETATM", 5, "O", "HOH", "C", 201, "O") + "END\n"

	// when
	extraction, sources := pdbSources(t, text)

	// then
	assert.Equal(t, []string{"MG", "ZN"}, extraction.Fields["cofactors"])
	assert.Equal(t, []string{"CA"}, extraction.Fields["ligands"])
	assert.ElementsMatch(t, []ReviewItem{
		{List: ListCofactors, ID: "MG", Reason: waterOnlyMetalReason},
		{List: ListLigands, ID: "CA", Reason: unlinkedMetalReason},
	}, extraction.Review)
	assert.Equal(t, source(ruleMetalc), sources.Cofactors["ZN"])
}

func Test_should_judge_each_metal_by_its_own_links_when_the_chain_id_is_blank(t *testing.T) {
	// given: on a blank chain, a LINK binds zinc 101 to a histidine; zinc 102 has no link.
	text := pdbLink("NE2", "HIS", " ", 1, "ZN", "ZN", " ", 101) +
		pdbRecord("ATOM", 1, "CA", "HIS", " ", 1, "C") +
		pdbRecord("HETATM", 2, "ZN", "ZN", " ", 101, "ZN") +
		pdbRecord("HETATM", 3, "ZN", "ZN", " ", 102, "ZN") + "END\n"

	// when
	extraction, _ := pdbSources(t, text)

	// then
	assert.Equal(t, []string{"ZN"}, extraction.Fields["cofactors"])
	assert.Equal(t, []string{"ZN"}, extraction.Fields["ligands"])
	assert.Equal(t, []ReviewItem{{List: ListLigands, ID: "ZN", Reason: unlinkedMetalReason}}, extraction.Review)
}

func Test_should_keep_metal_flagged_when_link_records_join_no_metal(t *testing.T) {
	// given: the only LINK joins a sugar to an asparagine.
	text := pdbLink("ND2", "ASN", "A", 1, "C1", "NAG", "B", 1) +
		pdbRecord("ATOM", 1, "CA", "ASN", "A", 1, "C") +
		pdbRecord("HETATM", 2, "ZN", "ZN", "B", 101, "ZN") + "END\n"

	// when
	extraction, _ := pdbSources(t, text)

	// then
	assert.Equal(t, []ReviewItem{{List: ListCofactors, ID: "ZN", Reason: noMetalcRowsReason}}, extraction.Review)
}

func Test_should_place_a_sugar_as_a_glycan_when_it_sits_outside_any_chain(t *testing.T) {
	// given
	text := pdbRecord("ATOM", 1, "CA", "ASN", "A", 1, "C") +
		pdbRecord("HETATM", 2, "C1", "NAG", "B", 1, "C") +
		pdbRecord("HETATM", 3, "C2", "NAG", "B", 1, "C") + "END\n"

	// when
	extraction, sources := pdbSources(t, text)

	// then
	assert.Equal(t, []string{"NAG"}, extraction.Fields["ligands"])
	assert.Equal(t, source(ruleBranched), sources.Ligands["NAG"])
}
