package models

import (
	"encoding/json"
	"fmt"
	"strings"
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
	Mutations       []string                        `json:"mutations,omitempty"`
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

// UnmarshalJSON reads mutations stored before they became a list, when they were one
// comma-joined string.
func (metadata *PolymerEntityMetadata) UnmarshalJSON(data []byte) error {
	type plain PolymerEntityMetadata
	stored := struct {
		*plain
		Mutations json.RawMessage `json:"mutations"`
	}{plain: (*plain)(metadata)}
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("decode polymer entity metadata: %w", err)
	}
	metadata.Mutations = nil
	if len(stored.Mutations) == 0 || string(stored.Mutations) == "null" {
		return nil
	}
	var list []string
	if err := json.Unmarshal(stored.Mutations, &list); err == nil {
		metadata.Mutations = list
		return nil
	}
	var joined string
	if err := json.Unmarshal(stored.Mutations, &joined); err != nil {
		return fmt.Errorf("decode polymer entity mutations: %w", err)
	}
	metadata.Mutations = SplitMutations(joined)
	return nil
}

// SplitMutations splits RCSB's comma-joined mutation text into substitution strings.
func SplitMutations(joined string) []string {
	var mutations []string
	for _, part := range strings.FieldsFunc(joined, func(r rune) bool { return r == ',' || r == ';' }) {
		if mutation := strings.TrimSpace(part); mutation != "" {
			mutations = append(mutations, mutation)
		}
	}
	return mutations
}

// DepositorPolymerEntity holds the polymer-entity values a depositor supplies on an
// entry request. They are matched to extracted entities by LabelEntityID.
type DepositorPolymerEntity struct {
	LabelEntityID   string                  `json:"label_entity_id"`
	Construct       *string                 `json:"construct,omitempty"`
	Mutations       []string                `json:"mutations,omitempty"`
	SourceOrganisms []PolymerEntityOrganism `json:"source_organisms,omitempty"`
}
