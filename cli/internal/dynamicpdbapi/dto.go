package dynamicpdbapi

import (
	"fmt"
	"slices"
	"strings"
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

// Error is a failed backend or storage response. Code and Message describe the first
// problem; Problems lists every JSON:API error object the backend sent, such as one
// per schema violation in a 422.
type Error struct {
	Status   int
	Code     string
	Message  string
	Problems []Problem
}

// Problem is one JSON:API error object. Pointer is the JSON pointer of the request
// field the problem is about, such as /model/runs/0/software_version, or empty when
// the problem is not about one field.
type Problem struct {
	Code    string
	Detail  string
	Pointer string
}

func (e *Error) Error() string {
	header := fmt.Sprintf("dynamicpdbapi: backend %d", e.Status)
	if len(e.Problems) <= 1 && !e.hasPointer() {
		if e.Code == "" {
			return fmt.Sprintf("%s: %s", header, e.Message)
		}
		return fmt.Sprintf("%s: %s: %s", header, e.Code, e.Message)
	}
	var message strings.Builder
	message.WriteString(header)
	message.WriteString(": ")
	message.WriteString(countNoun(len(e.Problems), "problem"))
	message.WriteString(":")
	for _, problem := range e.Problems {
		message.WriteString("\n  ")
		message.WriteString(problem.String())
	}
	return message.String()
}

func (e *Error) hasPointer() bool {
	return slices.ContainsFunc(e.Problems, func(problem Problem) bool { return problem.Pointer != "" })
}

// String prints the problem as "pointer: CODE: detail", leaving out empty parts.
func (p Problem) String() string {
	parts := make([]string, 0, 3)
	for _, part := range []string{p.Pointer, p.Code, p.Detail} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ": ")
}

func countNoun(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

type ListEntriesParams struct {
	PDBIDs []string
}

type Entry struct {
	EntryProperties
	ID    string  `json:"id"`
	Title *string `json:"title"`
}

type EntryProperties struct {
	ExternalRefs    map[string]string     `json:"external_refs,omitempty"`
	Details         *string               `json:"details,omitempty"`
	Resolution      *float64              `json:"resolution,omitempty"`
	Method          *string               `json:"method,omitempty"`
	SpaceGroup      *string               `json:"space_group,omitempty"`
	Crystallography *EntryCrystallography `json:"crystallography,omitempty"`
}

type EntryCrystallography struct {
	Crystals []EntryCrystal `json:"crystals,omitempty"`
}

type EntryCrystal struct {
	ID           string              `json:"id"`
	Growth       *EntryCrystalGrowth `json:"growth,omitempty"`
	Diffractions []EntryDiffraction  `json:"diffractions,omitempty"`
}

type EntryCrystalGrowth struct {
	PH                *float64 `json:"ph,omitempty"`
	TemperatureKelvin *float64 `json:"temperature_kelvin,omitempty"`
}

type EntryDiffraction struct {
	ID                string   `json:"id"`
	TemperatureKelvin *float64 `json:"temperature_kelvin,omitempty"`
}

type CreateEntryRequest struct {
	Entry           CreateEntryData     `json:"entry"`
	ModelOperations []AddModelOperation `json:"model_operations,omitempty"`
}

type CreateEntryResult struct {
	EntryID      string                   `json:"entry_id"`
	ModelResults []CreateEntryModelResult `json:"model_results"`
}

type CreateEntryModelResult struct {
	ModelID string `json:"model_id"`
}

type CreateEntryData struct {
	EntryProperties
	Title             string                  `json:"title"`
	ThumbnailImageURL *string                 `json:"thumbnail_image_url,omitempty"`
	Artifacts         []CreateArtifactRequest `json:"artifacts,omitempty"`
}

type AddModelOperation struct {
	Op   string       `json:"op"`
	Data AddModelData `json:"data"`
}

type AddModelData struct {
	Title             string                  `json:"title"`
	ThumbnailImageURL *string                 `json:"thumbnail_image_url,omitempty"`
	Metadata          map[string]any          `json:"metadata,omitempty"`
	IdempotencyKey    *string                 `json:"idempotency_key,omitempty"`
	PrimaryArtifactID *string                 `json:"primary_artifact_id,omitempty"`
	Artifacts         []CreateArtifactRequest `json:"artifacts,omitempty"`
	Runs              []CreateRunRequest      `json:"runs,omitempty"`
	Metrics           []CreateMetricRequest   `json:"metrics,omitempty"`
}

type CreateModelRequest struct {
	Model CreateModelData `json:"model"`
}

type CreateModelResult struct {
	ModelID string `json:"model_id"`
}

type CreateModelData struct {
	Title             string                  `json:"title"`
	ThumbnailImageURL *string                 `json:"thumbnail_image_url,omitempty"`
	Metadata          map[string]any          `json:"metadata,omitempty"`
	IdempotencyKey    *string                 `json:"idempotency_key,omitempty"`
	PrimaryArtifactID *string                 `json:"primary_artifact_id,omitempty"`
	Artifacts         []CreateArtifactRequest `json:"artifacts,omitempty"`
	Runs              []CreateRunRequest      `json:"runs,omitempty"`
	Metrics           []CreateMetricRequest   `json:"metrics,omitempty"`
}

type CreateArtifactRequest struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Level     string         `json:"level"`
	Type      string         `json:"type"`
	URI       *string        `json:"uri,omitempty"`
	SHA256    *string        `json:"sha256,omitempty"`
	Format    *string        `json:"format,omitempty"`
	SizeBytes *int64         `json:"size_bytes,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// CreateMetricRequest is one metric of a new model. A metric of a computed model
// names the software that produced it or links to that run with RunID.
type CreateMetricRequest struct {
	ID    string  `json:"id"`
	Key   string  `json:"key"`
	Value float64 `json:"value"`
	// SoftwareName and SoftwareVersion name the program that produced the value.
	SoftwareName    *string `json:"software_name,omitempty"`
	SoftwareVersion *string `json:"software_version,omitempty"`
	// RunID names the run, in the same request, that produced the value.
	RunID *string `json:"run_id,omitempty"`
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
