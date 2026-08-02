package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_create_and_list_entry_protein_sequences_when_repository_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "protein-sequences-entry", time.Now().UTC())
	entity := createDBTestEntity(
		t,
		entry.ID,
		nil,
		models.EntityTypeData,
		nil,
		"protein sequences",
	)
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
	err := testDB.ProteinSequences.Create(context.Background(), entry.ID, entity.ID, records)
	require.NoError(t, err)
	otherEntry := createDBTestEntry(t, "other-protein-sequences-entry", time.Now().UTC())
	otherEntity := createDBTestEntity(
		t,
		otherEntry.ID,
		nil,
		models.EntityTypeData,
		nil,
		"other protein sequence",
	)
	require.NoError(t, testDB.ProteinSequences.Create(
		context.Background(),
		otherEntry.ID,
		otherEntity.ID,
		[]models.FASTARecord{{Header: "other", Sequence: "TTTT"}},
	))
	sequences, err := testDB.ProteinSequences.List(context.Background(), entry.ID)

	// then
	require.NoError(t, err)
	require.Len(t, sequences, 2)
	assert.Equal(t, entry.ID, sequences[0].EntryID)
	assert.Equal(t, entity.ID, sequences[0].EntityID)
	assert.Equal(t, 0, sequences[0].RecordIndex)
	assert.Equal(t, "first", sequences[0].Header)
	assert.Equal(t, "ACGT", sequences[0].Sequence)
	assert.Equal(t, entry.ID, sequences[1].EntryID)
	assert.Equal(t, entity.ID, sequences[1].EntityID)
	assert.Equal(t, 1, sequences[1].RecordIndex)
	assert.Equal(t, "second", sequences[1].Header)
	assert.Equal(t, "ACGT", sequences[1].Sequence)
	assert.NotEqual(t, sequences[0].ID, sequences[1].ID)
	assert.NotEqual(t, uuid.Nil, sequences[0].ID)
	assert.NotEqual(t, uuid.Nil, sequences[1].ID)
}
