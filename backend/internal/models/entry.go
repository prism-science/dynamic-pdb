package models

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	entryIDPrefix       = "dpdb_"
	entryIDSuffixLength = 8
	entryIDAlphabet     = "0123456789abcdefghijklmnopqrstuvwxyz"
)

func NewEntryID() (string, error) {
	suffix := make([]byte, entryIDSuffixLength)
	randomBytes := make([]byte, entryIDSuffixLength)

	for position := 0; position < len(suffix); {
		if _, err := rand.Read(randomBytes); err != nil {
			return "", fmt.Errorf("generate entry ID: %w", err)
		}

		for _, randomByte := range randomBytes {
			if randomByte >= 252 {
				continue
			}

			suffix[position] = entryIDAlphabet[int(randomByte)%len(entryIDAlphabet)]
			position++
			if position == len(suffix) {
				break
			}
		}
	}

	return entryIDPrefix + string(suffix), nil
}

type Entry struct {
	ID        string
	State     EntryState
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

type EntryRevision struct {
	ID                uuid.UUID
	EntryID           string
	ParentRevisionID  *uuid.UUID
	RevisionNumber    *int
	State             RevisionState
	EntryState        EntryState
	ChangeSummary     *string
	PublishedAt       *time.Time
	Name              string
	Description       *string
	ThumbnailImageURL *string
	Metadata          EntryMetadata
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EntryMetadata struct {
	ExternalRefs map[EntrySource]string `json:"external_refs,omitempty"`
	Resolution   *float64               `json:"resolution,omitempty"`
	Organism     *string                `json:"organism,omitempty"`
	Method       *StructureMethod       `json:"method,omitempty"`
	SpaceGroup   *string                `json:"space_group,omitempty"`
}

type EntrySource string

const (
	EntrySourcePDB EntrySource = "pdb"
)

type StructureMethod string

const (
	StructureMethodXRayCrystallography StructureMethod = "X-ray crystallography"
	StructureMethodCryoEM              StructureMethod = "CryoEM"
)

type EntryState string

const (
	EntryStateNew     EntryState = "new"
	EntryStateActive  EntryState = "active"
	EntryStateDeleted EntryState = "deleted"
)
