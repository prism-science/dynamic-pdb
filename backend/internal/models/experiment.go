package models

import (
	"time"

	"github.com/google/uuid"
)

type Experiment struct {
	ID                uuid.UUID
	EntryID           uuid.UUID
	Name              string
	Description       *string
	ThumbnailImageURL *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
