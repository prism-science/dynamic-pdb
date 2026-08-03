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

func Test_should_return_model_revision_with_metadata_when_models_create_and_get_called(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "model-entry", time.Now().UTC())
	now := time.Now().UTC()
	description := "Refined coordinate model with electron-density support " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	createdBy := createDBTestUser(t)
	affiliation := "Department of Chemistry, Boston University"
	purpose := models.ModelPurposeRefinement
	modelType := models.StructureModelTypeMulticonformer
	revision := models.ModelRevision{
		ID:                uuid.New(),
		ModelID:           uuid.New(),
		State:             models.RevisionStatePending,
		Name:              "qFit run " + uuid.NewString(),
		Description:       &description,
		ThumbnailImageURL: &thumbnailImageURL,
		Metadata: models.ModelMetadata{
			Authors:     []string{"Hendrickson, W.A.", "Teeter, M.M."},
			Affiliation: &affiliation,
			Purpose:     &purpose,
			ModelType:   &modelType,
			Ligands:     []string{"HEM"},
		},
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	created, err := testDB.Models.Create(context.Background(), entryRevision.EntryID, revision)
	require.NoError(t, err)
	got, err := testDB.Models.Get(context.Background(), db.ModelRevisionFilters{ID: &created.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, revision.ID, got.ID)
	assert.Equal(t, entryRevision.EntryID, got.EntryID)
	assert.Equal(t, revision.ModelID, got.ModelID)
	assert.Equal(t, createdBy, got.CreatedBy)
	assert.Equal(t, revision.Name, got.Name)
	require.NotNil(t, got.Description)
	assert.Equal(t, description, *got.Description)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, []string{"Hendrickson, W.A.", "Teeter, M.M."}, got.Metadata.Authors)
	require.NotNil(t, got.Metadata.Affiliation)
	assert.Equal(t, affiliation, *got.Metadata.Affiliation)
	require.NotNil(t, got.Metadata.Purpose)
	assert.Equal(t, purpose, *got.Metadata.Purpose)
	require.NotNil(t, got.Metadata.ModelType)
	assert.Equal(t, modelType, *got.Metadata.ModelType)
	assert.Equal(t, []string{"HEM"}, got.Metadata.Ligands)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
}

func Test_should_return_not_found_when_models_get_misses(t *testing.T) {
	// given / when
	_, err := testDB.Models.Get(context.Background(), db.ModelRevisionFilters{ID: ptr(uuid.New())})

	// then
	require.ErrorIs(t, err, db.ErrModelRevisionNotFound)
}

func Test_should_list_model_revisions_for_entry_with_pagination_when_models_list_called(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "list-models-entry", time.Now().UTC())
	otherEntryRevision := createDBTestEntryRevision(t, "list-models-other-entry", time.Now().UTC())
	now := time.Now().UTC()
	first := createDBTestModelRevision(t, entryRevision.EntryID, "first model", now)
	second := createDBTestModelRevision(t, entryRevision.EntryID, "second model", now.Add(time.Second))
	_ = createDBTestModelRevision(t, otherEntryRevision.EntryID, "other model", now.Add(2*time.Second))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Models.List(context.Background(), db.ModelRevisionFilters{
		EntryID: &entryRevision.EntryID,
		Limit:   &limit,
		Offset:  &offset,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.NotEqual(t, first.ID, got[0].ID)
	assert.Equal(t, second.ID, got[0].ID)
}

func Test_should_mark_model_revision_deleted_when_models_delete_called_by_owner(t *testing.T) {
	// given
	ctx := context.Background()
	entryRevision := createDBTestEntryRevision(t, "deleted model entry", time.Now().UTC())
	revision := createDBTestModelRevision(t, entryRevision.EntryID, "deleted model revision", time.Now().UTC())

	// when
	err := testDB.Models.Delete(ctx, revision.ModelID, revision.ID, revision.CreatedBy)
	require.NoError(t, err)
	got, err := testDB.Models.Get(ctx, db.ModelRevisionFilters{ID: &revision.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, models.RevisionStateDeleted, got.State)
}

func Test_should_return_error_when_models_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Models.List(context.Background(), db.ModelRevisionFilters{Limit: &limit})

	// then
	require.Error(t, err)
}
