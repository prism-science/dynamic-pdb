package models

import (
	"time"

	"github.com/google/uuid"
)

// CoordinateIndexStatus is how far the coordinate-index job got with one model file.
type CoordinateIndexStatus string

const (
	// CoordinateIndexStatusPending: not read yet, waiting for a retry, or queued again because
	// newer entry FASTA records appeared.
	CoordinateIndexStatusPending CoordinateIndexStatus = "pending"
	CoordinateIndexStatusDone    CoordinateIndexStatus = "done"
	// CoordinateIndexStatusFailed: the file could not be read after the job's last attempt.
	CoordinateIndexStatusFailed CoordinateIndexStatus = "failed"
)

var CoordinateIndexStatuses = []CoordinateIndexStatus{
	CoordinateIndexStatusPending,
	CoordinateIndexStatusDone,
	CoordinateIndexStatusFailed,
}

func (status CoordinateIndexStatus) Validate() error {
	switch status {
	case CoordinateIndexStatusPending, CoordinateIndexStatusDone, CoordinateIndexStatusFailed:
		return nil
	default:
		return unknownValueError("coordinate index status", status, CoordinateIndexStatuses)
	}
}

// ChainLengthSource names where a chain's deposited length came from: the file itself
// (cif.ChainLengthSource), or the entry's FASTA record naming the chain.
type ChainLengthSource string

const (
	ChainLengthSourceEntityPolySeq ChainLengthSource = "entity_poly_seq"
	ChainLengthSourceSeqres        ChainLengthSource = "seqres"
	// ChainLengthSourceFASTAAuthChain: the record whose header names the chain as author chain
	// ("Chains A, B" or "Chain C[auth A]").
	ChainLengthSourceFASTAAuthChain ChainLengthSource = "fasta_auth_chain"
	// ChainLengthSourceFASTALabelChain: no record names the chain as author chain, and one names
	// it as label chain ("Chain A[auth C]"): computed files often keep the label chain IDs.
	ChainLengthSourceFASTALabelChain ChainLengthSource = "fasta_label_chain"
)

var ChainLengthSources = []ChainLengthSource{
	ChainLengthSourceEntityPolySeq,
	ChainLengthSourceSeqres,
	ChainLengthSourceFASTAAuthChain,
	ChainLengthSourceFASTALabelChain,
}

func (source ChainLengthSource) Validate() error {
	switch source {
	case ChainLengthSourceEntityPolySeq, ChainLengthSourceSeqres,
		ChainLengthSourceFASTAAuthChain, ChainLengthSourceFASTALabelChain:
		return nil
	default:
		return unknownValueError("chain length source", source, ChainLengthSources)
	}
}

// ProteinSource names the test that decided whether a chain is a protein chain
// (cif.ProteinSource).
type ProteinSource string

const (
	// ProteinSourceEntityPolyType: the _entity_poly.type of the chain's entity; polypeptide(L)
	// and polypeptide(D) are protein.
	ProteinSourceEntityPolyType ProteinSource = "entity_poly_type"
	// ProteinSourceResidueMajority: the file states no polymer type for the chain, so more than
	// half of its modeled residues must have a peptide CCD type.
	ProteinSourceResidueMajority ProteinSource = "residue_majority"
)

var ProteinSources = []ProteinSource{
	ProteinSourceEntityPolyType,
	ProteinSourceResidueMajority,
}

func (source ProteinSource) Validate() error {
	switch source {
	case ProteinSourceEntityPolyType, ProteinSourceResidueMajority:
		return nil
	default:
		return unknownValueError("protein source", source, ProteinSources)
	}
}

// IndexedChain is one author chain of a model file's first model, as the coordinate-index job
// stores it.
type IndexedChain struct {
	AuthAsymID string
	// EntityID is empty when the file states no entity.
	EntityID string
	// DepositedLength and LengthSource are both nil when no source gives a length.
	DepositedLength *int
	LengthSource    *ChainLengthSource
	ModeledResidues int
	Protein         bool
	ProteinSource   ProteinSource
}

// CoordinateIndexSet is what the coordinate-index job read from one model file. It belongs to the
// artifact, so every model revision with that file shares it.
type CoordinateIndexSet struct {
	ArtifactID uuid.UUID
	Status     CoordinateIndexStatus
	// Fill is set for a set the one-time fill queued.
	Fill bool
	// ReaderVersion is cif.CoordinateIndexVersion of a done set's read.
	ReaderVersion *string
	Warnings      []string
	Attempts      int
	LastError     *string
	ReadAt        *time.Time
	// FASTAEntryIDs are the entries of the models that used the file when it was read, in ID order.
	FASTAEntryIDs []string
	// Chains are in AuthAsymID order.
	Chains []IndexedChain
}

// CoordinateIndexClaim is a pending set the job took, with what it needs to read the file.
type CoordinateIndexClaim struct {
	ArtifactID uuid.UUID
	Name       string
	// URI is the artifact's own file.
	URI    *string
	Format *string
	// CopyStatus and StoredURI are the checksum job's copy of an external file (an archive model
	// file on RCSB): the file is read from StoredURI when CopyStatus is copied. CopyStatus is nil
	// until the checksum job has read the file.
	CopyStatus *CopyStatus
	StoredURI  *string
	// Attempts counts this claim.
	Attempts int
}

// ModelFileFASTA is what the entries of a model file's models say about its chains: the FASTA
// records of each entry's active revision.
type ModelFileFASTA struct {
	// EntryIDs are the entries of the models whose revisions use the file, in ID order.
	EntryIDs []string
	// Records are the active revisions' FASTA records of all of those entries.
	Records []FASTARecordLength
}

// FASTARecordLength is one stored FASTA record: its header and its sequence's length.
type FASTARecordLength struct {
	EntryID string
	Header  string
	Length  int
}
