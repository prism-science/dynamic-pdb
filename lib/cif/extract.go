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

	atoms, instances, warning := readAtomSite(document)
	result.Atoms = atoms
	if warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}
	if table := document.Table("_pdbx_entity_nonpoly"); table != nil {
		for _, row := range table.Rows {
			if id := strings.ToUpper(table.Value(row, "comp_id")); id != "" {
				instances[compoundInstance{compoundID: id}] = 0
			}
		}
	}

	ligands, cofactors, err := classifyCompounds(instances, result.Connections, reference)
	if err != nil {
		return Result{}, fmt.Errorf("classify compounds: %w", err)
	}
	result.Ligands = ligands
	result.Cofactors = cofactors
	return result, nil
}

// compoundInstance is one placed copy of a non-polymer compound. Instances read
// from _pdbx_entity_nonpoly alone have no location and carry only the compound
// ID. The maps of instances hold the number of distinct atoms placed for the
// instance, 0 when unknown.
type compoundInstance struct {
	compoundID string
	asymID     string
	authSeqID  string
	insCode    string
}

func (instance compoundInstance) located() bool {
	return instance.asymID != ""
}

func partnerInstance(partner Partner) compoundInstance {
	return compoundInstance{
		compoundID: strings.ToUpper(partner.LabelCompID),
		asymID:     partner.LabelAsymID,
		authSeqID:  partner.AuthSeqID,
		insCode:    partner.InsCode,
	}
}

// classifyCompounds applies the single ligand rule. Solvents, buffers and
// cryoprotectants are dropped; nothing else is. The metal rule is per instance:
// a metal instance is a cofactor when a metalc connection joins that instance to
// a protein residue or ligand. If the file has no metalc rows at all, its metals
// are cofactors flagged for review. A metal instance without a link in a file
// that has metalc rows becomes a ligand flagged for review, so it is never
// dropped and never placed unflagged.
func classifyCompounds(
	instances map[compoundInstance]int,
	connections []Connection,
	reference *Reference,
) ([]Compound, []Compound, error) {
	hasMetalcRows := false
	linkedInstances := map[compoundInstance]bool{}
	linkedCompounds := map[string]bool{}
	for _, connection := range connections {
		if !strings.EqualFold(connection.ConnTypeID, connTypeMetalCoordination) {
			continue
		}
		hasMetalcRows = true
		first := partnerInstance(connection.Partner1)
		second := partnerInstance(connection.Partner2)
		if reference.kind(first.compoundID) == kindMetalIon && second.compoundID != "" && !reference.isSolvent(second.compoundID) {
			linkedInstances[first] = true
			linkedCompounds[first.compoundID] = true
		}
		if reference.kind(second.compoundID) == kindMetalIon && first.compoundID != "" && !reference.isSolvent(first.compoundID) {
			linkedInstances[second] = true
			linkedCompounds[second.compoundID] = true
		}
	}

	// A compound seen only in _pdbx_entity_nonpoly is skipped when atom_site
	// already placed instances of it.
	placed := map[string]bool{}
	for instance := range instances {
		if instance.located() {
			placed[instance.compoundID] = true
		}
	}
	ordered := make([]compoundInstance, 0, len(instances))
	for instance := range instances {
		if !instance.located() && placed[instance.compoundID] {
			continue
		}
		ordered = append(ordered, instance)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.compoundID != right.compoundID {
			return left.compoundID < right.compoundID
		}
		if left.asymID != right.asymID {
			return left.asymID < right.asymID
		}
		if left.authSeqID != right.authSeqID {
			return left.authSeqID < right.authSeqID
		}
		return left.insCode < right.insCode
	})

	ligands := compoundList{}
	cofactors := compoundList{}
	for _, instance := range ordered {
		id := instance.compoundID
		kind := reference.kind(id)
		switch kind {
		case kindSolvent:
			continue
		case kindCofactor:
			cofactors.add(Compound{ID: id})
		case kindLigand:
			if instances[instance] == 1 {
				ligands.add(Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "single-atom compound is not in the metal ion reference list: confirm whether it is a metal ion",
				})
				continue
			}
			ligands.add(Compound{ID: id})
		case kindMetalIon:
			linked := linkedInstances[instance]
			if !instance.located() {
				linked = linkedCompounds[id]
			}
			switch {
			case linked:
				cofactors.add(Compound{ID: id})
			case !hasMetalcRows:
				cofactors.add(Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "metal ion kept for review: the file has no metalc connection rows",
				})
			default:
				ligands.add(Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "metal ion instance has no metalc connection to a protein residue or ligand",
				})
			}
		default:
			return nil, nil, fmt.Errorf("unknown compound kind %d for %s", kind, id)
		}
	}
	return ligands.compounds(), cofactors.compounds(), nil
}

// compoundList keeps one entry per compound ID; a flagged entry wins over an
// unflagged one so a review flag on any instance is never lost.
type compoundList struct {
	entries []Compound
	index   map[string]int
}

func (list *compoundList) add(compound Compound) {
	if list.index == nil {
		list.index = map[string]int{}
	}
	position, seen := list.index[compound.ID]
	if !seen {
		list.index[compound.ID] = len(list.entries)
		list.entries = append(list.entries, compound)
		return
	}
	if compound.NeedsReview && !list.entries[position].NeedsReview {
		list.entries[position] = compound
	}
}

func (list *compoundList) compounds() []Compound {
	if list.entries == nil {
		return []Compound{}
	}
	return list.entries
}

type residueKey struct {
	chain   string
	seq     string
	insCode string
}

func readAtomSite(document *Document) (*AtomStats, map[compoundInstance]int, string) {
	instances := map[compoundInstance]int{}
	atomNames := map[compoundInstance]map[string]struct{}{}
	table := document.Table("_atom_site")
	if table == nil {
		return nil, instances, ""
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
			instance := compoundInstance{
				compoundID: compound,
				asymID:     table.Value(row, "label_asym_id"),
				authSeqID:  table.Value(row, "auth_seq_id"),
				insCode:    table.Value(row, "pdbx_pdb_ins_code"),
			}
			if atomNames[instance] == nil {
				atomNames[instance] = map[string]struct{}{}
			}
			atomNames[instance][table.Value(row, "label_atom_id")] = struct{}{}
			instances[instance] = len(atomNames[instance])
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
		return nil, instances, "atom_site coordinates were cut off by the read limit: atom counts, residue counts and altloc fraction not extracted"
	}
	stats.ModeledResidues = len(residues)
	stats.ProteinChains = len(chains)
	altlocResidues := countAltlocResidues(positions)
	if stats.ModeledResidues > 0 {
		stats.AltlocFraction = float64(altlocResidues) / float64(stats.ModeledResidues)
	}
	if stats.AtomCount == 0 {
		return nil, instances, ""
	}
	return &stats, instances, ""
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
