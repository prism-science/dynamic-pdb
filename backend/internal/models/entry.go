package models

import (
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	ID                uuid.UUID
	Name              string
	Description       *string
	ThumbnailImageURL *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
