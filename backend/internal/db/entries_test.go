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

func Test_should_return_entry_with_optional_fields_when_entries_create_and_get_called(t *testing.T) {
	// given
	now := time.Now().UTC()
	description := "Entry description " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	createdBy := createDBTestUser(t)
	entry := models.Entry{
		ID:                uuid.New(),
		CreatedBy:         createdBy,
		Name:              "entry-" + uuid.NewString(),
		Description:       &description,
		ThumbnailImageURL: &thumbnailImageURL,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// when
	created, err := testDB.Entries.Create(context.Background(), entry)
	require.NoError(t, err)
	got, err := testDB.Entries.Get(context.Background(), created.ID)

	// then
	require.NoError(t, err)
	assert.Equal(t, entry.ID, got.ID)
	assert.Equal(t, createdBy, got.CreatedBy)
	assert.Equal(t, entry.Name, got.Name)
	require.NotNil(t, got.Description)
	assert.Equal(t, description, *got.Description)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
}

func Test_should_return_not_found_when_entries_get_misses(t *testing.T) {
	// given / when
	_, err := testDB.Entries.Get(context.Background(), uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrEntryNotFound)
}

func Test_should_list_entries_matching_search_with_pagination_when_entries_list_called(t *testing.T) {
	// given
	ctx := context.Background()
	token := "entrytoken" + strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()
	first := createDBTestEntry(t, "first "+token, now)
	second := createDBTestEntry(t, "second "+token, now.Add(time.Second))
	unmatched := createDBTestEntry(t, "unmatched "+uuid.NewString(), now.Add(2*time.Second))
	require.NoError(t, testDB.EntrySearch.IndexEntry(ctx, *first))
	require.NoError(t, testDB.EntrySearch.IndexEntry(ctx, *second))
	require.NoError(t, testDB.EntrySearch.IndexEntry(ctx, *unmatched))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Entries.List(ctx, db.EntryFilters{
		Query:  token,
		Limit:  &limit,
		Offset: &offset,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, second.ID, got[0].ID)
}

func Test_should_return_error_when_entries_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Entries.List(context.Background(), db.EntryFilters{Limit: &limit})

	// then
	require.Error(t, err)
}
