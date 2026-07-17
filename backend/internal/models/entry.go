package models

import (
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
