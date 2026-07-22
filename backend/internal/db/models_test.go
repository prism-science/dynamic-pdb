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

func Test_should_return_model_with_optional_fields_when_models_create_and_get_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "model-entry", time.Now().UTC())
	now := time.Now().UTC()
	description := "Refined coordinate model with electron-density support " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	model := models.Model{
		ID:                uuid.New(),
		EntryID:           entry.ID,
		Name:              "qFit run " + uuid.NewString(),
		Description:       &description,
		ThumbnailImageURL: &thumbnailImageURL,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// when
	created, err := testDB.Models.Create(context.Background(), model)
	require.NoError(t, err)
	got, err := testDB.Models.Get(context.Background(), entry.ID, created.ID)

	// then
	require.NoError(t, err)
	assert.Equal(t, model.ID, got.ID)
	assert.Equal(t, entry.ID, got.EntryID)
	assert.Equal(t, model.Name, got.Name)
	require.NotNil(t, got.Description)
	assert.Equal(t, description, *got.Description)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
}

func Test_should_return_not_found_when_models_get_misses(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "missing-model-entry", time.Now().UTC())

	// when
	_, err := testDB.Models.Get(context.Background(), entry.ID, uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrModelNotFound)
}

func Test_should_list_models_for_entry_with_pagination_when_models_list_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "list-models-entry", time.Now().UTC())
	otherEntry := createDBTestEntry(t, "list-models-other-entry", time.Now().UTC())
	now := time.Now().UTC()
	first := createDBTestModel(t, entry.ID, "first model", now)
	second := createDBTestModel(t, entry.ID, "second model", now.Add(time.Second))
	_ = createDBTestModel(t, otherEntry.ID, "other model", now.Add(2*time.Second))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Models.List(context.Background(), db.ModelFilters{
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

func Test_should_return_error_when_models_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Models.List(context.Background(), db.ModelFilters{Limit: &limit})

	// then
	require.Error(t, err)
}
