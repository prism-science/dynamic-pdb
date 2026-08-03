package models

import (
	"time"

	"github.com/google/uuid"
)

type ArtifactLevel string

const (
	ArtifactLevelL0 ArtifactLevel = "L0"
	ArtifactLevelL1 ArtifactLevel = "L1"
	ArtifactLevelL2 ArtifactLevel = "L2"
	ArtifactLevelL3 ArtifactLevel = "L3"
)

type Artifact struct {
	ID        uuid.UUID
	Name      string
	Level     ArtifactLevel
	URI       *string
	SHA256    *string
	Format    *string
	SizeBytes *int64
	Metadata  map[string]any
	CreatedBy uuid.UUID
	CreatedAt time.Time
}
