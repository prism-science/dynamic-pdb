package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_return_entry_when_entry_from_row_called(t *testing.T) {
	// given
	id := uuid.New()
	now := time.Now().UTC()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/4rug.png"
	row := entryRow{
		ID:   id,
		Name: "4RUG",
		ThumbnailImageURL: sql.NullString{
			String: thumbnailImageURL,
			Valid:  true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	entry := entryFromRow(&row)

	// then
	assert.Equal(t, id, entry.ID)
	assert.Equal(t, "4RUG", entry.Name)
	require.NotNil(t, entry.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *entry.ThumbnailImageURL)
	assert.Equal(t, now, entry.CreatedAt)
	assert.Equal(t, now, entry.UpdatedAt)
}

func Test_should_build_query_with_pagination_when_entry_list_query_called(t *testing.T) {
	// given
	limit := 50
	offset := 100
	filters := EntryFilters{
		Limit:  &limit,
		Offset: &offset,
	}

	// when
	query, args, err := entryListQuery(filters)

	// then
	require.NoError(t, err)
	assert.Contains(t, query, "from entries")
	assert.Contains(t, query, "order by created_at asc, id asc")
	assert.Contains(t, query, "limit :limit")
	assert.Contains(t, query, "offset :offset")
	assert.Equal(t, limit, args["limit"])
	assert.Equal(t, offset, args["offset"])
}

func Test_should_return_error_when_entry_list_query_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1
	filters := EntryFilters{
		Limit: &limit,
	}

	// when
	_, _, err := entryListQuery(filters)

	// then
	require.Error(t, err)
}
