package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_return_experiment_when_experiment_from_row_called(t *testing.T) {
	// given
	id := uuid.New()
	entryID := uuid.New()
	now := time.Now().UTC()
	description := "Refined coordinate model with electron-density support"
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/experiment.png"
	row := experimentRow{
		ID:      id,
		EntryID: entryID,
		Name:    "qFit run",
		Description: sql.NullString{
			String: description,
			Valid:  true,
		},
		ThumbnailImageURL: sql.NullString{
			String: thumbnailImageURL,
			Valid:  true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	experiment := experimentFromRow(&row)

	// then
	assert.Equal(t, id, experiment.ID)
	assert.Equal(t, entryID, experiment.EntryID)
	assert.Equal(t, "qFit run", experiment.Name)
	require.NotNil(t, experiment.Description)
	assert.Equal(t, description, *experiment.Description)
	require.NotNil(t, experiment.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *experiment.ThumbnailImageURL)
	assert.Equal(t, now, experiment.CreatedAt)
	assert.Equal(t, now, experiment.UpdatedAt)
}

func Test_should_build_query_with_entry_and_pagination_when_experiment_list_query_called(t *testing.T) {
	// given
	entryID := uuid.New()
	limit := 50
	offset := 100
	filters := ExperimentFilters{
		EntryID: &entryID,
		Limit:   &limit,
		Offset:  &offset,
	}

	// when
	query, args, err := experimentListQuery(filters)

	// then
	require.NoError(t, err)
	assert.Contains(t, query, "from experiments")
	assert.Contains(t, query, "where entry_id = :entry_id")
	assert.Contains(t, query, "order by created_at asc, id asc")
	assert.Contains(t, query, "limit :limit")
	assert.Contains(t, query, "offset :offset")
	assert.Equal(t, entryID, args["entry_id"])
	assert.Equal(t, limit, args["limit"])
	assert.Equal(t, offset, args["offset"])
}

func Test_should_return_error_when_experiment_list_query_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1
	filters := ExperimentFilters{
		Limit: &limit,
	}

	// when
	_, _, err := experimentListQuery(filters)

	// then
	require.Error(t, err)
}
