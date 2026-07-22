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

func Test_should_return_experiment_with_optional_fields_when_experiments_create_and_get_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "experiment-entry", time.Now().UTC())
	now := time.Now().UTC()
	description := "Refined coordinate model with electron-density support " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	experiment := models.Experiment{
		ID:                uuid.New(),
		EntryID:           entry.ID,
		Name:              "qFit run " + uuid.NewString(),
		Description:       &description,
		ThumbnailImageURL: &thumbnailImageURL,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// when
	created, err := testDB.Experiments.Create(context.Background(), experiment)
	require.NoError(t, err)
	got, err := testDB.Experiments.Get(context.Background(), entry.ID, created.ID)

	// then
	require.NoError(t, err)
	assert.Equal(t, experiment.ID, got.ID)
	assert.Equal(t, entry.ID, got.EntryID)
	assert.Equal(t, experiment.Name, got.Name)
	require.NotNil(t, got.Description)
	assert.Equal(t, description, *got.Description)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
}

func Test_should_return_not_found_when_experiments_get_misses(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "missing-experiment-entry", time.Now().UTC())

	// when
	_, err := testDB.Experiments.Get(context.Background(), entry.ID, uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrExperimentNotFound)
}

func Test_should_list_experiments_for_entry_with_pagination_when_experiments_list_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "list-experiments-entry", time.Now().UTC())
	otherEntry := createDBTestEntry(t, "list-experiments-other-entry", time.Now().UTC())
	now := time.Now().UTC()
	first := createDBTestExperiment(t, entry.ID, "first experiment", now)
	second := createDBTestExperiment(t, entry.ID, "second experiment", now.Add(time.Second))
	_ = createDBTestExperiment(t, otherEntry.ID, "other experiment", now.Add(2*time.Second))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Experiments.List(context.Background(), db.ExperimentFilters{
		EntryID: &entry.ID,
		Limit:   &limit,
		Offset:  &offset,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.NotEqual(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[0].ID)
}

func Test_should_return_error_when_experiments_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Experiments.List(context.Background(), db.ExperimentFilters{Limit: &limit})

	// then
	require.Error(t, err)
}
