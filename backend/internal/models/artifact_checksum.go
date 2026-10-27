package models

import (
	"time"

	"github.com/google/uuid"
)

// ChecksumStatus is the outcome of the checksum job for one artifact.
type ChecksumStatus string

const (
	ChecksumStatusPending  ChecksumStatus = "pending"
	ChecksumStatusVerified ChecksumStatus = "verified"
	ChecksumStatusMismatch ChecksumStatus = "mismatch"
	ChecksumStatusFailed   ChecksumStatus = "failed"
)

// ArtifactChecksum is what the checksum job computed from an artifact's bytes. SHA256 and
// SizeBytes are set once the bytes were read; StoredURI is our copy of an external file.
type ArtifactChecksum struct {
	Status    ChecksumStatus
	SHA256    *string
	SizeBytes *int64
	StoredURI *string
	CheckedAt *time.Time
}

// MissingChecksumsPage is one page of artifacts, in id order, that the checksum job gave a
// pending row where they had none. Scanned counts the artifacts in the page, Added the rows
// it added, and LastArtifactID is where the next page starts; it is uuid.Nil for an empty page.
type MissingChecksumsPage struct {
	Scanned        int
	Added          int64
	LastArtifactID uuid.UUID
}

// ChecksumClaim is an artifact the checksum job took for one attempt. SHA256 and SizeBytes
// are the uploader's declared values. Attempts counts this attempt.
type ChecksumClaim struct {
	ArtifactID uuid.UUID
	Name       string
	URI        *string
	SHA256     *string
	SizeBytes  *int64
	Attempts   int
}

// ArtifactLocation is the entry, and for a model file the model, an artifact belongs to.
type ArtifactLocation struct {
	EntryID string
	ModelID *string
}

// ChecksumResult is what the checksum job records for one claimed artifact. A pending result
// schedules a retry at NextAttemptAt and keeps Error as the reason.
type ChecksumResult struct {
	ArtifactID    uuid.UUID
	Status        ChecksumStatus
	SHA256        *string
	SizeBytes     *int64
	StoredURI     *string
	Error         *string
	CheckedAt     time.Time
	NextAttemptAt *time.Time
}
