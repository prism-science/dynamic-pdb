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
	// undeclaredChainReason flags a residue that could be a chain residue in a
	// file that does not say what its chains hold (no SEQRES, no _entity).
	undeclaredChainReason = "the file does not say which residues belong to its polymer chains: confirm this residue is not part of one"
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
	// ruleCCDType: a peptide or nucleotide residue outside any polymer chain
	// whose name the file's SEQRES records use (a free glutamate).
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
	sorter := compoundSorter{
		instances:   instances,
		hasMetalc:   links.hasRows,
		annotations: annotations,
		reference:   reference,
		warnedTypes: map[string]bool{},
	}
	partners, err := sorter.readMetalPartners(links)
	if err != nil {
		return sortedCompounds{}, fmt.Errorf("read metal links: %w", err)
	}
	sorter.partners = partners
	decisions := []decision{}
	for _, instance := range orderedInstances(instances, annotations.ihm) {
		placed, isCompound, err := sorter.decide(instance)
		if err != nil {
			return sortedCompounds{}, fmt.Errorf("place %s: %w", instance.compoundID, err)
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
		return sortedCompounds{}, fmt.Errorf("collect decisions: %w", err)
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
	shape, shapeReview, err := s.shapeOf(instance, facts)
	if err != nil {
		return decision{}, false, fmt.Errorf("read the shape of %s: %w", id, err)
	}
	review := s.unknownValueReview(instance, facts, shapeReview)
	if shape == shapeWater || shape == shapePolymer {
		return decision{}, false, nil
	}
	placed, err := s.placeByShape(instance, facts, shape)
	if err != nil {
		return decision{}, false, fmt.Errorf("place %s by its shape: %w", id, err)
	}
	if review != "" && placed.placement == placeExcluded && placed.rule == ruleList {
		// The file holds a value the parser does not know, so the lists may be
		// wrong about this compound: it is kept for review instead of left out.
		placed.placement = placeLigand
	}
	if review != "" && placed.placement != placeExcluded && !placed.compound.NeedsReview {
		placed.compound.NeedsReview, placed.compound.ReviewReason = true, review
	}
	return placed, true, nil
}

// unknownValueReview is the review reason a value the parser does not know (an
// entity type, a depositor flag, a _chem_comp.type) gives the instance, or ""
// when the file holds no such value for it.
func (s *compoundSorter) unknownValueReview(instance compoundInstance, facts instanceFacts, shapeReview string) string {
	if review := s.annotations.reviewFor(instance, facts.entityID); review != "" {
		return review
	}
	return shapeReview
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
	case shapePolymer, shapeWater:
		return decision{}, fmt.Errorf("%s is not a compound", id)
	default:
		return decision{}, fmt.Errorf("unknown compound shape %d for %s", shape, id)
	}
}

// shapeOf reads the instance's entity type. An instance whose entity the file
// does not list (a PDB-format file, or an mmCIF file without _entity) is
// polymer when the file names it as a chain residue (SEQRES, MODRES);
// otherwise its CCD type decides: the file's own _chem_comp.type, then the CCD
// table. When the file does not say what its chains hold, undeclaredShape
// decides instead. Otherwise a peptide or nucleotide residue within a chain's
// span (a HETATM-only chain of an archive file) counts as polymer. Outside any
// chain, it is a ligand to confirm when its name is in the file's SEQRES
// records (a free glutamate next to a chain of glutamates); otherwise the lists
// sort it like any non-polymer (GDP, which the CCD types as "RNA linking").
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
	if facts.chainsUndeclared {
		return undeclaredShape(instance.compoundID, facts.chain, category, review)
	}
	switch category {
	case CCDPeptide, CCDNucleotide:
		switch {
		case facts.chain == chainSpanOnly:
			return shapePolymer, "", nil
		case s.annotations.sequenceNames[instance.compoundID]:
			return shapeOutsideChain, review, nil
		default:
			return shapeListed, review, nil
		}
	case CCDSaccharide:
		return shapeBranched, review, nil
	case CCDNonPolymer:
		return shapeListed, review, nil
	default:
		return 0, "", fmt.Errorf("unknown CCD category %q for %s", category, instance.compoundID)
	}
}

// undeclaredShape sorts a residue of a file that does not say what its chains
// hold (no SEQRES records, or no _entity). A residue that could be a chain
// residue is not guessed: a peptide or nucleotide anywhere, and any residue
// within a chain's span, is sorted as a compound and flagged, and decide keeps
// it even when the lists would leave it out. Other residues outside the chains
// are sorted as usual. A review reason the CCD type already gives wins.
func undeclaredShape(compoundID string, chain chainPosition, category CCDCategory, review string) (compoundShape, string, error) {
	couldBeChainResidue := chain == chainSpanOnly
	var shape compoundShape
	switch category {
	case CCDPeptide, CCDNucleotide:
		shape, couldBeChainResidue = shapeListed, true
	case CCDSaccharide:
		shape = shapeBranched
	case CCDNonPolymer:
		shape = shapeListed
	default:
		return 0, "", fmt.Errorf("unknown CCD category %q for %s", category, compoundID)
	}
	if couldBeChainResidue && review == "" {
		review = undeclaredChainReason
	}
	return shape, review, nil
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
			return decision{}, fmt.Errorf("place ligand %s: %w", id, err)
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
			return sortedCompounds{}, fmt.Errorf("record %s: %w", placed.compound.ID, err)
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
		return fmt.Errorf("rank the new rule for %s: %w", id, err)
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
		return fmt.Errorf("rank the recorded rule for %s: %w", id, err)
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

// partnerFate is what happens to a metal's link partner, and so what the link
// means for the metal.
type partnerFate int

const (
	// partnerDropped: left out of the entry (a solvent, a compound the
	// depositor marks as having no functional role, an integrative model's
	// probe). The link does not count.
	partnerDropped partnerFate = iota
	// partnerKept: a polymer residue or a kept compound. The metal is linked.
	partnerKept
	// partnerWater: water. The metal may be linked only to water (R4).
	partnerWater
	// partnerBindsMetal: a compound kept only when a metal binds it
	// (imidazole). The metal is linked and the partner kept.
	partnerBindsMetal
)

func (s *compoundSorter) readMetalPartners(links metalLinks) (metalPartners, error) {
	partners := metalPartners{
		linkedInstances: map[compoundInstance]bool{},
		linkedCompounds: map[string]bool{},
		waterInstances:  map[compoundInstance]bool{},
		waterCompounds:  map[string]bool{},
		boundInstances:  map[compoundInstance]bool{},
		boundCompounds:  map[string]bool{},
	}
	for _, pair := range links.pairs {
		if err := s.addMetalPartner(partners, pair.first, pair.second); err != nil {
			return metalPartners{}, fmt.Errorf("read metalc pair %s-%s: %w", pair.first.compoundID, pair.second.compoundID, err)
		}
		if err := s.addMetalPartner(partners, pair.second, pair.first); err != nil {
			return metalPartners{}, fmt.Errorf("read metalc pair %s-%s: %w", pair.second.compoundID, pair.first.compoundID, err)
		}
	}
	return partners, nil
}

// addMetalPartner records one side of a metalc row: what the partner means
// for the metal.
func (s *compoundSorter) addMetalPartner(partners metalPartners, metal compoundInstance, partner compoundInstance) error {
	if s.reference.kind(metal.compoundID) != kindMetalIon || partner.compoundID == "" {
		return nil
	}
	fate, err := s.partnerFate(partner)
	if err != nil {
		return fmt.Errorf("place partner %s: %w", partner.compoundID, err)
	}
	switch fate {
	case partnerDropped:
	case partnerKept:
		partners.linkedInstances[metal.linkKey()] = true
		partners.linkedCompounds[metal.compoundID] = true
	case partnerWater:
		partners.waterInstances[metal.linkKey()] = true
		partners.waterCompounds[metal.compoundID] = true
	case partnerBindsMetal:
		partners.boundInstances[partner.linkKey()] = true
		partners.boundCompounds[partner.compoundID] = true
		partners.linkedInstances[metal.linkKey()] = true
		partners.linkedCompounds[metal.compoundID] = true
	default:
		return fmt.Errorf("unknown partner fate %d for %s", fate, partner.compoundID)
	}
	return nil
}

// partnerFate places a metal's link partner by the steps that place it as a
// compound, so a link counts only when the partner stays in the entry. A
// partner with coordinates, or one the file declares in _pdbx_entity_nonpoly
// without coordinates (cut off, or never written), goes by its shape (entity
// type or chain position), the depositor's flag and the probe exclusion first;
// a polymer residue of the ATOM records, and a partner left to the lists, go
// by the lists.
func (s *compoundSorter) partnerFate(partner compoundInstance) (partnerFate, error) {
	if isWater(partner.compoundID) {
		return partnerWater, nil
	}
	facts, ok := s.instances[partner]
	if !ok {
		facts, ok = s.instances[compoundInstance{compoundID: partner.compoundID}]
	}
	if ok {
		fate, decided, err := s.annotatedPartnerFate(partner, facts)
		if err != nil || decided {
			return fate, err
		}
	}
	kind := s.reference.kind(partner.compoundID)
	switch kind {
	case kindSolvent:
		return partnerDropped, nil
	case kindSolventUnlessMetalLinked:
		return partnerBindsMetal, nil
	case kindLigand, kindCofactor, kindMetalIon, kindAmbiguous, kindDraftSolvent:
		return partnerKept, nil
	default:
		return 0, fmt.Errorf("unknown compound kind %d for %s", kind, partner.compoundID)
	}
}

// annotatedPartnerFate applies steps 1 to 4 of the rule (see classifyCompounds)
// to a partner, with the facts of its coordinates or of its
// _pdbx_entity_nonpoly declaration. decided is false when the lists decide.
func (s *compoundSorter) annotatedPartnerFate(partner compoundInstance, facts instanceFacts) (partnerFate, bool, error) {
	shape, shapeReview, err := s.shapeOf(partner, facts)
	if err != nil {
		return 0, false, fmt.Errorf("read the shape of %s: %w", partner.compoundID, err)
	}
	switch shape {
	case shapeWater:
		return partnerWater, true, nil
	case shapePolymer:
		return partnerKept, true, nil
	case shapeBranched, shapeMacrolide, shapeOutsideChain, shapeListed:
	default:
		return 0, false, fmt.Errorf("unknown compound shape %d for %s", shape, partner.compoundID)
	}
	if flag, ok := s.annotations.flagFor(partner); ok {
		switch flag.feature {
		case featureSubjectOfInvestigation:
			return partnerKept, true, nil
		case featureNoFunctionalRole:
			return partnerDropped, true, nil
		case featureOther, featureUnknownType:
		default:
			return 0, false, fmt.Errorf("unknown depositor flag %d for %s", flag.feature, partner.compoundID)
		}
	}
	if s.annotations.probeEntities[facts.entityID] {
		return partnerDropped, true, nil
	}
	switch shape {
	case shapeBranched, shapeMacrolide, shapeOutsideChain:
		return partnerKept, true, nil
	case shapeListed:
		// decide keeps a solvent the file gives an unknown value for.
		if s.unknownValueReview(partner, facts, shapeReview) != "" && s.reference.kind(partner.compoundID) == kindSolvent {
			return partnerKept, true, nil
		}
		return 0, false, nil
	case shapeWater, shapePolymer:
		return 0, false, fmt.Errorf("%s is not a compound", partner.compoundID)
	default:
		return 0, false, fmt.Errorf("unknown compound shape %d for %s", shape, partner.compoundID)
	}
}

// state is what the metalc rows say about one metal instance. A row that
// names no location for the metal applies to every instance of it.
func (p metalPartners) state(metal compoundInstance) metalLinkState {
	linked, water := p.linkedCompounds[metal.compoundID], p.waterCompounds[metal.compoundID]
	if metal.located() {
		anywhere := compoundInstance{compoundID: metal.compoundID}
		linked = p.linkedInstances[metal] || p.linkedInstances[anywhere]
		water = p.waterInstances[metal] || p.waterInstances[anywhere]
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
	return p.boundInstances[instance] || p.boundInstances[compoundInstance{compoundID: instance.compoundID}]
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
