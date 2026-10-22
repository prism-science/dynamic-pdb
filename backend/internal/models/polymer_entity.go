package models

import (
	"encoding/json"
	"fmt"
	"regexp"
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
	MutationsRaw    *string                         `json:"mutations_raw,omitempty"`
	MutationsStatus *MutationsStatus                `json:"mutations_status,omitempty"`
	UniProtMappings []PolymerEntityUniProtReference `json:"uniprot_mappings,omitempty"`
}

// MutationsStatus tells how much of MutationsRaw was read into Mutations.
type MutationsStatus string

const (
	// MutationsStatusParsed means every token of the raw text is a plain substitution.
	MutationsStatusParsed MutationsStatus = "parsed"
	// MutationsStatusNeedsReview means some token is not a plain substitution; only the
	// plain ones are listed in Mutations and the original text is in MutationsRaw.
	MutationsStatusNeedsReview MutationsStatus = "needs_review"
	// MutationsStatusNone means the raw text names no mutation.
	MutationsStatusNone MutationsStatus = "none"
)

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
		if len(list) > 0 {
			metadata.Mutations = list
		}
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

// mutationSubstitutionPattern matches a one-letter substitution such as C54T: the
// original residue, its position, and the new residue.
var mutationSubstitutionPattern = regexp.MustCompile(`^[A-Z][1-9][0-9]*[A-Z]$`)

// IsMutationSubstitution reports whether value is one plain substitution such as C54T.
func IsMutationSubstitution(value string) bool {
	return mutationSubstitutionPattern.MatchString(value)
}

// ParseMutations reads RCSB's mutation text. Only plain substitutions become list
// entries; anything else (ENGINEERED MUTATION, DELETION, A(-3)G) is left to the caller to
// keep as raw text, flagged by MutationsStatusNeedsReview, and never guessed. The schema
// migration V20261023090200 applies the same rule to stored data.
func ParseMutations(raw string) ([]string, MutationsStatus) {
	var accepted []string
	tokenCount := 0
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' }) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		tokenCount++
		if IsMutationSubstitution(token) {
			accepted = append(accepted, token)
		}
	}
	switch {
	case tokenCount == 0:
		return nil, MutationsStatusNone
	case len(accepted) < tokenCount:
		return accepted, MutationsStatusNeedsReview
	default:
		return accepted, MutationsStatusParsed
	}
}

// DepositorPolymerEntity holds the polymer-entity values a depositor supplies on an
// entry request. They are matched to extracted entities by LabelEntityID.
type DepositorPolymerEntity struct {
	LabelEntityID   string                  `json:"label_entity_id"`
	Construct       *string                 `json:"construct,omitempty"`
	Mutations       []string                `json:"mutations,omitempty"`
	SourceOrganisms []PolymerEntityOrganism `json:"source_organisms,omitempty"`
}
