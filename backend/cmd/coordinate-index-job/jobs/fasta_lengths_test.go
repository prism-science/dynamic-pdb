package jobs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

func Test_should_read_label_and_author_chains_when_a_fasta_header_names_them(t *testing.T) {
	cases := map[string]struct {
		header string
		want   []fastaChain
	}{
		"one chain":                 {header: "101M_1|Chain A|MYOGLOBIN|Physeter catodon (9755)", want: []fastaChain{{label: "A", auth: "A"}}},
		"several chains":            {header: "4HHB_1|Chains A, C|Hemoglobin subunit alpha|Homo sapiens (9606)", want: []fastaChain{{label: "A", auth: "A"}, {label: "C", auth: "C"}}},
		"author chains in brackets": {header: "7XYZ_2|Chains B[auth D], C[auth E]|name|organism", want: []fastaChain{{label: "B", auth: "D"}, {label: "C", auth: "E"}}},
		"mixed":                     {header: "7XYZ_3|Chains AA, B[auth BB]|name", want: []fastaChain{{label: "AA", auth: "AA"}, {label: "B", auth: "BB"}}},
		"upper case word":           {header: "1ABC_1|CHAINS A, B|name", want: []fastaChain{{label: "A", auth: "A"}, {label: "B", auth: "B"}}},
		"no chain field":            {header: "my protein|some notes", want: nil},
		"one field only":            {header: "sp|P69905", want: nil},
		"no fields":                 {header: "just a name", want: nil},
		"bracket without auth":      {header: "1ABC_1|Chains A[B], C|name", want: []fastaChain{{label: "C", auth: "C"}}},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			chains := headerChains(testCase.header)

			// then
			if testCase.want == nil {
				assert.Empty(t, chains)
				return
			}
			assert.Equal(t, testCase.want, chains)
		})
	}
}

// readChains is a read with these chains; a chain without a protein source gets the residue
// majority one, as the reader names a source for every chain.
func readChains(chains ...cif.IndexedChain) cif.CoordinateIndex {
	for index := range chains {
		if chains[index].ProteinSource == "" {
			chains[index].ProteinSource = cif.ProteinFromResidueMajority
		}
	}
	return cif.CoordinateIndex{Chains: chains}
}

func oneEntry(records ...models.FASTARecordLength) models.ModelFileFASTA {
	return models.ModelFileFASTA{EntryIDs: []string{"entry-1"}, Records: records}
}

func Test_should_take_each_chain_length_from_the_first_source_that_gives_one(t *testing.T) {
	// given: A has its file length; B is named as author chain by a record; C only as label chain
	// (the file keeps label IDs: label C is author chain Z); D appears nowhere.
	read := readChains(
		cif.IndexedChain{AuthAsymID: "A", DepositedLength: 141, LengthSource: cif.LengthFromEntityPolySeq, ModeledResidues: 140, Protein: true},
		cif.IndexedChain{AuthAsymID: "B", ModeledResidues: 50, Protein: true},
		cif.IndexedChain{AuthAsymID: "C", ModeledResidues: 60, Protein: true},
		cif.IndexedChain{AuthAsymID: "D", ModeledResidues: 10, Protein: false},
	)
	fasta := oneEntry(
		models.FASTARecordLength{Header: "1ABC_1|Chain A|alpha", Length: 999},
		models.FASTARecordLength{Header: "1ABC_2|Chains B, Y|beta", Length: 55},
		models.FASTARecordLength{Header: "1ABC_3|Chain C[auth Z]|gamma", Length: 66},
	)

	// when
	chains, warnings, err := indexedChains(read, fasta)

	// then
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{
		"A 141/entity_poly_seq 140 true",
		"B 55/fasta_auth_chain 50 true",
		"C 66/fasta_label_chain 60 true",
		"D 0/ 10 false",
	}, chainSummaries(chains))
}

func Test_should_prefer_the_author_chain_record_when_another_record_names_the_id_as_label_chain(t *testing.T) {
	// given: label chain A is author chain B, and label chain C is author chain A.
	read := readChains(cif.IndexedChain{AuthAsymID: "A", ModeledResidues: 5, Protein: true})
	fasta := oneEntry(
		models.FASTARecordLength{Header: "1ABC_1|Chain A[auth B]|first", Length: 100},
		models.FASTARecordLength{Header: "1ABC_2|Chain C[auth A]|second", Length: 200},
	)

	// when
	chains, _, err := indexedChains(read, fasta)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"A 200/fasta_auth_chain 5 true"}, chainSummaries(chains))
}

func Test_should_leave_the_length_unknown_and_warn_when_records_disagree_about_a_chain(t *testing.T) {
	// given: two FASTA files of the entry name chain A with different lengths; a third agrees.
	read := readChains(cif.IndexedChain{AuthAsymID: "A", ModeledResidues: 5, Protein: true})
	fasta := oneEntry(
		models.FASTARecordLength{Header: "1ABC_1|Chain A|first", Length: 100},
		models.FASTARecordLength{Header: "1ABC_1|Chain A|again", Length: 120},
		models.FASTARecordLength{Header: "1ABC_1|Chain A|same", Length: 100},
	)

	// when
	chains, warnings, err := indexedChains(read, fasta)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"A 0/ 5 true"}, chainSummaries(chains))
	assert.Equal(t, []string{fmt.Sprintf(conflictingFASTAWarning, "A", "100, 120")}, warnings)
}

func Test_should_take_no_fasta_length_and_warn_when_models_of_several_entries_use_the_file(t *testing.T) {
	// given
	read := readChains(cif.IndexedChain{AuthAsymID: "A", ModeledResidues: 5, Protein: true})
	fasta := models.ModelFileFASTA{
		EntryIDs: []string{"entry-1", "entry-2"},
		Records:  []models.FASTARecordLength{{EntryID: "entry-1", Header: "1ABC_1|Chain A|first", Length: 100}},
	}

	// when
	chains, warnings, err := indexedChains(read, fasta)

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"A 0/ 5 true"}, chainSummaries(chains))
	assert.Equal(t, []string{fmt.Sprintf(manyEntriesWarning, 2)}, warnings)
}

func Test_should_keep_file_lengths_when_no_model_uses_the_file_yet(t *testing.T) {
	// given
	read := readChains(cif.IndexedChain{AuthAsymID: "A", DepositedLength: 30, LengthSource: cif.LengthFromSeqres, ModeledResidues: 5, Protein: true})

	// when
	chains, warnings, err := indexedChains(read, models.ModelFileFASTA{})

	// then
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{"A 30/seqres 5 true"}, chainSummaries(chains))
}

func Test_should_store_the_protein_test_the_reader_used_for_each_chain(t *testing.T) {
	// given
	read := readChains(
		cif.IndexedChain{AuthAsymID: "A", ModeledResidues: 5, Protein: true, ProteinSource: cif.ProteinFromEntityPolyType},
		cif.IndexedChain{AuthAsymID: "B", ModeledResidues: 5, Protein: true, ProteinSource: cif.ProteinFromResidueMajority},
	)

	// when
	chains, _, err := indexedChains(read, models.ModelFileFASTA{})

	// then
	require.NoError(t, err)
	require.Len(t, chains, 2)
	assert.Equal(t, models.ProteinSourceEntityPolyType, chains[0].ProteinSource)
	assert.Equal(t, models.ProteinSourceResidueMajority, chains[1].ProteinSource)
}

func Test_should_fail_when_the_reader_names_a_protein_source_the_job_does_not_know(t *testing.T) {
	// given
	read := readChains(cif.IndexedChain{AuthAsymID: "A", ModeledResidues: 5, Protein: true, ProteinSource: "guess"})

	// when
	_, _, err := indexedChains(read, models.ModelFileFASTA{})

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown protein source "guess"`)
}

func Test_should_fail_when_the_reader_names_a_length_source_the_job_does_not_know(t *testing.T) {
	// given
	read := readChains(cif.IndexedChain{AuthAsymID: "A", DepositedLength: 30, LengthSource: "guess", ModeledResidues: 5})

	// when
	_, _, err := indexedChains(read, models.ModelFileFASTA{})

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown chain length source "guess"`)
}
