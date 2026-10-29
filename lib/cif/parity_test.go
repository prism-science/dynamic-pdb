package cif

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const parityGoldenPath = "testdata/parity-golden.json"

// parityFiles are the files the website preview parser must agree with: the
// website test reads the same files and the same golden.
var parityFiles = []string{
	"4HHB.cif", "3CLN.cif", "6OIM.cif", "193L.cif", "1AKE.cif",
	"184D.cif", "4UG1.cif", "1W53.cif", "4HB1.cif",
	"parity/instances.cif", "parity/no_metalc.cif", "parity/two_models.cif",
	"parity/wrapped_rows.cif",
}

type parityCompound struct {
	List   string `json:"list"`
	ID     string `json:"id"`
	Review bool   `json:"needs_review,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type parityEntry struct {
	AtomCount           int              `json:"atom_count"`
	ModeledResidues     int              `json:"modeled_residues"`
	UniqueProteinChains int              `json:"unique_protein_chains"`
	Compounds           []parityCompound `json:"compounds"`
}

func parityGolden(t *testing.T) map[string]parityEntry {
	t.Helper()
	golden := map[string]parityEntry{}
	for _, name := range parityFiles {
		result, err := Extract(Parse(readFixture(t, name)))
		require.NoError(t, err)
		entry := parityEntry{Compounds: []parityCompound{}}
		if result.Atoms != nil {
			entry.AtomCount = result.Atoms.AtomCount
			entry.ModeledResidues = result.Atoms.ModeledResidues
			entry.UniqueProteinChains = result.Atoms.ProteinChains
		}
		for _, compound := range result.Ligands {
			entry.Compounds = append(entry.Compounds, parityCompound{ListLigands, compound.ID, compound.NeedsReview, compound.ReviewReason})
		}
		for _, compound := range result.Cofactors {
			entry.Compounds = append(entry.Compounds, parityCompound{ListCofactors, compound.ID, compound.NeedsReview, compound.ReviewReason})
		}
		sort.SliceStable(entry.Compounds, func(i, j int) bool {
			left, right := entry.Compounds[i], entry.Compounds[j]
			if left.List != right.List {
				return left.List < right.List
			}
			return left.ID < right.ID
		})
		golden[name] = entry
	}
	return golden
}

// Test_should_match_the_committed_parity_golden_when_files_are_extracted pins
// what the Go parser says about the files the website preview must agree with.
// Run with UPDATE_PARITY_GOLDEN=1 to rewrite the golden after a deliberate change.
func Test_should_match_the_committed_parity_golden_when_files_are_extracted(t *testing.T) {
	// given
	golden := parityGolden(t)
	encoded, err := json.MarshalIndent(golden, "", "  ")
	require.NoError(t, err)
	encoded = append(encoded, '\n')

	// when
	if os.Getenv("UPDATE_PARITY_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(filepath.FromSlash(parityGoldenPath), encoded, 0o644))
	}
	committed, err := os.ReadFile(filepath.FromSlash(parityGoldenPath))

	// then
	require.NoError(t, err)
	assert.JSONEq(t, string(encoded), string(committed), "run UPDATE_PARITY_GOLDEN=1 go test ./cif -run parity_golden to refresh it")
}
