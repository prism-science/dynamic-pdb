package mmseqs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_recover_output_file_from_checkpoint_when_previous_load_interrupted(t *testing.T) {
	// given
	path := filepath.Join(t.TempDir(), "sequences.fasta")
	require.NoError(t, os.WriteFile(path, []byte("complete-prefix-stale-suffix"), 0o644))

	// when
	err := recoverOutputFromCheckpoint(path, DataLoaderCheckpoint{FileSize: int64(len("complete-prefix"))})

	// then
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "complete-prefix", string(data))
}

func Test_should_write_fasta_records_with_cleaned_wrapped_sequences_when_batch_appended(t *testing.T) {
	// given
	id := uuid.New()
	var output strings.Builder
	writer := bufio.NewWriter(&output)
	sequence := strings.Repeat("A", 81) + "\n C\tD"

	// when
	err := appendSequenceBatch(writer, []models.ProteinSequence{
		{
			ID:       id,
			Sequence: sequence,
		},
	})
	require.NoError(t, err)
	require.NoError(t, writer.Flush())

	// then
	assert.Equal(t, ">"+id.String()+"\n"+strings.Repeat("A", 80)+"\nACD\n", output.String())
}
