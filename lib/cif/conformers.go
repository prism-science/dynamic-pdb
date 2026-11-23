package cif

import (
	"cmp"
	"maps"
	"slices"
	"strings"
)

// ResidueCountVersion identifies the rules of the conformer counts ReadCoordinateIndex returns.
// The coordinate-index job stores it with the counts of every read; bump it when a rule changes, so
// stored counts can be told apart and read again.
const ResidueCountVersion = "1.0.0"

// minConformerOccupancy is the occupancy an alternate position needs on at least one heavy atom of
// its residue to count: the schema's rule for altloc_fraction (minAltlocOccupancy).
const minConformerOccupancy = minAltlocOccupancy

// ResidueKey names how the residues of a file were told apart.
type ResidueKey string

const (
	// ResidueKeyLabel: label chain + label residue number + residue type (_atom_site.label_asym_id,
	// label_seq_id, label_comp_id), so two residue types at one position stay two residues.
	ResidueKeyLabel ResidueKey = "label"
	// ResidueKeyAuthor: author chain + residue number + insertion code + residue type, for files
	// without label numbering (PDB format).
	ResidueKeyAuthor ResidueKey = "author"
	// ResidueKeyMixed: an mmCIF file whose rows have label numbering except some (phenix writes "?"
	// as the label_seq_id of some DNA chains); those residues are keyed by author numbering.
	ResidueKeyMixed ResidueKey = "mixed"
)

// ResidueConformers is one row of a file's conformer counts: how many residues of a type have a
// number of conformers.
type ResidueConformers struct {
	ResidueType    string
	ConformerCount int
	Residues       int
}

// ConformerCounts is what the conformer counting found in one file's first model.
//
// A residue's conformer count is the number of its alternate location letters whose highest
// occupancy on a heavy atom of that residue is at least 0.1; a residue with none has 1. Letters
// found only on hydrogens never count, and atoms without a letter (shared by every conformer) are
// not a position of their own. Whether the counts apply is for the caller to decide: a file with
// more than one model frame, or the file of an Ensemble model, is an ensemble, and ensembles get no
// conformer counts.
type ConformerCounts struct {
	// Rows are in (ResidueType, ConformerCount) order; empty when the first model has no polymer
	// residues.
	Rows []ResidueConformers
	// Key is how residues were told apart; empty when there are none.
	Key ResidueKey
	// Residues counts the polymer residues; MultiConformerResidues those with 2 or more conformers.
	Residues               int
	MultiConformerResidues int
	// MultipleModelFrames is set when the file has coordinate rows of a model after the first,
	// whatever they hold: more than one _atom_site.pdbx_PDB_model_num, or more than one MODEL with
	// coordinate records.
	MultipleModelFrames bool
}

// conformerResidue is one residue of the first model: its type, and the highest heavy-atom
// occupancy of each alternate location letter.
type conformerResidue struct {
	compound  string
	occupancy map[string]float64
}

// conformerAccumulator keeps the residues of the first model with their alternate location letters.
type conformerAccumulator struct {
	residues       map[string]*conformerResidue
	labelKeyed     bool
	authorKeyed    bool
	skipped        int
	lettersDropped int
	// overflow is set once the residues ran past maxIndexedResidues.
	overflow bool
}

func newConformerAccumulator() *conformerAccumulator {
	return &conformerAccumulator{residues: map[string]*conformerResidue{}}
}

func (a *conformerAccumulator) addAtom(atom polymerAtom) {
	if !atom.firstModel {
		return
	}
	if a.overflow {
		return
	}
	if !plainCopyText(atom.labelAsymID, atom.labelSeqID, atom.altID, atom.atomName, atom.element, atom.occupancy) {
		a.skipped++
		return
	}
	key := a.residueKey(atom)
	residue, seen := a.residues[key]
	if !seen {
		if len(a.residues) >= maxIndexedResidues {
			a.overflow = true
			return
		}
		residue = &conformerResidue{compound: strings.Clone(atom.compound)}
		a.residues[key] = residue
	}
	if atom.altID == "" || isHydrogenAtom(atom.element, atom.atomName) {
		return
	}
	if residue.occupancy == nil {
		residue.occupancy = map[string]float64{}
	}
	highest, seenLetter := residue.occupancy[atom.altID]
	if !seenLetter && len(residue.occupancy) >= maxAltPositionsPerResidue {
		a.lettersDropped++
		return
	}
	occupancy := parseOccupancy(atom.occupancy)
	// Assigning to a string key replaces the stored key too, so every assignment clones it: the
	// row's strings share memory with the line or buffer they were read from.
	if !seenLetter || occupancy > highest {
		residue.occupancy[strings.Clone(atom.altID)] = occupancy
	}
}

// residueKey keys a residue by label chain, label number and type where the row has label
// numbering, else by author chain, number, insertion code and type. The two kinds start with
// different letters, so they never collide.
func (a *conformerAccumulator) residueKey(atom polymerAtom) string {
	if atom.labelAsymID != "" && atom.labelSeqID != "" {
		a.labelKeyed = true
		return "L\x00" + atom.labelAsymID + "\x00" + atom.labelSeqID + "\x00" + atom.compound
	}
	a.authorKeyed = true
	return "A\x00" + atom.chain + "\x00" + atom.authSeqID + "\x00" + atom.insCode + "\x00" + atom.compound
}

// conformerCount is the number of letters with a heavy atom at or above the occupancy floor,
// minimum 1.
func (r *conformerResidue) conformerCount() int {
	counted := 0
	for _, occupancy := range r.occupancy {
		if occupancy >= minConformerOccupancy {
			counted++
		}
	}
	return max(1, counted)
}

// counts sums up the residues; multipleModelFrames is what the reader saw of the file's models.
func (a *conformerAccumulator) counts(multipleModelFrames bool) ConformerCounts {
	counts := ConformerCounts{Residues: len(a.residues), MultipleModelFrames: multipleModelFrames}
	switch {
	case a.labelKeyed && a.authorKeyed:
		counts.Key = ResidueKeyMixed
	case a.labelKeyed:
		counts.Key = ResidueKeyLabel
	case a.authorKeyed:
		counts.Key = ResidueKeyAuthor
	default:
		// No residues: no key.
	}
	type rowKey struct {
		residueType    string
		conformerCount int
	}
	rows := map[rowKey]int{}
	for _, residue := range a.residues {
		count := residue.conformerCount()
		if count >= 2 {
			counts.MultiConformerResidues++
		}
		rows[rowKey{residueType: residue.compound, conformerCount: count}]++
	}
	keys := slices.SortedFunc(maps.Keys(rows), func(left, right rowKey) int {
		return cmp.Or(cmp.Compare(left.residueType, right.residueType), cmp.Compare(left.conformerCount, right.conformerCount))
	})
	counts.Rows = make([]ResidueConformers, 0, len(keys))
	for _, key := range keys {
		counts.Rows = append(counts.Rows, ResidueConformers{ResidueType: key.residueType, ConformerCount: key.conformerCount, Residues: rows[key]})
	}
	return counts
}

// isHydrogenAtom tells a hydrogen (or deuterium) by its element, or, when the file gives none, by
// its atom name: an H or D after any leading digits ("1HB", "HD21").
func isHydrogenAtom(element string, atomName string) bool {
	switch element {
	case "H", "D":
		return true
	case "":
		name := strings.TrimLeft(strings.ToUpper(strings.TrimSpace(atomName)), "0123456789")
		return strings.HasPrefix(name, "H") || strings.HasPrefix(name, "D")
	default:
		return false
	}
}
