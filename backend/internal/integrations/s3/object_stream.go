package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// objectWriterPartSize is the part size of uploads the backend streams itself. S3 needs parts
// of at least 5 MiB except the last; 16 MiB keeps a 1 GiB file to 64 parts and a writer small.
const objectWriterPartSize = 16 * 1024 * 1024

// OpenObject streams a whole object. The caller closes the returned reader.
func (b *RemoteBucket) OpenObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("s3: object key is required")
	}
	output, err := b.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(b.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var missing *s3types.NoSuchKey
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("s3: open object %q: %w", key, ErrObjectNotFound)
		}
		return nil, fmt.Errorf("s3: open object %q: %w", key, err)
	}
	return output.Body, nil
}

func (b *RemoteBucket) ObjectURL(key string) (string, error) {
	return b.objectURL(key)
}

// ObjectWriter uploads one object from a stream. Full parts are sent as bytes arrive, and the
// object appears in the bucket only on Complete; Abort discards what was sent. An object
// already at the key is replaced. It keeps the context it was created with, because Write
// cannot take one.
type ObjectWriter struct {
	ctx      context.Context
	bucket   *RemoteBucket
	key      string
	partSize int
	buffer   []byte
	uploadID string
	lastPart int32
	parts    []s3types.CompletedPart
	finished bool
}

// NewObjectWriter starts an upload to key. Nothing is sent until the first part is full.
func (b *RemoteBucket) NewObjectWriter(ctx context.Context, key string) (*ObjectWriter, error) {
	return b.newObjectWriter(ctx, key, objectWriterPartSize)
}

func (b *RemoteBucket) newObjectWriter(ctx context.Context, key string, partSize int) (*ObjectWriter, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("s3: object key is required")
	}
	return &ObjectWriter{
		ctx:      ctx,
		bucket:   b,
		key:      key,
		partSize: partSize,
		buffer:   make([]byte, 0, partSize),
	}, nil
}

func (w *ObjectWriter) Write(data []byte) (int, error) {
	if w.finished {
		return 0, fmt.Errorf("s3: write to finished upload %q", w.key)
	}
	written := 0
	for len(data) > 0 {
		room := w.partSize - len(w.buffer)
		chunk := min(room, len(data))
		w.buffer = append(w.buffer, data[:chunk]...)
		data = data[chunk:]
		written += chunk
		if len(w.buffer) == w.partSize {
			if err := w.sendPart(); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

// Complete stores the object. A stream shorter than one part is stored with one PutObject.
func (w *ObjectWriter) Complete() error {
	if w.finished {
		return fmt.Errorf("s3: complete finished upload %q", w.key)
	}
	w.finished = true
	if w.uploadID == "" {
		if _, err := w.bucket.client.PutObject(w.ctx, &awss3.PutObjectInput{
			Bucket:      aws.String(w.bucket.config.Bucket),
			Key:         aws.String(w.key),
			ContentType: aws.String(objectContentType(w.key)),
			Body:        bytes.NewReader(w.buffer),
		}); err != nil {
			return fmt.Errorf("s3: put object %q: %w", w.key, err)
		}
		return nil
	}
	if len(w.buffer) > 0 {
		if err := w.sendPart(); err != nil {
			return errors.Join(err, w.abortMultipart())
		}
	}
	if _, err := w.bucket.client.CompleteMultipartUpload(w.ctx, &awss3.CompleteMultipartUploadInput{
		Bucket:          aws.String(w.bucket.config.Bucket),
		Key:             aws.String(w.key),
		UploadId:        aws.String(w.uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: w.parts},
	}); err != nil {
		return errors.Join(fmt.Errorf("s3: complete multipart upload %q: %w", w.key, err), w.abortMultipart())
	}
	return nil
}

// Abort discards the upload. It does nothing after Complete or a previous Abort.
func (w *ObjectWriter) Abort() error {
	if w.finished {
		return nil
	}
	w.finished = true
	return w.abortMultipart()
}

func (w *ObjectWriter) sendPart() error {
	if w.uploadID == "" {
		created, err := w.bucket.client.CreateMultipartUpload(w.ctx, &awss3.CreateMultipartUploadInput{
			Bucket:      aws.String(w.bucket.config.Bucket),
			Key:         aws.String(w.key),
			ContentType: aws.String(objectContentType(w.key)),
		})
		if err != nil {
			return fmt.Errorf("s3: create multipart upload %q: %w", w.key, err)
		}
		if created.UploadId == nil {
			return fmt.Errorf("s3: create multipart upload %q returned no upload id", w.key)
		}
		w.uploadID = *created.UploadId
	}
	if w.lastPart >= MultipartMaxParts {
		return fmt.Errorf("s3: upload %q needs more than %d parts", w.key, MultipartMaxParts)
	}
	partNumber := w.lastPart + 1
	uploaded, err := w.bucket.client.UploadPart(w.ctx, &awss3.UploadPartInput{
		Bucket:     aws.String(w.bucket.config.Bucket),
		Key:        aws.String(w.key),
		UploadId:   aws.String(w.uploadID),
		PartNumber: aws.Int32(partNumber),
		Body:       bytes.NewReader(w.buffer),
	})
	if err != nil {
		return fmt.Errorf("s3: upload part %d of %q: %w", partNumber, w.key, err)
	}
	w.parts = append(w.parts, s3types.CompletedPart{PartNumber: aws.Int32(partNumber), ETag: uploaded.ETag})
	w.lastPart = partNumber
	w.buffer = w.buffer[:0]
	return nil
}

func (w *ObjectWriter) abortMultipart() error {
	if w.uploadID == "" {
		return nil
	}
	if err := w.bucket.AbortMultipartUpload(w.ctx, w.key, w.uploadID); err != nil {
		return fmt.Errorf("s3: discard upload of %q: %w", w.key, err)
	}
	return nil
}
