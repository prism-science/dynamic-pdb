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

	"dynamic-pdb/backend/internal/services/cdn"
)

func Test_should_presign_file_upload_when_create_file_upload_request_is_valid(t *testing.T) {
	// given
	entryID := uuid.New()
	artifactID := uuid.New()
	fileCDN := &uploadCDNStub{
		grant: cdn.UploadGrant{
			Key:       "entry/artifacts/artifact/model.cif",
			UploadID:  "upload-id",
			ObjectURL: "https://files.dynamicpdb.com/entry/artifacts/artifact/model.cif",
			PartSize:  64 * 1024 * 1024,
			Parts: []cdn.UploadPart{
				{PartNumber: 1, URL: "https://storage.example/part-1"},
			},
		},
	}
	server := &Server{
		fileCDN: fileCDN,
	}
	req := uploadJSONRequest(t, map[string]any{
		"entry_id":    entryID,
		"artifact_id": artifactID,
		"filename":    "model.cif",
		"size":        42,
	})
	rec := httptest.NewRecorder()

	// when
	server.CreateFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, entryID.String(), fileCDN.createdFile.EntryID)
	assert.Equal(t, artifactID.String(), fileCDN.createdFile.ArtifactID)
	assert.Equal(t, "model.cif", fileCDN.createdFile.OriginalFilename)
	assert.Equal(t, int64(42), fileCDN.createdFile.Size)

	var body FileUploadGrantResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, fileCDN.grant.Key, body.Key)
	assert.Equal(t, fileCDN.grant.UploadID, body.UploadId)
	assert.Equal(t, "https://files.dynamicpdb.com/entry/artifacts/artifact/model.cif", body.ObjectUrl)
	assert.Len(t, body.Parts, 1)
}

func Test_should_return_legacy_error_shape_when_create_file_upload_request_is_invalid(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"entry_id":    uuid.New(),
		"artifact_id": uuid.New(),
		"filename":    "model.cif",
		"size":        0,
	})
	rec := httptest.NewRecorder()

	// when
	server.CreateFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"code":"BAD_REQUEST","message":"size must be greater than zero"}`, rec.Body.String())
	assert.Zero(t, fileCDN.createCalls)
}

func Test_should_return_400_when_create_file_upload_request_is_invalid(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "zero size",
			body: map[string]any{
				"entry_id":    uuid.New(),
				"artifact_id": uuid.New(),
				"filename":    "model.cif",
				"size":        0,
			},
		},
		{
			name: "empty filename",
			body: map[string]any{
				"entry_id":    uuid.New(),
				"artifact_id": uuid.New(),
				"filename":    " ",
				"size":        1,
			},
		},
		{
			name: "nil entry id",
			body: map[string]any{
				"entry_id":    uuid.Nil,
				"artifact_id": uuid.New(),
				"filename":    "model.cif",
				"size":        1,
			},
		},
		{
			name: "nil artifact id",
			body: map[string]any{
				"entry_id":    uuid.New(),
				"artifact_id": uuid.Nil,
				"filename":    "model.cif",
				"size":        1,
			},
		},
		{
			name: "nil model id pointer",
			body: map[string]any{
				"entry_id":    uuid.New(),
				"artifact_id": uuid.New(),
				"model_id":    uuid.Nil,
				"filename":    "model.cif",
				"size":        1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fileCDN := &uploadCDNStub{}
			server := &Server{fileCDN: fileCDN}
			req := uploadJSONRequest(t, tt.body)
			rec := httptest.NewRecorder()

			// when
			server.CreateFileUpload(rec, req)

			// then
			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Zero(t, fileCDN.createCalls)
		})
	}
}

func Test_should_return_500_when_create_file_upload_service_fails(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{createErr: errors.New("create failed")}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"entry_id":    uuid.New(),
		"artifact_id": uuid.New(),
		"filename":    "model.cif",
		"size":        1,
	})
	rec := httptest.NewRecorder()

	// when
	server.CreateFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, fileCDN.createCalls)
}

func Test_should_return_400_when_cdn_rejects_file_upload(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{createErr: cdn.ErrInvalidFileUpload}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"entry_id":    uuid.New(),
		"artifact_id": uuid.New(),
		"filename":    "model.cif",
		"size":        1,
	})
	rec := httptest.NewRecorder()

	// when
	server.CreateFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, 1, fileCDN.createCalls)
}

func Test_should_return_413_when_file_size_exceeds_upload_limit(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{createErr: cdn.ErrFileTooLarge}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"entry_id":    uuid.New(),
		"artifact_id": uuid.New(),
		"filename":    "model.cif",
		"size":        1 << 30,
	})
	rec := httptest.NewRecorder()

	// when
	server.CreateFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, 1, fileCDN.createCalls)
	assert.Contains(t, rec.Body.String(), `"code":"FILE_TOO_LARGE"`)
}

func Test_should_complete_file_upload_when_complete_request_is_valid(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/artifacts/artifact/model.cif",
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
	assert.Equal(t, "entry/artifacts/artifact/model.cif", fileCDN.completedKey)
	assert.Equal(t, "upload-id", fileCDN.completedUploadID)
	assert.Equal(t, []cdn.CompletedPart{
		{PartNumber: 2, ETag: `"etag-2"`},
		{PartNumber: 1, ETag: `"etag-1"`},
	}, fileCDN.completedParts)
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
				"key":       "entry/artifacts/artifact/model.cif",
				"upload_id": "upload-id",
				"parts":     []map[string]any{},
			},
		},
		{
			name: "part number below range",
			body: map[string]any{
				"key":       "entry/artifacts/artifact/model.cif",
				"upload_id": "upload-id",
				"parts":     []map[string]any{{"part_number": 0, "etag": `"etag-1"`}},
			},
		},
		{
			name: "empty etag",
			body: map[string]any{
				"key":       "entry/artifacts/artifact/model.cif",
				"upload_id": "upload-id",
				"parts":     []map[string]any{{"part_number": 1, "etag": ""}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			fileCDN := &uploadCDNStub{}
			server := &Server{fileCDN: fileCDN}
			req := uploadJSONRequest(t, tt.body)
			rec := httptest.NewRecorder()

			// when
			server.CompleteFileUpload(rec, req)

			// then
			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Zero(t, fileCDN.completeCalls)
		})
	}
}

func Test_should_return_500_when_complete_file_upload_fails(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{completeErr: errors.New("complete failed")}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/artifacts/artifact/model.cif",
		"upload_id": "upload-id",
		"parts":     []map[string]any{{"part_number": 1, "etag": `"etag-1"`}},
	})
	rec := httptest.NewRecorder()

	// when
	server.CompleteFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, fileCDN.completeCalls)
}

func Test_should_abort_file_upload_when_abort_request_is_valid(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/artifacts/artifact/model.cif",
		"upload_id": "upload-id",
	})
	rec := httptest.NewRecorder()

	// when
	server.AbortFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "entry/artifacts/artifact/model.cif", fileCDN.abortedKey)
	assert.Equal(t, "upload-id", fileCDN.abortedUploadID)
}

func Test_should_return_500_when_abort_file_upload_fails(t *testing.T) {
	// given
	fileCDN := &uploadCDNStub{abortErr: errors.New("abort failed")}
	server := &Server{fileCDN: fileCDN}
	req := uploadJSONRequest(t, map[string]any{
		"key":       "entry/artifacts/artifact/model.cif",
		"upload_id": "upload-id",
	})
	rec := httptest.NewRecorder()

	// when
	server.AbortFileUpload(rec, req)

	// then
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, fileCDN.abortCalls)
}

func uploadJSONRequest(t *testing.T, body map[string]any) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))
	req := httptest.NewRequest(http.MethodPost, "/v1/files", &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

type uploadCDNStub struct {
	grant       cdn.UploadGrant
	createErr   error
	completeErr error
	abortErr    error

	createCalls int
	createdFile cdn.FileUpload

	completeCalls     int
	completedKey      string
	completedUploadID string
	completedParts    []cdn.CompletedPart

	abortCalls      int
	abortedKey      string
	abortedUploadID string
}

func (s *uploadCDNStub) CreateUpload(_ context.Context, file cdn.FileUpload) (cdn.UploadGrant, error) {
	s.createCalls++
	s.createdFile = file
	if s.createErr != nil {
		return cdn.UploadGrant{}, s.createErr
	}
	return s.grant, nil
}

func (s *uploadCDNStub) CompleteUpload(_ context.Context, key, uploadID string, parts []cdn.CompletedPart) error {
	s.completeCalls++
	s.completedKey = key
	s.completedUploadID = uploadID
	s.completedParts = parts
	return s.completeErr
}

func (s *uploadCDNStub) AbortUpload(_ context.Context, key, uploadID string) error {
	s.abortCalls++
	s.abortedKey = key
	s.abortedUploadID = uploadID
	return s.abortErr
}

var _ cdn.Service = (*uploadCDNStub)(nil)
