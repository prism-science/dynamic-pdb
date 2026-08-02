package models

import (
	"time"

	"github.com/google/uuid"
)

type ProteinSequence struct {
	ID          uuid.UUID
	EntryID     uuid.UUID
	EntityID    uuid.UUID
	RecordIndex int
	Header      string
	Sequence    string
	CreatedAt   time.Time
}
