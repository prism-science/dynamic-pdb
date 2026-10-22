package cif

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Extract derives the registry values from a parsed mmCIF document using the
// embedded cofactor reference list.
func Extract(document *Document) (Result, error) {
	reference, err := DefaultReference()
	if err != nil {
		return Result{}, fmt.Errorf("load reference list: %w", err)
	}
	return ExtractWithReference(document, reference)
}

// ExtractWithReference is Extract with an explicit reference list.
func ExtractWithReference(document *Document, reference *Reference) (Result, error) {
	result := Result{
		Ligands:          []Compound{},
		Cofactors:        []Compound{},
		ReferenceVersion: reference.Version,
		Warnings:         []string{},
	}
	result.Connections = readConnections(document)
	result.SequenceDifferences = readSequenceDifferences(document)
	result.Mutations = readMutations(document, result.SequenceDifferences)
	result.Program = mmcifProgram(document)

	atoms, compoundIDs, warning := readAtomSite(document)
	result.Atoms = atoms
	if warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}
	if table := document.Table("_pdbx_entity_nonpoly"); table != nil {
		for _, row := range table.Rows {
			if id := strings.ToUpper(table.Value(row, "comp_id")); id != "" {
				compoundIDs[id] = struct{}{}
			}
		}
	}

	ligands, cofactors, err := classifyCompounds(compoundIDs, result.Connections, reference)
	if err != nil {
		return Result{}, fmt.Errorf("classify compounds: %w", err)
	}
	result.Ligands = ligands
	result.Cofactors = cofactors
	return result, nil
}

// classifyCompounds applies the single ligand rule. Solvents, buffers and
// cryoprotectants are dropped; nothing else is. A metal ion is a cofactor when a
// metalc connection joins it to a protein residue or ligand; if the file has no
// metalc rows at all its metals stay as cofactors flagged for review.
func classifyCompounds(
	compoundIDs map[string]struct{},
	connections []Connection,
	reference *Reference,
) ([]Compound, []Compound, error) {
	hasMetalcRows := false
	linkedMetals := map[string]bool{}
	for _, connection := range connections {
		if !strings.EqualFold(connection.ConnTypeID, connTypeMetalCoordination) {
			continue
		}
		hasMetalcRows = true
		first := strings.ToUpper(connection.Partner1.LabelCompID)
		second := strings.ToUpper(connection.Partner2.LabelCompID)
		if reference.kind(first) == kindMetalIon && !reference.isSolvent(second) && second != "" {
			linkedMetals[first] = true
		}
		if reference.kind(second) == kindMetalIon && !reference.isSolvent(first) && first != "" {
			linkedMetals[second] = true
		}
	}

	ids := make([]string, 0, len(compoundIDs))
	for id := range compoundIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	ligands := []Compound{}
	cofactors := []Compound{}
	for _, id := range ids {
		kind := reference.kind(id)
		switch kind {
		case kindSolvent:
			continue
		case kindCofactor:
			cofactors = append(cofactors, Compound{ID: id})
		case kindLigand:
			ligands = append(ligands, Compound{ID: id})
		case kindMetalIon:
			switch {
			case linkedMetals[id]:
				cofactors = append(cofactors, Compound{ID: id})
			case !hasMetalcRows:
				cofactors = append(cofactors, Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "metal ion kept for review: the file has no metalc connection rows",
				})
			default:
				ligands = append(ligands, Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "metal ion has no metalc connection to a protein residue or ligand",
				})
			}
		default:
			return nil, nil, fmt.Errorf("unknown compound kind %d for %s", kind, id)
		}
	}
	return ligands, cofactors, nil
}

type residueKey struct {
	chain   string
	seq     string
	insCode string
}

func readAtomSite(document *Document) (*AtomStats, map[string]struct{}, string) {
	compoundIDs := map[string]struct{}{}
	table := document.Table("_atom_site")
	if table == nil {
		return nil, compoundIDs, ""
	}
	incomplete := !table.Complete
	stats := AtomStats{}
	residues := map[residueKey]struct{}{}
	chains := map[string]struct{}{}
	positions := map[residueKey]map[string]float64{}
	firstModel := ""
	for _, row := range table.Rows {
		model := table.Value(row, "pdbx_pdb_model_num")
		if firstModel == "" {
			firstModel = model
		}
		if model != firstModel {
			continue
		}
		group := strings.ToUpper(table.Value(row, "group_pdb"))
		if group == "" {
			group = "ATOM"
		}
		compound := strings.ToUpper(table.Value(row, "label_comp_id"))
		if group == "HETATM" && compound != "" {
			compoundIDs[compound] = struct{}{}
		}
		if group == "HETATM" && isWater(compound) {
			continue
		}
		if element := strings.ToUpper(table.Value(row, "type_symbol")); element == "H" || element == "D" {
			continue
		}
		stats.AtomCount++
		if group == "HETATM" {
			continue
		}
		chain := firstNonEmpty(table.Value(row, "auth_asym_id"), table.Value(row, "label_asym_id"))
		residue := residueKey{
			chain:   chain,
			seq:     firstNonEmpty(table.Value(row, "auth_seq_id"), table.Value(row, "label_seq_id")),
			insCode: table.Value(row, "pdbx_pdb_ins_code"),
		}
		residues[residue] = struct{}{}
		if chain != "" {
			chains[chain] = struct{}{}
		}
		if altID := table.Value(row, "label_alt_id"); altID != "" {
			occupancy := 1.0
			if raw := table.Value(row, "occupancy"); raw != "" {
				if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
					occupancy = parsed
				}
			}
			if positions[residue] == nil {
				positions[residue] = map[string]float64{}
			}
			positions[residue][altID] = max(positions[residue][altID], occupancy)
		}
	}
	if incomplete {
		return nil, compoundIDs, "atom_site coordinates were cut off by the read limit: atom counts, residue counts and altloc fraction not extracted"
	}
	stats.ModeledResidues = len(residues)
	stats.ProteinChains = len(chains)
	altlocResidues := countAltlocResidues(positions)
	if stats.ModeledResidues > 0 {
		stats.AltlocFraction = float64(altlocResidues) / float64(stats.ModeledResidues)
	}
	if stats.AtomCount == 0 {
		return nil, compoundIDs, ""
	}
	return &stats, compoundIDs, ""
}

const minAltlocOccupancy = 0.1

// countAltlocResidues counts residues with at least two alternate positions
// that each reach the minimum occupancy.
func countAltlocResidues(positions map[residueKey]map[string]float64) int {
	count := 0
	for _, byAltID := range positions {
		occupied := 0
		for _, occupancy := range byAltID {
			if occupancy >= minAltlocOccupancy {
				occupied++
			}
		}
		if occupied >= 2 {
			count++
		}
	}
	return count
}

func isWater(compound string) bool {
	switch compound {
	case "HOH", "DOD", "WAT", "H2O":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
