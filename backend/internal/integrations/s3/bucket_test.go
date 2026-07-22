package s3_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/integrations/s3"
)

func Test_should_reject_missing_bucket_config_when_new_bucket_called(t *testing.T) {
	// given
	cfg := s3.BucketConfig{Region: "us-east-1"}

	// when
	_, err := s3.NewBucket(context.Background(), cfg)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "bucket is required")
}

func Test_should_use_min_part_size_and_single_part_when_file_small(t *testing.T) {
	// given
	size := int64(100)

	// when
	partSize, partCount := s3.PartPlan(size)

	// then
	assert.Equal(t, int64(64*1024*1024), partSize)
	assert.Equal(t, 1, partCount)
}

func Test_should_split_into_multiple_parts_when_file_exceeds_part_size(t *testing.T) {
	// given
	size := int64(64*1024*1024)*2 + 1

	// when
	partSize, partCount := s3.PartPlan(size)

	// then
	assert.Equal(t, int64(64*1024*1024), partSize)
	assert.Equal(t, 3, partCount)
}

func Test_should_grow_part_size_to_stay_under_part_cap_when_file_very_large(t *testing.T) {
	// given
	size := int64(5) * 1024 * 1024 * 1024 * 1024

	// when
	partSize, partCount := s3.PartPlan(size)

	// then
	assert.LessOrEqual(t, partCount, 10000)
	assert.GreaterOrEqual(t, partSize, int64(64*1024*1024))
	assert.GreaterOrEqual(t, partSize*int64(partCount), size)
	assert.Less(t, partSize*int64(partCount-1), size)
	assert.Zero(t, partSize%(1024*1024))
}

func Test_should_build_entry_object_key_when_file_has_no_model(t *testing.T) {
	// given
	file := s3.FileUpload{
		EntryID:          "entry-id",
		EntityID:         "entity-id",
		OriginalFilename: "model.cif",
		Size:             100,
	}

	// when
	key, err := s3.ObjectKey(file)

	// then
	require.NoError(t, err)
	assert.Equal(t, "entry-id/entities/entity-id/model.cif", key)
}

func Test_should_build_model_object_key_when_file_has_model(t *testing.T) {
	// given
	file := s3.FileUpload{
		EntryID:          "entry-id",
		ModelID:          "model-id",
		EntityID:         "entity-id",
		OriginalFilename: "model.cif",
		Size:             100,
	}

	// when
	key, err := s3.ObjectKey(file)

	// then
	require.NoError(t, err)
	assert.Equal(t, "entry-id/models/model-id/entities/entity-id/model.cif", key)
}

func Test_should_strip_path_from_original_filename_when_building_object_key(t *testing.T) {
	// given
	file := s3.FileUpload{
		EntryID:          "entry-id",
		ModelID:          "model-id",
		EntityID:         "entity-id",
		OriginalFilename: "../unsafe/model.cif",
		Size:             100,
	}

	// when
	key, err := s3.ObjectKey(file)

	// then
	require.NoError(t, err)
	assert.Equal(t, "entry-id/models/model-id/entities/entity-id/model.cif", key)
}

func Test_should_reject_invalid_key_segment_when_building_object_key(t *testing.T) {
	// given
	file := s3.FileUpload{
		EntryID:          "entry/id",
		ModelID:          "model-id",
		EntityID:         "entity-id",
		OriginalFilename: "model.cif",
		Size:             100,
	}

	// when
	_, err := s3.ObjectKey(file)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "entry_id is invalid")
}
