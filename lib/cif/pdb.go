package cif

import (
	"context"
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
// The read stops after TimeBudget, see ExtractText.
func PDBExtraction(text string, truncated bool) (Extraction, error) {
	ctx, cancel := withTimeBudget(context.Background())
	defer cancel()
	return PDBExtractionContext(ctx, text, truncated)
}

// PDBExtractionContext is PDBExtraction bounded by ctx only.
func PDBExtractionContext(ctx context.Context, text string, truncated bool) (Extraction, error) {
	reference, err := DefaultReference()
	if err != nil {
		return Extraction{}, fmt.Errorf("load reference list: %w", err)
	}
	return pdbExtraction(ctx, text, truncated, reference)
}

func pdbExtraction(ctx context.Context, text string, truncated bool, reference *Reference) (Extraction, error) {
	if truncated {
		text = text[:strings.LastIndexByte(text, '\n')+1]
	}
	fields := map[string]any{}
	warnings := []string{}
	if !reference.Approved {
		warnings = append(warnings, DraftWarning)
	}
	tally := newCoordinateTally()
	notes := limitNotes{}
	modelStarted := false
	firstModelDone := false
	coordinatesEnded := false
	check := timeCheck{ctx: ctx}
	for rest := text; rest != ""; {
		if check.expired() {
			notes.add(timeBudgetNote)
			break
		}
		var line string
		line, rest, _ = strings.Cut(rest, "\n")
		record := recordName(line)
		switch record {
		case "REMARK":
			if len(fields) >= maxPDBFields {
				notes.add("more than %d REMARK fields: the rest were ignored", maxPDBFields)
				continue
			}
			addPDBRemarkFields(fields, line)
			continue
		case "HET":
			if instance, count := hetInstance(line); instance.compoundID != "" {
				tally.declare(instance, count)
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
		element := upperSlice(line, 76, 78)
		if record == "HETATM" && compound != "" {
			tally.addHetero(compoundInstance{
				compoundID: compound,
				asymID:     slice(line, 21, 22),
				authSeqID:  slice(line, 22, 26),
				insCode:    slice(line, 26, 27),
			}, heteroAtom{name: slice(line, 12, 16), element: element})
		}
		if record == "HETATM" && isWater(compound) {
			continue
		}
		if element == "H" || element == "D" {
			continue
		}
		tally.atomCount++
		if record == "HETATM" {
			continue
		}
		chain := slice(line, 21, 22)
		residue := residueKey{chain: chain, seq: slice(line, 22, 26), insCode: slice(line, 26, 27)}
		tally.addResidue(chain, residue, slice(line, 16, 17), func() float64 {
			return parseOccupancy(slice(line, 54, 60))
		})
	}
	coordinatesRead := (!truncated || coordinatesEnded) && check.err == nil
	instances := tally.instanceFacts(coordinatesRead)
	switch {
	case tally.overflow != "":
		coordinatesRead = false
		warnings = append(warnings, fmt.Sprintf("PDB has more distinct %s than the parser limit allows: atom counts, residue counts and altloc fraction not extracted", tally.overflow))
	case coordinatesRead:
		addPositiveInt(fields, "atom_count", tally.atomCount)
		addPositiveInt(fields, "modeled_residues", len(tally.residues))
		addPositiveInt(fields, "unique_protein_chains", len(tally.chains))
		if len(tally.residues) > 0 {
			addField(fields, "altloc_fraction", float64(countAltlocResidues(tally.positions))/float64(len(tally.residues)))
		}
	default:
		warnings = append(warnings, "PDB coordinates were cut off by the read limit: atom counts, residue counts and altloc fraction not extracted")
	}
	for _, note := range notes.messages {
		warnings = append(warnings, "parser limit: "+note)
	}
	sorted, err := classifyCompounds(instances, metalLinks{}, pdbAnnotations(), false, reference)
	if err != nil {
		return Extraction{}, fmt.Errorf("classify PDB compounds: %w", err)
	}
	if sorted.tooMany {
		warnings = append(warnings, tooManyCompoundsWarning)
	}
	review := addCompoundFields(fields, sorted.ligands, sorted.cofactors, coordinatesRead && tally.atomCount > 0)
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
