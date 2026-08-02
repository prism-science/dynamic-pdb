package models

import (
	"time"

	"github.com/google/uuid"
)

type Structure struct {
	ID                uuid.UUID
	CreatedBy         uuid.UUID
	Name              string
	Description       *string
	ThumbnailImageURL *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
