package models

import (
	"time"

	"github.com/google/uuid"
)

// InteractionSource names where the connections of an interaction set were read from.
type InteractionSource string

const (
	InteractionSourceMMCIFStructConn InteractionSource = "mmcif_struct_conn"
)

var InteractionSources = []InteractionSource{InteractionSourceMMCIFStructConn}

func (source InteractionSource) Validate() error {
	switch source {
	case InteractionSourceMMCIFStructConn:
		return nil
	default:
		return unknownValueError("interaction source", source, InteractionSources)
	}
}

// InteractionSet is the connections one parser run read from one model revision.
// The run names the parser and its version.
type InteractionSet struct {
	ID              uuid.UUID
	ModelRevisionID uuid.UUID
	RunID           uuid.UUID
	Source          InteractionSource
	CreatedAt       time.Time
}

// InteractionPartner identifies one side of a connection the way the source file
// does. Empty strings and nil mean the file had no value.
type InteractionPartner struct {
	LabelAsymID string
	LabelCompID string
	LabelSeqID  *int
	LabelAtomID string
	LabelAltID  string
	AuthAsymID  string
	AuthCompID  string
	AuthSeqID   string
	InsCode     string
}

// InteractionRecord is one connection. ConnectionType and SourceConnectionID are
// the file's own conn_type_id and id.
type InteractionRecord struct {
	ID                 uuid.UUID
	InteractionSetID   uuid.UUID
	Ordinal            int
	SourceConnectionID string
	ConnectionType     string
	PartnerA           InteractionPartner
	PartnerB           InteractionPartner
	SymmetryA          string
	SymmetryB          string
	DistanceAngstroms  *float64
	ValueOrder         string
}
