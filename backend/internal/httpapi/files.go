package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
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

var (
	errFileNotFound  = errors.New("file not found")
	errFileAmbiguous = errors.New("file is ambiguous")
)

func (s *Server) GetEntryFile(w http.ResponseWriter, r *http.Request, entryID string) {
	s.writeEntryFile(w, r, entryID)
}

func (s *Server) HeadEntryFile(w http.ResponseWriter, r *http.Request, entryID string) {
	s.writeEntryFile(w, r, entryID)
}

func (s *Server) GetModelFile(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID string,
	filename string,
) {
	s.writeModelFile(w, r, entryID, modelID, filename)
}

func (s *Server) HeadModelFile(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID string,
	filename string,
) {
	s.writeModelFile(w, r, entryID, modelID, filename)
}

func (s *Server) writeEntryFile(
	w http.ResponseWriter,
	r *http.Request,
	entryID string,
) {
	revision, err := s.activeEntryRevision(r.Context(), entryID)
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}
	if err != nil {
		slog.Error("get entry for file failed", "entry_id", entryID, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve file")
		return
	}
	artifacts, err := s.database.Artifacts.List(r.Context(), db.ArtifactFilters{
		EntryRevisionID: &revision.ID,
		Types:           []domainmodels.ArtifactType{domainmodels.ArtifactTypeFASTA},
	})
	if err != nil {
		slog.Error("list entry artifacts for file failed", "entry_id", entryID, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve file")
		return
	}
	artifact, err := selectFileArtifact(artifacts, []string{"fasta"})
	if s.writeFileSelectionError(w, err) {
		return
	}
	s.redirectToArtifact(w, *artifact)
}

func (s *Server) writeModelFile(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID, filename string,
) {
	revision, err := s.activeModelRevision(r.Context(), entryID, modelID)
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}
	if err != nil {
		slog.Error("get model for file failed", "entry_id", entryID, "model_id", modelID, "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve file")
		return
	}

	filters := db.ArtifactFilters{ModelRevisionID: &revision.ID}
	formats := []string{"cif", "mmcif"}
	switch filename {
	case entryID + ".cif":
		if revision.PrimaryArtifactID == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
			return
		}
		filters.ID = revision.PrimaryArtifactID
	case entryID + "-sf.cif":
		filters.Types = []domainmodels.ArtifactType{domainmodels.ArtifactTypeStructureFactors}
		formats = append(formats, "structure_factors_cif")
	default:
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}

	artifacts, err := s.database.Artifacts.List(r.Context(), filters)
	if err != nil {
		slog.Error(
			"list model artifacts for file failed",
			"entry_id", entryID,
			"model_id", modelID,
			"filename", filename,
			"err", err,
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve file")
		return
	}
	artifact, err := selectFileArtifact(artifacts, formats)
	if s.writeFileSelectionError(w, err) {
		return
	}
	s.redirectToArtifact(w, *artifact)
}

func selectFileArtifact(
	artifacts []domainmodels.Artifact,
	formats []string,
) (*domainmodels.Artifact, error) {
	if len(artifacts) == 0 {
		return nil, errFileNotFound
	}
	if len(artifacts) > 1 {
		return nil, errFileAmbiguous
	}
	artifact := artifacts[0]
	if artifact.Format == nil || artifact.URI == nil {
		return nil, errFileNotFound
	}
	format := strings.ToLower(strings.TrimSpace(*artifact.Format))
	validFormat := false
	for _, candidate := range formats {
		if format == candidate {
			validFormat = true
			break
		}
	}
	if !validFormat || strings.TrimSpace(*artifact.URI) == "" {
		return nil, errFileNotFound
	}
	return &artifact, nil
}

func (s *Server) writeFileSelectionError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errFileNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return true
	case errors.Is(err, errFileAmbiguous):
		writeError(w, http.StatusConflict, "CONFLICT", "file is ambiguous")
		return true
	case err != nil:
		slog.Error("select artifact for file failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to resolve file")
		return true
	default:
		return false
	}
}

func (s *Server) redirectToArtifact(w http.ResponseWriter, artifact domainmodels.Artifact) {
	parsedLocation, err := url.Parse(strings.TrimSpace(*artifact.URI))
	if err != nil ||
		(parsedLocation.Scheme != "http" && parsedLocation.Scheme != "https") ||
		parsedLocation.Host == "" {
		slog.Warn("resolved artifact location is invalid", "artifact_id", artifact.ID)
		writeError(w, http.StatusBadGateway, "BAD_GATEWAY", "artifact location could not be resolved")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", parsedLocation.String())
	w.WriteHeader(http.StatusFound)
}
