package mmseqs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_parse_hits_and_skip_self_matches_when_mmseqs_output_read(t *testing.T) {
	// given
	queryID := uuid.New()
	targetID := uuid.New()
	path := filepath.Join(t.TempDir(), "result.tsv")
	contents := queryID.String() + "\t" + queryID.String() + "\t1\t1\t1\t0\t200\t20\t1\t20\t1\t20\tSELF\tSELF\n" +
		queryID.String() + "\t" + targetID.String() + "\t0.9\t0.8\t0.7\t1e-20\t100\t19\t2\t20\t4\t22\tACDE\tACDF\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))

	// when
	hits, err := ParseOutput(path)

	// then
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, queryID, hits[0].QuerySequenceID)
	assert.Equal(t, targetID, hits[0].TargetSequenceID)
	assert.Equal(t, 0.9, hits[0].Fident)
	assert.Equal(t, 0.8, hits[0].Qcov)
	assert.Equal(t, 0.7, hits[0].Tcov)
	assert.Equal(t, 1e-20, hits[0].Evalue)
	assert.Equal(t, 100.0, hits[0].Bits)
	assert.Equal(t, 19, hits[0].AlignmentLength)
	assert.Equal(t, 2, hits[0].Qstart)
	assert.Equal(t, 20, hits[0].Qend)
	assert.Equal(t, 4, hits[0].Tstart)
	assert.Equal(t, 22, hits[0].Tend)
	assert.Equal(t, "ACDE", hits[0].Qaln)
	assert.Equal(t, "ACDF", hits[0].Taln)
}

func Test_should_stream_hits_and_read_max_bits_when_mmseqs_output_read(t *testing.T) {
	// given
	queryID := uuid.New()
	firstTargetID := uuid.New()
	secondTargetID := uuid.New()
	path := filepath.Join(t.TempDir(), "result.tsv")
	contents := queryID.String() + "\t" + queryID.String() + "\t1\t1\t1\t0\t999\t20\t1\t20\t1\t20\tSELF\tSELF\n" +
		queryID.String() + "\t" + firstTargetID.String() + "\t0.9\t0.8\t0.7\t1e-20\t100\t19\t2\t20\t4\t22\tACDE\tACDF\n" +
		queryID.String() + "\t" + secondTargetID.String() + "\t0.7\t0.6\t0.5\t1e-10\t250\t18\t3\t20\t5\t22\tQRST\tQRSS\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	streamedTargetIDs := make([]uuid.UUID, 0)

	// when
	err := ForEachOutputHit(path, func(hit Hit) error {
		streamedTargetIDs = append(streamedTargetIDs, hit.TargetSequenceID)
		return nil
	})
	maxBits, maxBitsErr := MaxOutputBits(path)

	// then
	require.NoError(t, err)
	require.NoError(t, maxBitsErr)
	assert.Equal(t, []uuid.UUID{firstTargetID, secondTargetID}, streamedTargetIDs)
	assert.Equal(t, 250.0, maxBits)
}

func Test_should_return_error_when_mmseqs_output_has_unexpected_field_count(t *testing.T) {
	// given
	path := filepath.Join(t.TempDir(), "result.tsv")
	require.NoError(t, os.WriteFile(path, []byte("too\tshort\n"), 0o644))

	// when
	_, err := ParseOutput(path)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 14 fields")
}
