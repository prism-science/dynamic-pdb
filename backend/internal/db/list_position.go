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

// RevisionGroupPosition is a keyset position in the revision group list, which is
// ordered by each entry's newest matching revision first, then by entry ID.
type RevisionGroupPosition struct {
	LatestCreatedAt time.Time
	EntryID         string
}

// SimilarEntryPosition is a keyset position in the similar entry list, which is
// ordered by best similarity score first, then by entry ID.
type SimilarEntryPosition struct {
	Score   float64
	EntryID string
}
