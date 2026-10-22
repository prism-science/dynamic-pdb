package cif

import (
	"fmt"
	"strconv"
	"strings"
)

// PDBFields reads a legacy PDB file into the same field map MMCIFFields builds.
// PDB files carry no metalc connection rows here, so metal ions are always
// flagged for review.
func PDBFields(text string) (map[string]any, error) {
	reference, err := DefaultReference()
	if err != nil {
		return nil, fmt.Errorf("load reference list: %w", err)
	}
	fields := map[string]any{}
	residues := map[residueKey]struct{}{}
	chains := map[string]struct{}{}
	positions := map[residueKey]map[string]float64{}
	compoundIDs := map[string]struct{}{}
	atomCount := 0
	for _, line := range strings.Split(text, "\n") {
		record := recordName(line)
		if record == "REMARK" {
			addPDBRemarkFields(fields, line)
			continue
		}
		if record != "ATOM" && record != "HETATM" {
			continue
		}
		compound := upperSlice(line, 17, 20)
		if record == "HETATM" && compound != "" {
			compoundIDs[compound] = struct{}{}
		}
		if record == "HETATM" && isWater(compound) {
			continue
		}
		if element := upperSlice(line, 76, 78); element == "H" || element == "D" {
			continue
		}
		atomCount++
		if record == "HETATM" {
			continue
		}
		chain := slice(line, 21, 22)
		residue := residueKey{chain: chain, seq: slice(line, 22, 26), insCode: slice(line, 26, 27)}
		residues[residue] = struct{}{}
		if chain != "" {
			chains[chain] = struct{}{}
		}
		if altID := slice(line, 16, 17); altID != "" {
			occupancy := 1.0
			if parsed, parseErr := strconv.ParseFloat(slice(line, 54, 60), 64); parseErr == nil {
				occupancy = parsed
			}
			if positions[residue] == nil {
				positions[residue] = map[string]float64{}
			}
			positions[residue][altID] = max(positions[residue][altID], occupancy)
		}
	}
	addPositiveInt(fields, "atom_count", atomCount)
	addPositiveInt(fields, "modeled_residues", len(residues))
	addPositiveInt(fields, "unique_protein_chains", len(chains))
	if len(residues) > 0 {
		addField(fields, "altloc_fraction", float64(countAltlocResidues(positions))/float64(len(residues)))
	}
	ligands, cofactors, err := classifyCompounds(compoundIDs, nil, reference)
	if err != nil {
		return nil, fmt.Errorf("classify PDB compounds: %w", err)
	}
	addCompoundFields(fields, ligands, cofactors)
	return fields, nil
}

func addPDBRemarkFields(fields map[string]any, line string) {
	if len(line) < 6 {
		return
	}
	body := strings.TrimSpace(line[6:])
	label, value, ok := strings.Cut(body, ":")
	if !ok {
		return
	}
	label = strings.Join(strings.Fields(label), " ")
	value = strings.TrimSpace(value)
	if label == "" || value == "" {
		return
	}
	addField(fields, "REMARK "+label, value)
	if strings.HasPrefix(label, "3 ") {
		addField(fields, "REMARK 3 "+strings.TrimSpace(strings.TrimPrefix(label, "3 ")), value)
	}
	if normalizedFieldKey(label) == normalizedFieldKey("3 PROGRAM") {
		program := splitProgram(value)
		addField(fields, "program.name", program.Name)
		addField(fields, "program.version", program.Version)
	}
}

func recordName(line string) string {
	if len(line) < 6 {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(line[:6])
}

func slice(line string, start int, end int) string {
	if start >= len(line) {
		return ""
	}
	if end > len(line) {
		end = len(line)
	}
	return strings.TrimSpace(line[start:end])
}

func upperSlice(line string, start int, end int) string {
	return strings.ToUpper(slice(line, start, end))
}
