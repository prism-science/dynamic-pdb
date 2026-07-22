package models

import (
	"time"

	"github.com/google/uuid"
)

type Model struct {
	ID                uuid.UUID
	EntryID           uuid.UUID
	Name              string
	Description       *string
	ThumbnailImageURL *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
