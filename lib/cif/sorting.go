package cif

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The ligand rule: where each compound instance of a file goes (ligands,
// cofactors, or left out) and which rule put it there.

const (
	singleAtomReason     = "single-atom compound is not in the metal ion reference list: confirm whether it is a metal ion"
	unknownAtomsReason   = "atom count unknown: the file was cut off inside this compound's coordinates, so it cannot be told whether it is a single-atom compound or a metal ion"
	noMetalcRowsReason   = "metal ion kept for review: the file has no metalc connection rows"
	unlinkedMetalReason  = "metal ion instance has no metalc connection to a protein residue or ligand"
	waterOnlyMetalReason = "metal linked only to water: confirm the site"
	outsideChainReason   = "residue outside the polymer chain: confirm it is a ligand"
)

// compoundRule names the rule that placed a compound, as the decision record
// (CompoundSources) stores it.
type compoundRule string

const (
	// ruleList: the reference lists placed it, including "on no list, so a ligand".
	ruleList compoundRule = "list"
	// ruleMetalc: the metal rule placed a metal ion, or the imidazole bound to one.
	ruleMetalc compoundRule = "metalc"
	// ruleBranched: a sugar of a branched entity (a glycan), or a saccharide by CCD type.
	ruleBranched  compoundRule = "branched"
	ruleMacrolide compoundRule = "macrolide"
	// ruleCCDType: a peptide or nucleotide residue outside any polymer chain.
	ruleCCDType compoundRule = "ccd_type"
	// ruleDepositorFlag: the depositor's _pdbx_entity_instance_feature flag.
	ruleDepositorFlag compoundRule = "depositor_flag"
	// ruleCovalentMap: a cofactor bound in a modified residue (PLP in LLP).
	ruleCovalentMap compoundRule = "covalent_map"
	// ruleIHMProbe: an experimental probe of an integrative model.
	ruleIHMProbe compoundRule = "ihm_probe"
)

// rulePrecedence orders the rules for an ID placed in the same group twice:
// the record keeps the more specific rule.
func rulePrecedence(rule compoundRule) (int, error) {
	switch rule {
	case ruleDepositorFlag:
		return 8, nil
	case ruleIHMProbe:
		return 7, nil
	case ruleCovalentMap:
		return 6, nil
	case ruleBranched:
		return 5, nil
	case ruleMacrolide:
		return 4, nil
	case ruleCCDType:
		return 3, nil
	case ruleMetalc:
		return 2, nil
	case ruleList:
		return 1, nil
	default:
		return 0, fmt.Errorf("unknown compound rule %q", rule)
	}
}

type placement int

const (
	placeLigand placement = iota
	placeCofactor
	placeExcluded
)

// decision is where one instance goes, and why.
type decision struct {
	compound  Compound
	placement placement
	rule      compoundRule
}

// compoundShape is what an instance's entity, or failing that its CCD type,
// says before the reference lists are asked.
type compoundShape int

const (
	// shapePolymer: part of a polymer chain, so not a compound at all.
	shapePolymer compoundShape = iota
	shapeWater
	// shapeListed: a non-polymer, sorted by the reference lists.
	shapeListed
	shapeBranched
	shapeMacrolide
	// shapeOutsideChain: a peptide or nucleotide residue outside any chain.
	shapeOutsideChain
)

// sortedCompounds is the outcome of the ligand rule for one file.
type sortedCompounds struct {
	ligands   []Compound
	cofactors []Compound
	sources   CompoundSources
	tooMany   bool
	warnings  []string
}

// compoundSorter holds what the ligand rule reads for one file.
type compoundSorter struct {
	instances   map[compoundInstance]instanceFacts
	partners    metalPartners
	hasMetalc   bool
	annotations fileAnnotations
	reference   *Reference
	// warnedTypes holds the unknown _chem_comp.type values already warned about.
	warnedTypes map[string]bool
	warnings    []string
}

// classifyCompounds applies the ligand rule, per instance:
//
//  1. Water is dropped and polymer residues (MSE in a chain) are not compounds.
//     The entity type decides; without one, the CCD type and the chain position.
//  2. The depositor's flag: SUBJECT OF INVESTIGATION keeps it as a ligand,
//     NO FUNCTIONAL ROLE leaves it out, whatever the lists say.
//  3. An integrative model's probes are left out.
//  4. Glycans (branched entities) and macrolides are ligands.
//  5. Everything else goes by the reference lists and the metal rule (see byLists).
//
// A modified residue that is a covalently bound cofactor (LLP) adds that
// cofactor (PLP). An integrative file's compounds come from every model.
func classifyCompounds(
	instances map[compoundInstance]instanceFacts,
	links metalLinks,
	annotations fileAnnotations,
	reference *Reference,
) (sortedCompounds, error) {
	partners, err := readMetalPartners(links, reference)
	if err != nil {
		return sortedCompounds{}, err
	}
	sorter := compoundSorter{
		instances:   instances,
		partners:    partners,
		hasMetalc:   links.hasRows,
		annotations: annotations,
		reference:   reference,
		warnedTypes: map[string]bool{},
	}
	decisions := []decision{}
	for _, instance := range orderedInstances(instances, annotations.ihm) {
		placed, isCompound, err := sorter.decide(instance)
		if err != nil {
			return sortedCompounds{}, err
		}
		if isCompound {
			decisions = append(decisions, placed)
		}
	}
	for _, residue := range annotations.modifiedResidues {
		if cofactor, ok := reference.covalentCofactor(residue); ok {
			decisions = append(decisions, decision{compound: Compound{ID: cofactor}, placement: placeCofactor, rule: ruleCovalentMap})
		}
	}
	sorted, err := collectDecisions(decisions, reference)
	if err != nil {
		return sortedCompounds{}, err
	}
	sorted.warnings = sorter.warnings
	return sorted, nil
}

// orderedInstances sorts the instances by compound and location. A compound
// seen only in _pdbx_entity_nonpoly is skipped when the coordinates placed
// instances of it, and instances of later models count only for an
// integrative file.
func orderedInstances(instances map[compoundInstance]instanceFacts, allModels bool) []compoundInstance {
	placed := map[string]bool{}
	for instance, facts := range instances {
		if instance.located() && (allModels || !facts.laterModel) {
			placed[instance.compoundID] = true
		}
	}
	ordered := make([]compoundInstance, 0, len(instances))
	for instance, facts := range instances {
		if facts.laterModel && !allModels {
			continue
		}
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
	return ordered
}

// decide places one instance; false when it is not a compound (water, a
// polymer residue).
func (s *compoundSorter) decide(instance compoundInstance) (decision, bool, error) {
	id := instance.compoundID
	facts := s.instances[instance]
	if isWater(id) {
		return decision{}, false, nil
	}
	shape, review, err := s.shapeOf(instance, facts)
	if err != nil {
		return decision{}, false, err
	}
	if annotationReview := s.annotations.reviewFor(instance, facts.entityID); annotationReview != "" {
		review = annotationReview
	}
	if shape == shapeWater || shape == shapePolymer {
		return decision{}, false, nil
	}
	placed, err := s.placeByShape(instance, facts, shape)
	if err != nil {
		return decision{}, false, err
	}
	if review != "" && placed.placement != placeExcluded && !placed.compound.NeedsReview {
		placed.compound.NeedsReview, placed.compound.ReviewReason = true, review
	}
	return placed, true, nil
}

// placeByShape applies steps 2 to 5 of the rule to a compound.
func (s *compoundSorter) placeByShape(instance compoundInstance, facts instanceFacts, shape compoundShape) (decision, error) {
	id := instance.compoundID
	if flag, ok := s.annotations.flagFor(instance); ok {
		switch flag.feature {
		case featureSubjectOfInvestigation:
			return decision{compound: Compound{ID: id}, placement: placeLigand, rule: ruleDepositorFlag}, nil
		case featureNoFunctionalRole:
			return decision{compound: Compound{ID: id}, placement: placeExcluded, rule: ruleDepositorFlag}, nil
		case featureOther, featureUnknownType:
		default:
			return decision{}, fmt.Errorf("unknown depositor flag %d for %s", flag.feature, id)
		}
	}
	if s.annotations.probeEntities[facts.entityID] {
		return decision{compound: Compound{ID: id}, placement: placeExcluded, rule: ruleIHMProbe}, nil
	}
	switch shape {
	case shapeBranched:
		return decision{compound: Compound{ID: id}, placement: placeLigand, rule: ruleBranched}, nil
	case shapeMacrolide:
		return decision{compound: Compound{ID: id}, placement: placeLigand, rule: ruleMacrolide}, nil
	case shapeOutsideChain:
		return decision{compound: Compound{ID: id, NeedsReview: true, ReviewReason: outsideChainReason}, placement: placeLigand, rule: ruleCCDType}, nil
	case shapeListed:
		return s.byLists(instance, facts)
	default:
		return decision{}, fmt.Errorf("unknown compound shape %d for %s", shape, id)
	}
}

// shapeOf reads the instance's entity type. An instance whose entity the file
// does not list (a PDB-format file, or an mmCIF file without _entity) is
// polymer when the file names it as a chain residue (SEQRES, MODRES);
// otherwise its CCD type decides: the file's own _chem_comp.type, then the CCD
// table. A peptide or nucleotide residue within a chain (an mmCIF label_seq_id,
// or a PDB chain without SEQRES) counts as polymer too; one outside any chain
// is a ligand to confirm.
func (s *compoundSorter) shapeOf(instance compoundInstance, facts instanceFacts) (compoundShape, string, error) {
	kind := s.annotations.entityOf(facts.entityID)
	switch kind {
	case entityWater:
		return shapeWater, "", nil
	case entityPolymer:
		return shapePolymer, "", nil
	case entityNonPolymer, entityUnknownType:
		return shapeListed, "", nil
	case entityBranched:
		return shapeBranched, "", nil
	case entityMacrolide:
		return shapeMacrolide, "", nil
	case entityNotRead:
	default:
		return 0, "", fmt.Errorf("unknown entity type %d for %s", kind, instance.compoundID)
	}
	switch facts.chain {
	case chainMember:
		return shapePolymer, "", nil
	case chainSpanOnly, chainNone:
	default:
		return 0, "", fmt.Errorf("unknown chain position %d for %s", facts.chain, instance.compoundID)
	}
	category, review := s.categoryOf(instance.compoundID)
	switch category {
	case CCDPeptide, CCDNucleotide:
		if facts.chain == chainSpanOnly {
			return shapePolymer, "", nil
		}
		return shapeOutsideChain, review, nil
	case CCDSaccharide:
		return shapeBranched, review, nil
	case CCDNonPolymer:
		return shapeListed, review, nil
	default:
		return 0, "", fmt.Errorf("unknown CCD category %q for %s", category, instance.compoundID)
	}
}

// categoryOf returns a compound's CCD category: from the file's _chem_comp.type
// when it has one, else from the CCD table. An unknown type is sorted by the
// lists and flagged.
func (s *compoundSorter) categoryOf(compoundID string) (CCDCategory, string) {
	if fileType := s.annotations.chemCompTypes[compoundID]; fileType != "" {
		category, err := CCDTypeCategory(fileType)
		if err == nil {
			return category, ""
		}
		if !s.warnedTypes[fileType] {
			s.warnedTypes[fileType] = true
			s.warnings = append(s.warnings, fmt.Sprintf("%s has unknown chemical component type %q: sorted by the reference lists and flagged for review", compoundID, fileType))
		}
		return CCDNonPolymer, fmt.Sprintf("unknown chemical component type %q: confirm where this compound belongs", fileType)
	}
	if category, ok := s.reference.ccdCategoryOf(compoundID); ok {
		return category, ""
	}
	return CCDNonPolymer, ""
}

// byLists sorts a non-polymer by the reference lists. Water is always dropped;
// other solvents, buffers and cryoprotectants are left out only once the lists
// are approved, and kept and flagged until then. Imidazole is kept when a
// metalc connection binds it to a metal. Ambiguous compounds (sulfate,
// phosphate, citrate, halides) are kept and flagged. A metal instance is a
// cofactor when a metalc connection joins it to a kept partner, and a flagged
// cofactor when water is its only partner. If the file has no metalc rows at
// all, its metals are cofactors flagged for review. A metal instance without a
// link in a file that has metalc rows becomes a ligand flagged for review, so
// it is never dropped and never placed unflagged.
func (s *compoundSorter) byLists(instance compoundInstance, facts instanceFacts) (decision, error) {
	id := instance.compoundID
	kind := s.reference.kind(id)
	switch kind {
	case kindSolvent:
		return decision{compound: Compound{ID: id}, placement: placeExcluded, rule: ruleList}, nil
	case kindSolventUnlessMetalLinked:
		if s.partners.boundToMetal(instance) {
			return decision{compound: Compound{ID: id}, placement: placeLigand, rule: ruleMetalc}, nil
		}
		return decision{compound: Compound{ID: id}, placement: placeExcluded, rule: ruleList}, nil
	case kindDraftSolvent:
		return decision{compound: Compound{ID: id, NeedsReview: true, ReviewReason: draftSolventReason}, placement: placeLigand, rule: ruleList}, nil
	case kindAmbiguous:
		return decision{compound: Compound{ID: id, NeedsReview: true, ReviewReason: ambiguousReason}, placement: placeLigand, rule: ruleList}, nil
	case kindCofactor:
		return decision{compound: Compound{ID: id}, placement: placeCofactor, rule: ruleList}, nil
	case kindLigand:
		compound, err := ligandCompound(id, facts, s.reference)
		if err != nil {
			return decision{}, err
		}
		return decision{compound: compound, placement: placeLigand, rule: ruleList}, nil
	case kindMetalIon:
		state := s.partners.state(instance)
		switch state {
		case metalLinked:
			return decision{compound: Compound{ID: id}, placement: placeCofactor, rule: ruleMetalc}, nil
		case metalWaterOnly:
			return decision{compound: Compound{ID: id, NeedsReview: true, ReviewReason: waterOnlyMetalReason}, placement: placeCofactor, rule: ruleMetalc}, nil
		case metalUnlinked:
			if s.hasMetalc {
				return decision{compound: Compound{ID: id, NeedsReview: true, ReviewReason: unlinkedMetalReason}, placement: placeLigand, rule: ruleMetalc}, nil
			}
			return decision{compound: Compound{ID: id, NeedsReview: true, ReviewReason: noMetalcRowsReason}, placement: placeCofactor, rule: ruleMetalc}, nil
		default:
			return decision{}, fmt.Errorf("unknown metal link state %d for %s", state, id)
		}
	default:
		return decision{}, fmt.Errorf("unknown compound kind %d for %s", kind, id)
	}
}

// ligandCompound places a compound no list names. A single-atom instance is
// flagged when its element is a metal (a metal missing from the metal list) or
// unknown; a single non-metal atom (xenon, a halide not on any list) is not.
func ligandCompound(id string, facts instanceFacts, reference *Reference) (Compound, error) {
	switch facts.atoms {
	case 1:
		class := reference.elementClass(facts.element)
		switch class {
		case elementNonmetal:
			return Compound{ID: id}, nil
		case elementMetal, elementUnknown:
			return Compound{ID: id, NeedsReview: true, ReviewReason: singleAtomReason}, nil
		default:
			return Compound{}, fmt.Errorf("unknown element class %d for %s", class, id)
		}
	case unknownAtomCount:
		return Compound{ID: id, NeedsReview: true, ReviewReason: unknownAtomsReason}, nil
	default:
		return Compound{ID: id}, nil
	}
}

// collectDecisions builds the ligand and cofactor lists and the decision
// record. Lists are in compound ID order; a flagged entry wins over an
// unflagged one, and the record keeps the more specific rule.
func collectDecisions(decisions []decision, reference *Reference) (sortedCompounds, error) {
	slices.SortStableFunc(decisions, func(left decision, right decision) int {
		return strings.Compare(left.compound.ID, right.compound.ID)
	})
	ligands, cofactors := compoundList{}, compoundList{}
	sources := sourceRecord{
		groups: CompoundSources{
			Ligands:   map[string]CompoundSource{},
			Cofactors: map[string]CompoundSource{},
			Excluded:  map[string]CompoundSource{},
		},
		listVersion: reference.Version,
	}
	for _, placed := range decisions {
		var group map[string]CompoundSource
		switch placed.placement {
		case placeLigand:
			ligands.add(placed.compound)
			group = sources.groups.Ligands
		case placeCofactor:
			cofactors.add(placed.compound)
			group = sources.groups.Cofactors
		case placeExcluded:
			group = sources.groups.Excluded
		default:
			return sortedCompounds{}, fmt.Errorf("unknown placement %d for %s", placed.placement, placed.compound.ID)
		}
		if err := sources.record(group, placed.compound.ID, placed.rule); err != nil {
			return sortedCompounds{}, err
		}
	}
	return sortedCompounds{
		ligands:   ligands.compounds(),
		cofactors: cofactors.compounds(),
		sources:   sources.groups,
		tooMany:   ligands.overflow || cofactors.overflow || sources.overflow,
	}, nil
}

// sourceRecord fills the decision record, bounded like the lists.
type sourceRecord struct {
	groups      CompoundSources
	listVersion string
	overflow    bool
}

func (r *sourceRecord) record(group map[string]CompoundSource, id string, rule compoundRule) error {
	newRank, err := rulePrecedence(rule)
	if err != nil {
		return err
	}
	current, seen := group[id]
	if !seen {
		if len(group) >= maxCompoundsPerList {
			r.overflow = true
			return nil
		}
		group[id] = CompoundSource{Rule: string(rule), ListVersion: r.listVersion}
		return nil
	}
	currentRank, err := rulePrecedence(compoundRule(current.Rule))
	if err != nil {
		return err
	}
	if newRank > currentRank {
		group[id] = CompoundSource{Rule: string(rule), ListVersion: r.listVersion}
	}
	return nil
}

// metalLinkState is what the metalc rows say about one metal instance.
type metalLinkState int

const (
	metalUnlinked metalLinkState = iota
	// metalLinked: joined to a kept partner (a protein residue, a ligand, a
	// cofactor, another metal, or a compound the link itself keeps, like imidazole).
	metalLinked
	// metalWaterOnly: joined to water and to nothing that is kept.
	metalWaterOnly
)

// metalPartners records, from the metalc rows, which metal instances are
// linked and which instances of keep-when-metal-linked compounds are bound to a
// metal. Instances without a location are looked up by compound ID.
type metalPartners struct {
	linkedInstances map[compoundInstance]bool
	linkedCompounds map[string]bool
	waterInstances  map[compoundInstance]bool
	waterCompounds  map[string]bool
	boundInstances  map[compoundInstance]bool
	boundCompounds  map[string]bool
}

func readMetalPartners(links metalLinks, reference *Reference) (metalPartners, error) {
	partners := metalPartners{
		linkedInstances: map[compoundInstance]bool{},
		linkedCompounds: map[string]bool{},
		waterInstances:  map[compoundInstance]bool{},
		waterCompounds:  map[string]bool{},
		boundInstances:  map[compoundInstance]bool{},
		boundCompounds:  map[string]bool{},
	}
	for _, pair := range links.pairs {
		if err := partners.add(pair.first, pair.second, reference); err != nil {
			return metalPartners{}, err
		}
		if err := partners.add(pair.second, pair.first, reference); err != nil {
			return metalPartners{}, err
		}
	}
	return partners, nil
}

// add records one side of a metalc row: what the partner means for the metal.
// A link counts when the partner is kept. Water makes the metal "linked only to
// water" (R4); another dropped solvent does not count.
func (p metalPartners) add(metal compoundInstance, partner compoundInstance, reference *Reference) error {
	if reference.kind(metal.compoundID) != kindMetalIon || partner.compoundID == "" {
		return nil
	}
	kind := reference.kind(partner.compoundID)
	switch kind {
	case kindSolvent:
		if isWater(partner.compoundID) {
			p.waterInstances[metal] = true
			p.waterCompounds[metal.compoundID] = true
		}
	case kindSolventUnlessMetalLinked:
		p.boundInstances[partner] = true
		p.boundCompounds[partner.compoundID] = true
		p.linkedInstances[metal] = true
		p.linkedCompounds[metal.compoundID] = true
	case kindLigand, kindCofactor, kindMetalIon, kindAmbiguous, kindDraftSolvent:
		p.linkedInstances[metal] = true
		p.linkedCompounds[metal.compoundID] = true
	default:
		return fmt.Errorf("unknown compound kind %d for metal partner %s", kind, partner.compoundID)
	}
	return nil
}

func (p metalPartners) state(metal compoundInstance) metalLinkState {
	linked, water := p.linkedInstances[metal], p.waterInstances[metal]
	if !metal.located() {
		linked, water = p.linkedCompounds[metal.compoundID], p.waterCompounds[metal.compoundID]
	}
	switch {
	case linked:
		return metalLinked
	case water:
		return metalWaterOnly
	default:
		return metalUnlinked
	}
}

func (p metalPartners) boundToMetal(instance compoundInstance) bool {
	if !instance.located() {
		return p.boundCompounds[instance.compoundID]
	}
	return p.boundInstances[instance]
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
