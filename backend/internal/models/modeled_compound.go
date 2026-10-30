package models

import (
	"time"

	"github.com/google/uuid"
)

// ModeledCompoundStatus is how far the model-compounds job got with one model file.
type ModeledCompoundStatus string

const (
	// ModeledCompoundStatusPending: not read yet, or waiting for a retry.
	ModeledCompoundStatusPending ModeledCompoundStatus = "pending"
	ModeledCompoundStatusDone    ModeledCompoundStatus = "done"
	// ModeledCompoundStatusFailed: the file could not be read after the job's last attempt.
	ModeledCompoundStatusFailed ModeledCompoundStatus = "failed"
)

var ModeledCompoundStatuses = []ModeledCompoundStatus{
	ModeledCompoundStatusPending,
	ModeledCompoundStatusDone,
	ModeledCompoundStatusFailed,
}

func (status ModeledCompoundStatus) Validate() error {
	switch status {
	case ModeledCompoundStatusPending, ModeledCompoundStatusDone, ModeledCompoundStatusFailed:
		return nil
	default:
		return unknownValueError("modeled compound status", status, ModeledCompoundStatuses)
	}
}

// ModeledCompoundSource names where a copy was read: an mmCIF scheme category, the mmCIF
// coordinates, or the HETATM records of a PDB-format file (cif.CopySource).
type ModeledCompoundSource string

const (
	ModeledCompoundSourceNonpolyScheme ModeledCompoundSource = "nonpoly_scheme"
	ModeledCompoundSourceBranchScheme  ModeledCompoundSource = "branch_scheme"
	ModeledCompoundSourceAtomSite      ModeledCompoundSource = "atom_site"
	ModeledCompoundSourcePDBHetatm     ModeledCompoundSource = "pdb_hetatm"
)

var ModeledCompoundSources = []ModeledCompoundSource{
	ModeledCompoundSourceNonpolyScheme,
	ModeledCompoundSourceBranchScheme,
	ModeledCompoundSourceAtomSite,
	ModeledCompoundSourcePDBHetatm,
}

func (source ModeledCompoundSource) Validate() error {
	switch source {
	case ModeledCompoundSourceNonpolyScheme, ModeledCompoundSourceBranchScheme,
		ModeledCompoundSourceAtomSite, ModeledCompoundSourcePDBHetatm:
		return nil
	default:
		return unknownValueError("modeled compound source", source, ModeledCompoundSources)
	}
}

// ModeledCompoundSet is what the model-compounds job read from one model file: every copy
// of every compound outside the polymer chains, water excepted. It belongs to the artifact,
// so every model revision with that file shares it.
type ModeledCompoundSet struct {
	ArtifactID uuid.UUID
	Status     ModeledCompoundStatus
	// RunID and ParserVersion name the parser run of a done set.
	RunID         *uuid.UUID
	ParserVersion *string
	// Complete is false when a limit stopped the list; nil until the set is done.
	Complete  *bool
	Warnings  []string
	Attempts  int
	LastError *string
	ReadAt    *time.Time
	// Copies are in the reader's order: CCD ID, then chain and residue number.
	Copies []ModeledCompoundCopy
}

// ModeledCompoundCopy is one copy of a compound in a model file. AuthAsymID, AuthSeqID and
// InsCode are the chain, residue number and insertion code viewers show; LabelAsymID and
// EntityID are empty for a PDB-format file.
type ModeledCompoundCopy struct {
	CCDID        string
	LabelAsymID  string
	AuthAsymID   string
	AuthSeqID    string
	InsCode      string
	EntityID     string
	Source       ModeledCompoundSource
	NeedsReview  bool
	ReviewReason string
}

// ModeledCompoundClaim is a pending set the job took, with what it needs to read the file.
type ModeledCompoundClaim struct {
	ArtifactID uuid.UUID
	Name       string
	URI        *string
	Format     *string
	// Attempts counts this claim.
	Attempts int
}
