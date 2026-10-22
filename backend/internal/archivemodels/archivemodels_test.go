package archivemodels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
)

func Test_should_accept_archive_metric_keys_when_validated_as_model_metric_keys(t *testing.T) {
	// given
	archiveKeys := []string{
		archive.MetricKeyRFree,
		archive.MetricKeyRWork,
		archive.MetricKeyClashscore,
		archive.MetricKeyRamachandranOutliers,
	}

	for _, archiveKey := range archiveKeys {
		// when
		err := models.MetricKey(archiveKey).Validate()

		// then
		assert.NoError(t, err, archiveKey)
	}
}

func Test_should_accept_archive_sources_when_validated_as_model_sources(t *testing.T) {
	// given
	sources := []archive.UniProtSource{
		archive.UniProtSourceSIFTS,
		archive.UniProtSourceStructRef,
		archive.UniProtSourceAlignment,
	}

	for _, source := range sources {
		// when
		err := models.UniProtReferenceSource(source).Validate()

		// then
		assert.NoError(t, err, string(source))
	}
}

func Test_should_match_model_methods_when_archive_methods_are_compared(t *testing.T) {
	// given
	archiveMethods := []string{archive.MethodXRayCrystallography, archive.MethodCryoEM}

	// when
	modelMethods := []string{string(models.StructureMethodXRayCrystallography), string(models.StructureMethodCryoEM)}

	// then
	assert.Equal(t, modelMethods, archiveMethods)
}

func Test_should_leave_alignment_mappings_out_when_not_requested(t *testing.T) {
	// given
	mapping := archive.PolymerEntityMapping{
		UniProtMappings: []archive.UniProtMapping{{Accession: "P1", Source: archive.UniProtSourceStructRef}},
		AlignmentMappings: []archive.UniProtMapping{
			{Accession: "Q2", Source: archive.UniProtSourceAlignment, UniProtRelease: new("2026_03")},
		},
	}

	// when
	withoutAlignments := PolymerEntityMetadata(mapping, false)
	withAlignments := PolymerEntityMetadata(mapping, true)

	// then
	require.Len(t, withoutAlignments.UniProtMappings, 1)
	require.Len(t, withAlignments.UniProtMappings, 2)
	assert.Equal(t, models.UniProtReferenceSourceAlignment, withAlignments.UniProtMappings[1].Source)
}
