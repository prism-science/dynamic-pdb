package e2etest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

type UploadsSuite struct {
	baseSuite
}

func TestUploads(t *testing.T) {
	suite.Run(t, new(UploadsSuite))
}

func (s *UploadsSuite) Test_should_create_complete_and_abort_file_upload_when_requests_are_valid() {
	// given
	token := issueEntryTokenForTest(s.T(), "upload-token")
	entryID := "entry-" + uuid.NewString()
	modelID := "model-" + uuid.NewString()
	artifactID := uuid.New()
	fileSize := int64(64*1024*1024 + 1)

	// when
	createResp := postJSONWithToken(s.T(), "/v1/files", map[string]any{
		"entry_id":    entryID,
		"model_id":    modelID,
		"artifact_id": artifactID,
		"filename":    "model.cif",
		"size":        fileSize,
	}, token)
	defer createResp.Body.Close()

	// then
	s.Equal(http.StatusOK, createResp.StatusCode)

	var grantDocument httpapi.FileUploadGrantDocument
	s.Require().NoError(json.NewDecoder(createResp.Body).Decode(&grantDocument))
	grant := grantDocument.Data.Attributes
	s.Equal(
		entryID+"/models/"+modelID+"/artifacts/"+artifactID.String()+"/model.cif",
		grant.Key,
	)
	s.Equal("upload-id", grant.UploadId)
	s.Equal(s3Stub.URL()+"/dynamic-pdb/"+grant.Key, grant.ObjectUrl)
	s.Equal(int64(64*1024*1024), grant.PartSize)
	s.Len(grant.Parts, 2)
	s.Equal(1, grant.Parts[0].PartNumber)
	s.Contains(grant.Parts[0].Url, s3Stub.URL())
	s.Contains(grant.Parts[0].Url, "partNumber=1")
	s.Contains(grant.Parts[0].Url, "uploadId=upload-id")
	s.Equal(2, grant.Parts[1].PartNumber)
	s.Contains(grant.Parts[1].Url, "partNumber=2")
	requests := s3Stub.Requests()
	s.Equal("/dynamic-pdb/"+grant.Key, requests.CreatePath)

	// when
	completeResp := postJSONWithToken(s.T(), "/v1/files/complete", map[string]any{
		"key":       grant.Key,
		"upload_id": grant.UploadId,
		"parts": []map[string]any{
			{"part_number": 2, "etag": `"etag-2"`},
			{"part_number": 1, "etag": `"etag-1"`},
		},
	}, token)
	defer completeResp.Body.Close()

	// then
	s.Equal(http.StatusOK, completeResp.StatusCode)

	var completeDocument httpapi.CompleteFileUploadDocument
	s.Require().NoError(json.NewDecoder(completeResp.Body).Decode(&completeDocument))
	completeBody := completeDocument.Data.Attributes
	s.Equal(grant.Key, completeBody.Key)
	requests = s3Stub.Requests()
	s.Equal("/dynamic-pdb/"+grant.Key, requests.CompletePath)
	s.Equal(grant.UploadId, requests.CompleteUploadID)
	s.Equal("*", requests.IfNoneMatch)
	s.Contains(requests.CompleteBody, "<PartNumber>1</PartNumber>")
	s.Contains(requests.CompleteBody, "<PartNumber>2</PartNumber>")
	s.Less(strings.Index(requests.CompleteBody, "<PartNumber>1</PartNumber>"), strings.Index(requests.CompleteBody, "<PartNumber>2</PartNumber>"))
	s.Contains(requests.CompleteBody, "<ETag>&#34;etag-1&#34;</ETag>")
	s.Contains(requests.CompleteBody, "<ETag>&#34;etag-2&#34;</ETag>")

	// when
	abortResp := postJSONWithToken(s.T(), "/v1/files/abort", map[string]any{
		"key":       grant.Key,
		"upload_id": grant.UploadId,
	}, token)
	defer abortResp.Body.Close()

	// then
	s.Equal(http.StatusNoContent, abortResp.StatusCode)
	body, err := io.ReadAll(abortResp.Body)
	s.Require().NoError(err)
	s.Empty(body)
	requests = s3Stub.Requests()
	s.Equal("/dynamic-pdb/"+grant.Key, requests.AbortPath)
	s.Equal(grant.UploadId, requests.AbortUploadID)
}

func (s *UploadsSuite) Test_should_return_500_when_s3_create_multipart_upload_returns_no_upload_id() {
	// given
	token := issueEntryTokenForTest(s.T(), "upload-no-id-token")
	s3Stub.ReturnNoUploadID()

	// when
	resp := postJSONWithToken(s.T(), "/v1/files", map[string]any{
		"entry_id":    uuid.New(),
		"artifact_id": uuid.New(),
		"filename":    "model.cif",
		"size":        1,
	}, token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusInternalServerError, resp.StatusCode)
}

func (s *UploadsSuite) Test_should_return_500_when_s3_abort_multipart_upload_fails() {
	// given
	token := issueEntryTokenForTest(s.T(), "upload-abort-failure-token")
	s3Stub.ReturnAbortStatus(http.StatusInternalServerError)

	// when
	resp := postJSONWithToken(s.T(), "/v1/files/abort", map[string]any{
		"key":       "entry/artifacts/artifact/file.cif",
		"upload_id": "upload-id",
	}, token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusInternalServerError, resp.StatusCode)
}

func (s *UploadsSuite) Test_should_return_400_when_file_upload_requests_are_invalid() {
	// given
	token := issueEntryTokenForTest(s.T(), "upload-validation-token")

	tests := []struct {
		name string
		path string
		body map[string]any
	}{
		{
			name: "create with empty filename",
			path: "/v1/files",
			body: map[string]any{
				"entry_id":    uuid.New(),
				"artifact_id": uuid.New(),
				"filename":    "  ",
				"size":        1,
			},
		},
		{
			name: "create with zero size",
			path: "/v1/files",
			body: map[string]any{
				"entry_id":    uuid.New(),
				"artifact_id": uuid.New(),
				"filename":    "data.fasta",
				"size":        0,
			},
		},
		{
			name: "create with missing artifact id",
			path: "/v1/files",
			body: map[string]any{
				"entry_id": uuid.New(),
				"filename": "data.fasta",
				"size":     1,
			},
		},
		{
			name: "complete with no parts",
			path: "/v1/files/complete",
			body: map[string]any{
				"key":       "entry/artifacts/artifact/file.cif",
				"upload_id": "upload-id",
				"parts":     []map[string]any{},
			},
		},
		{
			name: "complete with empty etag",
			path: "/v1/files/complete",
			body: map[string]any{
				"key":       "entry/artifacts/artifact/file.cif",
				"upload_id": "upload-id",
				"parts": []map[string]any{
					{"part_number": 1, "etag": ""},
				},
			},
		},
		{
			name: "abort with missing upload id",
			path: "/v1/files/abort",
			body: map[string]any{
				"key":       "entry/entities/entity/file.cif",
				"upload_id": "",
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// when
			resp := postJSONWithToken(s.T(), tt.path, tt.body, token)
			defer resp.Body.Close()

			// then
			s.Equal(http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func (s *UploadsSuite) Test_should_return_401_when_file_upload_called_without_token() {
	// given

	// when
	resp := postJSON(s.T(), "/v1/files", map[string]any{
		"entry_id":    uuid.New(),
		"artifact_id": uuid.New(),
		"filename":    "data.fasta",
		"size":        1,
	})
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}
