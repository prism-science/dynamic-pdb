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
	SourceOrganisms []SourceOrganism `json:"rcsb_entity_source_organism"`
}

type SourceOrganism struct {
	ScientificName string `json:"ncbi_scientific_name"`
}
