package archive

import (
	"sort"
	"strconv"
	"strings"

	"dynamic-pdb/lib/rcsb"
)

// Metric keys use the names of the Dynamic PDB metric vocabulary.
const (
	MetricKeyRFree                = "r_free"
	MetricKeyRWork                = "r_work"
	MetricKeyClashscore           = "clashscore"
	MetricKeyRamachandranOutliers = "ramachandran_outliers"
)

type Metric struct {
	Key   string
	Value float64
}

// ModelMapping is the model-level view of an RCSB entry, which describes the
// deposited model.
type ModelMapping struct {
	Annotations

	Authors             []string
	Details             *string
	Affiliation         *string
	AtomCount           *int
	ModeledResidues     *int
	UniqueProteinChains *int
	UnmodeledFraction   *float64
	Ligands             []string
	Metrics             []Metric
}

func MapModel(details rcsb.EntryDetails) ModelMapping {
	mapping := ModelMapping{Annotations: newAnnotations()}

	var authors []string
	for _, sourceAuthor := range details.Authors {
		authors = appendUniqueValue(authors, sourceAuthor.Name)
	}
	mapping.Authors = authors
	mapping.from("authors", "audit_author[].name")

	var affiliations []string
	for _, sourceAffiliation := range details.Publication.Affiliations {
		affiliations = appendUniqueValue(affiliations, sourceAffiliation)
	}
	mapping.Affiliation = optionalString(strings.Join(affiliations, "; "))
	mapping.from("affiliation", "pubmed.rcsb_pubmed_affiliation_info[]")

	var ligands []string
	for _, sourceLigand := range details.Info.NonpolymerBoundComponents {
		for _, value := range strings.FieldsFunc(sourceLigand, func(character rune) bool {
			return character == ',' || character == ';'
		}) {
			ligand := strings.ToUpper(strings.TrimSpace(value))
			if ligand == "HOH" || ligand == "DOD" || ligand == "WAT" || ligand == "H2O" {
				continue
			}
			ligands = appendUniqueValue(ligands, ligand)
		}
	}
	mapping.Ligands = ligands
	mapping.from("ligands", "rcsb_entry_info.nonpolymer_bound_components[]")

	mapping.Details = optionalString(details.Structure.ModelDetails)
	mapping.from("details", "struct.pdbx_model_details")
	mapping.AtomCount = details.Info.DepositedAtomCount
	mapping.from("atom_count", "rcsb_entry_info.deposited_atom_count")
	mapping.ModeledResidues = details.Info.DepositedModeledPolymerMonomerCount
	mapping.from("modeled_residues", "rcsb_entry_info.deposited_modeled_polymer_monomer_count")
	mapping.UniqueProteinChains = details.Info.DepositedPolymerEntityInstanceCount
	mapping.from("unique_protein_chains", "rcsb_entry_info.deposited_polymer_entity_instance_count")
	mapping.UnmodeledFraction = unmodeledFraction(details.Info)
	mapping.from(
		"unmodeled_fraction",
		"rcsb_entry_info.deposited_unmodeled_polymer_monomer_count/rcsb_entry_info.deposited_polymer_monomer_count",
	)

	mapping.mapMetrics(details)
	return mapping
}

func (mapping *ModelMapping) mapMetrics(details rcsb.EntryDetails) {
	refinementFree := make([]*float64, 0, len(details.Refinements))
	refinementWork := make([]*float64, 0, len(details.Refinements))
	for _, refinement := range details.Refinements {
		refinementFree = append(refinementFree, refinement.RFree)
		refinementWork = append(refinementWork, refinement.RWork)
	}
	geometryClashscore := make([]*float64, 0, len(details.ValidationGeometry))
	geometryOutliers := make([]*float64, 0, len(details.ValidationGeometry))
	for _, geometry := range details.ValidationGeometry {
		geometryClashscore = append(geometryClashscore, geometry.Clashscore)
		geometryOutliers = append(geometryOutliers, geometry.RamachandranOutliersPercent)
	}

	mapping.mapMetric(MetricKeyRFree, "refine[0].ls_R_factor_R_free", refinementFree)
	mapping.mapMetric(MetricKeyRWork, "refine[0].ls_R_factor_R_work", refinementWork)
	mapping.mapMetric(MetricKeyClashscore, "pdbx_vrpt_summary_geometry[0].clashscore", geometryClashscore)
	mapping.mapMetric(
		MetricKeyRamachandranOutliers,
		"pdbx_vrpt_summary_geometry[0].percent_ramachandran_outliers",
		geometryOutliers,
	)
	sort.Slice(mapping.Metrics, func(first int, second int) bool {
		return mapping.Metrics[first].Key < mapping.Metrics[second].Key
	})
}

// mapMetric keeps the first reported value, as the sync always has, and records a
// conflict when later records report another value.
func (mapping *ModelMapping) mapMetric(key string, path string, reported []*float64) {
	field := "metrics." + key
	mapping.from(field, path)
	if len(reported) == 0 || reported[0] == nil {
		return
	}
	mapping.Metrics = append(mapping.Metrics, Metric{Key: key, Value: *reported[0]})
	values := make([]string, 0, len(reported))
	for _, value := range reported {
		if value != nil {
			values = append(values, strconv.FormatFloat(*value, 'g', -1, 64))
		}
	}
	mapping.conflictIfDifferent(field, values)
}

func unmodeledFraction(info rcsb.EntryInfo) *float64 {
	if info.DepositedPolymerMonomerCount == nil || *info.DepositedPolymerMonomerCount <= 0 ||
		info.DepositedUnmodeledPolymerMonomerCount == nil {
		return nil
	}
	fraction := float64(*info.DepositedUnmodeledPolymerMonomerCount) /
		float64(*info.DepositedPolymerMonomerCount)
	return &fraction
}

func appendUniqueValue(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
