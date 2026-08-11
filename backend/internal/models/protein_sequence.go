package models

import (
	"time"

	"github.com/google/uuid"
)

type ProteinSequenceProcessingState string

const (
	ProteinSequenceProcessingStatePending   ProteinSequenceProcessingState = "pending"
	ProteinSequenceProcessingStateProcessed ProteinSequenceProcessingState = "processed"
)

type ProteinSequence struct {
	ID               uuid.UUID
	EntryRevisionID  uuid.UUID
	SourceArtifactID uuid.UUID
	RecordIndex      int
	Header           string
	Sequence         string
	ProcessingState  ProteinSequenceProcessingState
	CreatedAt        time.Time
}
