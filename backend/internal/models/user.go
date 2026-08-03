package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID          uuid.UUID
	Source      string
	ExternalRef string
	Email       *string
	DisplayName *string
	AvatarURL   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
