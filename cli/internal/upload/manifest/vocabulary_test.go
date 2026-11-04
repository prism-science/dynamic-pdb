package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// openAPIPath is the backend's API description, from this package's directory.
var openAPIPath = filepath.Join("..", "..", "..", "..", "backend", "api", "openapi.yaml")

func Test_should_match_openapi_enums_when_cli_checks_closed_vocabularies(t *testing.T) {
	// given
	contents, err := os.ReadFile(openAPIPath)
	require.NoError(t, err)
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Enum []string `yaml:"enum"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &document))
	vocabularies := map[string][]string{
		"MetricKey":          MetricKeys,
		"ArtifactLevel":      ArtifactLevels,
		"ArtifactType":       ArtifactTypes,
		"StructureModelType": ModelTypes,
		"ModelPurpose":       ModelPurposes,
	}

	for schema, cliValues := range vocabularies {
		// when
		apiValues := document.Components.Schemas[schema].Enum

		// then
		require.NotEmpty(t, apiValues, "openapi.yaml has no enum for %s", schema)
		assert.Equal(t, apiValues, cliValues, "CLI list for %s differs from openapi.yaml", schema)
	}
}
