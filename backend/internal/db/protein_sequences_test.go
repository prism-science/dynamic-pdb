package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_create_and_list_entry_protein_sequences_when_repository_called(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "protein-sequences-entry", time.Now().UTC())
	artifact := createDBTestArtifact(t, entryRevision.CreatedBy, "protein sequences", time.Now().UTC())
	records := []models.FASTARecord{
		{
			Header:   "first",
			Sequence: "ACGT",
		},
		{
			Header:   "second",
			Sequence: "ACGT",
		},
	}

	// when
	err := testDB.ProteinSequences.Create(context.Background(), entryRevision.ID, artifact.ID, records)
	require.NoError(t, err)
	otherEntryRevision := createDBTestEntryRevision(t, "other-protein-sequences-entry", time.Now().UTC())
	otherArtifact := createDBTestArtifact(t, otherEntryRevision.CreatedBy, "other protein sequence", time.Now().UTC())
	require.NoError(t, testDB.ProteinSequences.Create(
		context.Background(),
		otherEntryRevision.ID,
		otherArtifact.ID,
		[]models.FASTARecord{{Header: "other", Sequence: "TTTT"}},
	))
	sequences, err := testDB.ProteinSequences.List(context.Background(), db.ProteinSequenceFilters{
		EntryRevisionID: &entryRevision.ID,
	})
	require.NoError(t, err)
	require.Len(t, sequences, 2)
	pendingState := models.ProteinSequenceProcessingStatePending
	limit := 10
	pendingSequences, pendingErr := testDB.ProteinSequences.List(context.Background(), db.ProteinSequenceFilters{
		ProcessingState: &pendingState,
		Limit:           &limit,
	})
	require.NoError(t, pendingErr)
	require.NotEmpty(t, pendingSequences)
	require.NoError(t, testDB.ProteinSequences.UpdateProcessingState(
		context.Background(),
		[]uuid.UUID{sequences[0].ID},
		models.ProteinSequenceProcessingStateProcessed,
	))
	updatedSequences, err := testDB.ProteinSequences.List(context.Background(), db.ProteinSequenceFilters{
		EntryRevisionID: &entryRevision.ID,
	})

	// then
	require.NoError(t, err)
	require.Len(t, updatedSequences, 2)
	assert.Equal(t, entryRevision.ID, sequences[0].EntryRevisionID)
	assert.Equal(t, artifact.ID, sequences[0].SourceArtifactID)
	assert.Equal(t, 0, sequences[0].RecordIndex)
	assert.Equal(t, "first", sequences[0].Header)
	assert.Equal(t, "ACGT", sequences[0].Sequence)
	assert.Equal(t, models.ProteinSequenceProcessingStatePending, sequences[0].ProcessingState)
	assert.Equal(t, entryRevision.ID, sequences[1].EntryRevisionID)
	assert.Equal(t, artifact.ID, sequences[1].SourceArtifactID)
	assert.Equal(t, 1, sequences[1].RecordIndex)
	assert.Equal(t, "second", sequences[1].Header)
	assert.Equal(t, "ACGT", sequences[1].Sequence)
	assert.Equal(t, models.ProteinSequenceProcessingStatePending, sequences[1].ProcessingState)
	assert.Equal(t, models.ProteinSequenceProcessingStateProcessed, updatedSequences[0].ProcessingState)
	assert.Equal(t, models.ProteinSequenceProcessingStatePending, updatedSequences[1].ProcessingState)
	assert.NotEqual(t, sequences[0].ID, sequences[1].ID)
	assert.NotEqual(t, uuid.Nil, sequences[0].ID)
	assert.NotEqual(t, uuid.Nil, sequences[1].ID)
}
