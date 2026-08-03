package cdn

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/integrations/s3"
)

func Test_should_return_s3_object_url_when_cloudfront_is_not_configured(t *testing.T) {
	// given
	bucket := &bucketStub{
		grant: s3.MultipartUploadGrant{
			Key:       "entry/artifacts/artifact/model.cif",
			UploadID:  "upload-id",
			ObjectURL: "http://localhost:9000/dynamic-pdb/entry/artifacts/artifact/model.cif",
			PartSize:  64 * 1024 * 1024,
		},
	}
	service, err := NewService(bucket, Config{})
	require.NoError(t, err)

	// when
	grant, err := service.CreateUpload(context.Background(), validFileUpload())

	// then
	require.NoError(t, err)
	assert.Equal(t, bucket.grant.ObjectURL, grant.ObjectURL)
}

func Test_should_rewrite_s3_object_url_when_cloudfront_is_configured(t *testing.T) {
	// given
	bucket := &bucketStub{
		grant: s3.MultipartUploadGrant{
			Key:       "entry/artifacts/artifact/model.cif",
			UploadID:  "upload-id",
			ObjectURL: "https://dynamic-pdb-data.s3.us-west-1.amazonaws.com/entry/artifacts/artifact/model.cif",
			PartSize:  64 * 1024 * 1024,
			Parts: []s3.PresignedPart{
				{PartNumber: 1, URL: "https://storage.example/part-1"},
			},
		},
	}
	service, err := NewService(bucket, Config{
		S3: s3.BucketConfig{
			Region: "us-west-1",
			Bucket: "dynamic-pdb-data",
		},
		CloudFront: CloudFrontConfig{
			BaseURL: "https://files.dynamicpdb.com",
		},
	})
	require.NoError(t, err)

	// when
	grant, err := service.CreateUpload(context.Background(), validFileUpload())

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://files.dynamicpdb.com/entry/artifacts/artifact/model.cif", grant.ObjectURL)
	assert.Equal(t, "https://storage.example/part-1", grant.Parts[0].URL)
}

func Test_should_reject_s3_object_url_from_another_bucket(t *testing.T) {
	// given
	bucket := &bucketStub{
		grant: s3.MultipartUploadGrant{
			ObjectURL: "https://unrelated.example/entry/artifacts/artifact/model.cif",
		},
	}
	service, err := NewService(bucket, Config{
		S3: s3.BucketConfig{
			Region: "us-west-1",
			Bucket: "dynamic-pdb-data",
		},
		CloudFront: CloudFrontConfig{
			BaseURL: "https://files.dynamicpdb.com",
		},
	})
	require.NoError(t, err)

	// when
	_, err = service.CreateUpload(context.Background(), validFileUpload())

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "does not match configured bucket")
}

func Test_should_derive_path_style_s3_origin_from_custom_endpoint(t *testing.T) {
	// given
	bucket := &bucketStub{
		grant: s3.MultipartUploadGrant{
			ObjectURL: "http://localhost:9000/storage/dynamic-pdb/entry/artifacts/artifact/model.cif",
		},
	}
	service, err := NewService(bucket, Config{
		S3: s3.BucketConfig{
			Endpoint: "http://localhost:9000/storage",
			Bucket:   "dynamic-pdb",
		},
		CloudFront: CloudFrontConfig{
			BaseURL: "https://files.example.com",
		},
	})
	require.NoError(t, err)

	// when
	grant, err := service.CreateUpload(context.Background(), validFileUpload())

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://files.example.com/entry/artifacts/artifact/model.cif", grant.ObjectURL)
}

func Test_should_reject_invalid_file_upload_before_calling_s3(t *testing.T) {
	// given
	bucket := &bucketStub{}
	service, err := NewService(bucket, Config{})
	require.NoError(t, err)
	file := validFileUpload()
	file.OriginalFilename = ".."

	// when
	_, err = service.CreateUpload(context.Background(), file)

	// then
	require.ErrorIs(t, err, ErrInvalidFileUpload)
	assert.Zero(t, bucket.presignCalls)
}

func Test_should_proxy_complete_and_abort_to_s3(t *testing.T) {
	// given
	bucket := &bucketStub{}
	service, err := NewService(bucket, Config{})
	require.NoError(t, err)
	parts := []CompletedPart{
		{PartNumber: 2, ETag: `"etag-2"`},
		{PartNumber: 1, ETag: `"etag-1"`},
	}

	// when
	completeErr := service.CompleteUpload(context.Background(), "object-key", "upload-id", parts)
	abortErr := service.AbortUpload(context.Background(), "object-key", "upload-id")

	// then
	require.NoError(t, completeErr)
	require.NoError(t, abortErr)
	assert.Equal(t, []s3.CompletedPart{
		{PartNumber: 2, ETag: `"etag-2"`},
		{PartNumber: 1, ETag: `"etag-1"`},
	}, bucket.completedParts)
	assert.Equal(t, "object-key", bucket.abortedKey)
	assert.Equal(t, "upload-id", bucket.abortedUploadID)
}

func validFileUpload() FileUpload {
	return FileUpload{
		EntryID:          "entry",
		ArtifactID:       "entity",
		OriginalFilename: "model.cif",
		Size:             42,
	}
}

type bucketStub struct {
	grant       s3.MultipartUploadGrant
	presignErr  error
	completeErr error
	abortErr    error

	presignCalls int

	completedParts  []s3.CompletedPart
	abortedKey      string
	abortedUploadID string
}

func (b *bucketStub) PresignMultipartUpload(_ context.Context, _ s3.FileUpload) (s3.MultipartUploadGrant, error) {
	b.presignCalls++
	if b.presignErr != nil {
		return s3.MultipartUploadGrant{}, b.presignErr
	}
	return b.grant, nil
}

func (b *bucketStub) CompleteMultipartUpload(_ context.Context, _, _ string, parts []s3.CompletedPart) error {
	b.completedParts = parts
	return b.completeErr
}

func (b *bucketStub) AbortMultipartUpload(_ context.Context, key, uploadID string) error {
	b.abortedKey = key
	b.abortedUploadID = uploadID
	return b.abortErr
}

var _ s3.Bucket = (*bucketStub)(nil)
