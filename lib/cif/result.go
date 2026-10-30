package cif

// Result holds what the parser derives from one structure file.
type Result struct {
	// Atoms is nil when the atom coordinates are missing or were cut off.
	Atoms *AtomStats `json:"atoms,omitempty"`
	// AtomsCutOff is true when Atoms is nil because the read limit ended inside
	// the coordinate loop, as opposed to a file without coordinates.
	AtomsCutOff bool       `json:"atoms_cut_off,omitempty"`
	Ligands     []Compound `json:"ligands"`
	Cofactors   []Compound `json:"cofactors"`
	// CompoundSources records the rule that placed each compound, including
	// the compounds left out. Water and polymer residues are never recorded.
	CompoundSources CompoundSources `json:"compound_sources"`
	// Glycans holds one record per branched entity, with its WURCS descriptors.
	Glycans             []Glycan             `json:"glycans"`
	Mutations           []MutationSet        `json:"mutations"`
	SequenceDifferences []SequenceDifference `json:"sequence_differences"`
	Connections         []Connection         `json:"connections"`
	Program             Program              `json:"program"`
	ReferenceVersion    string               `json:"reference_version"`
	Warnings            []string             `json:"warnings"`
}

type AtomStats struct {
	AtomCount       int     `json:"atom_count"`
	ModeledResidues int     `json:"modeled_residues"`
	ProteinChains   int     `json:"unique_protein_chains"`
	AltlocFraction  float64 `json:"altloc_fraction"`
}

// Compound is a chemical component kept as a ligand or cofactor.
type Compound struct {
	ID           string `json:"id"`
	NeedsReview  bool   `json:"needs_review,omitempty"`
	ReviewReason string `json:"review_reason,omitempty"`
}

// CompoundSources is the decision record: per placement, the rule that put
// each CCD ID there. One ID can be in two groups, such as a zinc linked to the
// protein (a cofactor) and one linked to nothing (a ligand).
type CompoundSources struct {
	Ligands   map[string]CompoundSource `json:"ligands"`
	Cofactors map[string]CompoundSource `json:"cofactors"`
	Excluded  map[string]CompoundSource `json:"excluded"`
}

// CompoundSource is one entry of the decision record. ListVersion is the
// reference version in force (Result.ReferenceVersion).
type CompoundSource struct {
	Rule        string `json:"rule"`
	ListVersion string `json:"list_version"`
}

// Glycan is one branched entity: its sugars and the WURCS descriptors of
// _pdbx_entity_branch_descriptor, which keep the branching a list of IDs loses.
type Glycan struct {
	EntityID    string             `json:"entity_id"`
	CompoundIDs []string           `json:"compound_ids"`
	Descriptors []GlycanDescriptor `json:"descriptors"`
}

// GlycanDescriptor is one descriptor row of a branched entity.
type GlycanDescriptor struct {
	Descriptor     string `json:"descriptor"`
	Type           string `json:"type"`
	Program        string `json:"program,omitempty"`
	ProgramVersion string `json:"program_version,omitempty"`
}

type Program struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// MutationSet lists the engineered mutations of one entity against one UniProt
// accession. Mutations use UniProt numbering, written wild type, number, mutant
// in one-letter codes. Original is the entity's own pdbx_mutation text.
type MutationSet struct {
	EntityID     string   `json:"entity_id"`
	Accession    string   `json:"accession,omitempty"`
	Mutations    []string `json:"mutations"`
	Original     string   `json:"original,omitempty"`
	Unmapped     []string `json:"unmapped,omitempty"`
	NeedsReview  bool     `json:"needs_review,omitempty"`
	ReviewReason string   `json:"review_reason,omitempty"`
}

// SequenceDifference is one _struct_ref_seq_dif row.
type SequenceDifference struct {
	AlignID        string `json:"align_id"`
	PDBIDCode      string `json:"pdb_id_code"`
	MonID          string `json:"mon_id"`
	StrandID       string `json:"strand_id"`
	SeqNum         string `json:"seq_num"`
	InsCode        string `json:"ins_code"`
	SeqDBName      string `json:"seq_db_name"`
	SeqDBAccession string `json:"seq_db_accession_code"`
	DBMonID        string `json:"db_mon_id"`
	SeqDBSeqNum    string `json:"seq_db_seq_num"`
	Details        string `json:"details"`
	AuthSeqNum     string `json:"auth_seq_num"`
	Ordinal        string `json:"ordinal"`
}

// Connection is one _struct_conn row.
type Connection struct {
	ID                string   `json:"id"`
	ConnTypeID        string   `json:"conn_type_id"`
	LeavingAtomFlag   string   `json:"leaving_atom_flag,omitempty"`
	PDBID             string   `json:"pdb_id,omitempty"`
	Partner1          Partner  `json:"partner1"`
	Partner2          Partner  `json:"partner2"`
	Symmetry1         string   `json:"symmetry1,omitempty"`
	Symmetry2         string   `json:"symmetry2,omitempty"`
	DistanceAngstroms *float64 `json:"distance_angstroms,omitempty"`
	ValueOrder        string   `json:"value_order,omitempty"`
	Details           string   `json:"details,omitempty"`
}

// Partner identifies one side of a connection.
type Partner struct {
	LabelAsymID string `json:"label_asym_id"`
	LabelCompID string `json:"label_comp_id"`
	LabelSeqID  string `json:"label_seq_id"`
	LabelAtomID string `json:"label_atom_id"`
	LabelAltID  string `json:"label_alt_id"`
	AuthAsymID  string `json:"auth_asym_id"`
	AuthCompID  string `json:"auth_comp_id"`
	AuthSeqID   string `json:"auth_seq_id"`
	InsCode     string `json:"pdb_ins_code"`
}
