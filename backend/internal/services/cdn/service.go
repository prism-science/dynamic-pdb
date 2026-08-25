package cdn

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"dynamic-pdb/backend/internal/integrations/s3"
)

const MultipartMaxParts = s3.MultipartMaxParts

type Service interface {
	CreateUpload(ctx context.Context, file FileUpload) (UploadGrant, error)
	CompleteUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error
	AbortUpload(ctx context.Context, key, uploadID string) error
}

type FileUpload struct {
	EntryID          string
	ModelID          string
	ArtifactID       string
	OriginalFilename string
	Size             int64
}

type UploadGrant struct {
	Key       string
	UploadID  string
	ObjectURL string
	PartSize  int64
	Parts     []UploadPart
}

type UploadPart struct {
	PartNumber int32
	URL        string
}

type CompletedPart struct {
	PartNumber int32
	ETag       string
}

var (
	ErrFileTooLarge      = errors.New("cdn: file exceeds maximum upload size")
	ErrInvalidFileUpload = errors.New("cdn: invalid file upload")
)

type invalidFileUploadError struct {
	err error
}

func (e *invalidFileUploadError) Error() string {
	return fmt.Sprintf("cdn: invalid file upload: %v", e.err)
}

func (e *invalidFileUploadError) Unwrap() error {
	return e.err
}

func (e *invalidFileUploadError) Is(target error) bool {
	return target == ErrInvalidFileUpload
}

type service struct {
	bucket            s3.Bucket
	uploadMaxFileSize int64
	bucketBaseURL     *url.URL
	publicBaseURL     *url.URL
}

var _ Service = (*service)(nil)

func NewService(bucket s3.Bucket, cfg Config) (Service, error) {
	if bucket == nil {
		return nil, errors.New("cdn: S3 bucket is required")
	}
	if cfg.S3.UploadMaxFileSize <= 0 {
		return nil, errors.New("cdn: upload max file size must be positive")
	}

	publicBaseURL := strings.TrimSpace(cfg.CloudFront.BaseURL)
	if publicBaseURL == "" {
		return &service{bucket: bucket, uploadMaxFileSize: cfg.S3.UploadMaxFileSize}, nil
	}

	parsedPublicBaseURL, err := parseBaseURL("CloudFront base URL", publicBaseURL)
	if err != nil {
		return nil, err
	}
	bucketBaseURL, err := expectedBucketBaseURL(cfg.S3)
	if err != nil {
		return nil, err
	}

	return &service{
		bucket:            bucket,
		uploadMaxFileSize: cfg.S3.UploadMaxFileSize,
		bucketBaseURL:     bucketBaseURL,
		publicBaseURL:     parsedPublicBaseURL,
	}, nil
}

func (s *service) CreateUpload(ctx context.Context, file FileUpload) (UploadGrant, error) {
	if file.Size <= 0 {
		return UploadGrant{}, &invalidFileUploadError{
			err: errors.New("size must be greater than zero"),
		}
	}
	if file.Size > s.uploadMaxFileSize {
		return UploadGrant{}, fmt.Errorf(
			"%w: size %d exceeds maximum %d",
			ErrFileTooLarge,
			file.Size,
			s.uploadMaxFileSize,
		)
	}
	key, err := objectKey(file)
	if err != nil {
		return UploadGrant{}, &invalidFileUploadError{err: err}
	}

	storageGrant, err := s.bucket.PresignMultipartUpload(ctx, key, file.Size)
	if err != nil {
		return UploadGrant{}, fmt.Errorf("cdn: create S3 upload: %w", err)
	}
	publicObjectURL, err := s.publicObjectURL(storageGrant.ObjectURL)
	if err != nil {
		return UploadGrant{}, fmt.Errorf("cdn: build public object URL: %w", err)
	}

	parts := make([]UploadPart, 0, len(storageGrant.Parts))
	for _, part := range storageGrant.Parts {
		parts = append(parts, UploadPart{
			PartNumber: part.PartNumber,
			URL:        part.URL,
		})
	}
	return UploadGrant{
		Key:       storageGrant.Key,
		UploadID:  storageGrant.UploadID,
		ObjectURL: publicObjectURL,
		PartSize:  storageGrant.PartSize,
		Parts:     parts,
	}, nil
}

func objectKey(file FileUpload) (string, error) {
	entryID, err := requiredKeySegment("entry_id", file.EntryID)
	if err != nil {
		return "", fmt.Errorf("entry id segment: %w", err)
	}
	artifactID, err := requiredKeySegment("artifact_id", file.ArtifactID)
	if err != nil {
		return "", fmt.Errorf("artifact id segment: %w", err)
	}
	filename, err := originalFilename(file.OriginalFilename)
	if err != nil {
		return "", fmt.Errorf("original filename: %w", err)
	}
	modelID := strings.TrimSpace(file.ModelID)
	if modelID == "" {
		return path.Join(entryID, "artifacts", artifactID, filename), nil
	}
	modelID, err = requiredKeySegment("model_id", modelID)
	if err != nil {
		return "", fmt.Errorf("model id segment: %w", err)
	}
	return path.Join(entryID, "models", modelID, "artifacts", artifactID, filename), nil
}

func requiredKeySegment(name, value string) (string, error) {
	segment := strings.TrimSpace(value)
	if segment == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	if segment == "." || segment == ".." || strings.ContainsAny(segment, `/\`) {
		return "", fmt.Errorf("%s is invalid", name)
	}
	return segment, nil
}

func originalFilename(value string) (string, error) {
	filename := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if filename == "" {
		return "", errors.New("original_filename is required")
	}
	filename = strings.TrimSpace(path.Base(filename))
	if filename == "" || filename == "." || filename == ".." {
		return "", errors.New("original_filename is invalid")
	}

	filename = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, filename)
	if filename == "" || filename == "." || filename == ".." {
		return "", errors.New("original_filename is invalid")
	}
	return filename, nil
}

func (s *service) CompleteUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error {
	storageParts := make([]s3.CompletedPart, 0, len(parts))
	for _, part := range parts {
		storageParts = append(storageParts, s3.CompletedPart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		})
	}
	if err := s.bucket.CompleteMultipartUpload(ctx, key, uploadID, storageParts); err != nil {
		return fmt.Errorf("cdn: complete S3 upload: %w", err)
	}
	return nil
}

func (s *service) AbortUpload(ctx context.Context, key, uploadID string) error {
	if err := s.bucket.AbortMultipartUpload(ctx, key, uploadID); err != nil {
		return fmt.Errorf("cdn: abort S3 upload: %w", err)
	}
	return nil
}

func (s *service) publicObjectURL(bucketObjectURL string) (string, error) {
	if s.publicBaseURL == nil {
		return bucketObjectURL, nil
	}

	parsedBucketObjectURL, err := url.Parse(strings.TrimSpace(bucketObjectURL))
	if err != nil {
		return "", fmt.Errorf("parse S3 object URL: %w", err)
	}
	if parsedBucketObjectURL.Scheme == "" || parsedBucketObjectURL.Host == "" {
		return "", errors.New("S3 object URL must include scheme and host")
	}
	if parsedBucketObjectURL.User != nil ||
		parsedBucketObjectURL.RawQuery != "" ||
		parsedBucketObjectURL.Fragment != "" {
		return "", errors.New("S3 object URL must not include user info, query, or fragment")
	}
	if !strings.EqualFold(parsedBucketObjectURL.Scheme, s.bucketBaseURL.Scheme) ||
		!strings.EqualFold(parsedBucketObjectURL.Host, s.bucketBaseURL.Host) {
		return "", fmt.Errorf(
			"S3 object URL %q does not match configured bucket %q",
			bucketObjectURL,
			s.bucketBaseURL.String(),
		)
	}

	objectPath, ok := trimPathPrefix(parsedBucketObjectURL.Path, s.bucketBaseURL.Path)
	if !ok || objectPath == "" {
		return "", fmt.Errorf(
			"S3 object URL %q does not match configured bucket %q",
			bucketObjectURL,
			s.bucketBaseURL.String(),
		)
	}

	publicObjectURL := *s.publicBaseURL
	publicObjectURL.Path = "/" + strings.TrimPrefix(
		path.Join(publicObjectURL.Path, objectPath),
		"/",
	)
	publicObjectURL.RawPath = ""
	return publicObjectURL.String(), nil
}

func expectedBucketBaseURL(cfg s3.BucketConfig) (*url.URL, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, errors.New("cdn: S3 bucket is required when CloudFront is configured")
	}

	if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
		parsedEndpoint, err := parseBaseURL("S3 endpoint", endpoint)
		if err != nil {
			return nil, err
		}
		parsedEndpoint.Path = "/" + strings.TrimPrefix(
			path.Join(parsedEndpoint.Path, bucket),
			"/",
		)
		return parsedEndpoint, nil
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		return nil, errors.New("cdn: S3 region is required when CloudFront is configured")
	}
	return &url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s.s3.%s.amazonaws.com", bucket, region),
	}, nil
}

func parseBaseURL(name, value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return nil, fmt.Errorf("cdn: parse %s: %w", name, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("cdn: %s must be an HTTP URL with scheme and host", name)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("cdn: %s must not include user info, query, or fragment", name)
	}
	parsed.Path = cleanPathPrefix(parsed.Path)
	parsed.RawPath = ""
	return parsed, nil
}

func trimPathPrefix(value, prefix string) (string, bool) {
	cleanedValue := cleanPathPrefix(value)
	cleanedPrefix := cleanPathPrefix(prefix)
	if cleanedPrefix == "" {
		return strings.TrimPrefix(cleanedValue, "/"), true
	}
	if !strings.HasPrefix(cleanedValue, cleanedPrefix+"/") {
		return "", false
	}
	return strings.TrimPrefix(cleanedValue, cleanedPrefix+"/"), true
}

func cleanPathPrefix(value string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(value, "/"))
	if cleaned == "/" {
		return ""
	}
	return strings.TrimSuffix(cleaned, "/")
}
