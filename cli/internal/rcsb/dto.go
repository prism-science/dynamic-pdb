package rcsb

type Entry struct {
	Struct                    *Struct                    `json:"struct,omitempty"`
	Experiments               []Experiment               `json:"exptl,omitempty"`
	EntryInfo                 *EntryInfo                 `json:"rcsb_entry_info,omitempty"`
	Symmetry                  *Symmetry                  `json:"symmetry,omitempty"`
	EntryContainerIdentifiers *EntryContainerIdentifiers `json:"rcsb_entry_container_identifiers,omitempty"`
	Refinements               []Refinement               `json:"refine,omitempty"`
	AuditAuthors              []AuditAuthor              `json:"audit_author,omitempty"`
	PrimaryCitation           *PrimaryCitation           `json:"rcsb_primary_citation,omitempty"`
	PubMed                    *PubMed                    `json:"pubmed,omitempty"`
}

type Struct struct {
	Title string `json:"title,omitempty"`
}

type Experiment struct {
	Method string `json:"method,omitempty"`
}

type EntryInfo struct {
	ResolutionCombined                  []float64 `json:"resolution_combined,omitempty"`
	DepositedAtomCount                  *int      `json:"deposited_atom_count,omitempty"`
	DepositedModeledPolymerMonomerCount *int      `json:"deposited_modeled_polymer_monomer_count,omitempty"`
	DepositedPolymerMonomerCount        *int      `json:"deposited_polymer_monomer_count,omitempty"`
	DepositedPolymerEntityInstanceCount *int      `json:"deposited_polymer_entity_instance_count,omitempty"`
	NonpolymerBoundComponents           []string  `json:"nonpolymer_bound_components,omitempty"`
}

type Symmetry struct {
	SpaceGroupNameHM         string `json:"space_group_name_H_M,omitempty"`
	PDBXFullSpaceGroupNameHM string `json:"pdbx_full_space_group_name_H_M,omitempty"`
}

type EntryContainerIdentifiers struct {
	PolymerEntityIDs []string `json:"polymer_entity_ids,omitempty"`
}

type Refinement struct {
	LSRFactorRFree *float64 `json:"ls_R_factor_R_free,omitempty"`
	LSRFactorRWork *float64 `json:"ls_R_factor_R_work,omitempty"`
}

type AuditAuthor struct {
	Name string `json:"name,omitempty"`
}

type PrimaryCitation struct {
	Authors []string `json:"rcsb_authors,omitempty"`
}

type PubMed struct {
	Affiliations []string `json:"rcsb_pubmed_affiliation_info,omitempty"`
}

type PolymerEntity struct {
	SourceOrganisms []EntitySourceOrganism `json:"rcsb_entity_source_organism,omitempty"`
}

type EntitySourceOrganism struct {
	NCBIScientificName string `json:"ncbi_scientific_name,omitempty"`
}

type Artifact struct {
	Filename string
	Format   string
	URI      string
	Contents []byte
}

type PreviewImage struct {
	Filename string
	Format   string
	URI      string
	Contents []byte
}
