package db

import (
	"time"

	"github.com/google/uuid"
)

// ListPosition is a keyset position in a list ordered by (created_at, id).
// A list continued from it returns only the rows that come after it in that list's order.
type ListPosition struct {
	CreatedAt time.Time
	ID        uuid.UUID
}
