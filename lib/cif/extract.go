package cif

import (
	"fmt"
	"sort"
	"strings"
)

// Extract derives the registry values from a parsed mmCIF document using the
// embedded cofactor reference list.
func Extract(document *Document) (Result, error) {
	reference, err := DefaultReference()
	if err != nil {
		return Result{}, fmt.Errorf("load reference list: %w", err)
	}
	return ExtractWithReference(document, reference)
}

// extractionTables are the categories Extract reads besides _atom_site.
var extractionTables = map[string]struct{}{
	"_struct_conn":         {},
	"_struct_ref_seq_dif":  {},
	"_pdbx_entity_nonpoly": {},
	"_entity":              {},
	"_struct_ref":          {},
	"_struct_ref_seq":      {},
	"_software":            {},
}

// ExtractText parses text and extracts its values in a single pass. The
// coordinate loop and _struct_conn are reduced to counts and metal partners
// while they are read and the rows of categories Extract does not use are
// skipped, so memory stays far below Parse followed by Extract and does not
// grow with the file's content (see limits.go). Result.Connections is empty;
// use Parse and Extract when the connections themselves are needed. The returned document holds the scalar items and
// the small tables only. Set truncated for the head of a larger file.
func ExtractText(text string, truncated bool) (*Document, Result, error) {
	reference, err := DefaultReference()
	if err != nil {
		return nil, Result{}, fmt.Errorf("load reference list: %w", err)
	}
	return ExtractTextWithReference(text, truncated, reference)
}

// ExtractTextWithReference is ExtractText with an explicit reference list.
func ExtractTextWithReference(text string, truncated bool, reference *Reference) (*Document, Result, error) {
	document := parse(text, truncated, parseOptions{streamAtomSite: true, streamStructConn: true, onlyTables: extractionTables})
	result, err := ExtractWithReference(document, reference)
	if err != nil {
		return nil, Result{}, fmt.Errorf("extract mmCIF values: %w", err)
	}
	return document, result, nil
}

// ExtractWithReference is Extract with an explicit reference list.
func ExtractWithReference(document *Document, reference *Reference) (Result, error) {
	result := Result{
		Ligands:          []Compound{},
		Cofactors:        []Compound{},
		ReferenceVersion: reference.Version,
		Warnings:         []string{},
	}
	if !reference.Approved {
		result.Warnings = append(result.Warnings, DraftWarning)
	}
	var links metalLinks
	if document.metalLinks != nil {
		links = document.metalLinks.links
		if links.overflow {
			result.Warnings = append(result.Warnings, fmt.Sprintf("more than %d distinct metalc connections: the rest were ignored, so a metal ion may be classified without its link", maxMetalLinks))
		}
	} else {
		result.Connections = readConnections(document)
		links = metalLinksFromConnections(result.Connections)
	}
	result.SequenceDifferences = readSequenceDifferences(document)
	result.Mutations = readMutations(document, result.SequenceDifferences)
	result.Program = mmcifProgram(document)

	atoms, instances, warning := readAtomSite(document)
	result.Atoms = atoms
	if warning != "" {
		result.Warnings = append(result.Warnings, warning)
		result.AtomsCutOff = true
	}
	if table := document.Table("_pdbx_entity_nonpoly"); table != nil {
		for _, row := range table.Rows {
			if id := strings.ToUpper(table.Value(row, "comp_id")); id != "" {
				instances[compoundInstance{compoundID: id}] = 0
			}
		}
	}

	for _, note := range document.notes.messages {
		result.Warnings = append(result.Warnings, "parser limit: "+note)
	}

	ligands, cofactors, tooMany, err := classifyCompounds(instances, links, reference)
	if err != nil {
		return Result{}, fmt.Errorf("classify compounds: %w", err)
	}
	if tooMany {
		result.Warnings = append(result.Warnings, tooManyCompoundsWarning)
	}
	result.Ligands = ligands
	result.Cofactors = cofactors
	return result, nil
}

// compoundInstance is one placed copy of a non-polymer compound. Instances read
// from _pdbx_entity_nonpoly alone have no location and carry only the compound
// ID. The maps of instances hold the number of distinct atoms placed for the
// instance, 0 when unknown.
type compoundInstance struct {
	compoundID string
	asymID     string
	authSeqID  string
	insCode    string
}

func (instance compoundInstance) located() bool {
	return instance.asymID != ""
}

func partnerInstance(partner Partner) compoundInstance {
	return compoundInstance{
		compoundID: strings.ToUpper(partner.LabelCompID),
		asymID:     partner.LabelAsymID,
		authSeqID:  partner.AuthSeqID,
		insCode:    partner.InsCode,
	}
}

// classifyCompounds applies the single ligand rule. Water is always dropped;
// other solvents, buffers and cryoprotectants are dropped only once the
// reference lists are approved, and are kept and flagged until then. Nothing
// else is dropped. The metal rule is per instance: a metal instance is a
// cofactor when a metalc connection joins that instance to a protein residue or
// ligand. If the file has no metalc rows at all, its metals are cofactors
// flagged for review. A metal instance without a link in a file that has metalc
// rows becomes a ligand flagged for review, so it is never dropped and never
// placed unflagged.
func classifyCompounds(
	instances map[compoundInstance]int,
	links metalLinks,
	reference *Reference,
) ([]Compound, []Compound, bool, error) {
	hasMetalcRows := links.hasRows
	linkedInstances := map[compoundInstance]bool{}
	linkedCompounds := map[string]bool{}
	for _, pair := range links.pairs {
		first, second := pair.first, pair.second
		if reference.kind(first.compoundID) == kindMetalIon && second.compoundID != "" && !reference.isSolvent(second.compoundID) {
			linkedInstances[first] = true
			linkedCompounds[first.compoundID] = true
		}
		if reference.kind(second.compoundID) == kindMetalIon && first.compoundID != "" && !reference.isSolvent(first.compoundID) {
			linkedInstances[second] = true
			linkedCompounds[second.compoundID] = true
		}
	}

	// A compound seen only in _pdbx_entity_nonpoly is skipped when atom_site
	// already placed instances of it.
	placed := map[string]bool{}
	for instance := range instances {
		if instance.located() {
			placed[instance.compoundID] = true
		}
	}
	ordered := make([]compoundInstance, 0, len(instances))
	for instance := range instances {
		if !instance.located() && placed[instance.compoundID] {
			continue
		}
		ordered = append(ordered, instance)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.compoundID != right.compoundID {
			return left.compoundID < right.compoundID
		}
		if left.asymID != right.asymID {
			return left.asymID < right.asymID
		}
		if left.authSeqID != right.authSeqID {
			return left.authSeqID < right.authSeqID
		}
		return left.insCode < right.insCode
	})

	ligands := compoundList{}
	cofactors := compoundList{}
	for _, instance := range ordered {
		id := instance.compoundID
		kind := reference.kind(id)
		switch kind {
		case kindSolvent:
			continue
		case kindDraftSolvent:
			ligands.add(Compound{ID: id, NeedsReview: true, ReviewReason: draftSolventReason})
		case kindCofactor:
			cofactors.add(Compound{ID: id})
		case kindLigand:
			switch instances[instance] {
			case 1:
				ligands.add(Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "single-atom compound is not in the metal ion reference list: confirm whether it is a metal ion",
				})
			case unknownAtomCount:
				ligands.add(Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "atom count unknown: the file was cut off inside this compound's coordinates, so it cannot be told whether it is a single-atom compound or a metal ion",
				})
			default:
				ligands.add(Compound{ID: id})
			}
		case kindMetalIon:
			linked := linkedInstances[instance]
			if !instance.located() {
				linked = linkedCompounds[id]
			}
			switch {
			case linked:
				cofactors.add(Compound{ID: id})
			case !hasMetalcRows:
				cofactors.add(Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "metal ion kept for review: the file has no metalc connection rows",
				})
			default:
				ligands.add(Compound{
					ID:           id,
					NeedsReview:  true,
					ReviewReason: "metal ion instance has no metalc connection to a protein residue or ligand",
				})
			}
		default:
			return nil, nil, false, fmt.Errorf("unknown compound kind %d for %s", kind, id)
		}
	}
	return ligands.compounds(), cofactors.compounds(), ligands.overflow || cofactors.overflow, nil
}

// compoundList keeps one entry per compound ID; a flagged entry wins over an
// unflagged one so a review flag on any instance is never lost.
type compoundList struct {
	entries  []Compound
	index    map[string]int
	overflow bool
}

var tooManyCompoundsWarning = fmt.Sprintf("parser limit: more than %d distinct compounds in one list: the rest were ignored", maxCompoundsPerList)

func (list *compoundList) add(compound Compound) {
	if list.index == nil {
		list.index = map[string]int{}
	}
	position, seen := list.index[compound.ID]
	if !seen {
		if len(list.entries) >= maxCompoundsPerList {
			list.overflow = true
			return
		}
		list.index[compound.ID] = len(list.entries)
		list.entries = append(list.entries, compound)
		return
	}
	if compound.NeedsReview && !list.entries[position].NeedsReview {
		list.entries[position] = compound
	}
}

func (list *compoundList) compounds() []Compound {
	if list.entries == nil {
		return []Compound{}
	}
	return list.entries
}

type residueKey struct {
	chain   string
	seq     string
	insCode string
}

// atomSiteReader reduces _atom_site rows to counts as they arrive, so a large
// coordinate loop never has to be held in memory.
type atomSiteReader struct {
	columns atomSiteColumns
	tally   *coordinateTally
	// started is set after the first row; firstModel is that row's model number.
	started    bool
	firstModel string
}

// atomSiteColumns holds the index of each column the reader uses, -1 when the
// loop does not have it.
type atomSiteColumns struct {
	model, group, compound, asymID, authSeqID, insCode, atomID, element int
	authAsymID, labelSeqID, altID, occupancy                            int
}

func newAtomSiteReader(columns []string) *atomSiteReader {
	index := func(name string) int {
		for position, candidate := range columns {
			if candidate == name {
				return position
			}
		}
		return -1
	}
	return &atomSiteReader{
		columns: atomSiteColumns{
			model:      index("pdbx_pdb_model_num"),
			group:      index("group_pdb"),
			compound:   index("label_comp_id"),
			asymID:     index("label_asym_id"),
			authSeqID:  index("auth_seq_id"),
			insCode:    index("pdbx_pdb_ins_code"),
			atomID:     index("label_atom_id"),
			element:    index("type_symbol"),
			authAsymID: index("auth_asym_id"),
			labelSeqID: index("label_seq_id"),
			altID:      index("label_alt_id"),
			occupancy:  index("occupancy"),
		},
		tally: newCoordinateTally(),
	}
}

func cell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return cleanValue(row[index])
}

func (r *atomSiteReader) add(row []string) {
	columns := r.columns
	tally := r.tally
	model := cell(row, columns.model)
	if !r.started {
		r.started = true
		r.firstModel = model
	}
	if model != r.firstModel {
		return
	}
	group := strings.ToUpper(cell(row, columns.group))
	if group == "" {
		group = "ATOM"
	}
	compound := strings.ToUpper(cell(row, columns.compound))
	if group == "HETATM" && compound != "" {
		tally.addHetero(compoundInstance{
			compoundID: compound,
			asymID:     cell(row, columns.asymID),
			authSeqID:  cell(row, columns.authSeqID),
			insCode:    cell(row, columns.insCode),
		}, cell(row, columns.atomID))
	}
	if group == "HETATM" && isWater(compound) {
		return
	}
	if element := strings.ToUpper(cell(row, columns.element)); element == "H" || element == "D" {
		return
	}
	tally.atomCount++
	if group == "HETATM" {
		return
	}
	chain := firstNonEmpty(cell(row, columns.authAsymID), cell(row, columns.asymID))
	residue := residueKey{
		chain:   chain,
		seq:     firstNonEmpty(cell(row, columns.authSeqID), cell(row, columns.labelSeqID)),
		insCode: cell(row, columns.insCode),
	}
	tally.addResidue(chain, residue, cell(row, columns.altID), func() float64 {
		return parseOccupancy(cell(row, columns.occupancy))
	})
}

const (
	atomSiteCutOffWarning = "atom_site coordinates were cut off by the read limit: atom counts, residue counts and altloc fraction not extracted"
	atomSiteNotReached    = "the read limit ended before any atom_site coordinates: atom counts, residue counts and altloc fraction not extracted"
)

// result returns the statistics, or nil statistics with a warning when the loop
// was cut off or ran past a limit.
func (r *atomSiteReader) result(complete bool) (*AtomStats, map[compoundInstance]int, string) {
	tally := r.tally
	instances := tally.instanceCounts(complete)
	if tally.overflow != "" {
		return nil, instances, fmt.Sprintf("atom_site has more distinct %s than the parser limit allows: atom counts, residue counts and altloc fraction not extracted", tally.overflow)
	}
	if !complete {
		return nil, instances, atomSiteCutOffWarning
	}
	stats := AtomStats{
		AtomCount:       tally.atomCount,
		ModeledResidues: len(tally.residues),
		ProteinChains:   len(tally.chains),
	}
	if stats.ModeledResidues > 0 {
		stats.AltlocFraction = float64(countAltlocResidues(tally.positions)) / float64(stats.ModeledResidues)
	}
	if stats.AtomCount == 0 {
		return nil, instances, ""
	}
	return &stats, instances, ""
}

func readAtomSite(document *Document) (*AtomStats, map[compoundInstance]int, string) {
	table := document.Table("_atom_site")
	if table == nil {
		if document.truncated {
			return nil, map[compoundInstance]int{}, atomSiteNotReached
		}
		return nil, map[compoundInstance]int{}, ""
	}
	reader := document.atomSite
	if reader == nil {
		reader = newAtomSiteReader(table.Columns)
		for _, row := range table.Rows {
			reader.add(row)
		}
	}
	return reader.result(table.Complete)
}

const minAltlocOccupancy = 0.1

// countAltlocResidues counts residues with at least two alternate positions
// that each reach the minimum occupancy.
func countAltlocResidues(positions map[residueKey]map[string]float64) int {
	count := 0
	for _, byAltID := range positions {
		occupied := 0
		for _, occupancy := range byAltID {
			if occupancy >= minAltlocOccupancy {
				occupied++
			}
		}
		if occupied >= 2 {
			count++
		}
	}
	return count
}

func isWater(compound string) bool {
	switch compound {
	case "HOH", "DOD", "WAT", "H2O":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
