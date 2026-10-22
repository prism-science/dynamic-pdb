package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type vocabularyCase struct {
	name     string
	valid    []func() error
	unknown  func() error
	expected string
}

func vocabularyCases() []vocabularyCase {
	return []vocabularyCase{
		{
			name: "model purpose",
			valid: []func() error{
				ModelPurposeModelBuilding.Validate, ModelPurposeRefinement.Validate, ModelPurposeReference.Validate,
			},
			unknown:  ModelPurpose("Validation").Validate,
			expected: "unknown model purpose: Validation",
		},
		{
			name: "model type",
			valid: []func() error{
				StructureModelTypeSingleConformer.Validate, StructureModelTypeMulticonformer.Validate,
				StructureModelTypeEnsemble.Validate, StructureModelTypeDeposited.Validate,
			},
			unknown:  StructureModelType("Predicted").Validate,
			expected: "unknown model type: Predicted",
		},
		{
			name:     "model source",
			valid:    []func() error{ModelSourcePDB.Validate, ModelSourceEXT.Validate},
			unknown:  ModelSource("emdb").Validate,
			expected: "unknown model source: emdb",
		},
		{
			name: "metric key",
			valid: []func() error{
				MetricKeyRWork.Validate, MetricKeyRFree.Validate, MetricKeyCC.Validate, MetricKeyRSCC.Validate,
				MetricKeyClashscore.Validate, MetricKeyRamachandranOutliers.Validate,
				MetricKeySideChainOutliers.Validate, MetricKeyMolProbityScore.Validate,
				MetricKeyWilsonB.Validate, MetricKeyRMSF.Validate,
			},
			unknown:  MetricKey("r_factor").Validate,
			expected: "unknown metric key: r_factor",
		},
		{
			name: "artifact level",
			valid: []func() error{
				ArtifactLevelL0.Validate, ArtifactLevelL1.Validate, ArtifactLevelL2.Validate, ArtifactLevelL3.Validate,
			},
			unknown:  ArtifactLevel("L4").Validate,
			expected: "unknown artifact level: L4",
		},
		{
			name: "artifact type",
			valid: []func() error{
				ArtifactTypeModel.Validate, ArtifactTypeStructureFactors.Validate,
				ArtifactTypeFASTA.Validate, ArtifactTypeOther.Validate,
			},
			unknown:  ArtifactType("assembly").Validate,
			expected: "unknown artifact type: assembly",
		},
		{
			name:     "run artifact direction",
			valid:    []func() error{RunArtifactDirectionInput.Validate, RunArtifactDirectionOutput.Validate},
			unknown:  RunArtifactDirection("inout").Validate,
			expected: "unknown run artifact direction: inout",
		},
		{
			name: "revision state",
			valid: []func() error{
				RevisionStatePending.Validate, RevisionStateInReview.Validate, RevisionStateActive.Validate,
				RevisionStateRejected.Validate, RevisionStateArchived.Validate,
			},
			unknown:  RevisionState("draft").Validate,
			expected: "unknown revision state: draft",
		},
		{
			name:     "entry state",
			valid:    []func() error{EntryStateNew.Validate, EntryStateActive.Validate, EntryStateDeleted.Validate},
			unknown:  EntryState("archived").Validate,
			expected: "unknown entry state: archived",
		},
		{
			name:     "model state",
			valid:    []func() error{ModelStateNew.Validate, ModelStateActive.Validate, ModelStateDeleted.Validate},
			unknown:  ModelState("archived").Validate,
			expected: "unknown model state: archived",
		},
		{
			name: "UniProt mapping source",
			valid: []func() error{
				UniProtReferenceSourceSIFTS.Validate, UniProtReferenceSourceStructRef.Validate,
				UniProtReferenceSourceAlignment.Validate,
			},
			unknown:  UniProtReferenceSource("rcsb").Validate,
			expected: "unknown UniProt mapping source: rcsb",
		},
	}
}

func Test_should_accept_every_schema_value_when_vocabulary_validated(t *testing.T) {
	for _, vocabulary := range vocabularyCases() {
		t.Run(vocabulary.name, func(t *testing.T) {
			// given
			validators := vocabulary.valid

			// when
			var errs []error
			for _, validate := range validators {
				errs = append(errs, validate())
			}

			// then
			for _, err := range errs {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_should_reject_value_when_not_in_vocabulary(t *testing.T) {
	for _, vocabulary := range vocabularyCases() {
		t.Run(vocabulary.name, func(t *testing.T) {
			// given
			validate := vocabulary.unknown

			// when
			err := validate()

			// then
			require.EqualError(t, err, vocabulary.expected)
		})
	}
}

func Test_should_require_uniprot_release_when_source_is_sifts_or_alignment(t *testing.T) {
	tests := []struct {
		source   UniProtReferenceSource
		required bool
	}{
		{UniProtReferenceSourceSIFTS, true},
		{UniProtReferenceSourceAlignment, true},
		{UniProtReferenceSourceStructRef, false},
	}
	for _, test := range tests {
		t.Run(string(test.source), func(t *testing.T) {
			// given
			source := test.source

			// when
			required, err := source.RequiresRelease()

			// then
			require.NoError(t, err)
			assert.Equal(t, test.required, required)
		})
	}
}
