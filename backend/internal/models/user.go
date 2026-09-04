package models

import (
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/types"
)

const SystemUserID = "00000000-0000-0000-0000-000000000001"

type User struct {
	ID          uuid.UUID
	ExternalRef types.ExternalRef
	Email       string
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
