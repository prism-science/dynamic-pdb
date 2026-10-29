package cif

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const parityGoldenPath = "testdata/parity-golden.json"

// parityFiles are the files the website preview parser must agree with: the
// website test reads the same files and the same golden.
var parityFiles = []string{
	"4HHB.cif", "3CLN.cif", "6OIM.cif", "193L.cif", "1AKE.cif",
	"184D.cif", "4UG1.cif", "1W53.cif", "4HB1.cif", "1A62.cif", "7LTB.cif", "3CEB.cif",
	"9A8B.cif",
	"parity/instances.cif", "parity/no_metalc.cif", "parity/two_models.cif",
	"parity/wrapped_rows.cif", "parity/ihm_probe.cif",
	"1A62.pdb", "3CLN.pdb", "3CEB.pdb", "1II5.pdb", "7LTB.pdb", "6OIM.pdb",
	"parity/pdb_rules.pdb", "parity/pdb_no_metal_link.pdb",
}

// parityExcluded is the list name the golden gives a compound left out.
const parityExcluded = "excluded"

// parityCompound is one placed compound with the rule that placed it. A
// compound left out has the list "excluded".
type parityCompound struct {
	List   string `json:"list"`
	ID     string `json:"id"`
	Rule   string `json:"rule"`
	Review bool   `json:"needs_review,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type parityEntry struct {
	AtomCount           int              `json:"atom_count"`
	ModeledResidues     int              `json:"modeled_residues"`
	UniqueProteinChains int              `json:"unique_protein_chains"`
	Compounds           []parityCompound `json:"compounds"`
	Glycans             []Glycan         `json:"glycans,omitempty"`
}

// parityExtraction reads a parity file: a .pdb file through PDBExtraction,
// any other through Extract, as the CLI and the extract endpoint do.
func parityExtraction(t *testing.T, name string) Result {
	t.Helper()
	text := readFixture(t, name)
	if !strings.HasSuffix(name, ".pdb") {
		result, err := Extract(Parse(text))
		require.NoError(t, err)
		return result
	}
	extraction, err := PDBExtraction(text, false)
	require.NoError(t, err)
	reference, err := DefaultReference()
	require.NoError(t, err)
	result := Result{CompoundSources: extraction.CompoundSources, ReferenceVersion: reference.Version}
	if count, ok := extraction.Fields["atom_count"].(int); ok {
		result.Atoms = &AtomStats{AtomCount: count}
		result.Atoms.ModeledResidues, _ = extraction.Fields["modeled_residues"].(int)
		result.Atoms.ProteinChains, _ = extraction.Fields["unique_protein_chains"].(int)
	}
	reviews := map[string]ReviewItem{}
	for _, item := range extraction.Review {
		reviews[item.List+":"+item.ID] = item
	}
	compounds := func(list string) []Compound {
		ids, _ := extraction.Fields[list].([]string)
		listed := []Compound{}
		for _, id := range ids {
			compound := Compound{ID: id}
			if item, flagged := reviews[list+":"+id]; flagged {
				compound.NeedsReview, compound.ReviewReason = true, item.Reason
			}
			listed = append(listed, compound)
		}
		return listed
	}
	result.Ligands, result.Cofactors = compounds(ListLigands), compounds(ListCofactors)
	return result
}

func parityGolden(t *testing.T) map[string]parityEntry {
	t.Helper()
	golden := map[string]parityEntry{}
	for _, name := range parityFiles {
		result := parityExtraction(t, name)
		entry := parityEntry{Compounds: []parityCompound{}}
		if result.Atoms != nil {
			entry.AtomCount = result.Atoms.AtomCount
			entry.ModeledResidues = result.Atoms.ModeledResidues
			entry.UniqueProteinChains = result.Atoms.ProteinChains
		}
		for _, group := range []struct {
			list      string
			compounds []Compound
			sources   map[string]CompoundSource
		}{
			{ListLigands, result.Ligands, result.CompoundSources.Ligands},
			{ListCofactors, result.Cofactors, result.CompoundSources.Cofactors},
		} {
			require.Len(t, group.sources, len(group.compounds), "%s: every %s entry has one source", name, group.list)
			for _, compound := range group.compounds {
				source, ok := group.sources[compound.ID]
				require.True(t, ok, "%s: %s %s has no source", name, group.list, compound.ID)
				entry.Compounds = append(entry.Compounds, parityCompound{
					List: group.list, ID: compound.ID, Rule: source.Rule,
					Review: compound.NeedsReview, Reason: compound.ReviewReason,
				})
			}
		}
		for id, source := range result.CompoundSources.Excluded {
			entry.Compounds = append(entry.Compounds, parityCompound{List: parityExcluded, ID: id, Rule: source.Rule})
		}
		for _, sources := range []map[string]CompoundSource{result.CompoundSources.Ligands, result.CompoundSources.Cofactors, result.CompoundSources.Excluded} {
			for id, source := range sources {
				require.Equal(t, result.ReferenceVersion, source.ListVersion, "%s: %s", name, id)
			}
		}
		if len(result.Glycans) > 0 {
			entry.Glycans = result.Glycans
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
