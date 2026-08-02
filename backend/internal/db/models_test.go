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
	structure := createDBTestStructure(t, "model-structure", time.Now().UTC())
	now := time.Now().UTC()
	description := "Refined coordinate model with electron-density support " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	createdBy := createDBTestUser(t)
	model := models.Model{
		ID:                uuid.New(),
		StructureID:       structure.ID,
		CreatedBy:         createdBy,
		Name:              "qFit run " + uuid.NewString(),
		Description:       &description,
		ThumbnailImageURL: &thumbnailImageURL,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// when
	created, err := testDB.Models.Create(context.Background(), model)
	require.NoError(t, err)
	got, err := testDB.Models.Get(context.Background(), structure.ID, created.ID)

	// then
	require.NoError(t, err)
	assert.Equal(t, model.ID, got.ID)
	assert.Equal(t, structure.ID, got.StructureID)
	assert.Equal(t, createdBy, got.CreatedBy)
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
	structure := createDBTestStructure(t, "missing-model-structure", time.Now().UTC())

	// when
	_, err := testDB.Models.Get(context.Background(), structure.ID, uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrModelNotFound)
}

func Test_should_list_models_for_structure_with_pagination_when_models_list_called(t *testing.T) {
	// given
	structure := createDBTestStructure(t, "list-models-structure", time.Now().UTC())
	otherStructure := createDBTestStructure(t, "list-models-other-structure", time.Now().UTC())
	now := time.Now().UTC()
	first := createDBTestModel(t, structure.ID, "first model", now)
	second := createDBTestModel(t, structure.ID, "second model", now.Add(time.Second))
	_ = createDBTestModel(t, otherStructure.ID, "other model", now.Add(2*time.Second))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Models.List(context.Background(), db.ModelFilters{
		StructureID: &structure.ID,
		Limit:       &limit,
		Offset:      &offset,
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
