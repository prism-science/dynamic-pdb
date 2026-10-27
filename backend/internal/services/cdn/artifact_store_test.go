package cdn

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/integrations/s3"
)

func newTestArtifactStore(t *testing.T, cloudFrontBaseURL string, handler http.HandlerFunc) *ArtifactStore {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	bucketConfig := s3.BucketConfig{
		Endpoint:          server.URL,
		Region:            "us-west-1",
		Bucket:            "dynamic-pdb-data",
		AccessKeyID:       "AKIAEXAMPLE",
		SecretAccessKey:   "secret",
		UploadMaxFileSize: 1 << 30,
		UploadURLTTL:      time.Minute,
	}
	bucket, err := s3.NewBucket(context.Background(), bucketConfig)
	require.NoError(t, err)
	store, err := NewArtifactStore(bucket, Config{S3: bucketConfig, CloudFront: CloudFrontConfig{BaseURL: cloudFrontBaseURL}})
	require.NoError(t, err)
	return store
}

func Test_should_find_key_when_uri_points_into_our_bucket(t *testing.T) {
	// given
	store := newTestArtifactStore(t, "https://files.dynamicpdb.com", http.NotFound)

	// when
	key, stored := store.StoredKey("https://files.dynamicpdb.com/entry-1/artifacts/a1/model.cif")

	// then
	assert.True(t, stored)
	assert.Equal(t, "entry-1/artifacts/a1/model.cif", key)
}

func Test_should_report_external_file_when_uri_points_elsewhere(t *testing.T) {
	// given
	store := newTestArtifactStore(t, "https://files.dynamicpdb.com", http.NotFound)

	// when
	_, stored := store.StoredKey("https://files.rcsb.org/download/1ABC.cif")

	// then
	assert.False(t, stored)
}

func Test_should_return_not_found_error_when_stored_file_is_missing(t *testing.T) {
	// given
	store := newTestArtifactStore(t, "https://files.dynamicpdb.com", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, err := w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
		assert.NoError(t, err)
	})

	// when
	_, err := store.Open(context.Background(), "entry-1/artifacts/a1/model.cif")

	// then
	require.ErrorIs(t, err, ErrObjectNotFound)
}

func Test_should_stream_stored_file_when_opened(t *testing.T) {
	// given
	var requestedPath string
	store := newTestArtifactStore(t, "https://files.dynamicpdb.com", func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		_, err := w.Write([]byte("data_1abc\n"))
		assert.NoError(t, err)
	})

	// when
	reader, err := store.Open(context.Background(), "entry-1/artifacts/a1/model.cif")

	// then
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Equal(t, "data_1abc\n", string(data))
	assert.Equal(t, "/dynamic-pdb-data/entry-1/artifacts/a1/model.cif", requestedPath)
}

func Test_should_place_copy_in_the_artifact_upload_folder_when_copy_key_built(t *testing.T) {
	// given
	store := newTestArtifactStore(t, "https://files.dynamicpdb.com", http.NotFound)

	// when
	entryKey, entryErr := store.CopyKey(FileUpload{EntryID: "entry-1", ArtifactID: "a1", OriginalFilename: "1abc-sf.cif"})
	modelKey, modelErr := store.CopyKey(FileUpload{EntryID: "entry-1", ModelID: "model-1", ArtifactID: "a2", OriginalFilename: "1abc.cif"})

	// then
	require.NoError(t, entryErr)
	require.NoError(t, modelErr)
	assert.Equal(t, "entry-1/artifacts/a1/1abc-sf.cif", entryKey)
	assert.Equal(t, "entry-1/models/model-1/artifacts/a2/1abc.cif", modelKey)
}

func Test_should_return_cloudfront_url_when_cloudfront_configured(t *testing.T) {
	// given
	store := newTestArtifactStore(t, "https://files.dynamicpdb.com", http.NotFound)

	// when
	url, err := store.URL("entry-1/artifacts/a1/1abc-sf.cif")

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://files.dynamicpdb.com/entry-1/artifacts/a1/1abc-sf.cif", url)
}
