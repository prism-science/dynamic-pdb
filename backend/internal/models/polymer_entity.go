package models

import (
	"fmt"
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
	UniProtReferenceSourceAlignment UniProtReferenceSource = "alignment"
)

// RequiresRelease reports whether mappings from this source must carry the UniProt release they were made against.
func (source UniProtReferenceSource) RequiresRelease() (bool, error) {
	switch source {
	case UniProtReferenceSourceSIFTS, UniProtReferenceSourceAlignment:
		return true, nil
	case UniProtReferenceSourceStructRef:
		return false, nil
	default:
		return false, fmt.Errorf("unknown UniProt mapping source: %s", source)
	}
}

func (source UniProtReferenceSource) Validate() error {
	if _, err := source.RequiresRelease(); err != nil {
		return err
	}
	return nil
}
