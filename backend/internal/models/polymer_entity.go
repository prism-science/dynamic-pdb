package models

import (
	"time"

	"github.com/google/uuid"
)

type PolymerEntity struct {
	ID                uuid.UUID
	ProteinSequenceID uuid.UUID
	Metadata          PolymerEntityMetadata
	CreatedAt         time.Time
}

type PolymerEntityMetadata struct {
	LabelEntityID   *string                         `json:"label_entity_id,omitempty"`
	Description     *string                         `json:"description,omitempty"`
	SourceOrganisms []PolymerEntityOrganism         `json:"source_organisms,omitempty"`
	Construct       *string                         `json:"construct,omitempty"`
	Mutations       *string                         `json:"mutations,omitempty"`
	UniProtMappings []PolymerEntityUniProtReference `json:"uniprot_mappings,omitempty"`
}

type PolymerEntityOrganism struct {
	ScientificName string `json:"scientific_name"`
	NCBITaxonomyID *int   `json:"ncbi_taxonomy_id,omitempty"`
}

type PolymerEntityUniProtReference struct {
	Accession      string                 `json:"accession"`
	Source         UniProtReferenceSource `json:"source"`
	UniProtRelease *string                `json:"unp_release,omitempty"`
}

type UniProtReferenceSource string

const (
	UniProtReferenceSourceSIFTS     UniProtReferenceSource = "sifts"
	UniProtReferenceSourceStructRef UniProtReferenceSource = "struct_ref"
)
