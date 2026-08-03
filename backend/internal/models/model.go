package models

import (
	"time"

	"github.com/google/uuid"
)

type Model struct {
	ID        uuid.UUID
	EntryID   uuid.UUID
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

type ModelRevision struct {
	ID                uuid.UUID
	ModelID           uuid.UUID
	ParentRevisionID  *uuid.UUID
	PrimaryArtifactID *uuid.UUID
	RevisionNumber    *int
	State             RevisionState
	ChangeSummary     *string
	PublishedAt       *time.Time
	Name              string
	Description       *string
	ThumbnailImageURL *string
	Metadata          ModelMetadata
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ModelMetadata struct {
	Authors     []string `json:"authors,omitempty"`
	Affiliation *string  `json:"affiliation,omitempty"`
}

type Metric struct {
	ID        uuid.UUID
	Key       string
	Value     float64
	CreatedAt time.Time
}
