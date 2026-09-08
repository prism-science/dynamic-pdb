package models

import (
	"crypto/rand"
	"fmt"
	"maps"
	"reflect"
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
	Title             *string
	ThumbnailImageURL *string
	Metadata          EntryMetadata
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EntryMetadata struct {
	ExternalRefs    map[EntrySource]string `json:"external_refs,omitempty"`
	Details         *string                `json:"details,omitempty"`
	Resolution      *float64               `json:"resolution,omitempty"`
	Method          *StructureMethod       `json:"method,omitempty"`
	SpaceGroup      *string                `json:"space_group,omitempty"`
	Crystallography *EntryCrystallography  `json:"crystallography,omitempty"`
}

type EntryCrystallography struct {
	Crystals []EntryCrystal `json:"crystals,omitempty"`
}

type EntryCrystal struct {
	ID           string              `json:"id"`
	Growth       *EntryCrystalGrowth `json:"growth,omitempty"`
	Diffractions []EntryDiffraction  `json:"diffractions,omitempty"`
}

type EntryCrystalGrowth struct {
	PH                *float64 `json:"ph,omitempty"`
	TemperatureKelvin *float64 `json:"temperature_kelvin,omitempty"`
}

type EntryDiffraction struct {
	ID                string   `json:"id"`
	TemperatureKelvin *float64 `json:"temperature_kelvin,omitempty"`
}

func (revision EntryRevision) HasSameData(other EntryRevision) bool {
	return revision.EntryState == other.EntryState &&
		pointersEqual(revision.Title, other.Title) &&
		pointersEqual(revision.ThumbnailImageURL, other.ThumbnailImageURL) &&
		maps.Equal(revision.Metadata.ExternalRefs, other.Metadata.ExternalRefs) &&
		pointersEqual(revision.Metadata.Details, other.Metadata.Details) &&
		pointersEqual(revision.Metadata.Resolution, other.Metadata.Resolution) &&
		pointersEqual(revision.Metadata.Method, other.Metadata.Method) &&
		pointersEqual(revision.Metadata.SpaceGroup, other.Metadata.SpaceGroup) &&
		reflect.DeepEqual(revision.Metadata.Crystallography, other.Metadata.Crystallography)
}

func pointersEqual[T comparable](left *T, right *T) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
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
