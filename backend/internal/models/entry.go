package models

import (
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	ID        uuid.UUID
	State     EntryState
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

type EntryRevision struct {
	ID                uuid.UUID
	EntryID           uuid.UUID
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
