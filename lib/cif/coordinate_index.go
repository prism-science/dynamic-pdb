package cif

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// CoordinateIndexVersion identifies the rules of ReadCoordinateIndex. The coordinate-index job
// stores it with every read; bump it when what the index holds changes, so stored reads can be
// told apart and read again. 1.1.0: a chain whose entity has an _entity_poly.type is a protein
// chain by that type.
const CoordinateIndexVersion = "1.1.0"

// ChainLengthSource names where a chain's deposited sequence length was read in the file.
type ChainLengthSource string

const (
	// LengthFromEntityPolySeq: the distinct positions of the chain's entity in _entity_poly_seq.
	LengthFromEntityPolySeq ChainLengthSource = "entity_poly_seq"
	// LengthFromSeqres: the residue names of the chain's SEQRES records (PDB format).
	LengthFromSeqres ChainLengthSource = "seqres"
)

// ProteinSource names the test that decided whether a chain is a protein chain.
type ProteinSource string

const (
	// ProteinFromEntityPolyType: the _entity_poly.type of the chain's entity; polypeptide(L) and
	// polypeptide(D) are protein.
	ProteinFromEntityPolyType ProteinSource = "entity_poly_type"
	// ProteinFromResidueMajority: the file states no polymer type for the chain (PDB format,
	// computed mmCIF files), so more than half of its modeled residues must have a peptide CCD
	// type.
	ProteinFromResidueMajority ProteinSource = "residue_majority"
)

// proteinPolymerTypes are the _entity_poly.type values of protein entities, lower case.
var proteinPolymerTypes = map[string]bool{"polypeptide(l)": true, "polypeptide(d)": true}

// IndexedChain is one author chain of the first model of a structure file.
type IndexedChain struct {
	// AuthAsymID is the author chain: _atom_site.auth_asym_id, or PDB column 22. Phenix-written
	// mmCIF gives some modified residues a label chain of their own; they stay in their author
	// chain here.
	AuthAsymID string
	// EntityID is the label_entity_id most of the chain's residues have; empty when the file
	// states none (PDB format, most computed mmCIF files).
	EntityID string
	// DepositedLength is the chain's sequence length as the file states it, 0 when it does not;
	// LengthSource says where it was read, empty when it is 0.
	DepositedLength int
	LengthSource    ChainLengthSource
	// ModeledResidues counts the chain's polymer residues with coordinates: distinct author
	// residue numbers and insertion codes, so two residue types at one position count once.
	ModeledResidues int
	// Protein is set for a protein chain, as ProteinSource decided: by the _entity_poly.type of
	// the chain's entity when the file states it, else when more than half of the modeled residues
	// have a peptide CCD type.
	Protein       bool
	ProteinSource ProteinSource
}

// CoordinateIndex is what ReadCoordinateIndex found in one file.
type CoordinateIndex struct {
	// Chains are in AuthAsymID order.
	Chains   []IndexedChain
	Warnings []string
}

const (
	noGroupIndexWarning        = "the coordinates have no group_PDB column: only rows with a label_seq_id were counted as polymer residues"
	unexpectedGroupRowsWarning = "%d coordinate rows have a group_PDB value other than ATOM or HETATM: they were skipped"
	skippedRowsWarning         = "%d coordinate rows were skipped: a chain, residue or compound field is not plain text of at most %d bytes"
	skippedSequenceRowsWarning = "%d _entity_poly_seq rows were skipped: an entity or position is not plain text of at most %d bytes"
	skippedPolymerRowsWarning  = "%d _entity_poly rows were skipped: an entity or type is not plain text of at most %d bytes"
	noPolymerWarning           = "the first model has no polymer residues"
	cutOffFileError            = "the %s loop ended inside a row: the file is cut off"
	tooManyResiduesError       = "more than %d polymer residues in the first model"
	tooManyPolymerEntityError  = "more than %d _entity_poly entities"
	tooManyPendingAtomError    = "more than %d HETATM records wait to be sorted into or out of their chain"
)

// ErrCoordinateIndexLimit marks a file over one of ReadCoordinateIndex's memory limits: reading it
// again gives the same result.
var ErrCoordinateIndexLimit = errors.New("coordinate index limit")

// ReadCoordinateIndex reads the polymer chains of a structure file's first model in one streamed
// pass.
//
// Polymer residues are mmCIF ATOM rows, and HETATM rows with a label_seq_id (MSE, SEP); an ATOM
// row counts without a label_seq_id (phenix writes "?" for some DNA chains). In a PDB-format file
// they are ATOM records, and HETATM residues between ATOM residues of their chain or named in its
// SEQRES before its TER record. Water never counts.
//
// A chain is a protein chain when its entity's _entity_poly.type is polypeptide(L) or
// polypeptide(D); when the file states no type for it, when more than half of its modeled residues
// have a peptide CCD type.
//
// The pass hands every polymer-residue atom of every model to its accumulators; the chains are
// the first one's. Memory is bounded by the read buffer and the residues of the first model,
// never by the file. It fails when the stream cannot be read, ctx is done first, the coordinates
// end inside a row, or the file is over a limit (ErrCoordinateIndexLimit).
func ReadCoordinateIndex(ctx context.Context, source io.Reader, format StructureFormat) (CoordinateIndex, error) {
	reference, err := DefaultReference()
	if err != nil {
		return CoordinateIndex{}, fmt.Errorf("load reference list: %w", err)
	}
	chains := newChainAccumulator()
	sinks := []polymerAtomSink{chains}
	var read fileFacts
	switch format {
	case FormatMMCIF:
		read, err = readIndexMMCIF(ctx, source, sinks)
	case FormatPDB:
		read, err = readIndexPDB(ctx, source, sinks)
	default:
		return CoordinateIndex{}, fmt.Errorf("unknown structure format %d", format)
	}
	if err != nil {
		return CoordinateIndex{}, err
	}
	if chains.overflow {
		return CoordinateIndex{}, fmt.Errorf(tooManyResiduesError+": %w", maxIndexedResidues, ErrCoordinateIndexLimit)
	}
	index := CoordinateIndex{Chains: chains.indexedChains(read, reference), Warnings: read.warnings}
	if len(index.Chains) == 0 {
		index.Warnings = append(index.Warnings, noPolymerWarning)
	}
	return index, nil
}

// polymerAtom is one coordinate row of a polymer residue. Each accumulator takes what it needs;
// a later reader (per-residue rows) adds the fields it reads.
type polymerAtom struct {
	// firstModel is set for the rows of the file's first model.
	firstModel bool
	// chain is the author chain.
	chain     string
	entityID  string
	authSeqID string
	insCode   string
	compound  string
}

// polymerAtomSink takes the polymer-residue atoms of one read. The strings of an atom may share
// memory with the line they were read from, so a sink clones what it keeps.
type polymerAtomSink interface {
	addAtom(atom polymerAtom)
}

// fileFacts is what a read learned besides the atoms: the polymer types and deposited lengths the
// file states, and its warnings.
type fileFacts struct {
	// entityTypes holds the _entity_poly.type of each entity, lower case.
	entityTypes map[string]string
	// entityLengths counts the distinct _entity_poly_seq positions of each entity.
	entityLengths map[string]int
	// seqresLengths counts the SEQRES residue names of each chain.
	seqresLengths map[string]int
	warnings      []string
}

// residuePosition is one residue of a chain: author residue number and insertion code.
type residuePosition struct {
	authSeqID string
	insCode   string
}

// residueFacts is what the first atom read at a position says about its residue.
type residueFacts struct {
	compound string
	entityID string
}

// chainAccumulator keeps the residues of each author chain of the first model.
type chainAccumulator struct {
	chains   map[string]map[residuePosition]residueFacts
	residues int
	// overflow is set once the residues ran past maxIndexedResidues or the chains past
	// maxDistinctChains.
	overflow bool
}

func newChainAccumulator() *chainAccumulator {
	return &chainAccumulator{chains: map[string]map[residuePosition]residueFacts{}}
}

func (a *chainAccumulator) addAtom(atom polymerAtom) {
	if !atom.firstModel || a.overflow {
		return
	}
	positions, seen := a.chains[atom.chain]
	if !seen {
		if len(a.chains) >= maxDistinctChains {
			a.overflow = true
			return
		}
		positions = map[residuePosition]residueFacts{}
		a.chains[strings.Clone(atom.chain)] = positions
	}
	position := residuePosition{authSeqID: atom.authSeqID, insCode: atom.insCode}
	if _, seen := positions[position]; seen {
		return
	}
	if a.residues >= maxIndexedResidues {
		a.overflow = true
		return
	}
	position = residuePosition{authSeqID: strings.Clone(atom.authSeqID), insCode: strings.Clone(atom.insCode)}
	positions[position] = residueFacts{compound: strings.Clone(atom.compound), entityID: strings.Clone(atom.entityID)}
	a.residues++
}

// indexedChains turns the kept residues into chain rows, in chain order.
func (a *chainAccumulator) indexedChains(read fileFacts, reference *Reference) []IndexedChain {
	chainIDs := slices.Sorted(maps.Keys(a.chains))
	chains := make([]IndexedChain, 0, len(chainIDs))
	for _, chainID := range chainIDs {
		positions := a.chains[chainID]
		peptides := 0
		entityCounts := map[string]int{}
		for _, facts := range positions {
			if category, known := reference.ccdCategoryOf(facts.compound); known && category == CCDPeptide {
				peptides++
			}
			if facts.entityID != "" {
				entityCounts[facts.entityID]++
			}
		}
		chain := IndexedChain{
			AuthAsymID:      chainID,
			EntityID:        mostCommon(entityCounts),
			ModeledResidues: len(positions),
			Protein:         peptides*2 > len(positions),
			ProteinSource:   ProteinFromResidueMajority,
		}
		if polymerType := read.entityTypes[chain.EntityID]; chain.EntityID != "" && polymerType != "" {
			chain.Protein = proteinPolymerTypes[polymerType]
			chain.ProteinSource = ProteinFromEntityPolyType
		}
		switch {
		case chain.EntityID != "" && read.entityLengths[chain.EntityID] > 0:
			chain.DepositedLength = read.entityLengths[chain.EntityID]
			chain.LengthSource = LengthFromEntityPolySeq
		case read.seqresLengths[chainID] > 0:
			chain.DepositedLength = read.seqresLengths[chainID]
			chain.LengthSource = LengthFromSeqres
		}
		chains = append(chains, chain)
	}
	return chains
}

// mostCommon returns the key with the highest count, the smallest key on a tie; empty for no keys.
func mostCommon(counts map[string]int) string {
	best := ""
	for _, key := range slices.Sorted(maps.Keys(counts)) {
		if best == "" || counts[key] > counts[best] {
			best = key
		}
	}
	return best
}

// coordinateIndexCategories are the mmCIF categories ReadCoordinateIndex streams.
var coordinateIndexCategories = []string{"_atom_site", "_entity_poly", "_entity_poly_seq"}

// indexAtomSite turns _atom_site rows into polymer-residue atoms as they are read.
type indexAtomSite struct {
	columns struct {
		group, labelCompound, authCompound, authAsymID, labelAsymID, authSeqID, labelSeqID, insCode, entityID, model int
	}
	sinks []polymerAtomSink
	// firstModel is the pdbx_PDB_model_num of the first row.
	firstModel    string
	modelSeen     bool
	unknownGroups int
	skipped       int
	// noGroupColumn is set when the loop has no group_PDB column.
	noGroupColumn bool
}

func newIndexAtomSite(columns []string, sinks []polymerAtomSink) *indexAtomSite {
	site := &indexAtomSite{sinks: sinks}
	site.columns.group = slices.Index(columns, "group_pdb")
	site.columns.labelCompound = slices.Index(columns, "label_comp_id")
	site.columns.authCompound = slices.Index(columns, "auth_comp_id")
	site.columns.authAsymID = slices.Index(columns, "auth_asym_id")
	site.columns.labelAsymID = slices.Index(columns, "label_asym_id")
	site.columns.authSeqID = slices.Index(columns, "auth_seq_id")
	site.columns.labelSeqID = slices.Index(columns, "label_seq_id")
	site.columns.insCode = slices.Index(columns, "pdbx_pdb_ins_code")
	site.columns.entityID = slices.Index(columns, "label_entity_id")
	site.columns.model = slices.Index(columns, "pdbx_pdb_model_num")
	site.noGroupColumn = site.columns.group < 0
	return site
}

func (s *indexAtomSite) add(row []string) {
	// The first model is the one of the file's first coordinate row, whatever that row holds: a
	// first model of ligands and water only has no chains.
	model := cell(row, s.columns.model)
	if !s.modelSeen {
		s.firstModel = strings.Clone(model)
		s.modelSeen = true
	}
	labelSeqID := cell(row, s.columns.labelSeqID)
	if !s.isPolymerRow(row, labelSeqID != "") {
		return
	}
	compound := strings.ToUpper(firstNonEmpty(cell(row, s.columns.labelCompound), cell(row, s.columns.authCompound)))
	if compound == "" || isWater(compound) {
		return
	}
	atom := polymerAtom{
		firstModel: model == s.firstModel,
		chain:      firstNonEmpty(cell(row, s.columns.authAsymID), cell(row, s.columns.labelAsymID)),
		entityID:   cell(row, s.columns.entityID),
		authSeqID:  firstNonEmpty(cell(row, s.columns.authSeqID), labelSeqID),
		insCode:    cell(row, s.columns.insCode),
		compound:   compound,
	}
	if atom.chain == "" || atom.authSeqID == "" ||
		!plainCopyText(atom.chain, atom.entityID, atom.authSeqID, atom.insCode, atom.compound) {
		s.skipped++
		return
	}
	for _, sink := range s.sinks {
		sink.addAtom(atom)
	}
}

// isPolymerRow applies the polymer-residue rule to a row's group: ATOM rows count, HETATM rows
// only with a label_seq_id. Without a group_PDB column, only rows with a label_seq_id count.
func (s *indexAtomSite) isPolymerRow(row []string, hasLabelSeqID bool) bool {
	if s.noGroupColumn {
		return hasLabelSeqID
	}
	group := strings.ToUpper(cell(row, s.columns.group))
	switch group {
	case "ATOM":
		return true
	case "HETATM":
		return hasLabelSeqID
	default:
		s.unknownGroups++
		return false
	}
}

// entityPolySink keeps the polymer type of each entity: the first _entity_poly row of an entity
// with a type.
type entityPolySink struct {
	columns struct{ entityID, polymerType int }
	types   map[string]string
	// skipped counts the rows whose entity or type is not plain text of at most
	// maxCopyFieldBytes; overflow is set once the entities ran past maxDistinctChains.
	skipped  int
	overflow bool
}

func newEntityPolySink(columns []string) *entityPolySink {
	sink := &entityPolySink{types: map[string]string{}}
	sink.columns.entityID = slices.Index(columns, "entity_id")
	sink.columns.polymerType = slices.Index(columns, "type")
	return sink
}

func (s *entityPolySink) add(row []string) {
	entityID := cell(row, s.columns.entityID)
	polymerType := cell(row, s.columns.polymerType)
	if entityID == "" || polymerType == "" || s.overflow {
		return
	}
	if !plainCopyText(entityID, polymerType) {
		s.skipped++
		return
	}
	if _, seen := s.types[entityID]; seen {
		return
	}
	if len(s.types) >= maxDistinctChains {
		s.overflow = true
		return
	}
	s.types[strings.Clone(entityID)] = strings.ToLower(polymerType)
}

// entityPolySeqSink counts the distinct positions of each entity's sequence.
type entityPolySeqSink struct {
	columns struct{ entityID, num int }
	numbers map[string]map[string]struct{}
	kept    int
	// skipped counts the rows whose entity or number is not plain text of at most
	// maxCopyFieldBytes; overflow is set once the positions ran past maxSequencePositions.
	skipped  int
	overflow bool
}

func newEntityPolySeqSink(columns []string) *entityPolySeqSink {
	sink := &entityPolySeqSink{numbers: map[string]map[string]struct{}{}}
	sink.columns.entityID = slices.Index(columns, "entity_id")
	sink.columns.num = slices.Index(columns, "num")
	return sink
}

func (s *entityPolySeqSink) add(row []string) {
	entityID := cell(row, s.columns.entityID)
	number := cell(row, s.columns.num)
	if entityID == "" || number == "" || s.overflow {
		return
	}
	if !plainCopyText(entityID, number) {
		s.skipped++
		return
	}
	numbers, seen := s.numbers[entityID]
	if !seen {
		numbers = map[string]struct{}{}
		s.numbers[strings.Clone(entityID)] = numbers
	}
	if _, seen := numbers[number]; seen {
		return
	}
	if s.kept >= maxSequencePositions {
		s.overflow = true
		return
	}
	numbers[strings.Clone(number)] = struct{}{}
	s.kept++
}

func (s *entityPolySeqSink) lengths() map[string]int {
	lengths := make(map[string]int, len(s.numbers))
	for entityID, numbers := range s.numbers {
		lengths[entityID] = len(numbers)
	}
	return lengths
}

// indexCategories collects the streamed categories of one mmCIF read, so a category written in
// pair form (one row, no loop) can be told apart and fed afterwards.
type indexCategories struct {
	atoms    []polymerAtomSink
	atomSite *indexAtomSite
	polymer  *entityPolySink
	sequence *entityPolySeqSink
	started  map[string]bool
}

func (c *indexCategories) sinks() map[string]func(columns []string) rowSink {
	return map[string]func(columns []string) rowSink{
		"_atom_site": func(columns []string) rowSink {
			c.started["_atom_site"] = true
			c.atomSite = newIndexAtomSite(columns, c.atoms)
			return c.atomSite
		},
		"_entity_poly": func(columns []string) rowSink {
			c.started["_entity_poly"] = true
			c.polymer = newEntityPolySink(columns)
			return c.polymer
		},
		"_entity_poly_seq": func(columns []string) rowSink {
			c.started["_entity_poly_seq"] = true
			c.sequence = newEntityPolySeqSink(columns)
			return c.sequence
		},
	}
}

// feedPairForm feeds a category written as pairs (a single row) to its sink; a looped category
// was fed while it was read.
func (c *indexCategories) feedPairForm(document *Document, sinks map[string]func(columns []string) rowSink) {
	for _, category := range coordinateIndexCategories {
		table := document.Table(category)
		if c.started[category] || table == nil {
			continue
		}
		sink := sinks[category](table.Columns)
		for _, row := range table.Rows {
			sink.add(row)
		}
	}
}

func readIndexMMCIF(ctx context.Context, source io.Reader, atoms []polymerAtomSink) (fileFacts, error) {
	categories := &indexCategories{atoms: atoms, started: map[string]bool{}}
	sinks := categories.sinks()
	document, err := parseStream(ctx, source, parseOptions{
		onlyTables:      map[string]struct{}{},
		rowSinks:        sinks,
		keptValueBudget: maxKeptValueBytes,
	})
	if err != nil {
		return fileFacts{}, fmt.Errorf("read mmCIF: %w", err)
	}
	for _, category := range coordinateIndexCategories {
		if table := document.Table(category); table != nil && !table.Complete {
			return fileFacts{}, fmt.Errorf(cutOffFileError, category)
		}
	}
	categories.feedPairForm(document, sinks)
	read := fileFacts{entityTypes: map[string]string{}, entityLengths: map[string]int{}, warnings: []string{}}
	for _, note := range document.notes.messages {
		read.warnings = append(read.warnings, "parser limit: "+note)
	}
	if polymer := categories.polymer; polymer != nil {
		if polymer.overflow {
			return fileFacts{}, fmt.Errorf(tooManyPolymerEntityError+": %w", maxDistinctChains, ErrCoordinateIndexLimit)
		}
		read.entityTypes = polymer.types
		if polymer.skipped > 0 {
			read.warnings = append(read.warnings, fmt.Sprintf(skippedPolymerRowsWarning, polymer.skipped, maxCopyFieldBytes))
		}
	}
	if sequence := categories.sequence; sequence != nil {
		if sequence.overflow {
			return fileFacts{}, fmt.Errorf("more than %d _entity_poly_seq positions: %w", maxSequencePositions, ErrCoordinateIndexLimit)
		}
		read.entityLengths = sequence.lengths()
		if sequence.skipped > 0 {
			read.warnings = append(read.warnings, fmt.Sprintf(skippedSequenceRowsWarning, sequence.skipped, maxCopyFieldBytes))
		}
	}
	if site := categories.atomSite; site != nil {
		if site.noGroupColumn {
			read.warnings = append(read.warnings, noGroupIndexWarning)
		}
		if site.unknownGroups > 0 {
			read.warnings = append(read.warnings, fmt.Sprintf(unexpectedGroupRowsWarning, site.unknownGroups))
		}
		if site.skipped > 0 {
			read.warnings = append(read.warnings, fmt.Sprintf(skippedRowsWarning, site.skipped, maxCopyFieldBytes))
		}
	}
	return read, nil
}

// pendingAtom is a HETATM record of a PDB-format file waiting to be sorted into or out of its
// chain. afterAtom is set when an ATOM record of the chain came before it in the model.
type pendingAtom struct {
	atom      polymerAtom
	afterAtom bool
}

// pdbIndexReader applies the polymer-residue rule to the records of a PDB-format file, one model
// at a time.
type pdbIndexReader struct {
	sinks []polymerAtomSink
	// seqresNames holds the residue names of each chain's SEQRES records; seqresLengths counts
	// them.
	seqresNames   map[string]map[string]bool
	seqresLengths map[string]int
	nameCount     int
	notes         *limitNotes
	// member counts the models before the current one; memberHasCoordinates is set once the
	// current one has a coordinate record.
	member               int
	memberHasCoordinates bool
	// Per model: the chains with an ATOM record so far, the chains past their TER record, the
	// HETATM records waiting for their chain's next ATOM record or its end, and the chain of the
	// last coordinate record (for a TER record that names none).
	chainHasAtoms map[string]bool
	terminated    map[string]bool
	pending       map[string][]pendingAtom
	pendingCount  int
	lastChain     string
	skipped       int
	overflow      bool
}

func newPDBIndexReader(sinks []polymerAtomSink, notes *limitNotes) *pdbIndexReader {
	reader := &pdbIndexReader{
		sinks:         sinks,
		seqresNames:   map[string]map[string]bool{},
		seqresLengths: map[string]int{},
		notes:         notes,
	}
	reader.startModel()
	return reader
}

func (r *pdbIndexReader) startModel() {
	r.chainHasAtoms = map[string]bool{}
	r.terminated = map[string]bool{}
	r.pending = map[string][]pendingAtom{}
	r.pendingCount = 0
	r.lastChain = ""
}

// addSeqres reads a SEQRES record: the chain and the residue names in columns 20 to 70.
func (r *pdbIndexReader) addSeqres(line string) {
	chain := slice(line, 11, 12)
	for _, name := range strings.Fields(slice(line, 19, 70)) {
		r.seqresLengths[chain]++
		name = strings.ToUpper(name)
		names := r.seqresNames[chain]
		if names[name] {
			continue
		}
		if r.nameCount >= maxSequenceNames {
			r.notes.add("more than %d distinct SEQRES residue names: the rest were ignored, so a HETATM residue after its chain's last ATOM record may be left out", maxSequenceNames)
			continue
		}
		if names == nil {
			names = map[string]bool{}
			r.seqresNames[chain] = names
		}
		names[name] = true
		r.nameCount++
	}
}

// endModel sorts the records still waiting at the end of a model, as at a TER record.
func (r *pdbIndexReader) endModel() {
	for _, chain := range slices.Sorted(maps.Keys(r.pending)) {
		r.resolve(chain, false)
	}
	if r.memberHasCoordinates {
		r.member++
		r.memberHasCoordinates = false
	}
	r.startModel()
}

// addTer ends a chain's polymer block: its waiting HETATM residues count only when its SEQRES
// names them, and its later HETATM records do not count.
func (r *pdbIndexReader) addTer(line string) {
	chain := slice(line, 21, 22)
	if chain == "" {
		chain = r.lastChain
	}
	r.resolve(chain, false)
	r.terminated[chain] = true
}

// addCoordinate reads an ATOM or HETATM record of the current model.
func (r *pdbIndexReader) addCoordinate(record string, line string) {
	r.memberHasCoordinates = true
	compound := upperSlice(line, 17, 20)
	atom := polymerAtom{
		firstModel: r.member == 0,
		chain:      slice(line, 21, 22),
		authSeqID:  slice(line, 22, 26),
		insCode:    slice(line, 26, 27),
		compound:   compound,
	}
	r.lastChain = atom.chain
	if compound == "" || isWater(compound) {
		return
	}
	if atom.authSeqID == "" || !plainCopyText(atom.chain, atom.authSeqID, atom.insCode, atom.compound) {
		r.skipped++
		return
	}
	switch record {
	case "ATOM":
		delete(r.terminated, atom.chain)
		r.resolve(atom.chain, true)
		r.chainHasAtoms[atom.chain] = true
		r.emit(atom)
	case "HETATM":
		if r.terminated[atom.chain] {
			return
		}
		if r.pendingCount >= maxPendingAtoms {
			r.overflow = true
			return
		}
		atom.chain = strings.Clone(atom.chain)
		atom.authSeqID = strings.Clone(atom.authSeqID)
		atom.insCode = strings.Clone(atom.insCode)
		atom.compound = strings.Clone(atom.compound)
		r.pending[atom.chain] = append(r.pending[atom.chain], pendingAtom{atom: atom, afterAtom: r.chainHasAtoms[atom.chain]})
		r.pendingCount++
	default:
		panic(fmt.Sprintf("pdbIndexReader.addCoordinate called with record %q", record))
	}
}

// resolve sorts a chain's waiting HETATM records. Before an ATOM record of the chain
// (byAtom), a record that followed another ATOM record of the chain sits between ATOM residues
// and counts; otherwise a record counts only when the chain's SEQRES names its residue.
func (r *pdbIndexReader) resolve(chain string, byAtom bool) {
	waiting := r.pending[chain]
	if len(waiting) == 0 {
		return
	}
	names := r.seqresNames[chain]
	for _, held := range waiting {
		if (byAtom && held.afterAtom) || names[held.atom.compound] {
			r.emit(held.atom)
		}
	}
	r.pendingCount -= len(waiting)
	delete(r.pending, chain)
}

func (r *pdbIndexReader) emit(atom polymerAtom) {
	for _, sink := range r.sinks {
		sink.addAtom(atom)
	}
}

func readIndexPDB(ctx context.Context, source io.Reader, atoms []polymerAtomSink) (fileFacts, error) {
	lines := newLineReader(ctx, source)
	notes := limitNotes{}
	reader := newPDBIndexReader(atoms, &notes)
	for {
		raw, _, ok := lines.next()
		if !ok {
			break
		}
		line := string(raw)
		record := recordName(line)
		switch record {
		case "SEQRES":
			reader.addSeqres(line)
		case "MODEL":
			if reader.memberHasCoordinates {
				reader.endModel()
			}
		case "ENDMDL":
			reader.endModel()
		case "TER":
			reader.addTer(line)
		case "ATOM", "HETATM":
			reader.addCoordinate(record, line)
		default:
			// Every other record carries nothing the index needs.
		}
		if reader.overflow {
			return fileFacts{}, fmt.Errorf(tooManyPendingAtomError+": %w", maxPendingAtoms, ErrCoordinateIndexLimit)
		}
	}
	if err := lines.stopErr(); err != nil {
		return fileFacts{}, fmt.Errorf("read PDB file: %w", err)
	}
	reader.endModel()
	read := fileFacts{
		entityTypes:   map[string]string{},
		entityLengths: map[string]int{},
		seqresLengths: reader.seqresLengths,
		warnings:      []string{},
	}
	for _, note := range notes.messages {
		read.warnings = append(read.warnings, "parser limit: "+note)
	}
	if reader.skipped > 0 {
		read.warnings = append(read.warnings, fmt.Sprintf(skippedRowsWarning, reader.skipped, maxCopyFieldBytes))
	}
	return read, nil
}
