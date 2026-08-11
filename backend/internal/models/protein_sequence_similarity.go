package models

import (
	"time"

	"github.com/google/uuid"
)

type ProteinSequenceSimilarityRunState string

const (
	ProteinSequenceSimilarityRunStateQueued    ProteinSequenceSimilarityRunState = "queued"
	ProteinSequenceSimilarityRunStateRunning   ProteinSequenceSimilarityRunState = "running"
	ProteinSequenceSimilarityRunStateSucceeded ProteinSequenceSimilarityRunState = "succeeded"
	ProteinSequenceSimilarityRunStateFailed    ProteinSequenceSimilarityRunState = "failed"
)

type ProteinSequenceSimilarityRun struct {
	ID           uuid.UUID
	Tool         string
	Parameters   map[string]any
	State        ProteinSequenceSimilarityRunState
	ErrorMessage *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	CreatedAt    time.Time
}

type ProteinSequenceSimilarity struct {
	ID                uuid.UUID
	RunID             uuid.UUID
	SourceSequenceID  uuid.UUID
	SimilarSequenceID uuid.UUID
	Tool              string
	Score             float64
	Metadata          map[string]any
	CreatedAt         time.Time
}

type SimilarEntry struct {
	Entry   EntryRevision
	Score   float64
	Matches []ProteinSequenceSimilarityMatch
}

type ProteinSequenceSimilarityMatch struct {
	SourceSequenceID uuid.UUID
	SimilarSequence  ProteinSequence
	Similarity       ProteinSequenceSimilarity
}
