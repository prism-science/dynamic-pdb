package cif

import "strconv"

// unknownAtomCount marks a compound instance whose atoms were not all read
// because the input ended inside its coordinates.
const unknownAtomCount = -1

type instanceAtoms struct {
	firstAtom string
	multiple  bool
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

// addHetero records one atom of a non-polymer instance. Only whether the
// instance has one atom or several is kept.
func (t *coordinateTally) addHetero(instance compoundInstance, atomName string) {
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
		t.hetero[instance] = instanceAtoms{firstAtom: atomName}
	case !atoms.multiple && atoms.firstAtom != atomName:
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

// instanceCounts returns the number of distinct atoms per instance: 1 or 2 for
// "one" or "several" from the coordinates, replaced by the HET record's count
// when it has one. When the coordinates were cut off, the instance being read at
// the cut may be missing atoms, so a single atom seen there is unknown.
func (t *coordinateTally) instanceCounts(coordinatesComplete bool) map[compoundInstance]int {
	counts := make(map[compoundInstance]int, len(t.hetero)+len(t.declared))
	for instance, atoms := range t.hetero {
		count := 1
		if atoms.multiple {
			count = 2
		}
		if !coordinatesComplete && count == 1 && t.hasHetero && instance == t.lastHetero {
			count = unknownAtomCount
		}
		counts[instance] = count
	}
	for instance, declared := range t.declared {
		_, observed := counts[instance]
		switch {
		case declared > 0:
			counts[instance] = declared
		case !observed:
			counts[instance] = 0
		}
	}
	return counts
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
