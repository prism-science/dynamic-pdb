package models

import (
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	ID        uuid.UUID
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

type EntryRevision struct {
	ID                uuid.UUID
	EntryID           uuid.UUID
	ParentRevisionID  *uuid.UUID
	RevisionNumber    *int
	State             RevisionState
	ChangeSummary     *string
	PublishedAt       *time.Time
	Name              string
	Description       *string
	ThumbnailImageURL *string
	Metadata          map[string]any
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
