package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/integrations/s3"
)

func Test_should_presign_file_upload_when_create_file_upload_request_is_valid(t *testing.T) {
	// given
	entryID := uuid.New()
	entityID := uuid.New()
	bucket := &uploadBucketStub{
		grant: s3.MultipartUploadGrant{
			Key:       "entry/entities/entity/model.cif",
			UploadID:  "upload-id",
			ObjectURL: "s3://dynamic-pdb/entry/entities/entity/model.cif",
			PartSize:  64 * 1024 * 1024,
			Parts: []s3.PresignedPart{
				{PartNumber: 1, URL: "https://storage.example/part-1"},
			},
		},
	}
	server := &Server{fileUploadBucket: bucket}
	req := uploadJSONRequest(t, map[string]any{
		"entry_id":  entryID,
		"entity_id": entityID,
		"filename":  "model.cif",
		"size":      42,
	})
	rec := httptest.NewRecorder()

	// when
	server.CreateFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, entryID.String(), bucket.presignedFile.EntryID)
	assert.Equal(t, entityID.String(), bucket.presignedFile.EntityID)
	assert.Equal(t, "model.cif", bucket.presignedFile.OriginalFilename)
	assert.Equal(t, int64(42), bucket.presignedFile.Size)

	var body FileUploadGrantResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, bucket.grant.Key, body.Key)
	assert.Equal(t, bucket.grant.UploadID, body.UploadId)
	assert.Len(t, body.Parts, 1)
}

func Test_should_return_400_when_create_file_upload_request_is_invalid(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "zero size",
			body: map[string]any{
				"entry_id":  uuid.New(),
				"entity_id": uuid.New(),
				"filename":  "model.cif",
				"size":      0,
			},
		},
		{
			name: "empty filename",
			body: map[string]any{
				"entry_id":  uuid.New(),
				"entity_id": uuid.New(),
				"filename":  " ",
				"size":      1,
			},
		},
		{
			name: "nil entry id",
			body: map[string]any{
				"entry_id":  uuid.Nil,
				"entity_id": uuid.New(),
				"filename":  "model.cif",
				"size":      1,
			},
		},
		{
			name: "nil entity id",
			body: map[string]any{
				"entry_id":  uuid.New(),
				"entity_id": uuid.Nil,
				"filename":  "model.cif",
				"size":      1,
			},
		},
		{
			name: "nil model id pointer",
			body: map[string]any{
				"entry_id":  uuid.New(),
				"entity_id": uuid.New(),
				"model_id":  uuid.Nil,
				"filename":  "model.cif",
				"size":      1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			bucket := &uploadBucketStub{}
			server := &Server{fileUploadBucket: bucket}
			req := uploadJSONRequest(t, tt.body)
			rec := httptest.NewRecorder()

			// when
			server.CreateFileUpload(rec, req)

			// then
			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Zero(t, bucket.presignCalls)
		})
	}
}

func Test_should_return_500_when_create_file_upload_presign_fails(t *testing.T) {
	// given
	bucket := &uploadBucketStub{presignErr: errors.New("presign failed")}
	server := &Server{fileUploadBucket: bucket}
	req := uploadJSONRequest(t, map[string]any{
		"entry_id":  uuid.New(),
		"entity_id": uuid.New(),
		"filename":  "model.cif",
		"size":      1,
	})
	rec := httptest.NewRecorder()

	// when
	server.CreateFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, bucket.presignCalls)
}

func Test_should_complete_file_upload_when_complete_request_is_valid(t *testing.T) {
	// given
	bucket := &uploadBucketStub{}
	server := &Server{fileUploadBucket: bucket}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/entities/entity/model.cif",
		"upload_id": "upload-id",
		"parts": []map[string]any{
			{"part_number": 2, "etag": `"etag-2"`},
			{"part_number": 1, "etag": `"etag-1"`},
		},
	})
	rec := httptest.NewRecorder()

	// when
	server.CompleteFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "entry/entities/entity/model.cif", bucket.completedKey)
	assert.Equal(t, "upload-id", bucket.completedUploadID)
	assert.Equal(t, []s3.CompletedPart{
		{PartNumber: 2, ETag: `"etag-2"`},
		{PartNumber: 1, ETag: `"etag-1"`},
	}, bucket.completedParts)
}

func Test_should_return_400_when_complete_file_upload_request_is_invalid(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "missing key",
			body: map[string]any{
				"key":       "",
				"upload_id": "upload-id",
				"parts":     []map[string]any{{"part_number": 1, "etag": `"etag-1"`}},
			},
		},
		{
			name: "no parts",
			body: map[string]any{
				"key":       "entry/entities/entity/model.cif",
				"upload_id": "upload-id",
				"parts":     []map[string]any{},
			},
		},
		{
			name: "part number below range",
			body: map[string]any{
				"key":       "entry/entities/entity/model.cif",
				"upload_id": "upload-id",
				"parts":     []map[string]any{{"part_number": 0, "etag": `"etag-1"`}},
			},
		},
		{
			name: "empty etag",
			body: map[string]any{
				"key":       "entry/entities/entity/model.cif",
				"upload_id": "upload-id",
				"parts":     []map[string]any{{"part_number": 1, "etag": ""}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			bucket := &uploadBucketStub{}
			server := &Server{fileUploadBucket: bucket}
			req := uploadJSONRequest(t, tt.body)
			rec := httptest.NewRecorder()

			// when
			server.CompleteFileUpload(rec, req)

			// then
			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Zero(t, bucket.completeCalls)
		})
	}
}

func Test_should_return_500_when_complete_file_upload_fails(t *testing.T) {
	// given
	bucket := &uploadBucketStub{completeErr: errors.New("complete failed")}
	server := &Server{fileUploadBucket: bucket}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/entities/entity/model.cif",
		"upload_id": "upload-id",
		"parts":     []map[string]any{{"part_number": 1, "etag": `"etag-1"`}},
	})
	rec := httptest.NewRecorder()

	// when
	server.CompleteFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, bucket.completeCalls)
}

func Test_should_abort_file_upload_when_abort_request_is_valid(t *testing.T) {
	// given
	bucket := &uploadBucketStub{}
	server := &Server{fileUploadBucket: bucket}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/entities/entity/model.cif",
		"upload_id": "upload-id",
	})
	rec := httptest.NewRecorder()

	// when
	server.AbortFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "entry/entities/entity/model.cif", bucket.abortedKey)
	assert.Equal(t, "upload-id", bucket.abortedUploadID)
}

func Test_should_return_500_when_abort_file_upload_fails(t *testing.T) {
	// given
	bucket := &uploadBucketStub{abortErr: errors.New("abort failed")}
	server := &Server{fileUploadBucket: bucket}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/entities/entity/model.cif",
		"upload_id": "upload-id",
	})
	rec := httptest.NewRecorder()

	// when
	server.AbortFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, bucket.abortCalls)
}

func uploadJSONRequest(t *testing.T, body map[string]any) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))
	req := httptest.NewRequest(http.MethodPost, "/v1/files", &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

type uploadBucketStub struct {
	grant       s3.MultipartUploadGrant
	presignErr  error
	completeErr error
	abortErr    error

	presignCalls  int
	presignedFile s3.FileUpload

	completeCalls     int
	completedKey      string
	completedUploadID string
	completedParts    []s3.CompletedPart

	abortCalls      int
	abortedKey      string
	abortedUploadID string
}

func (b *uploadBucketStub) PresignMultipartUpload(_ context.Context, file s3.FileUpload) (s3.MultipartUploadGrant, error) {
	b.presignCalls++
	b.presignedFile = file
	if b.presignErr != nil {
		return s3.MultipartUploadGrant{}, b.presignErr
	}
	return b.grant, nil
}

func (b *uploadBucketStub) CompleteMultipartUpload(_ context.Context, key, uploadID string, parts []s3.CompletedPart) error {
	b.completeCalls++
	b.completedKey = key
	b.completedUploadID = uploadID
	b.completedParts = parts
	return b.completeErr
}

func (b *uploadBucketStub) AbortMultipartUpload(_ context.Context, key, uploadID string) error {
	b.abortCalls++
	b.abortedKey = key
	b.abortedUploadID = uploadID
	return b.abortErr
}
