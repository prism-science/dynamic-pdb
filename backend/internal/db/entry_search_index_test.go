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
	modelToken := "modeltoken" + token
	thumbnailToken := "thumbnailtoken" + token
	thumbnailURL := "https://images.example/" + thumbnailToken + ".jpeg"
	firstEntityToken := "firstentitytoken" + token
	secondEntityToken := "secondentitytoken" + token
	authorToken := "authortoken" + token
	affiliationToken := "affiliationtoken" + token
	fileURLToken := "fileurltoken" + token
	affiliation := "research affiliation " + affiliationToken
	entityLevel := models.EntityLevelL3
	createdBy := createDBTestUser(t)

	entry, err := testDB.Entries.Create(ctx, models.Entry{
		ID:        uuid.New(),
		CreatedBy: createdBy,
		Name:      "entry " + entryToken,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	entry.ThumbnailImageURL = &thumbnailURL

	model, err := testDB.Models.Create(ctx, models.Model{
		ID:        uuid.New(),
		EntryID:   entry.ID,
		CreatedBy: createdBy,
		Name:      "model " + modelToken,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	model.ThumbnailImageURL = &thumbnailURL

	firstEntity, err := testDB.Entities.Create(ctx, models.Entity{
		ID:      uuid.New(),
		EntryID: entry.ID,
		ModelID: &model.ID,
		Type:    models.EntityTypeData,
		Level:   &entityLevel,
		Name:    "first entity " + firstEntityToken,
		Payload: models.DataPayload{
			FileURL:     "https://files.example/" + fileURLToken + ".bin",
			Type:        "fasta",
			Authors:     []string{"Researcher " + authorToken},
			Affiliation: &affiliation,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	secondEntity, err := testDB.Entities.Create(ctx, models.Entity{
		ID:      uuid.New(),
		EntryID: entry.ID,
		ModelID: &model.ID,
		Type:    models.EntityTypeData,
		Name:    "second data file " + secondEntityToken,
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
	assertEntrySearchContains(ctx, t, entryToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, thumbnailToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, modelToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, firstEntityToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, secondEntityToken, entry.ID)

	// when
	err = testDB.EntrySearch.IndexModel(ctx, *model)

	// then
	require.NoError(t, err)
	assertEntrySearchContains(ctx, t, modelToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, thumbnailToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, firstEntityToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, secondEntityToken, entry.ID)

	// when
	err = testDB.EntrySearch.IndexEntity(ctx, *firstEntity)

	// then
	require.NoError(t, err)
	assertEntrySearchContains(ctx, t, firstEntityToken, entry.ID)
	assertEntrySearchContains(ctx, t, authorToken, entry.ID)
	assertEntrySearchContains(ctx, t, affiliationToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, fileURLToken, entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, "fasta", entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, string(entityLevel), entry.ID)
	assertEntrySearchDoesNotContain(ctx, t, secondEntityToken, entry.ID)

	// when
	err = testDB.EntrySearch.IndexEntity(ctx, *secondEntity)

	// then
	require.NoError(t, err)
	assertEntrySearchContains(ctx, t, secondEntityToken, entry.ID)
}

func assertEntrySearchContains(ctx context.Context, t *testing.T, query string, entryID uuid.UUID) {
	t.Helper()

	entries, err := testDB.Entries.List(ctx, db.EntryFilters{Query: query})
	require.NoError(t, err)
	assert.True(t, entryListContainsID(entries, entryID))
}

func assertEntrySearchDoesNotContain(ctx context.Context, t *testing.T, query string, entryID uuid.UUID) {
	t.Helper()

	entries, err := testDB.Entries.List(ctx, db.EntryFilters{Query: query})
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
