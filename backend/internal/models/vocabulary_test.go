package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_accept_every_schema_value_when_vocabularies_are_validated(t *testing.T) {
	// given
	validators := map[string][]interface{ Validate() error }{
		"model purpose":      valuesOf(ModelPurposes),
		"model type":         valuesOf(StructureModelTypes),
		"metric key":         valuesOf(MetricKeys),
		"artifact level":     valuesOf(ArtifactLevels),
		"artifact type":      valuesOf(ArtifactTypes),
		"run direction":      valuesOf(RunArtifactDirections),
		"UniProt source":     valuesOf(UniProtReferenceSources),
		"model external ref": valuesOf(ModelSources),
		"entry external ref": valuesOf(EntrySources),
	}

	for name, values := range validators {
		t.Run(name, func(t *testing.T) {
			require.NotEmpty(t, values)
			for _, value := range values {
				// when
				err := value.Validate()

				// then
				assert.NoError(t, err, "%v", value)
			}
		})
	}
}

func Test_should_list_schema_v0_2_0_values_when_vocabularies_are_read(t *testing.T) {
	// given
	// when
	// then
	assert.ElementsMatch(t, []ModelPurpose{"Model Building", "Refinement", "Reference"}, ModelPurposes)
	assert.ElementsMatch(t,
		[]StructureModelType{"Single Conformer", "Multiconformer", "Ensemble", "Deposited"},
		StructureModelTypes,
	)
	assert.ElementsMatch(t, []MetricKey{
		"r_work", "r_free", "cc", "rscc", "clashscore", "ramachandran_outliers",
		"side_chain_outliers", "molprobity_score", "wilson_b", "rmsf",
	}, MetricKeys)
	assert.ElementsMatch(t, []ArtifactLevel{"L0", "L1", "L2", "L3"}, ArtifactLevels)
	assert.ElementsMatch(t, []ArtifactType{"model", "structure_factors", "fasta", "other"}, ArtifactTypes)
	assert.ElementsMatch(t, []RunArtifactDirection{"input", "output"}, RunArtifactDirections)
	assert.ElementsMatch(t,
		[]UniProtReferenceSource{"sifts", "struct_ref", "alignment"},
		UniProtReferenceSources,
	)
	assert.ElementsMatch(t, []ModelSource{"pdb", "ext"}, ModelSources)
	assert.ElementsMatch(t, []EntrySource{"pdb"}, EntrySources)
}

func Test_should_name_allowed_values_when_vocabulary_value_is_unknown(t *testing.T) {
	// given
	purpose := ModelPurpose("Docking")

	// when
	err := purpose.Validate()

	// then
	require.Error(t, err)
	assert.Equal(t,
		`model purpose "Docking" is not one of "Model Building", "Refinement", "Reference"`,
		err.Error(),
	)
}

func Test_should_reject_unknown_value_when_any_vocabulary_is_validated(t *testing.T) {
	// given
	unknownValues := map[string]interface{ Validate() error }{
		"model purpose":      ModelPurpose("Docking"),
		"model type":         StructureModelType("Trajectory"),
		"metric key":         MetricKey("resolution"),
		"artifact level":     ArtifactLevel("L4"),
		"artifact type":      ArtifactType("assembly"),
		"run direction":      RunArtifactDirection("both"),
		"UniProt source":     UniProtReferenceSource("uniprot"),
		"model external ref": ModelSource("emdb"),
		"entry external ref": EntrySource("emdb"),
	}

	for name, value := range unknownValues {
		t.Run(name, func(t *testing.T) {
			// when
			err := value.Validate()

			// then
			assert.Error(t, err)
		})
	}
}

func Test_should_reject_empty_value_when_metric_key_is_validated(t *testing.T) {
	// given
	key := MetricKey("")

	// when
	err := key.Validate()

	// then
	assert.EqualError(t, err, `metric key "" is not one of "r_work", "r_free", "cc", "rscc", "clashscore", `+
		`"ramachandran_outliers", "side_chain_outliers", "molprobity_score", "wilson_b", "rmsf"`)
}

func valuesOf[T interface{ Validate() error }](values []T) []interface{ Validate() error } {
	result := make([]interface{ Validate() error }, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
