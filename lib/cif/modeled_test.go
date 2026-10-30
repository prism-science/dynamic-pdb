package cif

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readModeled(t *testing.T, text string, format StructureFormat) ModeledCompounds {
	t.Helper()
	result, err := ReadModeledCompounds(context.Background(), strings.NewReader(text), format)
	require.NoError(t, err)
	return result
}

// copyPositions writes each copy as "CCD chain residue", in result order.
func copyPositions(result ModeledCompounds) []string {
	positions := make([]string, 0, len(result.Copies))
	for _, copied := range result.Copies {
		positions = append(positions, strings.TrimSpace(copied.CCDID+" "+copied.AuthAsymID+" "+copied.AuthSeqID+copied.InsCode))
	}
	return positions
}

func Test_should_list_every_scheme_copy_when_the_file_has_scheme_categories(t *testing.T) {
	// when: 4HHB, deoxyhemoglobin: a heme in each chain, phosphate in B and D.
	result := readModeled(t, readFixture(t, "4HHB.cif"), FormatMMCIF)

	// then
	assert.True(t, result.Complete)
	assert.Empty(t, result.Warnings)
	assert.Equal(t, []string{"HEM A 142", "HEM B 148", "HEM C 142", "HEM D 148", "PO4 B 147", "PO4 D 147"}, copyPositions(result))
	assert.Equal(t, CompoundCopy{
		CCDID: "HEM", LabelAsymID: "E", AuthAsymID: "A", AuthSeqID: "142", EntityID: "3", Source: SourceNonpolyScheme,
	}, result.Copies[0])
}

func Test_should_list_each_sugar_when_the_file_has_a_branch_scheme(t *testing.T) {
	// when: 2VSD, an NAG-NAG-BMA glycan on an asparagine.
	result := readModeled(t, readFixture(t, "2VSD.cif"), FormatMMCIF)

	// then
	assert.Equal(t, []string{"BMA B 3", "NAG B 1", "NAG B 2"}, copyPositions(result))
	for _, copied := range result.Copies {
		assert.Equal(t, SourceBranchScheme, copied.Source)
		assert.Equal(t, "2", copied.EntityID)
	}
}

func Test_should_read_the_schemes_when_they_come_after_the_coordinates(t *testing.T) {
	// when: 1C0R writes _atom_site before both scheme categories. RCSB's
	// "bound components" leave all of these out.
	result := readModeled(t, readFixture(t, "1C0R.cif"), FormatMMCIF)

	// then
	assert.True(t, result.Complete)
	assert.Equal(t, []string{"BGC C 1", "BGC D 1", "CL A 20", "CL A 21", "CL B 22", "LAC B 23", "RER C 2", "RER D 2"}, copyPositions(result))
}

func Test_should_keep_buffers_and_list_the_same_copies_when_the_entry_is_in_either_format(t *testing.T) {
	for _, entry := range []string{"3CEB", "7LTB"} {
		// when
		mmcif := readModeled(t, readFixture(t, entry+".cif"), FormatMMCIF)
		pdb := readModeled(t, readFixture(t, entry+".pdb"), FormatPDB)

		// then: chain residues (MSE, LLP, 7LTB's antibiotic) are not copies;
		// glycerol, which the entry lists leave out, is.
		assert.Equal(t, copyPositions(mmcif), copyPositions(pdb), entry)
		for _, copied := range pdb.Copies {
			assert.Equal(t, SourcePDBHetatm, copied.Source, entry)
			assert.False(t, copied.NeedsReview, entry)
		}
	}
	assert.Equal(t, []string{"GOL A 199", "SO4 A 194", "SO4 A 195", "SO4 A 196", "SO4 A 197", "SO4 A 198"},
		copyPositions(readModeled(t, readFixture(t, "3CEB.cif"), FormatMMCIF)))
}

func Test_should_mark_possible_chain_residues_for_review_when_the_file_has_no_entity(t *testing.T) {
	// when: 3CEB.cif without _entity; its schemes still list sulfate and glycerol.
	result := readModeled(t, readFixture(t, "parity/no_entity_3CEB.cif"), FormatMMCIF)

	// then
	flagged := []string{}
	for _, copied := range result.Copies {
		if copied.NeedsReview {
			assert.Equal(t, undeclaredChainReason, copied.ReviewReason)
			assert.Equal(t, SourceAtomSite, copied.Source)
			flagged = append(flagged, copied.CCDID)
		}
	}
	assert.Equal(t, []string{"LLP", "MSE", "MSE", "MSE", "MSE"}, flagged)
	assert.Contains(t, result.Warnings, fmt.Sprintf(extraCopiesWarning, 5))
}

func Test_should_mark_possible_chain_residues_for_review_when_the_pdb_file_has_no_seqres(t *testing.T) {
	// when
	result := readModeled(t, readFixture(t, "parity/no_seqres_3CEB.pdb"), FormatPDB)

	// then
	flagged := []string{}
	for _, copied := range result.Copies {
		if copied.NeedsReview {
			flagged = append(flagged, copied.CCDID)
		}
	}
	assert.Equal(t, []string{"LLP", "MSE", "MSE", "MSE", "MSE"}, flagged)
	assert.Len(t, result.Copies, 11)
}

func Test_should_list_a_compound_of_a_later_model_when_the_mmcif_file_has_several_models(t *testing.T) {
	// given: ZN in both models, ATP only in model 2.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer", "3 non-polymer"),
		hetatmLoop(
			"ATOM ALA A 1 1 1 CA C 1",
			"HETATM ZN B 2 . 101 ZN ZN 1",
			"ATOM ALA A 1 1 1 CA C 2",
			"HETATM ZN B 2 . 101 ZN ZN 2",
			"HETATM ATP C 3 . 201 PG P 2",
		),
	)

	// when
	result := readModeled(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"ATP C 201", "ZN B 101"}, copyPositions(result))
	assert.True(t, result.Complete)
}

func Test_should_list_a_compound_of_a_later_model_when_the_pdb_file_has_several_models(t *testing.T) {
	// given
	text := pdbSeqres("A", "ALA") +
		"MODEL        1\n" +
		pdbRecord("ATOM", 1, "CA", "ALA", "A", 1, "C") +
		pdbTer(2, "ALA", "A", 1) +
		pdbRecord("HETATM", 3, "ZN", "ZN", "A", 101, "ZN") +
		"ENDMDL\nMODEL        2\n" +
		pdbRecord("ATOM", 1, "CA", "ALA", "A", 1, "C") +
		pdbTer(2, "ALA", "A", 1) +
		pdbRecord("HETATM", 3, "ZN", "ZN", "A", 101, "ZN") +
		pdbRecord("HETATM", 4, "PG", "ATP", "A", 201, "P") +
		"ENDMDL\nEND\n"

	// when
	result := readModeled(t, text, FormatPDB)

	// then
	assert.Equal(t, []string{"ATP A 201", "ZN A 101"}, copyPositions(result))
}

func Test_should_leave_out_water_when_the_file_names_it_any_of_its_ways(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 non-polymer", "2 water"),
		hetatmLoop(
			"HETATM HOH W 2 . 1 O O 1",
			"HETATM DOD W 2 . 2 O O 1",
			"HETATM WAT W 2 . 3 O O 1",
			"HETATM H2O W 2 . 4 O O 1",
			"HETATM GOL B 1 . 5 C1 C 1",
		),
	)

	// when
	result := readModeled(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"GOL B 5"}, copyPositions(result))
}

func Test_should_add_a_coordinate_copy_and_warn_when_the_schemes_leave_it_out(t *testing.T) {
	// given: the scheme lists ATP; the coordinates also hold a glycerol.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer", "3 non-polymer"),
		"loop_\n_pdbx_nonpoly_scheme.asym_id\n_pdbx_nonpoly_scheme.entity_id\n_pdbx_nonpoly_scheme.mon_id\n"+
			"_pdbx_nonpoly_scheme.pdb_seq_num\n_pdbx_nonpoly_scheme.pdb_strand_id\n_pdbx_nonpoly_scheme.pdb_ins_code\n"+
			"B 2 ATP 201 B .\n#",
		hetatmLoop(
			"HETATM ATP B 2 . 201 PG P 1",
			"HETATM GOL C 3 . 301 C1 C 1",
		),
	)

	// when
	result := readModeled(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []CompoundCopy{
		{CCDID: "ATP", LabelAsymID: "B", AuthAsymID: "B", AuthSeqID: "201", EntityID: "2", Source: SourceNonpolyScheme},
		{CCDID: "GOL", LabelAsymID: "C", AuthAsymID: "C", AuthSeqID: "301", EntityID: "3", Source: SourceAtomSite},
	}, result.Copies)
	assert.Equal(t, []string{fmt.Sprintf(extraCopiesWarning, 1)}, result.Warnings)
}

func Test_should_read_a_scheme_when_its_single_row_is_written_as_pairs(t *testing.T) {
	// given: one ligand and no water, so the scheme has no loop.
	text := mmcifFile(
		entityLoop("1 polymer", "2 non-polymer"),
		"_pdbx_nonpoly_scheme.asym_id B\n_pdbx_nonpoly_scheme.entity_id 2\n_pdbx_nonpoly_scheme.mon_id HEM\n"+
			"_pdbx_nonpoly_scheme.pdb_seq_num 501\n_pdbx_nonpoly_scheme.pdb_strand_id B\n_pdbx_nonpoly_scheme.pdb_ins_code .\n#",
		hetatmLoop("HETATM HEM B 2 . 501 FE FE 1"),
	)

	// when
	result := readModeled(t, text, FormatMMCIF)

	// then: the coordinates hold the same copy, so nothing is added.
	assert.Equal(t, []CompoundCopy{
		{CCDID: "HEM", LabelAsymID: "B", AuthAsymID: "B", AuthSeqID: "501", EntityID: "2", Source: SourceNonpolyScheme},
	}, result.Copies)
	assert.Empty(t, result.Warnings)
}

func Test_should_flag_a_copy_when_its_entity_type_is_unknown(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 polymer", "2 'mystery'"),
		hetatmLoop("HETATM QQQ B 2 . 101 C1 C 1"),
	)

	// when
	result := readModeled(t, text, FormatMMCIF)

	// then
	require.Len(t, result.Copies, 1)
	assert.True(t, result.Copies[0].NeedsReview)
	assert.Equal(t, `unknown entity type "mystery": confirm where this compound belongs`, result.Copies[0].ReviewReason)
}

func Test_should_order_residue_numbers_as_numbers_when_copies_are_listed(t *testing.T) {
	// given
	text := mmcifFile(
		entityLoop("1 non-polymer"),
		hetatmLoop(
			"HETATM SO4 A 1 . 100 S S 1",
			"HETATM SO4 A 1 . 9 S S 1",
			"HETATM SO4 A 1 . 10 S S 1",
		),
	)

	// when
	result := readModeled(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"SO4 A 9", "SO4 A 10", "SO4 A 100"}, copyPositions(result))
}

func Test_should_read_the_whole_stream_when_the_compound_is_past_the_head_read_limit(t *testing.T) {
	// given: more than HeadReadLimit of protein rows before the only ligand.
	protein := strings.Repeat("ATOM ALA A 1 1 1 CA C 1\n", HeadReadLimit/20)
	text := "data_X\n" + entityLoop("1 polymer", "2 non-polymer") + "\n" +
		strings.TrimSuffix(hetatmLoop(), "\n#") + protein + "HETATM ATP B 2 . 201 PG P 1\n#\n"
	require.Greater(t, len(text), HeadReadLimit)

	// when
	result := readModeled(t, text, FormatMMCIF)

	// then
	assert.Equal(t, []string{"ATP B 201"}, copyPositions(result))
}

func Test_should_stop_listing_and_warn_when_copies_pass_the_limit(t *testing.T) {
	// given: one more distinct ligand residue than the limit.
	var text strings.Builder
	for index := range maxModeledCopies + 1 {
		chain := string(rune('A' + index/9999))
		text.WriteString(pdbRecord("HETATM", index%99999, "C1", "LIG", chain, index%9999+1, "C"))
	}

	// when
	result := readModeled(t, text.String(), FormatPDB)

	// then
	assert.False(t, result.Complete)
	assert.Len(t, result.Copies, maxModeledCopies)
	assert.Contains(t, result.Warnings, fmt.Sprintf(tooManyCopiesWarning, maxModeledCopies))
}

func Test_should_fail_when_the_context_is_done_before_the_file_is_read(t *testing.T) {
	// given
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, format := range []StructureFormat{FormatMMCIF, FormatPDB} {
		// when
		_, err := ReadModeledCompounds(ctx, strings.NewReader(readFixture(t, "3CEB.cif")), format)

		// then
		require.ErrorIs(t, err, context.Canceled)
	}
}

func Test_should_fail_when_the_format_is_unknown(t *testing.T) {
	// when
	_, err := ReadModeledCompounds(context.Background(), strings.NewReader(""), StructureFormat(99))

	// then
	require.EqualError(t, err, "unknown structure format 99")
}
