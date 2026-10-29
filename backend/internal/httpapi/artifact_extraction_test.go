package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/lib/cif"
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
	attributes, err := buildArtifactExtraction(context.Background(), data, int64(len(data)), testArtifactSource)

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
	assert.Empty(t, warningDetails(attributes), "the approved reference lists add no warning")
	reference, err := cif.DefaultReference()
	require.NoError(t, err)
	assert.Equal(t, reference.Version, attributes.ReferenceVersion)
}

func warningDetails(attributes ArtifactExtractionAttributes) []string {
	details := make([]string, 0, len(attributes.Warnings))
	for _, warning := range attributes.Warnings {
		details = append(details, warning.Detail)
	}
	return details
}

func Test_should_flag_value_for_review_when_metal_has_no_metalc_row(t *testing.T) {
	// given
	data := []byte("data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 ZN\n")

	// when
	attributes, err := buildArtifactExtraction(context.Background(), data, int64(len(data)), testArtifactSource)

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
	attributes, err := buildArtifactExtraction(context.Background(), head, int64(len(full)), testArtifactSource)

	// then
	require.NoError(t, err)
	assert.True(t, attributes.Truncated)
	_, hasAtoms := extractedValueByName(attributes.Values, "atom_count")
	assert.False(t, hasAtoms)
	assert.Contains(t, strings.Join(warningDetails(attributes), "\n"), "atom_site")
	assert.NotContains(t, warningDetails(attributes), cif.DraftWarning)
	_, hasMutations := extractedValueByName(attributes.Values, "mutations")
	assert.True(t, hasMutations)
}

func Test_should_warn_when_file_has_no_coordinates(t *testing.T) {
	// given
	data := []byte("data_X\n_struct.title test\n")

	// when
	attributes, err := buildArtifactExtraction(context.Background(), data, int64(len(data)), testArtifactSource)

	// then
	require.NoError(t, err)
	require.Len(t, attributes.Warnings, 1)
	assert.Contains(t, attributes.Warnings[0].Detail, "no atom_site records found")
}

func Test_should_keep_sulfate_flagged_and_drop_water_when_reference_lists_are_approved(t *testing.T) {
	// given
	data := []byte("data_X\nloop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.comp_id\n2 SO4\n3 HOH\n")

	// when
	attributes, err := buildArtifactExtraction(context.Background(), data, int64(len(data)), testArtifactSource)

	// then
	require.NoError(t, err)
	ligands, ok := extractedValueByName(attributes.Values, "ligands")
	require.True(t, ok)
	assert.Equal(t, []cif.Compound{{
		ID:           "SO4",
		NeedsReview:  true,
		ReviewReason: "often from the buffer: confirm it is part of the sample",
	}}, ligands.Value)
	assert.True(t, ligands.NeedsReview)
	assert.NotContains(t, warningDetails(attributes), cif.DraftWarning)
}

func Test_should_return_the_rule_behind_each_compound_when_extracting_mmcif(t *testing.T) {
	// given: 3CEB has LLP (PLP on a lysine), sulfate and glycerol.
	data := readCIFFixture(t, "3CEB.cif")
	reference, err := cif.DefaultReference()
	require.NoError(t, err)
	record := func(rule string) cif.CompoundSource {
		return cif.CompoundSource{Rule: rule, ListVersion: reference.Version}
	}

	// when
	attributes, err := buildArtifactExtraction(context.Background(), data, int64(len(data)), testArtifactSource)

	// then
	require.NoError(t, err)
	sources, ok := extractedValueByName(attributes.Values, "compound_sources")
	require.True(t, ok)
	assert.Equal(t, testArtifactSource, sources.Source)
	assert.False(t, sources.NeedsReview)
	assert.Equal(t, cif.CompoundSources{
		Ligands:   map[string]cif.CompoundSource{"SO4": record("list")},
		Cofactors: map[string]cif.CompoundSource{"PLP": record("covalent_map")},
		Excluded:  map[string]cif.CompoundSource{"GOL": record("list")},
	}, sources.Value)
	_, hasGlycans := extractedValueByName(attributes.Values, "glycan_descriptors")
	assert.False(t, hasGlycans, "a file without branched entities has no glycan descriptors")
}

func Test_should_return_glycan_descriptors_when_file_has_a_branched_entity(t *testing.T) {
	// given
	data := readCIFFixture(t, "7LTB.cif")

	// when
	attributes, err := buildArtifactExtraction(context.Background(), data, int64(len(data)), testArtifactSource)

	// then
	require.NoError(t, err)
	glycans, ok := extractedValueByName(attributes.Values, "glycan_descriptors")
	require.True(t, ok)
	assert.Equal(t, []cif.Glycan{{
		EntityID:    "2",
		CompoundIDs: []string{"BGC", "Z6G"},
		Descriptors: []cif.GlycanDescriptor{{
			Descriptor:     "WURCS=2.0/2,2,1/[a2122h-1b_1-5][ad211m-1a_1-5_3*N]/1-2/a2-b1",
			Type:           "WURCS",
			Program:        "PDB2Glycan",
			ProgramVersion: "1.1.0",
		}},
	}}, glycans.Value)
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
