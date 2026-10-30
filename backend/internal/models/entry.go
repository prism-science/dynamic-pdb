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
	FieldSources      FieldSources
	// CompoundSources and GlycanDescriptors record how the composition lists were derived from
	// the entry's mmCIF. Nil means not derived; an empty glycan list means known to have none.
	CompoundSources   *CompoundSources
	GlycanDescriptors *[]Glycan
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
	// Ligands and Cofactors describe the sample composition. Omitted (nil) means unknown,
	// an empty list means known to be absent.
	Ligands   *[]string `json:"ligands,omitempty"`
	Cofactors *[]string `json:"cofactors,omitempty"`

	DepositorPolymerEntities []DepositorPolymerEntity `json:"depositor_polymer_entities,omitempty"`

	// ArchiveStatus is set only by the archive sync, when the archive stops serving the entry.
	ArchiveStatus *EntryArchiveStatus `json:"archive_status,omitempty"`
}

// EntryArchiveStatus records that the archive no longer serves the entry's PDB entry. It is
// archive metadata, not an entry state: the entry stays visible.
type EntryArchiveStatus struct {
	State        EntryArchiveState `json:"state"`
	ObsoletedAt  *time.Time        `json:"obsoleted_at,omitempty"`
	SupersededBy []string          `json:"superseded_by,omitempty"`
}

type EntryArchiveState string

// EntryArchiveStateObsolete means the archive obsoleted the entry, usually replacing it with
// the entries in SupersededBy.
const EntryArchiveStateObsolete EntryArchiveState = "obsolete"

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
		reflect.DeepEqual(revision.Metadata.Crystallography, other.Metadata.Crystallography) &&
		reflect.DeepEqual(revision.Metadata.Ligands, other.Metadata.Ligands) &&
		reflect.DeepEqual(revision.Metadata.Cofactors, other.Metadata.Cofactors) &&
		reflect.DeepEqual(revision.Metadata.DepositorPolymerEntities, other.Metadata.DepositorPolymerEntities) &&
		reflect.DeepEqual(revision.Metadata.ArchiveStatus, other.Metadata.ArchiveStatus) &&
		reflect.DeepEqual(revision.CompoundSources, other.CompoundSources) &&
		reflect.DeepEqual(revision.GlycanDescriptors, other.GlycanDescriptors)
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
