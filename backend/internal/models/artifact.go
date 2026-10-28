package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	ErrUnexpectedArtifactFormat = errors.New("models: unexpected artifact format")
	ErrInvalidArtifactMetadata  = errors.New("models: invalid artifact metadata")
	ErrInvalidFASTARecords      = errors.New("models: invalid FASTA records")
)

type ArtifactLevel string

const (
	ArtifactLevelL0 ArtifactLevel = "L0"
	ArtifactLevelL1 ArtifactLevel = "L1"
	ArtifactLevelL2 ArtifactLevel = "L2"
	ArtifactLevelL3 ArtifactLevel = "L3"
)

type ArtifactType string

const (
	ArtifactTypeModel            ArtifactType = "model"
	ArtifactTypeStructureFactors ArtifactType = "structure_factors"
	ArtifactTypeFASTA            ArtifactType = "fasta"
	ArtifactTypeOther            ArtifactType = "other"
)

type Artifact struct {
	ID        uuid.UUID
	Name      string
	Level     ArtifactLevel
	Type      ArtifactType
	URI       *string
	SHA256    *string
	Format    *string
	SizeBytes *int64
	Metadata  any
	CreatedBy uuid.UUID
	CreatedAt time.Time
	Checksum  ArtifactChecksum
}

type FASTAMetadata struct {
	Records []FASTARecord `json:"records"`
}

type FASTARecord struct {
	Header   string `json:"header"`
	Sequence string `json:"sequence"`
}

func (a Artifact) IsFASTA() bool {
	return a.Format != nil && *a.Format == "fasta"
}

func (a Artifact) FASTA() (*FASTAMetadata, error) {
	if !a.IsFASTA() {
		return nil, fmt.Errorf("%w: expected fasta, got %s", ErrUnexpectedArtifactFormat, artifactFormat(a.Format))
	}

	switch metadata := a.Metadata.(type) {
	case *FASTAMetadata:
		if metadata == nil {
			return nil, fmt.Errorf("%w: FASTA metadata is nil", ErrInvalidArtifactMetadata)
		}
		return metadata, nil
	case FASTAMetadata:
		return &metadata, nil
	default:
		return nil, fmt.Errorf("%w: FASTA metadata has type %T", ErrInvalidArtifactMetadata, a.Metadata)
	}
}

func (a Artifact) FASTARecords() ([]FASTARecord, error) {
	metadata, err := a.FASTA()
	if err != nil {
		return nil, fmt.Errorf("get FASTA metadata: %w", err)
	}

	records := make([]FASTARecord, len(metadata.Records))
	copy(records, metadata.Records)
	for index := range records {
		records[index].Header = strings.TrimSpace(records[index].Header)
		records[index].Sequence = normalizeProteinSequence(records[index].Sequence)
		if records[index].Sequence == "" {
			return nil, fmt.Errorf("FASTA record %d sequence is empty: %w", index, ErrInvalidFASTARecords)
		}
	}

	return records, nil
}

func artifactFormat(format *string) string {
	if format == nil {
		return "<nil>"
	}
	return *format
}

func normalizeProteinSequence(sequence string) string {
	var normalized strings.Builder
	normalized.Grow(len(sequence))
	for _, char := range sequence {
		if unicode.IsSpace(char) {
			continue
		}
		normalized.WriteRune(unicode.ToUpper(char))
	}
	return normalized.String()
}

// ArtifactMetadata is the set of keys artifact metadata may carry. Open data goes in
// Extensions.
type ArtifactMetadata struct {
	Records []FASTARecord           `json:"records,omitempty"`
	Source  *ArtifactMetadataSource `json:"_dynamic_pdb_source,omitempty"`
	// ArchiveVersion is the RCSB version (major.minor) an archive coordinate file stands for.
	// The archive sync sets it on the file it links when RCSB changes the coordinates.
	ArchiveVersion *string        `json:"archive_version,omitempty"`
	Extensions     map[string]any `json:"extensions,omitempty"`
}

// ArtifactMetadataSource records the external experiment a file came from.
type ArtifactMetadataSource struct {
	Provider     string `json:"provider"`
	ExperimentID string `json:"experiment_id,omitempty"`
	Path         string `json:"path,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
}
