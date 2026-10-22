package httpapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testArtifactSource = "file:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func readCIFFixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "lib", "cif", "testdata", name))
	require.NoError(t, err)
	return contents
}

func extractedValueByName(values []ExtractedValue, name string) (ExtractedValue, bool) {
	for _, value := range values {
		if value.Name == name {
			return value, true
		}
	}
	return ExtractedValue{}, false
}

func Test_should_tag_every_value_with_file_source_when_extracting_mmcif(t *testing.T) {
	// given
	data := readCIFFixture(t, "6OIM.cif")

	// when
	attributes, err := buildArtifactExtraction(data, int64(len(data)), testArtifactSource)

	// then
	require.NoError(t, err)
	assert.Equal(t, testArtifactSource, attributes.Source)
	assert.False(t, attributes.Truncated)
	require.NotEmpty(t, attributes.Values)
	for _, value := range attributes.Values {
		assert.Equal(t, testArtifactSource, value.Source, value.Name)
	}
	atoms, ok := extractedValueByName(attributes.Values, "atom_count")
	require.True(t, ok)
	assert.Equal(t, 134, atoms.Value)
	cofactors, ok := extractedValueByName(attributes.Values, "cofactors")
	require.True(t, ok)
	assert.False(t, cofactors.NeedsReview)
	mutations, ok := extractedValueByName(attributes.Values, "mutations")
	require.True(t, ok)
	assert.False(t, mutations.NeedsReview)
	assert.Empty(t, attributes.Warnings)
}

func Test_should_flag_value_for_review_when_metal_has_no_metalc_row(t *testing.T) {
	// given
	data := []byte("data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 ZN\n")

	// when
	attributes, err := buildArtifactExtraction(data, int64(len(data)), testArtifactSource)

	// then
	require.NoError(t, err)
	cofactors, ok := extractedValueByName(attributes.Values, "cofactors")
	require.True(t, ok)
	assert.True(t, cofactors.NeedsReview)
	require.NotNil(t, cofactors.ReviewReason)
}

func Test_should_warn_instead_of_fail_when_head_is_truncated_inside_coordinates(t *testing.T) {
	// given
	full := readCIFFixture(t, "6OIM.cif")
	head := full[:len(full)-len(full)/4]

	// when
	attributes, err := buildArtifactExtraction(head, int64(len(full)), testArtifactSource)

	// then
	require.NoError(t, err)
	assert.True(t, attributes.Truncated)
	_, hasAtoms := extractedValueByName(attributes.Values, "atom_count")
	assert.False(t, hasAtoms)
	require.NotEmpty(t, attributes.Warnings)
	assert.Contains(t, attributes.Warnings[0].Detail, "atom_site")
	_, hasMutations := extractedValueByName(attributes.Values, "mutations")
	assert.True(t, hasMutations)
}

func Test_should_warn_when_file_has_no_coordinates(t *testing.T) {
	// given
	data := []byte("data_X\n_struct.title test\n")

	// when
	attributes, err := buildArtifactExtraction(data, int64(len(data)), testArtifactSource)

	// then
	require.NoError(t, err)
	require.Len(t, attributes.Warnings, 1)
	assert.Contains(t, attributes.Warnings[0].Detail, "atom_site")
}

func Test_should_drop_value_and_warn_when_it_breaks_schema_rules(t *testing.T) {
	// given
	values := []ExtractedValue{
		{Name: "atom_count", Value: -4, Source: testArtifactSource},
		{Name: "altloc_fraction", Value: 0.25, Source: testArtifactSource},
	}
	atoms := atomValues{atomCount: -4, modeledResidues: 10, proteinChains: 1, altlocFraction: 0.25}

	// when
	kept, warnings, err := applySchemaRules(values, atoms)

	// then
	require.NoError(t, err)
	require.Len(t, kept, 1)
	assert.Equal(t, "altloc_fraction", kept[0].Name)
	require.Len(t, warnings, 1)
	require.NotNil(t, warnings[0].Pointer)
	assert.Equal(t, "/atom_count", *warnings[0].Pointer)
}
