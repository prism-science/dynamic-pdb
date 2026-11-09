package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

func NewModelID(entryID string, modelNumber int) (string, error) {
	if modelNumber < 1 || modelNumber > 999 {
		return "", fmt.Errorf("model number %d is outside the supported range 1..999", modelNumber)
	}

	return fmt.Sprintf("%s_m_%03d", entryID, modelNumber), nil
}

type Model struct {
	ID        string
	EntryID   string
	State     ModelState
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

type ModelRevision struct {
	ID                uuid.UUID
	EntryID           string
	ModelID           string
	ParentRevisionID  *uuid.UUID
	PrimaryArtifactID *uuid.UUID
	RevisionNumber    *int
	State             RevisionState
	ModelState        ModelState
	ChangeSummary     *string
	PublishedAt       *time.Time
	Title             *string
	ThumbnailImageURL *string
	Metadata          ModelMetadata
	FieldSources      FieldSources
	IdempotencyKey    *string
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ModelSource string

const (
	ModelSourcePDB ModelSource = "pdb"
	ModelSourceEXT ModelSource = "ext"
)

// ModelMetadata lists are null when unknown and [] when known to be empty, so the
// list fields have no omitempty.
type ModelMetadata struct {
	ExternalRefs map[ModelSource]string `json:"external_refs,omitempty"`
	// Origin is set for a model that is an unchanged archive deposition but keeps a model type
	// that describes its representation, such as a re-refinement deposited as an Ensemble.
	Origin *ModelOrigin `json:"origin,omitempty"`
	// RelatedEntries are the model's _pdbx_database_related rows.
	RelatedEntries      []RelatedEntry      `json:"related_entries,omitempty"`
	Details             *string             `json:"details,omitempty"`
	Authors             []string            `json:"authors"`
	Affiliation         *string             `json:"affiliation,omitempty"`
	Purpose             *ModelPurpose       `json:"purpose,omitempty"`
	ModelType           *StructureModelType `json:"model_type,omitempty"`
	AtomCount           *int                `json:"atom_count,omitempty"`
	ModeledResidues     *int                `json:"modeled_residues,omitempty"`
	UniqueProteinChains *int                `json:"unique_protein_chains,omitempty"`
	AltLocFraction      *float64            `json:"altloc_fraction,omitempty"`
	// MemberCount is how many whole-model states an Ensemble holds: its MODEL frames, or the
	// altloc letters on every polymer residue. Nil when the file could not be read to the end.
	MemberCount       *int           `json:"member_count,omitempty"`
	UnmodeledFraction *float64       `json:"unmodeled_fraction,omitempty"`
	Ligands           []string       `json:"ligands"`
	Cofactors         []string       `json:"cofactors"`
	ResidueData       []ResidueData  `json:"residue_data,omitempty"`
	Extensions        map[string]any `json:"extensions,omitempty"`
}

type ResidueData struct {
	LabelAsymID     string   `json:"label_asym_id"`
	LabelSeqID      int      `json:"label_seq_id"`
	LabelCompID     string   `json:"label_comp_id,omitempty"`
	AuthAsymID      *string  `json:"auth_asym_id,omitempty"`
	AuthSeqID       *int     `json:"auth_seq_id,omitempty"`
	PDBxPDBInsCode  *string  `json:"pdbx_pdb_ins_code,omitempty"`
	LabelAltID      *string  `json:"label_alt_id,omitempty"`
	UniProtPosition *string  `json:"uniprot_position,omitempty"`
	RSCC            *float64 `json:"rscc,omitempty"`
	BIso            *float64 `json:"b_iso,omitempty"`
	Occupancy       *float64 `json:"occupancy,omitempty"`
	ConformerCount  *int     `json:"conformer_count,omitempty"`
	RMSF            *float64 `json:"rmsf,omitempty"`
}

// IsArchiveModel reports whether the model is an unchanged archive deposition, whose provenance
// is its source accession rather than the runs that produced it.
func (metadata ModelMetadata) IsArchiveModel() bool {
	return metadata.HasArchiveOrigin() ||
		(metadata.ModelType != nil && *metadata.ModelType == StructureModelTypeDeposited)
}

// HasArchiveOrigin reports whether the model names the archive as its origin explicitly.
func (metadata ModelMetadata) HasArchiveOrigin() bool {
	return metadata.Origin != nil && *metadata.Origin == ModelOriginArchive
}

// ModelSearchSource says where a model comes from, as the model search's source filter names it.
type ModelSearchSource string

const (
	// ModelSearchSourcePDBArchive is a model taken unchanged from a PDB deposition.
	ModelSearchSourcePDBArchive ModelSearchSource = "pdb_archive"
	// ModelSearchSourceDynamicPDBDeposit is every other model: one people uploaded to Dynamic PDB.
	ModelSearchSourceDynamicPDBDeposit ModelSearchSource = "dynamic_pdb_deposit"
)

// SearchSource is where the model comes from: the archive for an unchanged archive deposition
// (IsArchiveModel), Dynamic PDB for every other model.
func (metadata ModelMetadata) SearchSource() ModelSearchSource {
	if metadata.IsArchiveModel() {
		return ModelSearchSourcePDBArchive
	}
	return ModelSearchSourceDynamicPDBDeposit
}

type ModelPurpose string

const (
	ModelPurposeModelBuilding ModelPurpose = "Model Building"
	ModelPurposeRefinement    ModelPurpose = "Refinement"
	ModelPurposeReference     ModelPurpose = "Reference"
)

// ModelOrigin says where a model's coordinates come from when its model type does not.
type ModelOrigin string

const ModelOriginArchive ModelOrigin = "archive"

type StructureModelType string

const (
	StructureModelTypeMulticonformer  StructureModelType = "Multiconformer"
	StructureModelTypeEnsemble        StructureModelType = "Ensemble"
	StructureModelTypeSingleConformer StructureModelType = "Single Conformer"
	StructureModelTypeDeposited       StructureModelType = "Deposited"
)

type MetricKey string

const (
	MetricKeyRFree                MetricKey = "r_free"
	MetricKeyRWork                MetricKey = "r_work"
	MetricKeyRamachandranOutliers MetricKey = "ramachandran_outliers"
	MetricKeyClashscore           MetricKey = "clashscore"
	MetricKeyMolProbityScore      MetricKey = "molprobity_score"
	MetricKeyRSCC                 MetricKey = "rscc"
	MetricKeyCC                   MetricKey = "cc"
	MetricKeySideChainOutliers    MetricKey = "side_chain_outliers"
	MetricKeyWilsonB              MetricKey = "wilson_b"
	MetricKeyRMSF                 MetricKey = "rmsf"
)

// Metric is one value on a model revision. Its producing software is named by
// SoftwareName and SoftwareVersion, or by RunID, the run that produced it.
type Metric struct {
	ID              uuid.UUID
	Key             MetricKey
	Value           float64
	SoftwareName    *string
	SoftwareVersion *string
	RunID           *uuid.UUID
	CreatedAt       time.Time
}

type ModelState string

const (
	ModelStateNew     ModelState = "new"
	ModelStateActive  ModelState = "active"
	ModelStateDeleted ModelState = "deleted"
)
