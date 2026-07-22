package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/integrations/s3"
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

	entryID := req.EntryId
	if entryID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "entry_id is required")
		return
	}
	entityID := req.EntityId
	if entityID == uuid.Nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "entity_id is required")
		return
	}
	modelID := ""
	if req.ModelId != nil {
		parsedModelID := *req.ModelId
		if parsedModelID == uuid.Nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "model_id is invalid")
			return
		}
		modelID = parsedModelID.String()
	}

	fileUpload := s3.FileUpload{
		EntryID:          entryID.String(),
		ModelID:          modelID,
		EntityID:         entityID.String(),
		OriginalFilename: req.Filename,
		Size:             req.Size,
	}
	if _, err := s3.ObjectKey(fileUpload); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid file upload path")
		return
	}

	grant, err := s.fileUploadBucket.PresignMultipartUpload(r.Context(), fileUpload)
	if err != nil {
		slog.Error("presign file upload failed", "err", err, "entry_id", entryID, "model_id", modelID, "entity_id", entityID)
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
	writeJSON(w, http.StatusOK, FileUploadGrantResponse{
		Key:       grant.Key,
		UploadId:  grant.UploadID,
		ObjectUrl: grant.ObjectURL,
		PartSize:  grant.PartSize,
		Parts:     parts,
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

	parts := make([]s3.CompletedPart, 0, len(req.Parts))
	for _, part := range req.Parts {
		if part.PartNumber < 1 || part.PartNumber > s3.MultipartMaxParts {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "part_number out of range")
			return
		}
		if part.Etag == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "etag is required for each part")
			return
		}
		parts = append(parts, s3.CompletedPart{
			PartNumber: int32(part.PartNumber),
			ETag:       part.Etag,
		})
	}

	if err := s.fileUploadBucket.CompleteMultipartUpload(r.Context(), req.Key, req.UploadId, parts); err != nil {
		slog.Error("complete file upload failed", "err", err, "key", req.Key)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to complete file upload")
		return
	}
	writeJSON(w, http.StatusOK, CompleteFileUploadResponse{Key: req.Key})
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

	if err := s.fileUploadBucket.AbortMultipartUpload(r.Context(), req.Key, req.UploadId); err != nil {
		slog.Error("abort file upload failed", "err", err, "key", req.Key)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to abort file upload")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
