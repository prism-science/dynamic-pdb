package rcsb

type Artifact struct {
	Filename string
	Format   string
	URI      string
	Contents []byte
}

type EntryDetails struct {
	Structure          EntryStructure            `json:"struct"`
	Experiments        []EntryExperiment         `json:"exptl"`
	Crystals           []EntryCrystal            `json:"exptl_crystal"`
	CrystalGrowth      []EntryCrystalGrowth      `json:"exptl_crystal_grow"`
	Diffractions       []EntryDiffraction        `json:"diffrn"`
	Info               EntryInfo                 `json:"rcsb_entry_info"`
	Symmetry           EntrySymmetry             `json:"symmetry"`
	Identifiers        EntryContainerIdentifiers `json:"rcsb_entry_container_identifiers"`
	Authors            []EntryAuthor             `json:"audit_author"`
	Publication        EntryPublication          `json:"pubmed"`
	Refinements        []EntryRefinement         `json:"refine"`
	ValidationGeometry []EntryValidationGeometry `json:"pdbx_vrpt_summary_geometry"`
}

type EntryStructure struct {
	Title        string `json:"title"`
	Details      string `json:"pdbx_details"`
	ModelDetails string `json:"pdbx_model_details"`
}

type EntryExperiment struct {
	Method string `json:"method"`
}

type EntryCrystal struct {
	ID string `json:"id"`
}

type EntryCrystalGrowth struct {
	CrystalID         string   `json:"crystal_id"`
	PH                *float64 `json:"pH"`
	TemperatureKelvin *float64 `json:"temp"`
}

type EntryDiffraction struct {
	ID                string   `json:"id"`
	CrystalID         string   `json:"crystal_id"`
	TemperatureKelvin *float64 `json:"ambient_temp"`
}

type EntryInfo struct {
	CombinedResolution                    []float64 `json:"resolution_combined"`
	DepositedAtomCount                    *int      `json:"deposited_atom_count"`
	DepositedPolymerMonomerCount          *int      `json:"deposited_polymer_monomer_count"`
	DepositedModeledPolymerMonomerCount   *int      `json:"deposited_modeled_polymer_monomer_count"`
	DepositedUnmodeledPolymerMonomerCount *int      `json:"deposited_unmodeled_polymer_monomer_count"`
	DepositedPolymerEntityInstanceCount   *int      `json:"deposited_polymer_entity_instance_count"`
	NonpolymerBoundComponents             []string  `json:"nonpolymer_bound_components"`
}

type EntrySymmetry struct {
	SpaceGroup string `json:"space_group_name_H_M"`
}

type EntryContainerIdentifiers struct {
	PolymerEntityIDs []string `json:"polymer_entity_ids"`
}

type EntryAuthor struct {
	Name string `json:"name"`
}

type EntryPublication struct {
	Affiliations []string `json:"rcsb_pubmed_affiliation_info"`
}

type EntryRefinement struct {
	RFree *float64 `json:"ls_R_factor_R_free"`
	RWork *float64 `json:"ls_R_factor_R_work"`
}

type EntryValidationGeometry struct {
	Clashscore                  *float64 `json:"clashscore"`
	RamachandranOutliersPercent *float64 `json:"percent_ramachandran_outliers"`
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
