package s3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_reject_missing_bucket_config_when_new_bucket_called(t *testing.T) {
	// given
	cfg := BucketConfig{Region: "us-east-1"}

	// when
	_, err := NewBucket(context.Background(), cfg)

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "bucket is required")
}

func Test_should_use_min_part_size_and_single_part_when_file_small(t *testing.T) {
	// given
	size := int64(100)

	// when
	partSize, partCount := PartPlan(size)

	// then
	assert.Equal(t, int64(64*1024*1024), partSize)
	assert.Equal(t, 1, partCount)
}

func Test_should_split_into_multiple_parts_when_file_exceeds_part_size(t *testing.T) {
	// given
	size := int64(64*1024*1024)*2 + 1

	// when
	partSize, partCount := PartPlan(size)

	// then
	assert.Equal(t, int64(64*1024*1024), partSize)
	assert.Equal(t, 3, partCount)
}

func Test_should_grow_part_size_to_stay_under_part_cap_when_file_very_large(t *testing.T) {
	// given
	size := int64(5) * 1024 * 1024 * 1024 * 1024

	// when
	partSize, partCount := PartPlan(size)

	// then
	assert.LessOrEqual(t, partCount, 10000)
	assert.GreaterOrEqual(t, partSize, int64(64*1024*1024))
	assert.GreaterOrEqual(t, partSize*int64(partCount), size)
	assert.Less(t, partSize*int64(partCount-1), size)
	assert.Zero(t, partSize%(1024*1024))
}

func Test_should_build_full_aws_object_url_when_endpoint_is_not_configured(t *testing.T) {
	// given
	bucket := &RemoteBucket{
		config: BucketConfig{
			Region: "us-west-1",
			Bucket: "dynamic-pdb-data",
		},
	}

	// when
	objectURL, err := bucket.objectURL("entry/artifacts/artifact/model.cif")

	// then
	require.NoError(t, err)
	assert.Equal(
		t,
		"https://dynamic-pdb-data.s3.us-west-1.amazonaws.com/entry/artifacts/artifact/model.cif",
		objectURL,
	)
}

func Test_should_build_full_endpoint_object_url_when_endpoint_is_configured(t *testing.T) {
	// given
	bucket := &RemoteBucket{
		config: BucketConfig{
			Endpoint: "https://storage.example/root",
			Region:   "us-east-1",
			Bucket:   "dynamic-pdb",
		},
	}

	// when
	objectURL, err := bucket.objectURL("entry/artifacts/artifact/model.cif")

	// then
	require.NoError(t, err)
	assert.Equal(
		t,
		"https://storage.example/root/dynamic-pdb/entry/artifacts/artifact/model.cif",
		objectURL,
	)
}

func Test_should_detect_object_content_type_from_filename(t *testing.T) {
	// given

	// when
	imageContentType := objectContentType("preview.jpeg")
	unknownContentType := objectContentType("model.unknown-extension")

	// then
	assert.Equal(t, "image/jpeg", imageContentType)
	assert.Equal(t, "application/octet-stream", unknownContentType)
}
