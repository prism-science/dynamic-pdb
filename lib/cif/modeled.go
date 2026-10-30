package cif

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// StructureFormat is the format of a structure file.
type StructureFormat int

const (
	FormatMMCIF StructureFormat = iota + 1
	FormatPDB
)

// CopySource names where a compound copy was read.
type CopySource string

const (
	// SourceNonpolyScheme and SourceBranchScheme: an mmCIF scheme row.
	SourceNonpolyScheme CopySource = "nonpoly_scheme"
	SourceBranchScheme  CopySource = "branch_scheme"
	// SourceAtomSite: mmCIF coordinates, for a file without the scheme
	// categories or a copy they leave out.
	SourceAtomSite CopySource = "atom_site"
	// SourcePDBHetatm: the HETATM records of a PDB-format file.
	SourcePDBHetatm CopySource = "pdb_hetatm"
)

// CompoundCopy is one copy of a compound in a structure file.
type CompoundCopy struct {
	CCDID string `json:"ccd_id"`
	// LabelAsymID is the mmCIF label chain; empty in a PDB-format file.
	LabelAsymID string `json:"label_asym_id,omitempty"`
	// AuthAsymID, AuthSeqID and InsCode are the chain, residue number and
	// insertion code viewers show: _atom_site.auth_*, the schemes' pdb_* items,
	// or PDB columns 22 to 27.
	AuthAsymID string `json:"auth_asym_id,omitempty"`
	AuthSeqID  string `json:"auth_seq_id,omitempty"`
	InsCode    string `json:"pdb_ins_code,omitempty"`
	// EntityID is the mmCIF entity; empty when the file does not say.
	EntityID string     `json:"entity_id,omitempty"`
	Source   CopySource `json:"source"`
	// NeedsReview is set when the file cannot tell whether the residue is a
	// compound at all: it could be a chain residue (no SEQRES, no _entity), or
	// its entity or chemical component type is a value the parser does not know.
	NeedsReview  bool   `json:"needs_review,omitempty"`
	ReviewReason string `json:"review_reason,omitempty"`
}

// ModeledCompounds is what ReadModeledCompounds found in one file.
type ModeledCompounds struct {
	// Copies are in CCD ID order, then by chain and residue number.
	Copies []CompoundCopy `json:"copies"`
	// Complete is false when a limit stopped the list, or the file could not
	// tell compounds from chain residues: copies may be missing.
	Complete bool     `json:"complete"`
	Warnings []string `json:"warnings"`
}

const (
	tooManyCopiesWarning = "more than %d compound copies: the rest were not listed"
	noGroupColumnWarning = "the coordinates have no group_PDB column, so compounds could not be told from polymer residues in them: copies come from the scheme categories only"
	unfinishedRowWarning = "the %s loop ended inside a row: copies may be missing"
	extraCopiesWarning   = "%d compound copies in the coordinates are missing from the scheme categories: they were added from the coordinates"
	skippedCopiesWarning = "%d compound copies were skipped: a compound, chain or residue field is not plain text of at most %d bytes"
)

// plainCopyText reports whether every field of a copy can be stored and shown
// as it is: valid UTF-8 of at most maxCopyFieldBytes, without control
// characters (PostgreSQL rejects a NUL in text).
func plainCopyText(fields ...string) bool {
	for _, field := range fields {
		if len(field) > maxCopyFieldBytes || !utf8.ValidString(field) || strings.ContainsFunc(field, unicode.IsControl) {
			return false
		}
	}
	return true
}

// shortReason bounds a review reason that quotes a file's value, on a rune
// boundary.
func shortReason(reason string) string {
	if len(reason) <= maxReviewReasonBytes {
		return reason
	}
	cut := maxReviewReasonBytes
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return reason[:cut] + "…"
}

// ReadModeledCompounds reads every copy of every compound a structure file
// models outside its polymer chains, water excepted.
//
// An mmCIF file's _pdbx_nonpoly_scheme and _pdbx_branch_scheme rows are the
// copies; the coordinates add a non-polymer or branched residue the schemes
// leave out, or make the list when the file has no schemes. A file without
// _entity, or a PDB-format file without SEQRES, does not say which residues
// belong to its chains: a residue that could be a chain residue is listed and
// marked for review, as the ligand rule flags it. Copies are de-duplicated
// across models, so a compound modeled only in a later model is listed.
//
// The whole stream is read, whatever its size: memory is bounded by the read
// buffer and maxModeledCopies, never by the file. It fails when the stream
// cannot be read or ctx is done first.
func ReadModeledCompounds(ctx context.Context, source io.Reader, format StructureFormat) (ModeledCompounds, error) {
	reference, err := DefaultReference()
	if err != nil {
		return ModeledCompounds{}, fmt.Errorf("load reference list: %w", err)
	}
	switch format {
	case FormatMMCIF:
		return readModeledMMCIF(ctx, source, reference)
	case FormatPDB:
		return readModeledPDB(ctx, source, reference)
	default:
		return ModeledCompounds{}, fmt.Errorf("unknown structure format %d", format)
	}
}

// copyKey identifies one copy: a compound at one position.
type copyKey struct {
	ccdID, labelAsymID, authAsymID, authSeqID, insCode string
}

func (c CompoundCopy) key() copyKey {
	return copyKey{ccdID: c.CCDID, labelAsymID: c.LabelAsymID, authAsymID: c.AuthAsymID, authSeqID: c.AuthSeqID, insCode: c.InsCode}
}

// copySet keeps the first copy read at each position, up to maxModeledCopies.
// skipped counts the copies whose fields are not plain text.
type copySet struct {
	copies  map[copyKey]CompoundCopy
	full    bool
	skipped int
}

func newCopySet() *copySet {
	return &copySet{copies: map[copyKey]CompoundCopy{}}
}

// add keeps a copy unless one is already kept at its position; past the limit
// it keeps nothing and marks the set full.
func (s *copySet) add(copied CompoundCopy) {
	if !plainCopyText(copied.CCDID, copied.LabelAsymID, copied.AuthAsymID, copied.AuthSeqID, copied.InsCode, copied.EntityID) {
		s.skipped++
		return
	}
	key := copied.key()
	if _, seen := s.copies[key]; seen {
		return
	}
	if len(s.copies) >= maxModeledCopies {
		s.full = true
		return
	}
	s.copies[key] = copied
}

func (s *copySet) has(key copyKey) bool {
	_, seen := s.copies[key]
	return seen
}

// sorted returns the copies by CCD ID, chain, residue number (numbers in
// numeric order), insertion code and label chain.
func (s *copySet) sorted() []CompoundCopy {
	copies := make([]CompoundCopy, 0, len(s.copies))
	for _, copied := range s.copies {
		copies = append(copies, copied)
	}
	slices.SortFunc(copies, func(left CompoundCopy, right CompoundCopy) int {
		if order := strings.Compare(left.CCDID, right.CCDID); order != 0 {
			return order
		}
		if order := strings.Compare(left.AuthAsymID, right.AuthAsymID); order != 0 {
			return order
		}
		if order := compareResidueNumbers(left.AuthSeqID, right.AuthSeqID); order != 0 {
			return order
		}
		if order := strings.Compare(left.InsCode, right.InsCode); order != 0 {
			return order
		}
		return strings.Compare(left.LabelAsymID, right.LabelAsymID)
	})
	return copies
}

// compareResidueNumbers orders numbers numerically and puts anything else after
// them, in text order.
func compareResidueNumbers(left string, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	switch {
	case leftErr == nil && rightErr == nil:
		return leftNumber - rightNumber
	case leftErr == nil:
		return -1
	case rightErr == nil:
		return 1
	default:
		return strings.Compare(left, right)
	}
}

// modeledTables are the categories readModeledMMCIF keeps besides the streamed
// ones: the entity types and the file's own chemical component types.
var modeledTables = map[string]struct{}{
	"_entity":    {},
	"_chem_comp": {},
}

// schemeReader turns the rows of _pdbx_nonpoly_scheme or _pdbx_branch_scheme
// into copies as they are read.
type schemeReader struct {
	source  CopySource
	copies  *copySet
	columns struct{ asymID, entityID, monID, seqNum, chain, insCode int }
}

func newSchemeReader(source CopySource, copies *copySet, columns []string) (*schemeReader, error) {
	reader := &schemeReader{source: source, copies: copies}
	reader.columns.asymID = slices.Index(columns, "asym_id")
	reader.columns.entityID = slices.Index(columns, "entity_id")
	reader.columns.monID = slices.Index(columns, "mon_id")
	reader.columns.seqNum = slices.Index(columns, "pdb_seq_num")
	reader.columns.insCode = slices.Index(columns, "pdb_ins_code")
	switch source {
	case SourceNonpolyScheme:
		reader.columns.chain = slices.Index(columns, "pdb_strand_id")
	case SourceBranchScheme:
		reader.columns.chain = slices.Index(columns, "pdb_asym_id")
	case SourceAtomSite, SourcePDBHetatm:
		return nil, fmt.Errorf("%s is not a scheme category", source)
	default:
		return nil, fmt.Errorf("unknown copy source %q", source)
	}
	return reader, nil
}

func (r *schemeReader) add(row []string) {
	compound := strings.ToUpper(cell(row, r.columns.monID))
	if compound == "" || isWater(compound) {
		return
	}
	r.copies.add(CompoundCopy{
		CCDID:       compound,
		LabelAsymID: cell(row, r.columns.asymID),
		AuthAsymID:  cell(row, r.columns.chain),
		AuthSeqID:   cell(row, r.columns.seqNum),
		InsCode:     cell(row, r.columns.insCode),
		EntityID:    cell(row, r.columns.entityID),
		Source:      r.source,
	})
}

// modeledAtom is what the first coordinate row of a copy says about it.
type modeledAtom struct {
	entityID string
	inChain  bool
}

// modeledAtomSite keeps the HETATM residues of every model, one entry per
// position, as the coordinate rows are read.
type modeledAtomSite struct {
	columns struct {
		group, compound, asymID, authAsymID, authSeqID, labelSeqID, insCode, entityID int
	}
	residues map[copyKey]modeledAtom
	full     bool
	// skipped counts the rows whose fields are not plain text.
	skipped int
}

func newModeledAtomSite(columns []string) *modeledAtomSite {
	site := &modeledAtomSite{residues: map[copyKey]modeledAtom{}}
	site.columns.group = slices.Index(columns, "group_pdb")
	site.columns.compound = slices.Index(columns, "label_comp_id")
	site.columns.asymID = slices.Index(columns, "label_asym_id")
	site.columns.authAsymID = slices.Index(columns, "auth_asym_id")
	site.columns.authSeqID = slices.Index(columns, "auth_seq_id")
	site.columns.labelSeqID = slices.Index(columns, "label_seq_id")
	site.columns.insCode = slices.Index(columns, "pdbx_pdb_ins_code")
	site.columns.entityID = slices.Index(columns, "label_entity_id")
	return site
}

func (s *modeledAtomSite) add(row []string) {
	if !strings.EqualFold(cell(row, s.columns.group), "HETATM") {
		return
	}
	compound := strings.ToUpper(cell(row, s.columns.compound))
	if compound == "" || isWater(compound) {
		return
	}
	labelAsymID := cell(row, s.columns.asymID)
	key := copyKey{
		ccdID:       compound,
		labelAsymID: labelAsymID,
		authAsymID:  firstNonEmpty(cell(row, s.columns.authAsymID), labelAsymID),
		authSeqID:   firstNonEmpty(cell(row, s.columns.authSeqID), cell(row, s.columns.labelSeqID)),
		insCode:     cell(row, s.columns.insCode),
	}
	entityID := cell(row, s.columns.entityID)
	if !plainCopyText(key.ccdID, key.labelAsymID, key.authAsymID, key.authSeqID, key.insCode, entityID) {
		s.skipped++
		return
	}
	if _, seen := s.residues[key]; seen {
		return
	}
	if len(s.residues) >= maxModeledCopies {
		s.full = true
		return
	}
	s.residues[key] = modeledAtom{
		entityID: entityID,
		inChain:  cell(row, s.columns.labelSeqID) != "",
	}
}

// modeledCategories collects the streamed categories of one mmCIF read: the
// sink each made, so a category written in pair form (one row, no loop) can be
// told apart and fed afterwards.
type modeledCategories struct {
	schemes  *copySet
	atomSite *modeledAtomSite
	started  map[string]bool
	// buildErr is a sink that could not be made.
	buildErr error
}

func (c *modeledCategories) sinks() map[string]func(columns []string) rowSink {
	scheme := func(category string, source CopySource) func(columns []string) rowSink {
		return func(columns []string) rowSink {
			c.started[category] = true
			reader, err := newSchemeReader(source, c.schemes, columns)
			if err != nil {
				c.buildErr = err
				return discardRows{}
			}
			return reader
		}
	}
	return map[string]func(columns []string) rowSink{
		"_pdbx_nonpoly_scheme": scheme("_pdbx_nonpoly_scheme", SourceNonpolyScheme),
		"_pdbx_branch_scheme":  scheme("_pdbx_branch_scheme", SourceBranchScheme),
		"_atom_site": func(columns []string) rowSink {
			c.started["_atom_site"] = true
			c.atomSite = newModeledAtomSite(columns)
			return c.atomSite
		},
	}
}

type discardRows struct{}

func (discardRows) add([]string) {}

// feedPairForm feeds a category written as pairs (a single row) to its sink;
// a looped category was fed while it was read.
func (c *modeledCategories) feedPairForm(document *Document, sinks map[string]func(columns []string) rowSink) {
	for category, newSink := range sinks {
		table := document.Table(category)
		if c.started[category] || table == nil {
			continue
		}
		sink := newSink(table.Columns)
		for _, row := range table.Rows {
			sink.add(row)
		}
	}
}

func readModeledMMCIF(ctx context.Context, source io.Reader, reference *Reference) (ModeledCompounds, error) {
	categories := &modeledCategories{schemes: newCopySet(), started: map[string]bool{}}
	sinks := categories.sinks()
	document, err := parseStream(ctx, source, parseOptions{onlyTables: modeledTables, rowSinks: sinks, keptValueBudget: maxKeptValueBytes})
	if err != nil {
		return ModeledCompounds{}, fmt.Errorf("read mmCIF: %w", err)
	}
	categories.feedPairForm(document, sinks)
	if categories.buildErr != nil {
		return ModeledCompounds{}, fmt.Errorf("read scheme categories: %w", categories.buildErr)
	}
	result := ModeledCompounds{Complete: !document.valueBudgetHit, Warnings: []string{}}
	for _, note := range document.notes.messages {
		result.Warnings = append(result.Warnings, "parser limit: "+note)
	}
	for _, category := range []string{"_pdbx_nonpoly_scheme", "_pdbx_branch_scheme", "_atom_site"} {
		if table := document.Table(category); table != nil && !table.Complete {
			result.Complete = false
			result.Warnings = append(result.Warnings, fmt.Sprintf(unfinishedRowWarning, category))
		}
	}
	copies := categories.schemes
	hasSchemes := categories.started["_pdbx_nonpoly_scheme"] || categories.started["_pdbx_branch_scheme"]
	site := categories.atomSite
	switch {
	case site == nil:
	case site.columns.group < 0:
		if !hasSchemes {
			result.Complete = false
		}
		result.Warnings = append(result.Warnings, noGroupColumnWarning)
	default:
		annotations := readAnnotations(document, false)
		sorter := compoundSorter{annotations: annotations, reference: reference, warnedTypes: map[string]bool{}}
		extras := 0
		for _, key := range sortedKeys(site.residues) {
			if copies.has(key) {
				continue
			}
			atom := site.residues[key]
			copied, isCompound, err := modeledCopy(&sorter, key, instanceFacts{
				entityID:         atom.entityID,
				chain:            chainPositionOf(atom.inChain),
				chainsUndeclared: !annotations.entitiesRead,
			}, SourceAtomSite)
			if err != nil {
				return ModeledCompounds{}, fmt.Errorf("sort the coordinates: %w", err)
			}
			if !isCompound {
				continue
			}
			copies.add(copied)
			extras++
		}
		if hasSchemes && extras > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf(extraCopiesWarning, extras))
		}
	}
	if copies.full || (site != nil && site.full) {
		result.Complete = false
		result.Warnings = append(result.Warnings, fmt.Sprintf(tooManyCopiesWarning, maxModeledCopies))
	}
	skipped := copies.skipped
	if site != nil {
		skipped += site.skipped
	}
	if skipped > 0 {
		result.Complete = false
		result.Warnings = append(result.Warnings, fmt.Sprintf(skippedCopiesWarning, skipped, maxCopyFieldBytes))
	}
	result.Copies = copies.sorted()
	return result, nil
}

// sortedKeys returns the keys of residues in a fixed order, so a copy limit
// always keeps the same copies.
func sortedKeys[V any](residues map[copyKey]V) []copyKey {
	keys := make([]copyKey, 0, len(residues))
	for key := range residues {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left copyKey, right copyKey) int {
		return cmp.Or(
			strings.Compare(left.ccdID, right.ccdID),
			strings.Compare(left.labelAsymID, right.labelAsymID),
			strings.Compare(left.authAsymID, right.authAsymID),
			strings.Compare(left.authSeqID, right.authSeqID),
			strings.Compare(left.insCode, right.insCode),
		)
	})
	return keys
}

// modeledCopy decides whether a coordinate residue is a compound copy, with
// the ligand rule's first step only: water and polymer residues are not, and a
// residue that could be a chain residue, or whose entity or chemical component
// type is unknown, is marked for review. False when it is not a compound.
func modeledCopy(sorter *compoundSorter, key copyKey, facts instanceFacts, source CopySource) (CompoundCopy, bool, error) {
	instance := compoundInstance{compoundID: key.ccdID, asymID: firstNonEmpty(key.labelAsymID, key.authAsymID), authSeqID: key.authSeqID, insCode: key.insCode}
	shape, review, err := sorter.shapeOf(instance, facts)
	if err != nil {
		return CompoundCopy{}, false, fmt.Errorf("read the shape of %s: %w", key.ccdID, err)
	}
	switch shape {
	case shapePolymer, shapeWater:
		return CompoundCopy{}, false, nil
	case shapeListed, shapeBranched, shapeMacrolide, shapeOutsideChain:
	default:
		return CompoundCopy{}, false, fmt.Errorf("unknown compound shape %d for %s", shape, key.ccdID)
	}
	if entityReview := sorter.annotations.entityReview[facts.entityID]; entityReview != "" {
		review = entityReview
	}
	review = shortReason(review)
	return CompoundCopy{
		CCDID:        key.ccdID,
		LabelAsymID:  key.labelAsymID,
		AuthAsymID:   key.authAsymID,
		AuthSeqID:    key.authSeqID,
		InsCode:      key.insCode,
		EntityID:     facts.entityID,
		Source:       source,
		NeedsReview:  review != "",
		ReviewReason: review,
	}, true, nil
}

func readModeledPDB(ctx context.Context, source io.Reader, reference *Reference) (ModeledCompounds, error) {
	lines := newLineReader(ctx, source)
	notes := limitNotes{}
	chains := newPDBChains(&notes)
	residues := map[copyKey]struct{}{}
	full := false
	skipped := 0
	modelStarted := false
	firstModelDone := false
	lineNumber := 0
	for {
		raw, _, ok := lines.next()
		if !ok {
			break
		}
		lineNumber++
		line := string(raw)
		record := recordName(line)
		switch record {
		case "SEQRES":
			chains.addSeqres(line)
			continue
		case "MODRES":
			chains.addModres(line)
			continue
		case "TER":
			if !firstModelDone {
				chains.addTer(line, lineNumber)
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
			continue
		case "ATOM", "HETATM":
		default:
			// Every other record carries nothing the copies need.
			continue
		}
		compound := upperSlice(line, 17, 20)
		instance := compoundInstance{
			compoundID: compound,
			asymID:     slice(line, 21, 22),
			authSeqID:  slice(line, 22, 26),
			insCode:    slice(line, 26, 27),
		}
		// The chain positions come from the first model, as for the ligand
		// rule; the copies come from every model.
		if !firstModelDone {
			chains.addCoordinate(record, line, lineNumber, instance)
		}
		if record != "HETATM" || compound == "" || isWater(compound) {
			continue
		}
		key := copyKey{ccdID: compound, authAsymID: instance.asymID, authSeqID: instance.authSeqID, insCode: instance.insCode}
		if !plainCopyText(key.ccdID, key.authAsymID, key.authSeqID, key.insCode) {
			skipped++
			continue
		}
		if _, seen := residues[key]; seen {
			continue
		}
		if len(residues) >= maxModeledCopies {
			full = true
			continue
		}
		residues[key] = struct{}{}
	}
	if err := lines.stopErr(); err != nil {
		return ModeledCompounds{}, fmt.Errorf("read PDB file: %w", err)
	}

	annotations := pdbAnnotations()
	annotations.sequenceNames = chains.sequenceNames()
	sorter := compoundSorter{annotations: annotations, reference: reference, warnedTypes: map[string]bool{}}
	copies := newCopySet()
	for _, key := range sortedKeys(residues) {
		instance := compoundInstance{compoundID: key.ccdID, asymID: key.authAsymID, authSeqID: key.authSeqID, insCode: key.insCode}
		copied, isCompound, err := modeledCopy(&sorter, key, instanceFacts{
			chain:            chains.position(instance),
			chainsUndeclared: chains.chainsUndeclared(instance),
		}, SourcePDBHetatm)
		if err != nil {
			return ModeledCompounds{}, fmt.Errorf("sort the coordinates: %w", err)
		}
		if isCompound {
			copies.add(copied)
		}
	}
	result := ModeledCompounds{Complete: !full && skipped == 0, Warnings: []string{}}
	for _, note := range notes.messages {
		result.Warnings = append(result.Warnings, "parser limit: "+note)
	}
	if full {
		result.Warnings = append(result.Warnings, fmt.Sprintf(tooManyCopiesWarning, maxModeledCopies))
	}
	if skipped > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf(skippedCopiesWarning, skipped, maxCopyFieldBytes))
	}
	result.Copies = copies.sorted()
	return result, nil
}
