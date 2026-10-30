package models

import "maps"

// CompoundRule names the rule that put a compound in an entry's lists, or left it out. The
// parser's rules are the ones lib/cif applies (docs/cif-extraction.md); CompoundRuleDepositor
// marks a compound a person added to a list.
type CompoundRule string

const (
	CompoundRuleList          CompoundRule = "list"
	CompoundRuleMetalc        CompoundRule = "metalc"
	CompoundRuleBranched      CompoundRule = "branched"
	CompoundRuleMacrolide     CompoundRule = "macrolide"
	CompoundRuleCCDType       CompoundRule = "ccd_type"
	CompoundRuleDepositorFlag CompoundRule = "depositor_flag"
	CompoundRuleCovalentMap   CompoundRule = "covalent_map"
	CompoundRuleIHMProbe      CompoundRule = "ihm_probe"
	CompoundRuleDepositor     CompoundRule = "depositor"
)

// CompoundSources is the decision record of an entry's composition lists: per placement, the
// rule that put each CCD ID there. One ID can be in two groups, such as a zinc linked to the
// protein (a cofactor) and one linked to nothing (a ligand). Water and polymer residues are
// never recorded.
type CompoundSources struct {
	Ligands   map[string]CompoundSource `json:"ligands"`
	Cofactors map[string]CompoundSource `json:"cofactors"`
	Excluded  map[string]CompoundSource `json:"excluded"`
}

// CompoundSource is one entry of the decision record. ListVersion is the reference version the
// parser applied; a compound a person added has none. ReviewReason is set when the parser
// flagged the compound for review, such as a metal ion in a file without metalc rows.
type CompoundSource struct {
	Rule         CompoundRule `json:"rule"`
	ListVersion  string       `json:"list_version,omitempty"`
	ReviewReason string       `json:"review_reason,omitempty"`
}

// EditedCompoundSources returns the decision record of a revision whose lists a person may have
// changed, given record, the record of the revision they edited. A compound still in its list
// keeps its record, one the person added gets rule depositor, and one they removed leaves the
// record. Excluded compounds stay as the parser recorded them. A nil record stays nil: lists that
// were never derived have no record to keep in step.
func EditedCompoundSources(record *CompoundSources, metadata EntryMetadata) *CompoundSources {
	if record == nil {
		return nil
	}
	return &CompoundSources{
		Ligands:   editedCompoundGroup(record.Ligands, metadata.Ligands),
		Cofactors: editedCompoundGroup(record.Cofactors, metadata.Cofactors),
		Excluded:  maps.Clone(record.Excluded),
	}
}

// editedCompoundGroup returns the record of one list: the IDs in list, with the record group's
// value where it has one and rule depositor where it has none. A nil (unknown) list has no IDs.
func editedCompoundGroup(group map[string]CompoundSource, list *[]string) map[string]CompoundSource {
	edited := map[string]CompoundSource{}
	if list == nil {
		return edited
	}
	for _, id := range *list {
		if source, ok := group[id]; ok {
			edited[id] = source
			continue
		}
		edited[id] = CompoundSource{Rule: CompoundRuleDepositor}
	}
	return edited
}

// EntryComposition is an entry revision's composition: its ligand and cofactor lists, their
// decision record and its glycans. Nil means unset.
type EntryComposition struct {
	Ligands           *[]string        `json:"ligands"`
	Cofactors         *[]string        `json:"cofactors"`
	CompoundSources   *CompoundSources `json:"compound_sources"`
	GlycanDescriptors *[]Glycan        `json:"glycan_descriptors"`
}

// Composition returns the revision's composition.
func (revision EntryRevision) Composition() EntryComposition {
	return EntryComposition{
		Ligands:           revision.Metadata.Ligands,
		Cofactors:         revision.Metadata.Cofactors,
		CompoundSources:   revision.CompoundSources,
		GlycanDescriptors: revision.GlycanDescriptors,
	}
}

// SetComposition replaces the revision's composition with composition.
func (revision *EntryRevision) SetComposition(composition EntryComposition) {
	revision.Metadata.Ligands = composition.Ligands
	revision.Metadata.Cofactors = composition.Cofactors
	revision.CompoundSources = composition.CompoundSources
	revision.GlycanDescriptors = composition.GlycanDescriptors
}

// Glycan is one branched entity of an entry: its sugars and the WURCS descriptors that keep
// the branching a list of IDs loses.
type Glycan struct {
	EntityID    string             `json:"entity_id"`
	CompoundIDs []string           `json:"compound_ids"`
	Descriptors []GlycanDescriptor `json:"descriptors"`
}

// GlycanDescriptor is one _pdbx_entity_branch_descriptor row of a branched entity.
type GlycanDescriptor struct {
	Descriptor     string `json:"descriptor"`
	Type           string `json:"type"`
	Program        string `json:"program,omitempty"`
	ProgramVersion string `json:"program_version,omitempty"`
}
