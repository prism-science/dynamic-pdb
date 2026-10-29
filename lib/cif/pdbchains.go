package cif

import (
	"slices"
	"strconv"
	"strings"
)

// pdbChains collects what a PDB-format file says about its polymer chains and
// metal links: the residue names in each chain's SEQRES records, the residues
// MODRES lists, where each chain's TER record falls, the residue numbers of its
// ATOM records, and the LINK records. After the read, position decides whether
// a HETATM residue is part of a chain (selenomethionine) or not (a free
// glutamate after the chain's TER record).
type pdbChains struct {
	seqres map[string]map[string]bool
	// modified holds the MODRES residues; modifiedNames their IDs in file order.
	modified      map[compoundInstance]bool
	modifiedNames []string
	// terLine is the line number of each chain's first TER record.
	terLine map[string]int
	// atomRange is the span of residue numbers of each chain's ATOM records.
	atomRange map[string]residueRange
	// firstLine is the line number of each instance's first HETATM record.
	firstLine map[compoundInstance]int
	// lastChain is the chain of the last coordinate record, for a TER record
	// that names none.
	lastChain string
	linkPairs []metalPair
	linkSeen  map[metalPair]struct{}
	// linksOverflow is set when distinct LINK pairs ran past maxMetalLinks.
	linksOverflow bool
}

type residueRange struct {
	first, last int
}

func newPDBChains() *pdbChains {
	return &pdbChains{
		seqres:    map[string]map[string]bool{},
		modified:  map[compoundInstance]bool{},
		terLine:   map[string]int{},
		atomRange: map[string]residueRange{},
		firstLine: map[compoundInstance]int{},
		linkSeen:  map[metalPair]struct{}{},
	}
}

// addSeqres reads a SEQRES record: the chain and up to 13 residue names.
func (c *pdbChains) addSeqres(line string) {
	chain := slice(line, 11, 12)
	if len(line) <= 19 {
		return
	}
	names := c.seqres[chain]
	if names == nil {
		names = map[string]bool{}
		c.seqres[chain] = names
	}
	for _, name := range strings.Fields(line[19:]) {
		names[strings.ToUpper(name)] = true
	}
}

// addModres reads a MODRES record: a modified residue of a chain.
func (c *pdbChains) addModres(line string) {
	instance := compoundInstance{
		compoundID: upperSlice(line, 12, 15),
		asymID:     slice(line, 16, 17),
		authSeqID:  slice(line, 18, 22),
		insCode:    slice(line, 22, 23),
	}
	if instance.compoundID == "" || len(c.modified) >= maxDistinctInstances {
		return
	}
	c.modified[instance] = true
	if !slices.Contains(c.modifiedNames, instance.compoundID) {
		c.modifiedNames = append(c.modifiedNames, instance.compoundID)
	}
}

// addTer records where a chain's polymer block ends. A TER record without a
// chain ends the chain of the coordinates just read.
func (c *pdbChains) addTer(line string, lineNumber int) {
	chain := slice(line, 21, 22)
	if chain == "" {
		chain = c.lastChain
	}
	if _, seen := c.terLine[chain]; !seen {
		c.terLine[chain] = lineNumber
	}
}

// addCoordinate records the chain of a coordinate record, the residue span of
// ATOM records, and the line of each instance's first HETATM record.
func (c *pdbChains) addCoordinate(record string, line string, lineNumber int, instance compoundInstance) {
	chain := slice(line, 21, 22)
	c.lastChain = chain
	if record == "ATOM" {
		if number, err := strconv.Atoi(slice(line, 22, 26)); err == nil {
			span, seen := c.atomRange[chain]
			if !seen {
				span = residueRange{first: number, last: number}
			}
			c.atomRange[chain] = residueRange{first: min(span.first, number), last: max(span.last, number)}
		}
		return
	}
	if _, seen := c.firstLine[instance]; !seen && len(c.firstLine) < maxDistinctInstances {
		c.firstLine[instance] = lineNumber
	}
}

// addLink reads a LINK record: two residues joined by a covalent or metal
// coordination bond. Identical pairs are kept once.
func (c *pdbChains) addLink(line string) {
	pair := metalPair{
		first: compoundInstance{
			compoundID: upperSlice(line, 17, 20),
			asymID:     slice(line, 21, 22),
			authSeqID:  slice(line, 22, 26),
			insCode:    slice(line, 26, 27),
		},
		second: compoundInstance{
			compoundID: upperSlice(line, 47, 50),
			asymID:     slice(line, 51, 52),
			authSeqID:  slice(line, 52, 56),
			insCode:    slice(line, 56, 57),
		},
	}
	if _, duplicate := c.linkSeen[pair]; duplicate {
		return
	}
	if len(c.linkSeen) >= maxMetalLinks {
		c.linksOverflow = true
		return
	}
	c.linkSeen[pair] = struct{}{}
	c.linkPairs = append(c.linkPairs, pair)
}

// sequenceNames returns the residue names of every chain's SEQRES records.
func (c *pdbChains) sequenceNames() map[string]bool {
	names := map[string]bool{}
	for _, chainNames := range c.seqres {
		for name := range chainNames {
			names[name] = true
		}
	}
	return names
}

// metalLinks returns the LINK records as the metal rule reads metalc rows. A
// file with a LINK record that involves a metal ion is treated like an mmCIF
// file with metalc rows; LINK records between other residues do not count.
func (c *pdbChains) metalLinks(reference *Reference) metalLinks {
	links := metalLinks{pairs: c.linkPairs, overflow: c.linksOverflow}
	for _, pair := range c.linkPairs {
		if reference.kind(pair.first.compoundID) == kindMetalIon || reference.kind(pair.second.compoundID) == kindMetalIon {
			links.hasRows = true
			break
		}
	}
	return links
}

// position decides whether a HETATM residue is part of a polymer chain. A
// residue MODRES lists is a member. Otherwise it must sit in the chain's
// polymer block: before the chain's TER record, or, for a chain without one or
// a residue the coordinates did not reach, within the residue numbers of the
// chain's ATOM records. In the block, it is a member when its name is in the
// chain's SEQRES, and only "within the span" when the chain has no SEQRES.
func (c *pdbChains) position(instance compoundInstance) chainPosition {
	if c.modified[instance] {
		return chainMember
	}
	names, hasSeqres := c.seqres[instance.asymID]
	switch {
	case !c.inPolymerBlock(instance):
		return chainNone
	case !hasSeqres:
		return chainSpanOnly
	case names[instance.compoundID]:
		return chainMember
	default:
		return chainNone
	}
}

func (c *pdbChains) inPolymerBlock(instance compoundInstance) bool {
	terLine, hasTer := c.terLine[instance.asymID]
	firstLine, seen := c.firstLine[instance]
	if hasTer && seen {
		return firstLine < terLine
	}
	span, hasAtoms := c.atomRange[instance.asymID]
	number, err := strconv.Atoi(instance.authSeqID)
	return hasAtoms && err == nil && number >= span.first && number <= span.last
}
