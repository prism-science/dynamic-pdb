package cif

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ccdTypeGoldenPath = "testdata/ccd-type-categories.json"

// Test_should_match_the_committed_ccd_type_categories_when_every_known_type_is_sorted
// pins the category of every CCD type. The website test checks its own
// ccdTypeCategory switch against the same file, so the two switches cannot
// drift apart. Run with UPDATE_PARITY_GOLDEN=1 to rewrite it after a deliberate
// change.
func Test_should_match_the_committed_ccd_type_categories_when_every_known_type_is_sorted(t *testing.T) {
	// given
	categories := map[string]CCDCategory{}
	for _, ccdType := range KnownCCDTypes {
		category, err := CCDTypeCategory(ccdType)
		require.NoError(t, err, ccdType)
		categories[ccdType] = category
	}
	encoded, err := json.MarshalIndent(categories, "", "  ")
	require.NoError(t, err)
	encoded = append(encoded, '\n')

	// when
	if os.Getenv("UPDATE_PARITY_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(filepath.FromSlash(ccdTypeGoldenPath), encoded, 0o644))
	}
	committed, err := os.ReadFile(filepath.FromSlash(ccdTypeGoldenPath))

	// then
	require.NoError(t, err)
	assert.JSONEq(t, string(encoded), string(committed), "run UPDATE_PARITY_GOLDEN=1 go test ./cif -run ccd_type_categories to refresh it")
}
