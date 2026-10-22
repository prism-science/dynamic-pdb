package models

import (
	"time"

	"github.com/google/uuid"
)

type Run struct {
	ID              uuid.UUID
	Name            string
	SoftwareName    *string
	SoftwareVersion *string
	Command         *string
	Parameters      map[string]any
	Metadata        map[string]any
	StartedAt       *time.Time
	FinishedAt      *time.Time
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RunArtifactDirection string

const (
	RunArtifactDirectionInput  RunArtifactDirection = "input"
	RunArtifactDirectionOutput RunArtifactDirection = "output"
)

// RunMetadata is the set of keys run metadata may carry. Open data goes in Extensions.
type RunMetadata struct {
	Extensions map[string]any `json:"extensions,omitempty"`
}
