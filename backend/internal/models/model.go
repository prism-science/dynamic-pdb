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

type ModelMetadata struct {
	ExternalRefs        map[ModelSource]string `json:"external_refs,omitempty"`
	Details             *string                `json:"details,omitempty"`
	Authors             []string               `json:"authors,omitempty"`
	Affiliation         *string                `json:"affiliation,omitempty"`
	Purpose             *ModelPurpose          `json:"purpose,omitempty"`
	ModelType           *StructureModelType    `json:"model_type,omitempty"`
	AtomCount           *int                   `json:"atom_count,omitempty"`
	ModeledResidues     *int                   `json:"modeled_residues,omitempty"`
	UniqueProteinChains *int                   `json:"unique_protein_chains,omitempty"`
	AltLocFraction      *float64               `json:"altloc_fraction,omitempty"`
	UnmodeledFraction   *float64               `json:"unmodeled_fraction,omitempty"`
	Ligands             []string               `json:"ligands,omitempty"`
	Cofactors           []string               `json:"cofactors,omitempty"`
	ResidueData         []ResidueData          `json:"residue_data,omitempty"`
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

type ModelPurpose string

const (
	ModelPurposeModelBuilding ModelPurpose = "Model Building"
	ModelPurposeRefinement    ModelPurpose = "Refinement"
)

type StructureModelType string

const (
	StructureModelTypeMulticonformer  StructureModelType = "Multiconformer"
	StructureModelTypeEnsemble        StructureModelType = "Ensemble"
	StructureModelTypeSingleConformer StructureModelType = "Single Conformer"
)

type MetricKey string

const (
	MetricKeyRFree                MetricKey = "r_free"
	MetricKeyRWork                MetricKey = "r_work"
	MetricKeyRamachandranOutliers MetricKey = "ramachandran_outliers"
	MetricKeyClashscore           MetricKey = "clashscore"
	MetricKeyMolProbityScore      MetricKey = "molprobity_score"
	MetricKeyRSCC                 MetricKey = "rscc"
)

type Metric struct {
	ID        uuid.UUID
	Key       MetricKey
	Value     float64
	CreatedAt time.Time
}

type ModelState string

const (
	ModelStateNew     ModelState = "new"
	ModelStateActive  ModelState = "active"
	ModelStateDeleted ModelState = "deleted"
)
