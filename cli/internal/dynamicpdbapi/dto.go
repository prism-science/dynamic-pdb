package dynamicpdbapi

import (
	"fmt"
	"time"
)

type TokenResponse struct {
	TokenType   string    `json:"token_type"`
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Login       string    `json:"login"`
}

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("dynamicpdbapi: backend %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("dynamicpdbapi: backend %d: %s: %s", e.Status, e.Code, e.Message)
}

type ListEntriesParams struct {
	PDBIDs []string
}

type Entry struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type entryListResponse struct {
	Items []Entry `json:"items"`
}

type CreateEntryRequest struct {
	Entry           CreateEntryData     `json:"entry"`
	ModelOperations []AddModelOperation `json:"model_operations,omitempty"`
}

type CreateEntryData struct {
	ID                *string                 `json:"id,omitempty"`
	Name              string                  `json:"name"`
	Description       *string                 `json:"description,omitempty"`
	ThumbnailImageURL *string                 `json:"thumbnail_image_url,omitempty"`
	Metadata          map[string]any          `json:"metadata,omitempty"`
	Artifacts         []CreateArtifactRequest `json:"artifacts,omitempty"`
}

type AddModelOperation struct {
	Op   string       `json:"op"`
	Data AddModelData `json:"data"`
}

type AddModelData struct {
	ModelID           *string                 `json:"model_id,omitempty"`
	Name              string                  `json:"name"`
	Description       *string                 `json:"description,omitempty"`
	ThumbnailImageURL *string                 `json:"thumbnail_image_url,omitempty"`
	Metadata          map[string]any          `json:"metadata,omitempty"`
	PrimaryArtifactID *string                 `json:"primary_artifact_id,omitempty"`
	Artifacts         []CreateArtifactRequest `json:"artifacts,omitempty"`
	Runs              []CreateRunRequest      `json:"runs,omitempty"`
	Metrics           []CreateMetricRequest   `json:"metrics,omitempty"`
}

type CreateModelRequest struct {
	Model CreateModelData `json:"model"`
}

type CreateModelData struct {
	ID                *string                 `json:"id,omitempty"`
	Name              string                  `json:"name"`
	Description       *string                 `json:"description,omitempty"`
	ThumbnailImageURL *string                 `json:"thumbnail_image_url,omitempty"`
	Metadata          map[string]any          `json:"metadata,omitempty"`
	PrimaryArtifactID *string                 `json:"primary_artifact_id,omitempty"`
	Artifacts         []CreateArtifactRequest `json:"artifacts,omitempty"`
	Runs              []CreateRunRequest      `json:"runs,omitempty"`
	Metrics           []CreateMetricRequest   `json:"metrics,omitempty"`
}

type CreateArtifactRequest struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Level     string         `json:"level"`
	URI       *string        `json:"uri,omitempty"`
	SHA256    *string        `json:"sha256,omitempty"`
	Format    *string        `json:"format,omitempty"`
	SizeBytes *int64         `json:"size_bytes,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type CreateMetricRequest struct {
	ID    string  `json:"id"`
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

type CreateRunRequest struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	SoftwareName    *string                    `json:"software_name,omitempty"`
	SoftwareVersion *string                    `json:"software_version,omitempty"`
	Command         *string                    `json:"command,omitempty"`
	Parameters      map[string]any             `json:"parameters,omitempty"`
	Metadata        map[string]any             `json:"metadata,omitempty"`
	Artifacts       []CreateRunArtifactRequest `json:"artifacts,omitempty"`
}

type CreateRunArtifactRequest struct {
	ArtifactID string `json:"artifact_id"`
	Direction  string `json:"direction"`
	Position   *int   `json:"position,omitempty"`
}

type CreateFileUploadRequest struct {
	EntryID    string  `json:"entry_id"`
	ModelID    *string `json:"model_id,omitempty"`
	ArtifactID string  `json:"artifact_id"`
	Filename   string  `json:"filename"`
	Size       int64   `json:"size"`
}

type FileUploadGrantResponse struct {
	Key       string           `json:"key"`
	UploadID  string           `json:"upload_id"`
	ObjectURL string           `json:"object_url"`
	PartSize  int64            `json:"part_size"`
	Parts     []FileUploadPart `json:"parts"`
}

type FileUploadPart struct {
	PartNumber int    `json:"part_number"`
	URL        string `json:"url"`
}

type CompleteFileUploadRequest struct {
	Key      string                    `json:"key"`
	UploadID string                    `json:"upload_id"`
	Parts    []CompletedFileUploadPart `json:"parts"`
}

type CompletedFileUploadPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}
