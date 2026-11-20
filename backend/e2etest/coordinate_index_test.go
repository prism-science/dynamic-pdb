package e2etest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func coordinateIndexStatusForTest(t *testing.T, artifactID uuid.UUID) models.CoordinateIndexStatus {
	t.Helper()
	set, err := testDatabase.CoordinateIndex.Get(context.Background(), artifactID)
	require.NoError(t, err)
	return set.Status
}

func Test_should_queue_the_model_files_of_an_entry_again_only_when_activation_changes_its_fasta(t *testing.T) {
	// given: a model whose file in our bucket the coordinate-index job has read.
	token := issueEntryTokenForGitHubIDForTest(t, "coordinate-index-fasta-owner", 8816)
	artifactID := uuid.New()
	created := createDepositedModelWithFileForTest(t, token, artifactID, storedModelURI(artifactID))
	require.Equal(t, models.CoordinateIndexStatusPending, coordinateIndexStatusForTest(t, artifactID), "the API queues the upload")
	require.NoError(t, saveChainsForTest(artifactID, models.IndexedChain{
		AuthAsymID: "A", ModeledResidues: 3, Protein: true, ProteinSource: models.ProteinSourceResidueMajority,
	}))

	// when: an entry revision that only changes the title is activated.
	renamed := createEntryRevisionForTest(t, token, created.EntryId, map[string]any{
		"entry": map[string]any{"title": "renamed coordinate index entry"},
	})
	activateEntryRevisionForTest(t, renamed)

	// then: the file keeps its read.
	assert.Equal(t, models.CoordinateIndexStatusDone, coordinateIndexStatusForTest(t, artifactID))

	// when: another model of the entry, with a FASTA file, is activated: its records become the
	// entry's.
	createAndActivateModelForTest(t, token, created.EntryId, map[string]any{
		"model": map[string]any{
			"title":    "model with sequences",
			"metadata": map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{
				artifactRequest(uuid.New(), "model FASTA", "L2", "fasta", "s3://model/chains.fasta", map[string]any{
					"records": []map[string]any{{"header": "1ABC_1|Chain A|protein", "sequence": "MACDEFGHIK"}},
				}),
			},
		},
	})

	// then: the file is queued again, so chain A can take the record's length.
	assert.Equal(t, models.CoordinateIndexStatusPending, coordinateIndexStatusForTest(t, artifactID))
}
