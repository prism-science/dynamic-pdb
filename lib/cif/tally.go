package cif

import "strconv"

// unknownAtomCount marks a compound instance whose atoms were not all read
// because the input ended inside its coordinates.
const unknownAtomCount = -1

// heteroAtom is one HETATM row as the compound rules need it.
type heteroAtom struct {
	name string
	// element is the upper-case element symbol.
	element string
	// entityID is the row's label_entity_id; empty in PDB-format files.
	entityID string
	// inChain is set when the row has a label_seq_id: the residue sits in a
	// polymer chain.
	inChain bool
	// laterModel is set for a row of any model after the first.
	laterModel bool
}

type instanceAtoms struct {
	// first is the instance's first atom; its facts stand for the instance.
	first    heteroAtom
	multiple bool
}

// chainPosition is what a file says about a residue's place in a polymer chain.
type chainPosition int

const (
	// chainNone: nothing places it in a chain.
	chainNone chainPosition = iota
	// chainMember: a PDB-format MODRES record, or a name in the chain's SEQRES
	// at a position in its polymer block. Polymer whatever its CCD type.
	chainMember
	// chainSpanOnly: within a chain, but nothing names it as a chain residue:
	// an mmCIF label_seq_id (writers set it unevenly for ligands), or a
	// PDB-format chain without SEQRES records. When the file says what its
	// chains hold, only a peptide or nucleotide counts as in the chain; when it
	// does not (instanceFacts.chainsUndeclared), the residue is flagged.
	chainSpanOnly
)

func chainPositionOf(hasLabelSeqID bool) chainPosition {
	if hasLabelSeqID {
		return chainSpanOnly
	}
	return chainNone
}

// instanceFacts is what the coordinates say about one compound instance.
type instanceFacts struct {
	// atoms is the number of distinct atoms: 1, or 2 for several, replaced by
	// the HET record's count when it has one; 0 when unknown, unknownAtomCount
	// when the coordinates were cut off inside the instance.
	atoms int
	// element is the upper-case element symbol of the first atom read, empty
	// when no atom of the instance was read.
	element string
	// entityID is the instance's label_entity_id, empty when unknown.
	entityID string
	// chain is what the file says about the residue's place in a polymer
	// chain: an mmCIF label_seq_id places it within one; a PDB-format file
	// decides it after the read (pdbChains.position).
	chain chainPosition
	// chainsUndeclared is set when the file does not say which residues the
	// instance's polymer chain holds: an mmCIF file without _entity, a
	// PDB-format file without SEQRES records, or a PDB-format chain with ATOM
	// records but no SEQRES. A residue that could be a chain residue is then
	// flagged instead of guessed.
	chainsUndeclared bool
	// laterModel is set when the instance appears only in models after the
	// first; an integrative (IHM) file lists those compounds too.
	laterModel bool
}

// coordinateTally reduces coordinate rows to counts as they arrive, with every
// set bounded, so a large or hostile coordinate block never has to be held.
type coordinateTally struct {
	atomCount  int
	residues   map[residueKey]struct{}
	chains     map[string]struct{}
	positions  map[residueKey]map[string]float64
	hetero     map[compoundInstance]instanceAtoms
	declared   map[compoundInstance]int
	lastHetero compoundInstance
	hasHetero  bool
	// overflow names the limit that stopped the tally; counts are unusable then.
	overflow string
}

func newCoordinateTally() *coordinateTally {
	return &coordinateTally{
		residues:  map[residueKey]struct{}{},
		chains:    map[string]struct{}{},
		positions: map[residueKey]map[string]float64{},
		hetero:    map[compoundInstance]instanceAtoms{},
		declared:  map[compoundInstance]int{},
	}
}

// addHetero records one HETATM row of an instance. Only whether the instance
// has one atom or several, and the facts of its first atom, are kept. Rows of a
// later model add an instance the first model lacks, never atoms to one it has.
func (t *coordinateTally) addHetero(instance compoundInstance, atom heteroAtom) {
	if t.overflow != "" {
		return
	}
	t.lastHetero, t.hasHetero = instance, true
	atoms, seen := t.hetero[instance]
	switch {
	case !seen:
		if len(t.hetero) >= maxDistinctInstances {
			t.overflow = "compound instances"
			return
		}
		t.hetero[instance] = instanceAtoms{first: atom}
	case atom.laterModel && !atoms.first.laterModel:
		return
	case !atoms.multiple && atoms.first.name != atom.name:
		atoms.multiple = true
		t.hetero[instance] = atoms
	}
}

// declare records the atom count a HET record gives for an instance; 0 means
// the record has none.
func (t *coordinateTally) declare(instance compoundInstance, count int) {
	if t.overflow != "" {
		return
	}
	if _, seen := t.declared[instance]; !seen && len(t.declared) >= maxDistinctInstances {
		t.overflow = "compound instances"
		return
	}
	t.declared[instance] = count
}

// addResidue records one polymer atom. altID is empty without an alternate
// location; occupancy is only read when there is one.
func (t *coordinateTally) addResidue(chain string, residue residueKey, altID string, occupancy func() float64) {
	if t.overflow != "" {
		return
	}
	if _, seen := t.residues[residue]; !seen {
		if len(t.residues) >= maxDistinctResidues {
			t.overflow = "residues"
			return
		}
		t.residues[residue] = struct{}{}
	}
	if chain != "" {
		if _, seen := t.chains[chain]; !seen {
			if len(t.chains) >= maxDistinctChains {
				t.overflow = "chains"
				return
			}
			t.chains[chain] = struct{}{}
		}
	}
	if altID == "" {
		return
	}
	byAltID := t.positions[residue]
	if byAltID == nil {
		byAltID = map[string]float64{}
		t.positions[residue] = byAltID
	}
	if _, seen := byAltID[altID]; !seen && len(byAltID) >= maxAltPositionsPerResidue {
		return
	}
	byAltID[altID] = max(byAltID[altID], occupancy())
}

// instanceFacts returns, per instance, the number of distinct atoms (1 or 2
// for "one" or "several" from the coordinates, replaced by the HET record's
// count when it has one) and the first atom's element. When the coordinates
// were cut off, the instance being read at the cut may be missing atoms, so a
// single atom seen there is unknown.
func (t *coordinateTally) instanceFacts(coordinatesComplete bool) map[compoundInstance]instanceFacts {
	facts := make(map[compoundInstance]instanceFacts, len(t.hetero)+len(t.declared))
	for instance, atoms := range t.hetero {
		count := 1
		if atoms.multiple {
			count = 2
		}
		if !coordinatesComplete && count == 1 && t.hasHetero && instance == t.lastHetero {
			count = unknownAtomCount
		}
		facts[instance] = instanceFacts{
			atoms:      count,
			element:    atoms.first.element,
			entityID:   atoms.first.entityID,
			chain:      chainPositionOf(atoms.first.inChain),
			laterModel: atoms.first.laterModel,
		}
	}
	for instance, declared := range t.declared {
		observed, seen := facts[instance]
		switch {
		case declared > 0:
			observed.atoms = declared
			facts[instance] = observed
		case !seen:
			facts[instance] = instanceFacts{}
		}
	}
	return facts
}

// parseOccupancy reads an occupancy; a missing or unreadable value counts as 1.
func parseOccupancy(raw string) float64 {
	if raw == "" {
		return 1.0
	}
	if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
		return parsed
	}
	return 1.0
}
