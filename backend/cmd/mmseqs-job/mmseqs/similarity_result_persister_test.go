package mmseqs

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_score_by_identity_and_coverage_and_keep_mmseqs_metadata_when_hits_converted_to_similarities(t *testing.T) {
	// given
	runID := uuid.New()
	firstSourceID := uuid.New()
	firstTargetID := uuid.New()
	secondSourceID := uuid.New()
	secondTargetID := uuid.New()
	hits := []Hit{
		{
			QuerySequenceID:  firstSourceID,
			TargetSequenceID: firstTargetID,
			Fident:           0.91,
			Qcov:             0.8,
			Tcov:             0.7,
			Evalue:           1e-20,
			Bits:             50,
			AlignmentLength:  18,
			Qstart:           2,
			Qend:             19,
			Tstart:           4,
			Tend:             21,
			Qaln:             "ACDE",
			Taln:             "ACDF",
		},
		{
			QuerySequenceID:  secondSourceID,
			TargetSequenceID: secondTargetID,
			Fident:           0.95,
			Qcov:             1,
			Tcov:             1,
			Evalue:           1e-30,
			Bits:             100,
			AlignmentLength:  20,
			Qstart:           1,
			Qend:             20,
			Tstart:           1,
			Tend:             20,
			Qaln:             "QRST",
			Taln:             "QRSS",
		},
	}

	// when
	similarities := similaritiesFromHits(runID, hits)

	// then
	require.Len(t, similarities, 2)
	assert.Equal(t, runID, similarities[0].RunID)
	assert.Equal(t, firstSourceID, similarities[0].SourceSequenceID)
	assert.Equal(t, firstTargetID, similarities[0].SimilarSequenceID)
	assert.Equal(t, "mmseqs2", similarities[0].Tool)
	assert.InDelta(t, 0.637, similarities[0].Score, 0.0000001)
	assert.Equal(t, 0.91, similarities[0].Metadata["fident"])
	assert.Equal(t, 0.8, similarities[0].Metadata["qcov"])
	assert.Equal(t, 0.7, similarities[0].Metadata["tcov"])
	assert.Equal(t, 1e-20, similarities[0].Metadata["evalue"])
	assert.Equal(t, 50.0, similarities[0].Metadata["bits"])
	assert.Equal(t, 18, similarities[0].Metadata["alignment_length"])
	assert.Equal(t, 2, similarities[0].Metadata["qstart"])
	assert.Equal(t, 19, similarities[0].Metadata["qend"])
	assert.Equal(t, 4, similarities[0].Metadata["tstart"])
	assert.Equal(t, 21, similarities[0].Metadata["tend"])
	assert.Equal(t, "ACDE", similarities[0].Metadata["qaln"])
	assert.Equal(t, "ACDF", similarities[0].Metadata["taln"])
	assert.Equal(t, "fident_min_coverage", similarities[0].Metadata["score_source"])
	assert.NotContains(t, similarities[0].Metadata, "score_normalization")
	assert.NotContains(t, similarities[0].Metadata, "max_bits_in_run")

	assert.Equal(t, 0.95, similarities[1].Score)
	assert.NotZero(t, similarities[0].ID)
	assert.False(t, similarities[0].CreatedAt.IsZero())
}
