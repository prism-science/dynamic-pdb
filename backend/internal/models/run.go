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
	Metadata        RunMetadata
	StartedAt       *time.Time
	FinishedAt      *time.Time
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RunMetadata struct {
	SoftwareHighlight *string `json:"software_highlight,omitempty"`
}

type RunArtifactDirection string

const (
	RunArtifactDirectionInput  RunArtifactDirection = "input"
	RunArtifactDirectionOutput RunArtifactDirection = "output"
)
