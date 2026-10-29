package cif

import (
	"fmt"
	"slices"
	"strings"
)

// entityType is the _entity.type of an entity.
type entityType int

const (
	// entityNotRead: the instance names no entity the file lists.
	entityNotRead entityType = iota
	entityPolymer
	entityNonPolymer
	entityBranched
	entityMacrolide
	entityWater
	// entityUnknownType: a type value the parser does not know; its compounds
	// are sorted by the lists and flagged.
	entityUnknownType
)

// featureType is the _pdbx_entity_instance_feature.feature_type of a
// depositor flag.
type featureType int

const (
	featureSubjectOfInvestigation featureType = iota
	featureNoFunctionalRole
	featureOther
	// featureUnknownType: a value the parser does not know; no override, and
	// the compound is flagged.
	featureUnknownType
)

// depositorFlag is one _pdbx_entity_instance_feature row. An empty asymID or
// authSeqID matches every instance of the compound.
type depositorFlag struct {
	compoundID string
	asymID     string
	authSeqID  string
	feature    featureType
}

// fileAnnotations is what the categories besides _atom_site say about the
// compounds of a file.
type fileAnnotations struct {
	// entitiesRead is false when the file has no _entity (or a cut-off read may
	// have lost it): compounds are then sorted by their CCD type.
	entitiesRead bool
	entityTypes  map[string]entityType
	// chemCompTypes is the file's own _chem_comp.type per compound ID.
	chemCompTypes map[string]string
	flags         []depositorFlag
	// modifiedResidues are the label_comp_id values of _pdbx_struct_mod_residue.
	modifiedResidues []string
	glycans          []Glycan
	// ihm is set for an integrative file: its compounds come from every model.
	ihm           bool
	probeEntities map[string]bool
	// entityReview and compoundReview flag the compounds of an entity, or a
	// compound, whose annotation holds a value the parser does not know.
	entityReview   map[string]string
	compoundReview map[string]string
	warnings       []string
}

// pdbAnnotations is the starting point for a PDB-format file: it has no
// categories, so only its MODRES residues are added (see pdbExtraction).
func pdbAnnotations() fileAnnotations {
	return fileAnnotations{
		entityTypes:    map[string]entityType{},
		chemCompTypes:  map[string]string{},
		probeEntities:  map[string]bool{},
		entityReview:   map[string]string{},
		compoundReview: map[string]string{},
	}
}

// cutOffRules names the rule each category feeds, for the warning a cut-off
// read gives.
var cutOffRules = map[string]string{
	"_entity":                        "entity types; compounds sorted by CCD type instead",
	"_pdbx_entity_instance_feature":  "depositor flags",
	"_pdbx_struct_mod_residue":       "covalently bound cofactors",
	"_pdbx_entity_branch_descriptor": "glycan descriptors",
	"_pdbx_entity_branch_list":       "glycan compound lists",
	"_ihm_ligand_probe":              "IHM probe exclusion",
}

// mayBeCutOff reports whether a category may be missing or partial because the
// read ended early. Archive files do not write categories in a fixed order, so
// a missing category is only known to be absent when the whole file was read.
func (d *Document) mayBeCutOff(category string) bool {
	if !d.truncated && !d.timedOut {
		return false
	}
	table := d.Table(category)
	return table == nil || !table.Complete
}

// readAnnotations reads the categories the compound rules use. A category a
// cut-off read may have lost feeds no rule: the rule is skipped, never guessed,
// and one warning names every such category. atomSiteIHM is set when the
// coordinate loop has an ihm_model_id column.
func readAnnotations(document *Document, atomSiteIHM bool) fileAnnotations {
	annotations := fileAnnotations{
		entityTypes:    map[string]entityType{},
		chemCompTypes:  map[string]string{},
		probeEntities:  map[string]bool{},
		entityReview:   map[string]string{},
		compoundReview: map[string]string{},
	}
	cutOff := []string{}
	skip := func(category string) bool {
		if document.mayBeCutOff(category) {
			cutOff = append(cutOff, category)
			return true
		}
		return false
	}

	if !skip("_entity") {
		annotations.readEntities(document.Table("_entity"))
	}
	annotations.readChemComps(document.Table("_chem_comp"))
	if !skip("_pdbx_entity_instance_feature") {
		annotations.readFlags(document.Table("_pdbx_entity_instance_feature"))
	}
	if !skip("_pdbx_struct_mod_residue") {
		annotations.readModifiedResidues(document.Table("_pdbx_struct_mod_residue"))
	}
	if annotations.hasBranchedEntities() {
		var descriptors, members *Table
		if !skip("_pdbx_entity_branch_descriptor") {
			descriptors = document.Table("_pdbx_entity_branch_descriptor")
		}
		if !skip("_pdbx_entity_branch_list") {
			members = document.Table("_pdbx_entity_branch_list")
		}
		annotations.readGlycans(descriptors, members)
	}
	annotations.ihm = atomSiteIHM || document.Table("_ihm_model_list") != nil
	if annotations.ihm && !skip("_ihm_ligand_probe") {
		annotations.readProbes(document.Table("_ihm_ligand_probe"))
	}

	if len(cutOff) > 0 {
		parts := make([]string, 0, len(cutOff))
		for _, category := range cutOff {
			parts = append(parts, category+" ("+cutOffRules[category]+")")
		}
		annotations.warnings = append(annotations.warnings,
			"the read limit may have cut these categories, so the rules they feed were not applied: "+strings.Join(parts, ", "))
	}
	return annotations
}

func (a *fileAnnotations) readEntities(table *Table) {
	if table == nil {
		return
	}
	a.entitiesRead = true
	for _, row := range table.Rows {
		id := table.Value(row, "id")
		if id == "" {
			continue
		}
		value := table.Value(row, "type")
		switch strings.ToLower(value) {
		case "polymer":
			a.entityTypes[id] = entityPolymer
		case "non-polymer":
			a.entityTypes[id] = entityNonPolymer
		case "branched":
			a.entityTypes[id] = entityBranched
		case "macrolide":
			a.entityTypes[id] = entityMacrolide
		case "water":
			a.entityTypes[id] = entityWater
		default:
			a.entityTypes[id] = entityUnknownType
			a.entityReview[id] = fmt.Sprintf("unknown entity type %q: confirm where this compound belongs", value)
			a.warnings = append(a.warnings, fmt.Sprintf("entity %s has unknown type %q: its compounds are sorted by the reference lists and flagged for review", id, value))
		}
	}
}

func (a *fileAnnotations) readChemComps(table *Table) {
	if table == nil {
		return
	}
	for _, row := range table.Rows {
		if id := strings.ToUpper(table.Value(row, "id")); id != "" {
			a.chemCompTypes[id] = table.Value(row, "type")
		}
	}
}

func (a *fileAnnotations) readFlags(table *Table) {
	if table == nil {
		return
	}
	for _, row := range table.Rows {
		flag := depositorFlag{
			compoundID: strings.ToUpper(table.Value(row, "comp_id")),
			asymID:     table.Value(row, "asym_id"),
			authSeqID:  table.Value(row, "auth_seq_num"),
		}
		if flag.compoundID == "" {
			continue
		}
		value := table.Value(row, "feature_type")
		switch strings.ToUpper(value) {
		case "SUBJECT OF INVESTIGATION":
			flag.feature = featureSubjectOfInvestigation
		case "NO FUNCTIONAL ROLE":
			flag.feature = featureNoFunctionalRole
		case "OTHER":
			flag.feature = featureOther
		default:
			flag.feature = featureUnknownType
			a.compoundReview[flag.compoundID] = fmt.Sprintf("unknown depositor flag %q: confirm where this compound belongs", value)
			a.warnings = append(a.warnings, fmt.Sprintf("%s has unknown depositor flag %q: no override applied, compound flagged for review", flag.compoundID, value))
		}
		a.flags = append(a.flags, flag)
	}
}

func (a *fileAnnotations) readModifiedResidues(table *Table) {
	if table == nil {
		return
	}
	for _, row := range table.Rows {
		if id := strings.ToUpper(table.Value(row, "label_comp_id")); id != "" && !slices.Contains(a.modifiedResidues, id) {
			a.modifiedResidues = append(a.modifiedResidues, id)
		}
	}
}

func (a *fileAnnotations) hasBranchedEntities() bool {
	for _, kind := range a.entityTypes {
		if kind == entityBranched {
			return true
		}
	}
	return false
}

// readGlycans builds one record per branched entity: its sugars from
// _pdbx_entity_branch_list and its WURCS descriptors. Other known descriptor
// types are not kept; an unknown type flags the entity's compounds.
func (a *fileAnnotations) readGlycans(descriptors *Table, members *Table) {
	byEntity := map[string]*Glycan{}
	ids := []string{}
	for id, kind := range a.entityTypes {
		if kind == entityBranched {
			byEntity[id] = &Glycan{EntityID: id, CompoundIDs: []string{}, Descriptors: []GlycanDescriptor{}}
			ids = append(ids, id)
		}
	}
	if members != nil {
		for _, row := range members.Rows {
			glycan := byEntity[members.Value(row, "entity_id")]
			compound := strings.ToUpper(members.Value(row, "comp_id"))
			if glycan != nil && compound != "" && !slices.Contains(glycan.CompoundIDs, compound) {
				glycan.CompoundIDs = append(glycan.CompoundIDs, compound)
			}
		}
	}
	if descriptors != nil {
		for _, row := range descriptors.Rows {
			entityID := descriptors.Value(row, "entity_id")
			glycan := byEntity[entityID]
			if glycan == nil {
				continue
			}
			value := descriptors.Value(row, "type")
			switch strings.ToUpper(value) {
			case "WURCS":
				glycan.Descriptors = append(glycan.Descriptors, GlycanDescriptor{
					Descriptor:     descriptors.Value(row, "descriptor"),
					Type:           value,
					Program:        descriptors.Value(row, "program"),
					ProgramVersion: descriptors.Value(row, "program_version"),
				})
			case "LINUCS", "GLYCAM CONDENSED SEQUENCE", "GLYCAM CONDENSED CORE SEQUENCE":
				// Known types the researchers did not ask for: WURCS alone is kept.
			default:
				a.entityReview[entityID] = fmt.Sprintf("unknown glycan descriptor type %q: confirm the glycan", value)
				a.warnings = append(a.warnings, fmt.Sprintf("entity %s has a glycan descriptor of unknown type %q: its compounds are flagged for review", entityID, value))
			}
		}
	}
	slices.SortFunc(ids, compareEntityIDs)
	for _, id := range ids {
		slices.Sort(byEntity[id].CompoundIDs)
		a.glycans = append(a.glycans, *byEntity[id])
	}
}

func (a *fileAnnotations) readProbes(table *Table) {
	if table == nil {
		return
	}
	for _, row := range table.Rows {
		if id := table.Value(row, "entity_id"); id != "" {
			a.probeEntities[id] = true
		}
	}
}

// compareEntityIDs orders entity IDs as numbers when both are, as archive
// files number them, and as text otherwise.
func compareEntityIDs(left string, right string) int {
	if len(left) != len(right) && isDigits(left) && isDigits(right) {
		return len(left) - len(right)
	}
	return strings.Compare(left, right)
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// entityOf returns the type of an instance's entity, entityNotRead when the
// file lists no such entity.
func (a fileAnnotations) entityOf(entityID string) entityType {
	if !a.entitiesRead || entityID == "" {
		return entityNotRead
	}
	kind, ok := a.entityTypes[entityID]
	if !ok {
		return entityNotRead
	}
	return kind
}

// flagFor returns the depositor flag that matches an instance. An instance
// without a location matches only flags without one.
func (a fileAnnotations) flagFor(instance compoundInstance) (depositorFlag, bool) {
	for _, flag := range a.flags {
		if flag.compoundID != instance.compoundID {
			continue
		}
		if flag.asymID != "" && flag.asymID != instance.asymID {
			continue
		}
		if flag.authSeqID != "" && flag.authSeqID != instance.authSeqID {
			continue
		}
		return flag, true
	}
	return depositorFlag{}, false
}

// reviewFor returns the reason an annotation flags an instance, or "".
func (a fileAnnotations) reviewFor(instance compoundInstance, entityID string) string {
	if reason := a.compoundReview[instance.compoundID]; reason != "" {
		return reason
	}
	return a.entityReview[entityID]
}
