package s3

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	multipartMinPartSize  = 64 * 1024 * 1024
	MultipartMaxParts     = 10_000
	multipartUploadURLTTL = 12 * time.Hour
)

type Bucket interface {
	PresignMultipartUpload(ctx context.Context, file FileUpload) (MultipartUploadGrant, error)
	CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(ctx context.Context, key, uploadID string) error
}

type RemoteBucket struct {
	config    BucketConfig
	client    *awss3.Client
	presigner *awss3.PresignClient
}

var _ Bucket = (*RemoteBucket)(nil)

func NewBucket(ctx context.Context, cfg BucketConfig) (*RemoteBucket, error) {
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, fmt.Errorf("s3: region is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("s3: bucket is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("s3: load aws config: %w", err)
	}
	if cfg.AccessKeyID != "" {
		awsCfg.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	}

	client := awss3.NewFromConfig(awsCfg)
	return &RemoteBucket{
		config:    cfg,
		client:    client,
		presigner: awss3.NewPresignClient(client),
	}, nil
}

type MultipartUploadGrant struct {
	Key       string
	UploadID  string
	ObjectURL string
	PartSize  int64
	Parts     []PresignedPart
}

type FileUpload struct {
	EntryID          string
	ExperimentID     string
	EntityID         string
	OriginalFilename string
	Size             int64
}

type PresignedPart struct {
	PartNumber int32
	URL        string
}

type CompletedPart struct {
	PartNumber int32
	ETag       string
}

func PartPlan(size int64) (partSize int64, partCount int) {
	partSize = int64(multipartMinPartSize)
	if size > partSize*int64(MultipartMaxParts) {
		const mib = 1024 * 1024
		partSize = ((size+int64(MultipartMaxParts)-1)/int64(MultipartMaxParts) + mib - 1) / mib * mib
	}
	partCount = int((size + partSize - 1) / partSize)
	if partCount < 1 {
		partCount = 1
	}
	return partSize, partCount
}

func ObjectKey(file FileUpload) (string, error) {
	entryID, err := requiredKeySegment("entry_id", file.EntryID)
	if err != nil {
		return "", fmt.Errorf("entry id segment: %w", err)
	}
	entityID, err := requiredKeySegment("entity_id", file.EntityID)
	if err != nil {
		return "", fmt.Errorf("entity id segment: %w", err)
	}
	filename, err := originalFilename(file.OriginalFilename)
	if err != nil {
		return "", fmt.Errorf("original filename: %w", err)
	}
	experimentID := strings.TrimSpace(file.ExperimentID)
	if experimentID == "" {
		return path.Join(entryID, "entities", entityID, filename), nil
	}
	experimentID, err = requiredKeySegment("experiment_id", experimentID)
	if err != nil {
		return "", fmt.Errorf("experiment id segment: %w", err)
	}
	return path.Join(entryID, "models", experimentID, "entities", entityID, filename), nil
}

func (b *RemoteBucket) PresignMultipartUpload(ctx context.Context, file FileUpload) (MultipartUploadGrant, error) {
	if file.Size <= 0 {
		return MultipartUploadGrant{}, errors.New("s3: file size must be greater than zero")
	}

	key, err := ObjectKey(file)
	if err != nil {
		return MultipartUploadGrant{}, fmt.Errorf("s3: build object key: %w", err)
	}

	created, err := b.client.CreateMultipartUpload(ctx, &awss3.CreateMultipartUploadInput{
		Bucket: aws.String(b.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return MultipartUploadGrant{}, fmt.Errorf("s3: create multipart upload: %w", err)
	}
	if created.UploadId == nil {
		return MultipartUploadGrant{}, fmt.Errorf("s3: create multipart upload returned no upload id")
	}
	uploadID := *created.UploadId

	partSize, partCount := PartPlan(file.Size)
	parts := make([]PresignedPart, 0, partCount)
	for partNumber := 1; partNumber <= partCount; partNumber++ {
		req, presignErr := b.presigner.PresignUploadPart(ctx, &awss3.UploadPartInput{
			Bucket:     aws.String(b.config.Bucket),
			Key:        aws.String(key),
			UploadId:   aws.String(uploadID),
			PartNumber: aws.Int32(int32(partNumber)),
		}, func(opts *awss3.PresignOptions) {
			opts.Expires = multipartUploadURLTTL
		})
		if presignErr != nil {
			if abortErr := b.AbortMultipartUpload(ctx, key, uploadID); abortErr != nil {
				slog.Error("abort multipart upload after presign failure", "err", abortErr, "key", key, "upload_id", uploadID)
			}
			return MultipartUploadGrant{}, fmt.Errorf("s3: presign upload part %d: %w", partNumber, presignErr)
		}
		parts = append(parts, PresignedPart{PartNumber: int32(partNumber), URL: req.URL})
	}

	return MultipartUploadGrant{
		Key:       key,
		UploadID:  uploadID,
		ObjectURL: b.objectURL(key),
		PartSize:  partSize,
		Parts:     parts,
	}, nil
}

func (b *RemoteBucket) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error {
	ordered := make([]CompletedPart, len(parts))
	copy(ordered, parts)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].PartNumber < ordered[j].PartNumber
	})

	completed := make([]s3types.CompletedPart, 0, len(ordered))
	for _, part := range ordered {
		completed = append(completed, s3types.CompletedPart{
			PartNumber: aws.Int32(part.PartNumber),
			ETag:       aws.String(part.ETag),
		})
	}

	if _, err := b.client.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{
		Bucket:          aws.String(b.config.Bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: completed},
	}); err != nil {
		return fmt.Errorf("s3: complete multipart upload %q: %w", key, err)
	}
	return nil
}

func (b *RemoteBucket) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	if _, err := b.client.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(b.config.Bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	}); err != nil {
		return fmt.Errorf("s3: abort multipart upload %q: %w", key, err)
	}
	return nil
}

func (b *RemoteBucket) objectURL(key string) string {
	return "s3://" + b.config.Bucket + "/" + strings.TrimPrefix(key, "/")
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
