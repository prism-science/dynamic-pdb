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
	Name              string
	Description       *string
	ThumbnailImageURL *string
	Metadata          ModelMetadata
	IdempotencyKey    *string
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ModelMetadata struct {
	Authors             []string            `json:"authors,omitempty"`
	Affiliation         *string             `json:"affiliation,omitempty"`
	Purpose             *ModelPurpose       `json:"purpose,omitempty"`
	ModelType           *StructureModelType `json:"model_type,omitempty"`
	AtomCount           *int                `json:"atom_count,omitempty"`
	ModeledResidues     *int                `json:"modeled_residues,omitempty"`
	UniqueProteinChains *int                `json:"unique_protein_chains,omitempty"`
	Ligands             []string            `json:"ligands,omitempty"`
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
