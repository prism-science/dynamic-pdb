package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_index_only_requested_record_when_index_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	entryToken := "entrytoken" + token
	experimentToken := "experimenttoken" + token
	firstEntityToken := "firstentitytoken" + token
	secondEntityToken := "secondentitytoken" + token

	entry, err := testDB.Entries.Create(ctx, models.Entry{
		ID:        uuid.New(),
		Name:      "entry " + entryToken,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	experiment, err := testDB.Experiments.Create(ctx, models.Experiment{
		ID:        uuid.New(),
		EntryID:   entry.ID,
		Name:      "experiment " + experimentToken,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	firstEntity, err := testDB.Entities.Create(ctx, models.Entity{
		ID:           uuid.New(),
		EntryID:      entry.ID,
		ExperimentID: &experiment.ID,
		Type:         models.EntityTypeData,
		Name:         "first data file " + firstEntityToken,
		Payload: models.DataPayload{
			FileURL: "s3://dynamic-pdb/test/first.fasta",
			Type:    "fasta",
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	secondEntity, err := testDB.Entities.Create(ctx, models.Entity{
		ID:           uuid.New(),
		EntryID:      entry.ID,
		ExperimentID: &experiment.ID,
		Type:         models.EntityTypeData,
		Name:         "second data file " + secondEntityToken,
		Payload: models.DataPayload{
			FileURL: "s3://dynamic-pdb/test/second.fasta",
			Type:    "fasta",
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	// when
	err = testDB.EntrySearch.IndexEntry(ctx, *entry)

	// then
	require.NoError(t, err)
	assertEntrySearchContains(t, ctx, entryToken, entry.ID)
	assertEntrySearchDoesNotContain(t, ctx, experimentToken, entry.ID)
	assertEntrySearchDoesNotContain(t, ctx, firstEntityToken, entry.ID)
	assertEntrySearchDoesNotContain(t, ctx, secondEntityToken, entry.ID)

	// when
	err = testDB.EntrySearch.IndexExperiment(ctx, *experiment)

	// then
	require.NoError(t, err)
	assertEntrySearchContains(t, ctx, experimentToken, entry.ID)
	assertEntrySearchDoesNotContain(t, ctx, firstEntityToken, entry.ID)
	assertEntrySearchDoesNotContain(t, ctx, secondEntityToken, entry.ID)

	// when
	err = testDB.EntrySearch.IndexEntity(ctx, *firstEntity)

	// then
	require.NoError(t, err)
	assertEntrySearchContains(t, ctx, firstEntityToken, entry.ID)
	assertEntrySearchDoesNotContain(t, ctx, secondEntityToken, entry.ID)

	// when
	err = testDB.EntrySearch.IndexEntity(ctx, *secondEntity)

	// then
	require.NoError(t, err)
	assertEntrySearchContains(t, ctx, secondEntityToken, entry.ID)
}

func assertEntrySearchContains(t *testing.T, ctx context.Context, query string, entryID uuid.UUID) {
	t.Helper()

	entries, err := testDB.Entries.List(ctx, db.EntryFilters{Search: query})
	require.NoError(t, err)
	assert.True(t, entryListContainsID(entries, entryID))
}

func assertEntrySearchDoesNotContain(t *testing.T, ctx context.Context, query string, entryID uuid.UUID) {
	t.Helper()

	entries, err := testDB.Entries.List(ctx, db.EntryFilters{Search: query})
	require.NoError(t, err)
	assert.False(t, entryListContainsID(entries, entryID))
}

func entryListContainsID(entries []models.Entry, entryID uuid.UUID) bool {
	for _, entry := range entries {
		if entry.ID == entryID {
			return true
		}
	}
	return false
}
