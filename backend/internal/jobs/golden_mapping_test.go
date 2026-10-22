package jobs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
)

const (
	fixtureDirectory       = "../../../lib/archive/testdata"
	updateGoldenEnvVarName = "UPDATE_GOLDEN"
)

type goldenMapping struct {
	Entry         models.EntryRevision          `json:"entry"`
	Model         models.ModelRevision          `json:"model"`
	Metrics       []models.Metric               `json:"metrics"`
	PolymerEntity *models.PolymerEntityMetadata `json:"polymer_entity,omitempty"`
	ResidueData   []models.ResidueData          `json:"residue_data,omitempty"`
}

func readFixture[T any](t *testing.T, name string) T {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(fixtureDirectory, name))
	require.NoError(t, err)
	var value T
	require.NoError(t, json.Unmarshal(contents, &value))
	return value
}

func Test_should_map_recorded_rcsb_responses_to_pinned_sync_values(t *testing.T) {
	cases := []struct {
		name           string
		pdbID          string
		withPolymer    bool
		uniProtRelease *string
	}{
		{name: "xray_with_polymer_entity", pdbID: "4HHB", withPolymer: true, uniProtRelease: new("2026_03")},
		{name: "xray_with_crystal_growth", pdbID: "6LU7"},
		{name: "cryoem", pdbID: "6VXX"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// given
			details := readFixture[rcsb.EntryDetails](t, "entry_"+testCase.pdbID+".json")

			// when
			var mapping goldenMapping
			applyRCSBEntryDetails(&mapping.Entry, details)
			applyRCSBModelDetails(&mapping.Model, details)
			mapping.Metrics = modelMetricsFromRCSB(details)
			if testCase.withPolymer {
				entity := readFixture[rcsb.PolymerEntityDetails](t, "polymer_entity_"+testCase.pdbID+"_1.json")
				instance := readFixture[rcsb.PolymerEntityInstanceDetails](t, "polymer_entity_instance_"+testCase.pdbID+"_A.json")
				metadata := polymerEntityMetadataFromRCSB(entity, "1", testCase.uniProtRelease)
				mapping.PolymerEntity = &metadata
				mapping.ResidueData = residueDataFromRCSB(entity, instance, "A")
			}

			// then
			actual, err := json.MarshalIndent(mapping, "", "  ")
			require.NoError(t, err)
			goldenPath := filepath.Join(fixtureDirectory, "golden_"+testCase.name+".json")
			if os.Getenv(updateGoldenEnvVarName) != "" {
				require.NoError(t, os.WriteFile(goldenPath, append(actual, '\n'), 0o600))
			}
			expected, err := os.ReadFile(goldenPath)
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(actual))
		})
	}
}
