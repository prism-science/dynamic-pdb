package httpapi

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

// The website builds its dropdowns from the openapi.yaml enums, and the API checks values
// against the domain lists. Both must hold the same values in the same order. The test
// reads the file, not GetSwagger: the embedded spec leaves out schemas no operation
// references, such as StructureMethod.
func Test_should_list_the_openapi_enum_values_when_domain_vocabularies_are_read(t *testing.T) {
	// given
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	require.NoError(t, err)
	vocabularies := map[string][]string{
		"StructureMethod":      stringsOf(domainmodels.StructureMethods),
		"ModelPurpose":         stringsOf(domainmodels.ModelPurposes),
		"StructureModelType":   stringsOf(domainmodels.StructureModelTypes),
		"MetricKey":            stringsOf(domainmodels.MetricKeys),
		"ArtifactLevel":        stringsOf(domainmodels.ArtifactLevels),
		"ArtifactType":         stringsOf(domainmodels.ArtifactTypes),
		"RunArtifactDirection": stringsOf(domainmodels.RunArtifactDirections),
	}

	for name, domainValues := range vocabularies {
		t.Run(name, func(t *testing.T) {
			// when
			schema, found := spec.Components.Schemas[name]
			require.True(t, found, "openapi.yaml has no schema %s", name)
			require.NotNil(t, schema.Value)
			specValues := make([]string, 0, len(schema.Value.Enum))
			for _, value := range schema.Value.Enum {
				text, isText := value.(string)
				require.True(t, isText, "%s enum value %v is not a string", name, value)
				specValues = append(specValues, text)
			}

			// then
			assert.Equal(t, specValues, domainValues)
		})
	}
}

func stringsOf[T ~string](values []T) []string {
	texts := make([]string, 0, len(values))
	for _, value := range values {
		texts = append(texts, string(value))
	}
	return texts
}
