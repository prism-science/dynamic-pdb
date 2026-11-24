package cif

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ResidueCountVersion identifies the rules of the conformer counts ReadCoordinateIndex returns.
// The coordinate-index job stores it with the counts of every read; bump it when a rule changes, so
// stored counts can be told apart and read again.
const ResidueCountVersion = "1.1.0"

// minConformerOccupancy is the occupancy an alternate position needs on at least one heavy atom of
// its residue to count (researchers' answer of 17 Nov, Q1). conformer_count and altloc_fraction use
// it both: altloc_fraction is ConformerCounts.AltlocFraction.
const minConformerOccupancy = 0.1

// ResidueKey names how the residues of a file were told apart.
type ResidueKey string

const (
	// ResidueKeyLabel: label chain + label residue number + residue type (_atom_site.label_asym_id,
	// label_seq_id, label_comp_id), so two residue types at one position stay two residues.
	ResidueKeyLabel ResidueKey = "label"
	// ResidueKeyAuthor: author chain + residue number + insertion code + residue type, for files
	// without label numbering (PDB format).
	ResidueKeyAuthor ResidueKey = "author"
	// ResidueKeyMapped: residues without label numbering in the file were given it through the
	// entry's deposited mmCIF (_pdbx_poly_seq_scheme, ResidueMapping); every residue is keyed by
	// label numbering.
	ResidueKeyMapped ResidueKey = "mapped"
	// ResidueKeyMixed: some residues are keyed by label numbering (from the file or mapped) and
	// some by author numbering: rows without label numbering that no mapping covered (phenix writes
	// "?" as the label_seq_id of some DNA chains).
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
	// AuthorKeyedResidues counts the residues still keyed by author numbering: a mapping
	// (CoordinateIndex.ConformersMapped) may give them label numbering.
	AuthorKeyedResidues int
	// MappingConflicts counts the residues a mapping left keyed by author numbering because the
	// label residue it names is a different residue of the file: the file's own label numbering
	// differs from the deposited one there. Only ConformersMapped sets it.
	MappingConflicts int
	// MergedLettersDropped counts the alternate location letters left out when a mapped residue
	// merged with the file's label rows of the same residue and the two had more than
	// maxAltPositionsPerResidue letters together. Only ConformersMapped sets it.
	MergedLettersDropped int
}

const (
	mappingConflictsWarning     = "%d residues kept author numbering: the deposited mmCIF gives each the label numbering of a different residue in this file"
	mergedLettersDroppedWarning = "%d alternate location letters were left out of conformer counts: a residue mapped to label numbering has more than %d with its label rows"
)

// MappingWarnings says what a mapping (CoordinateIndex.ConformersMapped) could not do; empty for
// counts read without one.
func (counts ConformerCounts) MappingWarnings() []string {
	var warnings []string
	if counts.MappingConflicts > 0 {
		warnings = append(warnings, fmt.Sprintf(mappingConflictsWarning, counts.MappingConflicts))
	}
	if counts.MergedLettersDropped > 0 {
		warnings = append(warnings, fmt.Sprintf(mergedLettersDroppedWarning, counts.MergedLettersDropped, maxAltPositionsPerResidue))
	}
	return warnings
}

// AltlocFraction is the share of the counted residues with two or more conformers: the model's
// altloc_fraction. The researchers asked for one rule for conformer_count and altloc_fraction (17
// Nov, Q1), so both come from the same residues, key, hydrogen test and floor. False when no
// residue was counted.
func (counts ConformerCounts) AltlocFraction() (float64, bool) {
	if counts.Residues == 0 {
		return 0, false
	}
	return float64(counts.MultiConformerResidues) / float64(counts.Residues), true
}

// conformerResidue is one residue of the first model: its type, and the highest heavy-atom
// occupancy of each alternate location letter. author is set for a residue keyed by author
// numbering, so a mapping can give it label numbering. authorOfLabel is the author numbering the
// rows of a residue keyed by label numbering give (authorIdentity), empty when they give none: a
// mapped residue merges with it only when both name the same author residue.
type conformerResidue struct {
	compound      string
	occupancy     map[string]float64
	author        *authorResidue
	authorOfLabel string
}

// authorResidue is a residue's author numbering and type, as a PDB-format file writes them.
type authorResidue struct {
	chain, seq, insCode, compound string
}

// identity is the residue's authorIdentity.
func (r authorResidue) identity() string {
	return authorIdentity(r.chain, r.seq, r.insCode)
}

// authorIdentity joins an author chain, residue number and insertion code into one new string (it
// shares no memory with the row); empty when there is no number.
func authorIdentity(chain string, seq string, insCode string) string {
	if seq == "" {
		return ""
	}
	return chain + "\x00" + seq + "\x00" + insCode
}

// conformerAccumulator keeps the residues of the first model with their alternate location letters.
type conformerAccumulator struct {
	residues       map[string]*conformerResidue
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
	key, byLabel := conformerResidueKey(atom)
	residue, seen := a.residues[key]
	if !seen {
		if len(a.residues) >= maxIndexedResidues {
			a.overflow = true
			return
		}
		residue = &conformerResidue{compound: strings.Clone(atom.compound)}
		if byLabel {
			residue.authorOfLabel = authorIdentity(atom.chain, atom.authSeqID, atom.insCode)
		} else {
			residue.author = &authorResidue{
				chain: strings.Clone(atom.chain), seq: strings.Clone(atom.authSeqID),
				insCode: strings.Clone(atom.insCode), compound: residue.compound,
			}
		}
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

// conformerResidueKey keys a residue by label chain, label number and type where the row has label
// numbering (byLabel), else by author chain, number, insertion code and type. The two kinds start
// with different letters, so they never collide.
func conformerResidueKey(atom polymerAtom) (key string, byLabel bool) {
	if atom.labelAsymID != "" && atom.labelSeqID != "" {
		return labelResidueKey(atom.labelAsymID, atom.labelSeqID, atom.compound), true
	}
	return "A\x00" + atom.chain + "\x00" + atom.authSeqID + "\x00" + atom.insCode + "\x00" + atom.compound, false
}

func labelResidueKey(asymID string, seqID string, compound string) string {
	return "L\x00" + asymID + "\x00" + seqID + "\x00" + compound
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

// counts sums up the residues; multipleModelFrames is what the reader saw of the file's models. With
// a mapping, a residue keyed by author numbering that the mapping knows takes label numbering; when
// that names a residue the file already keys by label numbering, the two are one residue and their
// letters are merged.
func (a *conformerAccumulator) counts(multipleModelFrames bool, mapping *ResidueMapping) ConformerCounts {
	result := a.residuesMapped(mapping)
	residues := result.residues
	counts := ConformerCounts{
		Residues: len(residues), MultipleModelFrames: multipleModelFrames, AuthorKeyedResidues: result.authorKeyed,
		MappingConflicts: result.conflicts, MergedLettersDropped: result.lettersDropped,
	}
	switch {
	case result.authorKeyed > 0 && result.labelKeyed+result.mapped > 0:
		counts.Key = ResidueKeyMixed
	case result.authorKeyed > 0:
		counts.Key = ResidueKeyAuthor
	case result.mapped > 0:
		counts.Key = ResidueKeyMapped
	case result.labelKeyed > 0:
		counts.Key = ResidueKeyLabel
	default:
		// No residues: no key.
	}
	type rowKey struct {
		residueType    string
		conformerCount int
	}
	rows := map[rowKey]int{}
	for _, residue := range residues {
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

// mappedResidues are a file's residues with a mapping applied: how many are keyed by the file's
// label numbering, by mapped label numbering and by author numbering; conflicts counts the author
// residues left unmapped because their label residue is another residue of the file, lettersDropped
// the letters left out when two rows of one residue merged.
type mappedResidues struct {
	residues                        map[string]*conformerResidue
	labelKeyed, mapped, authorKeyed int
	conflicts, lettersDropped       int
}

// residuesMapped gives the residues keyed by author numbering the label numbering the mapping knows.
// A mapped residue whose label residue the file already has merges with it only when the file's label
// rows give the same author numbering; otherwise they are two residues and the mapped one keeps its
// author key (a conflict). Author residues are taken in key order, so the result does not depend on
// map order.
func (a *conformerAccumulator) residuesMapped(mapping *ResidueMapping) mappedResidues {
	result := mappedResidues{residues: a.residues}
	for _, residue := range a.residues {
		if residue.author == nil {
			result.labelKeyed++
		} else {
			result.authorKeyed++
		}
	}
	if mapping == nil || result.authorKeyed == 0 {
		return result
	}
	result.residues = make(map[string]*conformerResidue, len(a.residues))
	authorKeys := make([]string, 0, result.authorKeyed)
	for key, residue := range a.residues {
		if residue.author == nil {
			result.residues[key] = residue
		} else {
			authorKeys = append(authorKeys, key)
		}
	}
	slices.Sort(authorKeys)
	result.authorKeyed = 0
	for _, key := range authorKeys {
		residue := a.residues[key]
		label, known := mapping.label(*residue.author)
		if !known {
			result.residues[key] = residue
			result.authorKeyed++
			continue
		}
		labelKey := labelResidueKey(label.asymID, label.seqID, residue.compound)
		existing, seen := result.residues[labelKey]
		switch {
		case !seen:
			result.residues[labelKey] = residue
			result.mapped++
		case existing.author == nil && existing.authorOfLabel != "" && existing.authorOfLabel == residue.author.identity():
			merged, dropped := mergeConformerLetters(existing, residue)
			result.residues[labelKey] = merged
			result.lettersDropped += dropped
			result.mapped++
		default:
			result.residues[key] = residue
			result.authorKeyed++
			result.conflicts++
		}
	}
	return result
}

// mergeConformerLetters is one residue with the letters of both: the highest occupancy of each
// letter, existing's letters first and other's in letter order up to maxAltPositionsPerResidue. It
// returns how many of other's letters were left out.
func mergeConformerLetters(existing *conformerResidue, other *conformerResidue) (*conformerResidue, int) {
	merged := &conformerResidue{compound: existing.compound, occupancy: maps.Clone(existing.occupancy), authorOfLabel: existing.authorOfLabel}
	dropped := 0
	for _, letter := range slices.Sorted(maps.Keys(other.occupancy)) {
		occupancy := other.occupancy[letter]
		highest, seen := merged.occupancy[letter]
		switch {
		case seen:
			merged.occupancy[letter] = max(highest, occupancy)
		case len(merged.occupancy) >= maxAltPositionsPerResidue:
			dropped++
		default:
			if merged.occupancy == nil {
				merged.occupancy = map[string]float64{}
			}
			merged.occupancy[letter] = occupancy
		}
	}
	return merged, dropped
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
