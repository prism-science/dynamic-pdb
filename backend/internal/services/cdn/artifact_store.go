package cdn

import (
	"context"
	"errors"
	"fmt"
	"io"

	"dynamic-pdb/backend/internal/integrations/s3"
)

// ArtifactStore gives background jobs direct access to artifact files in the bucket: reading
// a stored file by key, never through CloudFront, and storing a copy of an external file.
type ArtifactStore struct {
	objectURLs
	bucket *s3.RemoteBucket
}

// ObjectWriter streams one object into the bucket; the object appears only on Complete.
type ObjectWriter interface {
	io.Writer
	Complete() error
	Abort() error
}

func NewArtifactStore(bucket *s3.RemoteBucket, cfg Config) (*ArtifactStore, error) {
	if bucket == nil {
		return nil, errors.New("cdn: S3 bucket is required")
	}
	urls, err := newObjectURLs(cfg)
	if err != nil {
		return nil, fmt.Errorf("cdn: artifact store urls: %w", err)
	}
	return &ArtifactStore{objectURLs: urls, bucket: bucket}, nil
}

// StoredKey returns the bucket key of uri when uri points at a file in our bucket.
func (s *ArtifactStore) StoredKey(uri string) (string, bool) {
	key, err := s.keyFromObjectURL(uri)
	if err != nil {
		return "", false
	}
	return key, true
}

// Open streams the file stored at key. A missing object is ErrObjectNotFound.
func (s *ArtifactStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := s.bucket.OpenObject(ctx, key)
	if errors.Is(err, s3.ErrObjectNotFound) {
		return nil, fmt.Errorf("cdn: open object %q: %w", key, ErrObjectNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("cdn: open object: %w", err)
	}
	return reader, nil
}

// CopyKey is where a copy of an artifact's external file goes: the folder its upload would use.
func (s *ArtifactStore) CopyKey(file FileUpload) (string, error) {
	key, err := objectKey(file)
	if err != nil {
		return "", fmt.Errorf("cdn: copy key: %w", err)
	}
	return key, nil
}

// NewWriter starts storing an object at key, replacing any object already there.
func (s *ArtifactStore) NewWriter(ctx context.Context, key string) (ObjectWriter, error) {
	writer, err := s.bucket.NewObjectWriter(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("cdn: start object upload: %w", err)
	}
	return writer, nil
}

// URL is the URL an artifact uses for the object at key: the CloudFront URL when one is
// configured, otherwise the bucket URL.
func (s *ArtifactStore) URL(key string) (string, error) {
	bucketURL, err := s.bucket.ObjectURL(key)
	if err != nil {
		return "", fmt.Errorf("cdn: bucket object url: %w", err)
	}
	publicURL, err := s.publicObjectURL(bucketURL)
	if err != nil {
		return "", fmt.Errorf("cdn: public object url: %w", err)
	}
	return publicURL, nil
}
