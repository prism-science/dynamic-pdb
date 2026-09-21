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
	LabelAsymID     *string                         `json:"label_asym_id,omitempty"`
	AuthAsymID      *string                         `json:"auth_asym_id,omitempty"`
	Description     *string                         `json:"description,omitempty"`
	SourceOrganisms []PolymerEntityOrganism         `json:"source_organisms,omitempty"`
	Construct       *string                         `json:"construct,omitempty"`
	Mutations       *string                         `json:"mutations,omitempty"`
	UniProtMappings []PolymerEntityUniProtReference `json:"uniprot_mappings,omitempty"`
	ResidueData     []ResidueData                   `json:"residue_data,omitempty"`
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
