package models

import (
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/types"
)

type User struct {
	ID          uuid.UUID
	ExternalRef types.ExternalRef
	Email       string
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
