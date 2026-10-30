package jobs

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/lib/cif"
	"dynamic-pdb/lib/rcsb"
)

const mmCIFWithMutationAndSulfate = mmCIFWithMutation +
	"loop_\n_pdbx_entity_nonpoly.entity_id\n_pdbx_entity_nonpoly.name\n_pdbx_entity_nonpoly.comp_id\n2 'SULFATE ION' SO4\n" +
	"#\n"

func Test_should_read_mutations_and_composition_when_rcsb_mmcif_is_complete(t *testing.T) {
	// given
	strategy, _ := entryStrategyServing(t, mmCIFWithMutationAndSulfate)

	// when
	file, err := readArchiveFile(context.Background(), strategy.rcsbClient, strategy.logger, "1ABC")

	// then
	require.NoError(t, err)
	assert.True(t, file.mutations.complete)
	require.Len(t, file.mutations.sets, 1)
	require.NotNil(t, file.composition)
	assert.Equal(t, []string{"SO4"}, file.composition.ligands)
	assert.NotEmpty(t, file.composition.sources.Ligands["SO4"].ReviewReason)
	assert.NotEmpty(t, file.referenceVersion)
	assert.Empty(t, file.incompleteReason)
}

func Test_should_give_no_composition_and_say_why_when_rcsb_mmcif_ends_before_coordinates(t *testing.T) {
	// given
	strategy, logs := entryStrategyServing(t,
		mmCIFWithMutationAndSulfate+"#"+string(bytes.Repeat([]byte(" "), int(cif.HeadReadLimit))))

	// when
	file, err := readArchiveFile(context.Background(), strategy.rcsbClient, strategy.logger, "1ABC")

	// then
	require.NoError(t, err)
	assert.Nil(t, file.composition)
	assert.False(t, file.mutations.complete)
	assert.Equal(t, "the head of the RCSB mmCIF ends before its coordinates", file.incompleteReason)
	assert.Contains(t, logs.String(), "level=WARN")
}

func Test_should_return_not_found_when_rcsb_serves_no_mmcif_file(t *testing.T) {
	// given
	strategy, _ := entryStrategyServing(t, mmCIFWithMutation)

	// when
	_, err := readArchiveFile(context.Background(), strategy.rcsbClient, strategy.logger, "2XYZ")

	// then
	require.ErrorIs(t, err, rcsb.ErrNotFound)
}
