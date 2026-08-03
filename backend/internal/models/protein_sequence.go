package models

import (
	"time"

	"github.com/google/uuid"
)

type ProteinSequence struct {
	ID               uuid.UUID
	EntryRevisionID  uuid.UUID
	SourceArtifactID uuid.UUID
	RecordIndex      int
	Header           string
	Sequence         string
	CreatedAt        time.Time
}
