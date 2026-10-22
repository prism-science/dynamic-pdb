package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	multipartMinPartSize = 64 * 1024 * 1024
	MultipartMaxParts    = 10_000
)

type Bucket interface {
	PresignMultipartUpload(ctx context.Context, key string, size int64) (MultipartUploadGrant, error)
	CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(ctx context.Context, key, uploadID string) error
	// ReadObjectHead reads at most limit bytes from the start of an object with a ranged GET.
	ReadObjectHead(ctx context.Context, key string, limit int64) (ObjectHead, error)
}

// ErrObjectNotFound is returned when the object does not exist.
var ErrObjectNotFound = errors.New("s3: object not found")

// ObjectHead is the first bytes of an object and the object's full size.
type ObjectHead struct {
	Data      []byte
	TotalSize int64
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
	if cfg.UploadMaxFileSize <= 0 {
		return nil, fmt.Errorf("s3: upload max file size must be positive")
	}
	if cfg.UploadURLTTL <= 0 {
		return nil, fmt.Errorf("s3: upload URL TTL must be positive")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("s3: load aws config: %w", err)
	}
	if cfg.AccessKeyID != "" {
		awsCfg.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	}

	clientOptions := []func(*awss3.Options){}
	if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
		clientOptions = append(clientOptions, func(opts *awss3.Options) {
			opts.BaseEndpoint = aws.String(endpoint)
			opts.UsePathStyle = true
		})
	}

	client := awss3.NewFromConfig(awsCfg, clientOptions...)
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
	if size <= 0 {
		return partSize, 1
	}
	if size > partSize*int64(MultipartMaxParts) {
		const mib = 1024 * 1024
		minimumPartSize := (size-1)/int64(MultipartMaxParts) + 1
		partSize = ((minimumPartSize-1)/mib + 1) * mib
	}
	partCount = int((size-1)/partSize + 1)
	return partSize, partCount
}

func (b *RemoteBucket) PresignMultipartUpload(ctx context.Context, key string, size int64) (MultipartUploadGrant, error) {
	if strings.TrimSpace(key) == "" {
		return MultipartUploadGrant{}, errors.New("s3: object key is required")
	}
	if size <= 0 {
		return MultipartUploadGrant{}, errors.New("s3: file size must be greater than zero")
	}
	if size > b.config.UploadMaxFileSize {
		return MultipartUploadGrant{}, fmt.Errorf(
			"s3: file size %d exceeds configured maximum %d",
			size,
			b.config.UploadMaxFileSize,
		)
	}

	objectURL, err := b.objectURL(key)
	if err != nil {
		return MultipartUploadGrant{}, fmt.Errorf("s3: build object url: %w", err)
	}

	created, err := b.client.CreateMultipartUpload(ctx, &awss3.CreateMultipartUploadInput{
		Bucket:      aws.String(b.config.Bucket),
		Key:         aws.String(key),
		ContentType: aws.String(objectContentType(key)),
	})
	if err != nil {
		return MultipartUploadGrant{}, fmt.Errorf("s3: create multipart upload: %w", err)
	}
	if created.UploadId == nil {
		return MultipartUploadGrant{}, fmt.Errorf("s3: create multipart upload returned no upload id")
	}
	uploadID := *created.UploadId

	partSize, partCount := PartPlan(size)
	parts := make([]PresignedPart, 0, partCount)
	partNumber := int32(1)
	for range partCount {
		req, presignErr := b.presignUploadPart(ctx, key, uploadID, size, partSize, partNumber)
		if presignErr != nil {
			if abortErr := b.AbortMultipartUpload(ctx, key, uploadID); abortErr != nil {
				slog.Error("abort multipart upload after presign failure", "err", abortErr, "key", key, "upload_id", uploadID)
			}
			return MultipartUploadGrant{}, fmt.Errorf("s3: presign upload part %d: %w", partNumber, presignErr)
		}
		parts = append(parts, PresignedPart{PartNumber: partNumber, URL: req.URL})
		partNumber++
	}

	return MultipartUploadGrant{
		Key:       key,
		UploadID:  uploadID,
		ObjectURL: objectURL,
		PartSize:  partSize,
		Parts:     parts,
	}, nil
}

func (b *RemoteBucket) presignUploadPart(
	ctx context.Context,
	key, uploadID string,
	fileSize, partSize int64,
	partNumber int32,
) (*v4.PresignedHTTPRequest, error) {
	return b.presigner.PresignUploadPart(ctx, uploadPartInput(
		b.config.Bucket,
		key,
		uploadID,
		fileSize,
		partSize,
		partNumber,
	), func(opts *awss3.PresignOptions) {
		opts.Expires = b.config.UploadURLTTL
	})
}

func uploadPartInput(bucket, key, uploadID string, fileSize, partSize int64, partNumber int32) *awss3.UploadPartInput {
	contentLength := partSize
	if remaining := fileSize - int64(partNumber-1)*partSize; remaining < contentLength {
		contentLength = remaining
	}
	return &awss3.UploadPartInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		UploadId:      aws.String(uploadID),
		PartNumber:    aws.Int32(partNumber),
		ContentLength: aws.Int64(contentLength),
	}
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
		IfNoneMatch:     aws.String("*"),
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

func objectContentType(filename string) string {
	if contentType := mime.TypeByExtension(path.Ext(filename)); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

func (b *RemoteBucket) objectURL(key string) (string, error) {
	trimmedKey := strings.TrimPrefix(key, "/")
	if endpoint := strings.TrimSpace(b.config.Endpoint); endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", fmt.Errorf("parse endpoint: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return "", errors.New("endpoint must include scheme and host")
		}
		parsed.Path = path.Join(parsed.Path, b.config.Bucket, trimmedKey)
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String(), nil
	}

	parsed := url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s.s3.%s.amazonaws.com", b.config.Bucket, b.config.Region),
		Path:   "/" + trimmedKey,
	}
	return parsed.String(), nil
}

func (b *RemoteBucket) ReadObjectHead(ctx context.Context, key string, limit int64) (ObjectHead, error) {
	if strings.TrimSpace(key) == "" {
		return ObjectHead{}, errors.New("s3: object key is required")
	}
	if limit <= 0 {
		return ObjectHead{}, errors.New("s3: read limit must be greater than zero")
	}
	output, err := b.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(b.config.Bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", limit-1)),
	})
	if err != nil {
		var missing *s3types.NoSuchKey
		if errors.As(err, &missing) {
			return ObjectHead{}, fmt.Errorf("s3: read object head %q: %w", key, ErrObjectNotFound)
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidRange" {
			return ObjectHead{Data: []byte{}}, nil
		}
		return ObjectHead{}, fmt.Errorf("s3: read object head %q: %w", key, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(output.Body, limit))
	closeErr := output.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return ObjectHead{}, fmt.Errorf("s3: read object body %q: %w", key, err)
	}
	return ObjectHead{Data: data, TotalSize: objectTotalSize(aws.ToString(output.ContentRange), int64(len(data)))}, nil
}

// objectTotalSize reads the size after the slash of a Content-Range header such
// as "bytes 0-99/1234"; without one the whole object was returned.
func objectTotalSize(contentRange string, returned int64) int64 {
	_, total, found := strings.Cut(contentRange, "/")
	if !found {
		return returned
	}
	size, err := strconv.ParseInt(total, 10, 64)
	if err != nil || size < returned {
		return returned
	}
	return size
}
