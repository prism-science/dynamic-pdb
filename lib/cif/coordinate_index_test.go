package cif

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readIndex(t *testing.T, text string, format StructureFormat) CoordinateIndex {
	t.Helper()
	index, err := ReadCoordinateIndex(context.Background(), strings.NewReader(text), format)
	require.NoError(t, err)
	return index
}

// chainSummaries writes each chain as "chain entity length/source residues protein", in order.
func chainSummaries(index CoordinateIndex) []string {
	summaries := make([]string, 0, len(index.Chains))
	for _, chain := range index.Chains {
		summaries = append(summaries, fmt.Sprintf("%s %s %d/%s %d %t",
			chain.AuthAsymID, chain.EntityID, chain.DepositedLength, chain.LengthSource, chain.ModeledResidues, chain.Protein))
	}
	return summaries
}

// atomSiteHeader is the _atom_site loop of the synthetic mmCIF files below, in the column order
// archive files use.
const atomSiteHeader = `data_TEST
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.pdbx_PDB_ins_code
_atom_site.auth_seq_id
_atom_site.auth_asym_id
_atom_site.pdbx_PDB_model_num
`

// atomRow writes one _atom_site row of atomSiteHeader.
func atomRow(group string, id int, atom, alt, compound, labelChain, entity, labelSeq, insCode, authSeq, authChain string, model int) string {
	return fmt.Sprintf("%s %d %s %s %s %s %s %s %s %s %s %d\n",
		group, id, atom, alt, compound, labelChain, entity, labelSeq, insCode, authSeq, authChain, model)
}

// pdbAtom writes one ATOM or HETATM record with the columns the reader reads.
func pdbAtom(record string, serial int, atom, compound, chain string, residue int, insCode string) string {
	return fmt.Sprintf("%-6s%5d %-4s %3s %1s%4d%1s   %8.3f%8.3f%8.3f  1.00 10.00\n",
		record, serial, atom, compound, chain, residue, insCode, 0.0, 0.0, 0.0)
}

func Test_should_read_each_chain_with_its_entity_length_when_an_archive_file_has_entity_poly_seq(t *testing.T) {
	// given: 4HHB, deoxyhemoglobin: alpha chains A and C (entity 1, 141 residues), beta chains
	// B and D (entity 2, 146), a heme in each and two phosphates. The fixture keeps the first 8
	// residues of each chain.
	text := readFixture(t, "4HHB.cif")

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then: the hemes, phosphates and waters are not chain residues.
	assert.Empty(t, index.Warnings)
	assert.Equal(t, []string{
		"A 1 141/entity_poly_seq 8 true",
		"B 2 146/entity_poly_seq 8 true",
		"C 1 141/entity_poly_seq 8 true",
		"D 2 146/entity_poly_seq 8 true",
	}, chainSummaries(index))
}

func Test_should_give_the_same_chains_when_the_entry_is_in_either_format(t *testing.T) {
	for _, entry := range []string{"1A62", "3CEB", "3CLN", "6OIM", "7LTB"} {
		// given
		mmcifText, pdbText := readFixture(t, entry+".cif"), readFixture(t, entry+".pdb")

		// when
		mmcif := readIndex(t, mmcifText, FormatMMCIF)
		pdb := readIndex(t, pdbText, FormatPDB)

		// then: the same chains, residues and protein flags; mmCIF names the entity and reads
		// the length from _entity_poly_seq, PDB format from SEQRES.
		require.Len(t, pdb.Chains, len(mmcif.Chains), entry)
		for index, chain := range mmcif.Chains {
			other := pdb.Chains[index]
			assert.Equal(t, chain.AuthAsymID, other.AuthAsymID, entry)
			assert.Equal(t, chain.ModeledResidues, other.ModeledResidues, "%s chain %s", entry, chain.AuthAsymID)
			assert.Equal(t, chain.Protein, other.Protein, "%s chain %s", entry, chain.AuthAsymID)
			assert.Equal(t, chain.DepositedLength, other.DepositedLength, "%s chain %s", entry, chain.AuthAsymID)
			assert.Equal(t, LengthFromEntityPolySeq, chain.LengthSource, entry)
			assert.Equal(t, LengthFromSeqres, other.LengthSource, entry)
			assert.Empty(t, other.EntityID, entry)
		}
	}
}

func Test_should_count_selenomethionine_as_a_chain_residue_when_it_is_written_as_hetatm(t *testing.T) {
	// given: 1A62 keeps residues 2 to 9 as ATOM records and MSE 1, 21 and 29 as HETATM, in both
	// formats. In PDB format MSE 1 comes before the chain's first ATOM record and MSE 21 and 29
	// after its last one, before TER; SEQRES names MSE.
	for _, format := range []struct {
		file   string
		format StructureFormat
	}{{"1A62.cif", FormatMMCIF}, {"1A62.pdb", FormatPDB}} {
		// when
		index := readIndex(t, readFixture(t, format.file), format.format)

		// then
		require.Len(t, index.Chains, 1, format.file)
		assert.Equal(t, 11, index.Chains[0].ModeledResidues, format.file)
		assert.Equal(t, 130, index.Chains[0].DepositedLength, format.file)
		assert.True(t, index.Chains[0].Protein, format.file)
	}
}

func Test_should_keep_a_modified_residue_in_its_author_chain_when_it_has_a_label_chain_of_its_own(t *testing.T) {
	// given: a phenix-style file gives MSE 3 label chain B, entity 2, while its author chain is A.
	text := atomSiteHeader +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("ATOM", 2, "CA", ".", "GLY", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("HETATM", 3, "CA", ".", "MSE", "B", "2", "3", "?", "3", "A", 1) +
		atomRow("ATOM", 4, "CA", ".", "LYS", "A", "1", "4", "?", "4", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then: one chain, entity 1 (most of its residues), no stated length.
	assert.Equal(t, []string{"A 1 0/ 4 true"}, chainSummaries(index))
}

func Test_should_count_atom_rows_without_a_label_seq_id_when_a_dna_chain_has_none(t *testing.T) {
	// given: phenix writes "?" as the label_seq_id of every row of some DNA chains.
	text := atomSiteHeader +
		atomRow("ATOM", 1, "P", ".", "DA", "A", "1", "?", "?", "1", "A", 1) +
		atomRow("ATOM", 2, "P", ".", "DC", "A", "1", "?", "?", "2", "A", 1) +
		atomRow("ATOM", 3, "P", ".", "DG", "A", "1", "?", "?", "3", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then: a DNA chain of three residues, not a protein.
	assert.Equal(t, []string{"A 1 0/ 3 false"}, chainSummaries(index))
}

func Test_should_leave_out_ligands_and_water_when_they_share_a_chain_with_polymer_residues(t *testing.T) {
	// given: chain A has two residues, a heme and two waters; chain W has only waters.
	text := atomSiteHeader +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("ATOM", 2, "CA", ".", "GLY", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("HETATM", 3, "FE", ".", "HEM", "B", "2", ".", "?", "101", "A", 1) +
		atomRow("HETATM", 4, "O", ".", "HOH", "C", "3", ".", "?", "201", "A", 1) +
		atomRow("HETATM", 5, "O", ".", "HOH", "C", "3", ".", "?", "202", "W", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 0/ 2 true"}, chainSummaries(index))
	assert.Empty(t, index.Warnings)
}

func Test_should_read_the_first_model_only_when_an_ensemble_has_several(t *testing.T) {
	// given: model 2 has an extra residue and an extra chain.
	text := atomSiteHeader +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("ATOM", 2, "CA", ".", "GLY", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("ATOM", 3, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 2) +
		atomRow("ATOM", 4, "CA", ".", "GLY", "A", "1", "2", "?", "2", "A", 2) +
		atomRow("ATOM", 5, "CA", ".", "LYS", "A", "1", "3", "?", "3", "A", 2) +
		atomRow("ATOM", 6, "CA", ".", "LYS", "B", "1", "1", "?", "1", "B", 2)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 0/ 2 true"}, chainSummaries(index))
}

func Test_should_skip_and_warn_about_sequence_rows_when_their_values_are_not_short_plain_text(t *testing.T) {
	// given: one position of entity 1 has a 100-character number.
	text := "data_TEST\nloop_\n_entity_poly_seq.entity_id\n_entity_poly_seq.num\n_entity_poly_seq.mon_id\n" +
		"1 1 ALA\n1 2 GLY\n1 " + strings.Repeat("9", 100) + " LYS\n#\n" + strings.TrimPrefix(atomSiteHeader, "data_TEST\n") +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 2/entity_poly_seq 1 true"}, chainSummaries(index))
	assert.Equal(t, []string{fmt.Sprintf(skippedSequenceRowsWarning, 1, maxCopyFieldBytes)}, index.Warnings)
}

func Test_should_index_no_chain_when_the_first_model_holds_only_ligands_and_water(t *testing.T) {
	// given: model 1 has a heme and a water; model 2 has protein residues.
	text := atomSiteHeader +
		atomRow("HETATM", 1, "FE", ".", "HEM", "B", "2", ".", "?", "101", "A", 1) +
		atomRow("HETATM", 2, "O", ".", "HOH", "C", "3", ".", "?", "201", "A", 1) +
		atomRow("ATOM", 3, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 2) +
		atomRow("ATOM", 4, "CA", ".", "GLY", "A", "1", "2", "?", "2", "A", 2)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Empty(t, index.Chains)
	assert.Equal(t, []string{noPolymerWarning}, index.Warnings)
}

func Test_should_count_a_position_once_when_it_has_alternate_locations_or_two_residue_types(t *testing.T) {
	// given: residue 2 has conformers A and B; residue 3 is SER or THR (microheterogeneity);
	// residue 4 and 4A differ by insertion code.
	text := atomSiteHeader +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("ATOM", 2, "CB", "A", "LEU", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("ATOM", 3, "CB", "B", "LEU", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("ATOM", 4, "CA", "A", "SER", "A", "1", "3", "?", "3", "A", 1) +
		atomRow("ATOM", 5, "CA", "B", "THR", "A", "1", "3", "?", "3", "A", 1) +
		atomRow("ATOM", 6, "CA", ".", "GLY", "A", "1", "4", "?", "4", "A", 1) +
		atomRow("ATOM", 7, "CA", ".", "GLY", "A", "1", "5", "A", "4", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 0/ 5 true"}, chainSummaries(index))
}

func Test_should_take_the_length_of_the_chains_entity_when_entity_poly_seq_lists_positions(t *testing.T) {
	// given: entity 1 has 5 positions, one of them with two residue types (SER or THR).
	text := `data_TEST
loop_
_entity_poly_seq.entity_id
_entity_poly_seq.num
_entity_poly_seq.mon_id
_entity_poly_seq.hetero
1 1 ALA n
1 2 GLY n
1 3 SER y
1 3 THR y
1 4 LYS n
1 5 LYS n
#
` + strings.TrimPrefix(atomSiteHeader, "data_TEST\n") +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("ATOM", 2, "CA", ".", "GLY", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("ATOM", 3, "P", ".", "DA", "B", "2", "?", "?", "1", "B", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then: entity 2 is not in _entity_poly_seq, so chain B has no stated length.
	assert.Equal(t, []string{"A 1 5/entity_poly_seq 2 true", "B 2 0/ 1 false"}, chainSummaries(index))
}

func Test_should_read_pair_form_categories_when_a_file_has_a_single_atom_and_sequence_position(t *testing.T) {
	// given: one position and one atom, both written as item-value pairs after the other.
	text := `data_TEST
_atom_site.group_PDB ATOM
_atom_site.id 1
_atom_site.label_comp_id GLY
_atom_site.label_asym_id A
_atom_site.label_entity_id 1
_atom_site.label_seq_id 1
_atom_site.auth_seq_id 1
_atom_site.auth_asym_id A
#
_entity_poly_seq.entity_id 1
_entity_poly_seq.num 1
_entity_poly_seq.mon_id GLY
`

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 1/entity_poly_seq 1 true"}, chainSummaries(index))
}

func Test_should_count_only_rows_with_a_label_seq_id_and_warn_when_the_coordinates_have_no_group_column(t *testing.T) {
	// given
	text := `data_TEST
loop_
_atom_site.id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.auth_seq_id
_atom_site.auth_asym_id
1 ALA A 1 1 1 A
2 GLY A 1 2 2 A
3 HEM B 2 . 101 A
`

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 0/ 2 true"}, chainSummaries(index))
	assert.Equal(t, []string{noGroupIndexWarning}, index.Warnings)
}

func Test_should_skip_rows_and_warn_when_their_group_is_neither_atom_nor_hetatm(t *testing.T) {
	// given
	text := atomSiteHeader +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("ANISOU", 2, "CA", ".", "GLY", "A", "1", "2", "?", "2", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 0/ 1 true"}, chainSummaries(index))
	assert.Equal(t, []string{fmt.Sprintf(unexpectedGroupRowsWarning, 1)}, index.Warnings)
}

func Test_should_warn_when_the_first_model_has_no_polymer_residues(t *testing.T) {
	// given: a ligand and a water only.
	text := atomSiteHeader +
		atomRow("HETATM", 1, "FE", ".", "HEM", "A", "1", ".", "?", "1", "A", 1) +
		atomRow("HETATM", 2, "O", ".", "HOH", "B", "2", ".", "?", "2", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Empty(t, index.Chains)
	assert.Equal(t, []string{noPolymerWarning}, index.Warnings)
}

func Test_should_fail_when_the_coordinates_end_inside_a_row(t *testing.T) {
	// given: the last row stops after its fourth value.
	text := atomSiteHeader +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1) +
		"ATOM 2 CA .\n"

	// when
	_, err := ReadCoordinateIndex(context.Background(), strings.NewReader(text), FormatMMCIF)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "_atom_site loop ended inside a row")
}

func Test_should_call_a_chain_protein_when_most_of_its_residues_are_modified_or_unknown_amino_acids(t *testing.T) {
	// given: chain A is MSE, SEP, UNK and one DA; chain B is two DA and one ALA.
	text := atomSiteHeader +
		atomRow("HETATM", 1, "CA", ".", "MSE", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("HETATM", 2, "CA", ".", "SEP", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("ATOM", 3, "CA", ".", "UNK", "A", "1", "3", "?", "3", "A", 1) +
		atomRow("ATOM", 4, "P", ".", "DA", "A", "1", "4", "?", "4", "A", 1) +
		atomRow("ATOM", 5, "P", ".", "DA", "B", "2", "1", "?", "1", "B", 1) +
		atomRow("ATOM", 6, "P", ".", "DA", "B", "2", "2", "?", "2", "B", 1) +
		atomRow("ATOM", 7, "CA", ".", "ALA", "B", "2", "3", "?", "3", "B", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 0/ 4 true", "B 2 0/ 3 false"}, chainSummaries(index))
	assert.Equal(t, []string{"A true/residue_majority", "B false/residue_majority"}, proteinSummaries(index))
}

// proteinSummaries writes each chain as "chain protein/source", in order.
func proteinSummaries(index CoordinateIndex) []string {
	summaries := make([]string, 0, len(index.Chains))
	for _, chain := range index.Chains {
		summaries = append(summaries, fmt.Sprintf("%s %t/%s", chain.AuthAsymID, chain.Protein, chain.ProteinSource))
	}
	return summaries
}

func Test_should_call_a_chain_protein_by_its_entity_poly_type_when_the_file_states_one(t *testing.T) {
	// given: entity 1 is polypeptide(D) of D-alanines, entity 2 a DNA entity whose modeled
	// residues are amino acids, entity 3 polypeptide(L) (written in another case) whose modeled
	// residues are nucleotides. Entity 4 has no _entity_poly row.
	text := `data_TEST
loop_
_entity_poly.entity_id
_entity_poly.type
_entity_poly.nstd_linkage
1 'polypeptide(D)' no
2 polydeoxyribonucleotide no
3 'Polypeptide(L)' no
#
` + strings.TrimPrefix(atomSiteHeader, "data_TEST\n") +
		atomRow("HETATM", 1, "CA", ".", "DAL", "A", "1", "1", "?", "1", "A", 1) +
		atomRow("HETATM", 2, "CA", ".", "DAL", "A", "1", "2", "?", "2", "A", 1) +
		atomRow("ATOM", 3, "CA", ".", "ALA", "B", "2", "1", "?", "1", "B", 1) +
		atomRow("ATOM", 4, "CA", ".", "GLY", "B", "2", "2", "?", "2", "B", 1) +
		atomRow("ATOM", 5, "P", ".", "DA", "C", "3", "1", "?", "1", "C", 1) +
		atomRow("ATOM", 6, "P", ".", "DT", "C", "3", "2", "?", "2", "C", 1) +
		atomRow("ATOM", 7, "CA", ".", "ALA", "D", "4", "1", "?", "1", "D", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then: the stated type wins over the residues; chain D falls back to them.
	assert.Empty(t, index.Warnings)
	assert.Equal(t, []string{
		"A true/entity_poly_type",
		"B false/entity_poly_type",
		"C true/entity_poly_type",
		"D true/residue_majority",
	}, proteinSummaries(index))
}

func Test_should_read_the_polymer_type_when_a_single_entity_file_writes_entity_poly_as_pairs(t *testing.T) {
	// given: one entity, written as item-value pairs with a multi-line sequence, as archive
	// files with a single polymer entity do.
	text := `data_TEST
_entity_poly.entity_id 1
_entity_poly.type 'polypeptide(L)'
_entity_poly.pdbx_seq_one_letter_code
;MVLSPADKTNVKAAWGKVGAHAGEYGAEALERMFLSFPTTKTYFPHF
DLSHGSAQVKGHGKKVADALTNAVAHVDDMPNALSALSDLHAHKL
;
#
` + strings.TrimPrefix(atomSiteHeader, "data_TEST\n") +
		atomRow("ATOM", 1, "P", ".", "DA", "A", "1", "1", "?", "1", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A true/entity_poly_type"}, proteinSummaries(index))
}

func Test_should_skip_and_warn_about_a_polymer_type_when_it_is_not_short_plain_text(t *testing.T) {
	// given: entity 1's type is longer than maxCopyFieldBytes.
	text := `data_TEST
loop_
_entity_poly.entity_id
_entity_poly.type
1 ` + strings.Repeat("x", maxCopyFieldBytes+1) + `
#
` + strings.TrimPrefix(atomSiteHeader, "data_TEST\n") +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "1", "?", "1", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then: the chain falls back to its residues.
	assert.Equal(t, []string{"A true/residue_majority"}, proteinSummaries(index))
	assert.Equal(t, []string{fmt.Sprintf(skippedPolymerRowsWarning, 1, maxCopyFieldBytes)}, index.Warnings)
}

func Test_should_take_the_protein_test_from_the_file_when_reading_archive_and_pdb_fixtures(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		format  StructureFormat
		want    string
	}{
		// 4HHB: two polypeptide(L) entities.
		{"4HHB.cif", FormatMMCIF, "A true/entity_poly_type"},
		// 184D: a polydeoxyribonucleotide entity.
		{"184D.cif", FormatMMCIF, "A false/entity_poly_type"},
		// PDB format states no polymer type.
		{"1A62.pdb", FormatPDB, "A true/residue_majority"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			// when
			index := readIndex(t, readFixture(t, tc.fixture), tc.format)

			// then
			require.NotEmpty(t, index.Chains)
			assert.Equal(t, tc.want, proteinSummaries(index)[0])
		})
	}
}

func Test_should_count_unmodeled_residues_toward_the_deposited_length_when_most_of_the_chain_has_no_coordinates(t *testing.T) {
	// given: entity 1 has 10 positions; chain A has coordinates for 3 of them.
	sequence := ""
	for number := 1; number <= 10; number++ {
		sequence += fmt.Sprintf("1 %d ALA n\n", number)
	}
	text := `data_TEST
loop_
_entity_poly_seq.entity_id
_entity_poly_seq.num
_entity_poly_seq.mon_id
_entity_poly_seq.hetero
` + sequence + `#
` + strings.TrimPrefix(atomSiteHeader, "data_TEST\n") +
		atomRow("ATOM", 1, "CA", ".", "ALA", "A", "1", "4", "?", "4", "A", 1) +
		atomRow("ATOM", 2, "CA", ".", "ALA", "A", "1", "5", "?", "5", "A", 1) +
		atomRow("ATOM", 3, "CA", ".", "ALA", "A", "1", "9", "?", "9", "A", 1)

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"A 1 10/entity_poly_seq 3 true"}, chainSummaries(index))
}

func Test_should_index_each_copy_as_a_chain_when_identical_chains_share_an_entity(t *testing.T) {
	// given: five copies (chains A to E) of entity 1, three positions long.
	text := `data_TEST
loop_
_entity_poly_seq.entity_id
_entity_poly_seq.num
_entity_poly_seq.mon_id
_entity_poly_seq.hetero
1 1 ALA n
1 2 GLY n
1 3 LYS n
#
` + strings.TrimPrefix(atomSiteHeader, "data_TEST\n")
	id := 0
	for _, chain := range []string{"A", "B", "C", "D", "E"} {
		for number, compound := range []string{"ALA", "GLY", "LYS"} {
			id++
			text += atomRow("ATOM", id, "CA", ".", compound, chain, "1",
				fmt.Sprint(number+1), "?", fmt.Sprint(number+1), chain, 1)
		}
	}

	// when
	index := readIndex(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{
		"A 1 3/entity_poly_seq 3 true",
		"B 1 3/entity_poly_seq 3 true",
		"C 1 3/entity_poly_seq 3 true",
		"D 1 3/entity_poly_seq 3 true",
		"E 1 3/entity_poly_seq 3 true",
	}, chainSummaries(index))
}

func Test_should_sort_pdb_hetatm_residues_into_or_out_of_their_chain_by_ter_and_seqres(t *testing.T) {
	// given: chain A's SEQRES lists ALA MSE GLY MSE (4 residues). MSE 2 sits between ATOM
	// residues; MSE 4 follows the last ATOM residue, before TER; a SEQRES-named MSE 5 and a
	// heme come after TER. Chain B has no SEQRES: a heme after its last ATOM residue, with no TER,
	// is not a chain residue, but an ACE before its first ATOM residue is not either.
	text := "SEQRES   1 A    4  ALA MSE GLY MSE\n" +
		pdbAtom("ATOM", 1, "CA", "ALA", "A", 1, "") +
		pdbAtom("HETATM", 2, "CA", "MSE", "A", 2, "") +
		pdbAtom("ATOM", 3, "CA", "GLY", "A", 3, "") +
		pdbAtom("HETATM", 4, "CA", "MSE", "A", 4, "") +
		"TER       5      MSE A   4\n" +
		pdbAtom("HETATM", 6, "CA", "MSE", "A", 5, "") +
		pdbAtom("HETATM", 7, "FE", "HEM", "A", 101, "") +
		pdbAtom("HETATM", 8, "C", "ACE", "B", 0, "") +
		pdbAtom("ATOM", 9, "CA", "LYS", "B", 1, "") +
		pdbAtom("ATOM", 10, "CA", "LYS", "B", 2, "") +
		pdbAtom("HETATM", 11, "FE", "HEM", "B", 101, "") +
		pdbAtom("HETATM", 12, "O", "HOH", "B", 201, "") +
		"END\n"

	// when
	index := readIndex(t, text, FormatPDB)

	// then
	assert.Equal(t, []string{"A  4/seqres 4 true", "B  0/ 2 true"}, chainSummaries(index))
	assert.Empty(t, index.Warnings)
}

func Test_should_count_hetatm_residues_between_atom_residues_when_the_chain_has_no_seqres(t *testing.T) {
	// given: a computed model without SEQRES or TER: MSE 2 between ATOM residues, a ligand at
	// the end.
	text := pdbAtom("ATOM", 1, "CA", "ALA", "A", 1, "") +
		pdbAtom("HETATM", 2, "CA", "MSE", "A", 2, "") +
		pdbAtom("ATOM", 3, "CA", "GLY", "A", 3, "") +
		pdbAtom("HETATM", 4, "C1", "LIG", "A", 4, "")

	// when
	index := readIndex(t, text, FormatPDB)

	// then
	assert.Equal(t, []string{"A  0/ 3 true"}, chainSummaries(index))
}

func Test_should_read_the_first_model_only_when_a_pdb_file_has_model_blocks(t *testing.T) {
	// given: model 2 adds a residue and a chain.
	text := "MODEL        1\n" +
		pdbAtom("ATOM", 1, "CA", "ALA", "A", 1, "") +
		pdbAtom("ATOM", 2, "CA", "GLY", "A", 2, "") +
		"TER       3      GLY A   2\nENDMDL\nMODEL        2\n" +
		pdbAtom("ATOM", 1, "CA", "ALA", "A", 1, "") +
		pdbAtom("ATOM", 2, "CA", "GLY", "A", 2, "") +
		pdbAtom("ATOM", 3, "CA", "LYS", "A", 3, "") +
		pdbAtom("ATOM", 4, "CA", "LYS", "B", 1, "") +
		"ENDMDL\nEND\n"

	// when
	index := readIndex(t, text, FormatPDB)

	// then
	assert.Equal(t, []string{"A  0/ 2 true"}, chainSummaries(index))
}

func Test_should_keep_insertion_codes_apart_when_a_pdb_chain_uses_them(t *testing.T) {
	// given: residues 52, 52A and 52B.
	text := pdbAtom("ATOM", 1, "CA", "ALA", "H", 52, "") +
		pdbAtom("ATOM", 2, "CA", "GLY", "H", 52, "A") +
		pdbAtom("ATOM", 3, "CA", "SER", "H", 52, "B")

	// when
	index := readIndex(t, text, FormatPDB)

	// then
	assert.Equal(t, []string{"H  0/ 3 true"}, chainSummaries(index))
}

func Test_should_fail_to_index_when_the_format_is_unknown(t *testing.T) {
	// when
	_, err := ReadCoordinateIndex(context.Background(), strings.NewReader("data_TEST\n"), StructureFormat(0))

	// then
	require.Error(t, err)
}
