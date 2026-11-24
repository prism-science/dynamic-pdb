package e2etest

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
)

// Test PDB IDs. The tests of a run share its schema (TestMain), and test data stays there until
// the run ends. The API refuses a PDB ID that another entry holds, as the entry's own PDB ID or on
// one of its models (one deposition is one model of one entry), so a test given a PDB ID that an
// earlier test stored gets 409 on its create.

// testPDBIDs counts the PDB IDs this test process has handed out.
var testPDBIDs struct {
	sync.Mutex
	next int
}

// nextTestPDBIDCandidate returns the next PDB ID of the run, "9" and three base-36 digits: 9000,
// 9001, … 9ZZZ. No two calls return the same one, so two IDs a test takes before storing either
// cannot be equal.
func nextTestPDBIDCandidate(t *testing.T) string {
	t.Helper()
	const symbols = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const suffixLength = 3
	testPDBIDs.Lock()
	index := testPDBIDs.next
	testPDBIDs.next++
	testPDBIDs.Unlock()
	suffix := make([]byte, suffixLength)
	for position := suffixLength - 1; position >= 0; position-- {
		suffix[position] = symbols[index%len(symbols)]
		index /= len(symbols)
	}
	if index > 0 {
		require.FailNow(t, "every 9xxx test PDB ID is used in this run")
	}
	return "9" + string(suffix)
}

// unusedPDBIDForTest returns a PDB ID that no earlier call returned and that no entry revision or
// model revision of the run names, in any state.
func unusedPDBIDForTest(t *testing.T) string {
	t.Helper()
	return firstUnusedPDBIDForTest(t, nextTestPDBIDCandidate)
}

// firstUnusedPDBIDForTest returns the first PDB ID from candidate that no entry revision or model
// revision names, in any state.
func firstUnusedPDBIDForTest(t *testing.T, candidate func(*testing.T) string) string {
	t.Helper()
	for range 50 {
		pdbID := candidate(t)
		if !pdbIDNamedForTest(t, pdbID) {
			return pdbID
		}
	}
	require.FailNow(t, "no unused test PDB ID after 50 tries")
	return ""
}

// pdbIDNamedForTest reports whether an entry revision or a model revision names pdbID, in any
// state.
func pdbIDNamedForTest(t *testing.T, pdbID string) bool {
	t.Helper()
	limit := 1
	entries, err := testDatabase.Entries.List(t.Context(), db.EntryRevisionFilters{
		PDBIDs: []string{pdbID},
		Limit:  &limit,
	})
	require.NoError(t, err)
	if len(entries) > 0 {
		return true
	}
	modelRevisions, err := testDatabase.Models.List(t.Context(), db.ModelRevisionFilters{
		PDBIDs:             []string{pdbID},
		Limit:              &limit,
		ExcludeResidueData: true,
	})
	require.NoError(t, err)
	return len(modelRevisions) > 0
}

// A PDB ID that only a model holds is not handed out: the API refuses it to another entry (409),
// as an entry's own PDB ID or on one of its models.
func (s *SchemaValidationSuite) Test_should_not_hand_out_a_test_pdb_id_that_a_model_of_another_entry_holds() {
	// given: an active model naming heldPDBID, in an entry with another PDB ID
	token := issueEntryTokenForGitHubIDForTest(s.T(), "test-pdb-id-owner", 9109)
	entryPDBID, heldPDBID, freePDBID := unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T())
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{
		"entry": map[string]any{"title": "entry of the model", "external_refs": map[string]string{"pdb": entryPDBID}},
	})
	createModelFiledBeforeTheRuleForTest(s.T(), token, entry.EntryId,
		archiveEnsembleForTest(heldPDBID, entryPDBID, coordinateFileForTest(heldPDBID)))
	candidates := []string{heldPDBID, freePDBID}

	// when
	pdbID := firstUnusedPDBIDForTest(s.T(), func(*testing.T) string {
		next := candidates[0]
		candidates = candidates[1:]
		return next
	})

	// then
	s.Equal(freePDBID, pdbID)
}
