package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/services/cdn"
)

func (s *Server) CreateFileUpload(w http.ResponseWriter, r *http.Request) {
	var req CreateFileUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if req.Size <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "size must be greater than zero")
		return
	}
	if strings.TrimSpace(req.Filename) == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "filename is required")
		return
	}

	entryID := strings.TrimSpace(req.EntryId)
	if entryID == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "entry_id is required")
		return
	}
	artifactID := req.ArtifactId
	if artifactID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "artifact_id is required")
		return
	}
	modelID := ""
	if req.ModelId != nil {
		modelID = strings.TrimSpace(*req.ModelId)
		if modelID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "model_id is invalid")
			return
		}
	}

	fileUpload := cdn.FileUpload{
		EntryID:          entryID,
		ModelID:          modelID,
		ArtifactID:       artifactID.String(),
		OriginalFilename: req.Filename,
		Size:             req.Size,
	}

	grant, err := s.fileCDN.CreateUpload(r.Context(), fileUpload)
	switch {
	case errors.Is(err, cdn.ErrFileTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "file exceeds maximum upload size")
		return
	case errors.Is(err, cdn.ErrInvalidFileUpload):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid file upload path")
		return
	case err != nil:
		slog.Error("create file upload failed", "err", err, "entry_id", entryID, "model_id", modelID, "artifact_id", artifactID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to presign file upload")
		return
	}

	parts := make([]FileUploadPart, 0, len(grant.Parts))
	for _, part := range grant.Parts {
		parts = append(parts, FileUploadPart{
			PartNumber: int(part.PartNumber),
			Url:        part.URL,
		})
	}
	attributes := FileUploadGrantAttributes{
		Key:       grant.Key,
		UploadId:  grant.UploadID,
		ObjectUrl: grant.ObjectURL,
		PartSize:  grant.PartSize,
		Parts:     parts,
	}
	writeJSON(w, http.StatusOK, FileUploadGrantDocument{
		Data: FileUploadGrantData{
			Type:       jsonAPITypeFileUploads,
			Id:         attributes.UploadId,
			Attributes: attributes,
		},
	})
}

func (s *Server) CompleteFileUpload(w http.ResponseWriter, r *http.Request) {
	var req CompleteFileUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if req.Key == "" || req.UploadId == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "key and upload_id are required")
		return
	}
	if len(req.Parts) == 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "parts must not be empty")
		return
	}

	parts := make([]cdn.CompletedPart, 0, len(req.Parts))
	for _, part := range req.Parts {
		if part.PartNumber < 1 || part.PartNumber > cdn.MultipartMaxParts {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "part_number out of range")
			return
		}
		if part.Etag == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "etag is required for each part")
			return
		}
		parts = append(parts, cdn.CompletedPart{
			PartNumber: int32(part.PartNumber),
			ETag:       part.Etag,
		})
	}

	if err := s.fileCDN.CompleteUpload(r.Context(), req.Key, req.UploadId, parts); err != nil {
		slog.Error("complete file upload failed", "err", err, "key", req.Key)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to complete file upload")
		return
	}
	attributes := CompleteFileUploadAttributes{Key: req.Key}
	writeJSON(w, http.StatusOK, CompleteFileUploadDocument{
		Data: CompleteFileUploadData{
			Type:       jsonAPITypeFileUploads,
			Id:         attributes.Key,
			Attributes: attributes,
		},
	})
}

func (s *Server) AbortFileUpload(w http.ResponseWriter, r *http.Request) {
	var req AbortFileUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if req.Key == "" || req.UploadId == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "key and upload_id are required")
		return
	}

	if err := s.fileCDN.AbortUpload(r.Context(), req.Key, req.UploadId); err != nil {
		slog.Error("abort file upload failed", "err", err, "key", req.Key)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to abort file upload")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
