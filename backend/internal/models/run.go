package models

import (
	"fmt"
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

func (direction RunArtifactDirection) Validate() error {
	switch direction {
	case RunArtifactDirectionInput, RunArtifactDirectionOutput:
		return nil
	default:
		return fmt.Errorf("unknown run artifact direction: %s", direction)
	}
}
