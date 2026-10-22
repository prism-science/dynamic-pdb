package cif

import (
	"fmt"
	"strconv"
	"strings"
)

// PDBFields reads a legacy PDB file into the same field map MMCIFFields builds.
// See PDBExtraction.
func PDBFields(text string) (map[string]any, error) {
	extraction, err := PDBExtraction(text, false)
	if err != nil {
		return nil, err
	}
	return extraction.Fields, nil
}

// PDBExtraction reads a legacy PDB file. PDB files carry no metalc connection
// rows here, so metal ions are always flagged for review. Only the first model
// is counted, as for mmCIF. Set truncated when text is only the head of a larger
// file: a line cut off at the end is dropped, and when the coordinates were not
// seen to end (no ENDMDL, CONECT, MASTER or END record) the counts that need all
// of them are omitted.
func PDBExtraction(text string, truncated bool) (Extraction, error) {
	reference, err := DefaultReference()
	if err != nil {
		return Extraction{}, fmt.Errorf("load reference list: %w", err)
	}
	return pdbExtraction(text, truncated, reference)
}

func pdbExtraction(text string, truncated bool, reference *Reference) (Extraction, error) {
	if truncated {
		text = text[:strings.LastIndexByte(text, '\n')+1]
	}
	fields := map[string]any{}
	warnings := []string{}
	if !reference.Approved {
		warnings = append(warnings, DraftWarning)
	}
	residues := map[residueKey]struct{}{}
	chains := map[string]struct{}{}
	positions := map[residueKey]map[string]float64{}
	instances := map[compoundInstance]int{}
	atomNames := map[compoundInstance]map[string]struct{}{}
	atomCount := 0
	modelStarted := false
	firstModelDone := false
	coordinatesEnded := false
	for rest := text; rest != ""; {
		var line string
		line, rest, _ = strings.Cut(rest, "\n")
		record := recordName(line)
		switch record {
		case "REMARK":
			addPDBRemarkFields(fields, line)
			continue
		case "HET":
			if instance, count := hetInstance(line); instance.compoundID != "" {
				instances[instance] = count
			}
			continue
		case "MODEL":
			if modelStarted {
				firstModelDone = true
			}
			modelStarted = true
			continue
		case "ENDMDL":
			firstModelDone = true
			coordinatesEnded = true
			continue
		case "CONECT", "MASTER", "END":
			coordinatesEnded = true
			continue
		case "ATOM", "HETATM":
		default:
			// Every other record (TER, SEQRES, CRYST1, ...) carries nothing we read.
			continue
		}
		if firstModelDone {
			continue
		}
		compound := upperSlice(line, 17, 20)
		if record == "HETATM" && compound != "" {
			instance := compoundInstance{
				compoundID: compound,
				asymID:     slice(line, 21, 22),
				authSeqID:  slice(line, 22, 26),
				insCode:    slice(line, 26, 27),
			}
			if atomNames[instance] == nil {
				atomNames[instance] = map[string]struct{}{}
			}
			atomNames[instance][slice(line, 12, 16)] = struct{}{}
			instances[instance] = len(atomNames[instance])
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
	coordinatesRead := !truncated || coordinatesEnded
	if coordinatesRead {
		addPositiveInt(fields, "atom_count", atomCount)
		addPositiveInt(fields, "modeled_residues", len(residues))
		addPositiveInt(fields, "unique_protein_chains", len(chains))
		if len(residues) > 0 {
			addField(fields, "altloc_fraction", float64(countAltlocResidues(positions))/float64(len(residues)))
		}
	} else {
		warnings = append(warnings, "PDB coordinates were cut off by the read limit: atom counts, residue counts and altloc fraction not extracted")
	}
	ligands, cofactors, err := classifyCompounds(instances, nil, reference)
	if err != nil {
		return Extraction{}, fmt.Errorf("classify PDB compounds: %w", err)
	}
	review := addCompoundFields(fields, ligands, cofactors, coordinatesRead && atomCount > 0)
	return Extraction{Fields: fields, Warnings: warnings, Review: review}, nil
}

// hetInstance reads a HET record: compound, chain, number and insertion code,
// then the number of atoms (0 when the count is missing).
func hetInstance(line string) (compoundInstance, int) {
	instance := compoundInstance{
		compoundID: upperSlice(line, 7, 10),
		asymID:     slice(line, 12, 13),
		authSeqID:  slice(line, 13, 17),
		insCode:    slice(line, 17, 18),
	}
	count, err := strconv.Atoi(slice(line, 20, 25))
	if err != nil {
		return instance, 0
	}
	return instance, count
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
