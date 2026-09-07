package rcsb

type Artifact struct {
	Filename string
	Format   string
	URI      string
	Contents []byte
}

type EntryDetails struct {
	Structure   EntryStructure            `json:"struct"`
	Experiments []EntryExperiment         `json:"exptl"`
	Info        EntryInfo                 `json:"rcsb_entry_info"`
	Symmetry    EntrySymmetry             `json:"symmetry"`
	Identifiers EntryContainerIdentifiers `json:"rcsb_entry_container_identifiers"`
}

type EntryStructure struct {
	Title string `json:"title"`
}

type EntryExperiment struct {
	Method string `json:"method"`
}

type EntryInfo struct {
	CombinedResolution []float64 `json:"resolution_combined"`
}

type EntrySymmetry struct {
	SpaceGroup string `json:"space_group_name_H_M"`
}

type EntryContainerIdentifiers struct {
	PolymerEntityIDs []string `json:"polymer_entity_ids"`
}

type PolymerEntityDetails struct {
	Entity          PolymerEntityData                 `json:"rcsb_polymer_entity"`
	Polymer         PolymerData                       `json:"entity_poly"`
	Identifiers     PolymerEntityContainerIdentifiers `json:"rcsb_polymer_entity_container_identifiers"`
	SourceOrganisms []SourceOrganism                  `json:"rcsb_entity_source_organism"`
}

type PolymerEntityData struct {
	Description string `json:"pdbx_description"`
	Fragment    string `json:"pdbx_fragment"`
	Mutation    string `json:"pdbx_mutation"`
}

type PolymerData struct {
	CanonicalSequence string `json:"pdbx_seq_one_letter_code_can"`
}

type PolymerEntityContainerIdentifiers struct {
	EntityID                     string                        `json:"entity_id"`
	ReferenceSequenceIdentifiers []ReferenceSequenceIdentifier `json:"reference_sequence_identifiers"`
}

type ReferenceSequenceIdentifier struct {
	DatabaseAccession string `json:"database_accession"`
	DatabaseName      string `json:"database_name"`
	ProvenanceSource  string `json:"provenance_source"`
}

type SourceOrganism struct {
	ScientificName string `json:"ncbi_scientific_name"`
	NCBITaxonomyID *int   `json:"ncbi_taxonomy_id"`
}
