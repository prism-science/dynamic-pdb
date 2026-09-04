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

func Test_should_preserve_protein_sequence_ids_when_artifact_moved_to_new_revision(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	createdBy := createDBTestUser(t)
	entryID := "entry-" + uuid.NewString()
	activeRevision, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID: uuid.New(), EntryID: entryID, State: models.RevisionStateActive,
		EntryState: models.EntryStateActive, Name: "active protein revision", CreatedBy: createdBy,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	targetRevision, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID: uuid.New(), EntryID: entryID, ParentRevisionID: new(activeRevision.ID),
		State: models.RevisionStatePending, EntryState: models.EntryStateActive,
		Name: "target protein revision", CreatedBy: createdBy,
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	})
	require.NoError(t, err)
	artifact := createDBTestArtifact(t, createdBy, "retained protein artifact", now)
	require.NoError(t, testDB.ProteinSequences.Create(ctx, activeRevision.ID, artifact.ID, []models.FASTARecord{
		{Header: "retained", Sequence: "ACDEFGHIK"},
	}))
	before, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &activeRevision.ID,
	})
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.NoError(t, testDB.ProteinSequences.UpdateProcessingState(
		ctx,
		[]uuid.UUID{before[0].ID},
		models.ProteinSequenceProcessingStateProcessed,
	))

	// when
	err = testDB.ProteinSequences.MoveEntryRevisionArtifacts(
		ctx,
		activeRevision.ID,
		targetRevision.ID,
		[]uuid.UUID{artifact.ID},
	)

	// then
	require.NoError(t, err)
	oldSequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: new(activeRevision.ID),
	})
	require.NoError(t, err)
	assert.Empty(t, oldSequences)
	movedSequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: new(targetRevision.ID),
	})
	require.NoError(t, err)
	require.Len(t, movedSequences, 1)
	assert.Equal(t, before[0].ID, movedSequences[0].ID)
	assert.Equal(t, models.ProteinSequenceProcessingStateProcessed, movedSequences[0].ProcessingState)
}

func Test_should_move_all_protein_sequences_to_new_entry_revision(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	createdBy := createDBTestUser(t)
	entryID := "entry-" + uuid.NewString()
	activeRevision, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID: uuid.New(), EntryID: entryID, State: models.RevisionStateActive,
		EntryState: models.EntryStateActive, Name: "active protein revision", CreatedBy: createdBy,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	targetRevision, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID: uuid.New(), EntryID: entryID, ParentRevisionID: &activeRevision.ID,
		State: models.RevisionStatePending, EntryState: models.EntryStateActive,
		Name: "target protein revision", CreatedBy: createdBy,
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	})
	require.NoError(t, err)
	firstArtifact := createDBTestArtifact(t, createdBy, "first protein artifact", now)
	secondArtifact := createDBTestArtifact(t, createdBy, "second protein artifact", now)
	require.NoError(t, testDB.ProteinSequences.Create(ctx, activeRevision.ID, firstArtifact.ID, []models.FASTARecord{
		{Header: "first", Sequence: "ACDEFGHIK"},
	}))
	require.NoError(t, testDB.ProteinSequences.Create(ctx, activeRevision.ID, secondArtifact.ID, []models.FASTARecord{
		{Header: "second", Sequence: "LMNPQRSTV"},
	}))

	// when
	err = testDB.ProteinSequences.MoveEntryRevision(ctx, activeRevision.ID, targetRevision.ID)

	// then
	require.NoError(t, err)
	oldSequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &activeRevision.ID,
	})
	require.NoError(t, err)
	assert.Empty(t, oldSequences)
	movedSequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &targetRevision.ID,
	})
	require.NoError(t, err)
	assert.Len(t, movedSequences, 2)
}
