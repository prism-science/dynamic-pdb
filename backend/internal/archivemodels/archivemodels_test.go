package archivemodels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
)

func Test_should_use_metric_keys_from_the_metric_vocabulary(t *testing.T) {
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

func Test_should_use_uniprot_sources_from_the_source_vocabulary(t *testing.T) {
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

func Test_should_use_structure_methods_from_the_method_vocabulary(t *testing.T) {
	// given
	assert.Equal(t, string(models.StructureMethodXRayCrystallography), archive.MethodXRayCrystallography)
	assert.Equal(t, string(models.StructureMethodCryoEM), archive.MethodCryoEM)
}

func Test_should_leave_alignment_mappings_out_unless_requested(t *testing.T) {
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
