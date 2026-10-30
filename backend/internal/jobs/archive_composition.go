package jobs

import (
	"fmt"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// archiveComposition is the sample composition an entry's RCSB mmCIF gives it: the ligand and
// cofactor lists, the decision record that says which rule placed each compound, and the
// glycans. The entry sync and the archive re-derivation job both derive it from the same head
// of the file, so they store the same values.
type archiveComposition struct {
	ligands   []string
	cofactors []string
	sources   models.CompoundSources
	glycans   []models.Glycan
}

// compositionFromResult turns what the parser read from an entry's mmCIF into its composition.
// A compound the parser flagged for review keeps the reason in its record (T1-C2 decision D2).
// A rule the backend does not know is an error, so a parser change cannot store a record the
// API cannot return.
func compositionFromResult(result cif.Result) (archiveComposition, error) {
	ligands, ligandSources, err := compoundGroupFromResult(result.Ligands, result.CompoundSources.Ligands)
	if err != nil {
		return archiveComposition{}, fmt.Errorf("ligands: %w", err)
	}
	cofactors, cofactorSources, err := compoundGroupFromResult(result.Cofactors, result.CompoundSources.Cofactors)
	if err != nil {
		return archiveComposition{}, fmt.Errorf("cofactors: %w", err)
	}
	excluded, err := compoundSourcesFromResult(result.CompoundSources.Excluded, nil)
	if err != nil {
		return archiveComposition{}, fmt.Errorf("excluded compounds: %w", err)
	}
	return archiveComposition{
		ligands:   ligands,
		cofactors: cofactors,
		sources:   models.CompoundSources{Ligands: ligandSources, Cofactors: cofactorSources, Excluded: excluded},
		glycans:   glycansFromResult(result.Glycans),
	}, nil
}

// applyArchiveComposition puts into desired the composition the entry sync takes from the entry's
// mmCIF, and reports whether it took it from the file. Lists a person set, and the stored values
// when the file could not be read completely, are kept as current holds them, with the decision
// record and glycans that go with them.
func applyArchiveComposition(current models.EntryRevision, desired *models.EntryRevision, file archiveFile) bool {
	if entryListsPersonSet(current) || file.composition == nil {
		desired.Metadata.Ligands = current.Metadata.Ligands
		desired.Metadata.Cofactors = current.Metadata.Cofactors
		desired.CompoundSources = current.CompoundSources
		desired.GlycanDescriptors = current.GlycanDescriptors
		return false
	}
	values := compoundValuesFromComposition(*file.composition)
	desired.Metadata.Ligands = values.Ligands
	desired.Metadata.Cofactors = values.Cofactors
	desired.CompoundSources = values.CompoundSources
	desired.GlycanDescriptors = values.GlycanDescriptors
	return true
}

// entryListsPersonSet reports whether a person set the revision's composition lists, so they are
// kept. Lists a person changed carry depositor sources. Lists without a decision record were not
// derived from the file by these rules, so a person typed them; the C1 migrations set no lists
// on archive entries.
func entryListsPersonSet(revision models.EntryRevision) bool {
	if revision.FieldSources.IsDepositor(models.EntryFieldLigands) ||
		revision.FieldSources.IsDepositor(models.EntryFieldCofactors) {
		return true
	}
	listsSet := revision.Metadata.Ligands != nil || revision.Metadata.Cofactors != nil
	return listsSet && revision.CompoundSources == nil
}

func compoundValuesFromComposition(composition archiveComposition) models.ArchiveCompoundValues {
	return models.ArchiveCompoundValues{
		Ligands:           new(composition.ligands),
		Cofactors:         new(composition.cofactors),
		CompoundSources:   new(composition.sources),
		GlycanDescriptors: new(composition.glycans),
	}
}

// compoundGroupFromResult returns the CCD IDs of one list, in the parser's order, and their
// record. Every compound in the list must have a record.
func compoundGroupFromResult(
	compounds []cif.Compound,
	sources map[string]cif.CompoundSource,
) ([]string, map[string]models.CompoundSource, error) {
	ids := make([]string, 0, len(compounds))
	listed := map[string]bool{}
	reviewReasons := map[string]string{}
	for _, compound := range compounds {
		if _, ok := sources[compound.ID]; !ok {
			return nil, nil, fmt.Errorf("compound %s has no record", compound.ID)
		}
		if !listed[compound.ID] {
			listed[compound.ID] = true
			ids = append(ids, compound.ID)
		}
		if compound.NeedsReview && reviewReasons[compound.ID] == "" {
			reviewReasons[compound.ID] = compound.ReviewReason
		}
	}
	record, err := compoundSourcesFromResult(sources, reviewReasons)
	if err != nil {
		return nil, nil, err
	}
	return ids, record, nil
}

// compoundSourcesFromResult converts one group of the parser's record, adding the review reason
// reviewReasons gives each CCD ID.
func compoundSourcesFromResult(
	sources map[string]cif.CompoundSource,
	reviewReasons map[string]string,
) (map[string]models.CompoundSource, error) {
	record := make(map[string]models.CompoundSource, len(sources))
	for id, source := range sources {
		rule, err := compoundRuleFromResult(source.Rule)
		if err != nil {
			return nil, fmt.Errorf("compound %s: %w", id, err)
		}
		record[id] = models.CompoundSource{
			Rule:         rule,
			ListVersion:  source.ListVersion,
			ReviewReason: reviewReasons[id],
		}
	}
	return record, nil
}

// compoundRuleFromResult maps a rule the parser applied. CompoundRuleDepositor is not one: only
// a person's edit gives a compound that rule.
func compoundRuleFromResult(rule string) (models.CompoundRule, error) {
	switch models.CompoundRule(rule) {
	case models.CompoundRuleList:
		return models.CompoundRuleList, nil
	case models.CompoundRuleMetalc:
		return models.CompoundRuleMetalc, nil
	case models.CompoundRuleBranched:
		return models.CompoundRuleBranched, nil
	case models.CompoundRuleMacrolide:
		return models.CompoundRuleMacrolide, nil
	case models.CompoundRuleCCDType:
		return models.CompoundRuleCCDType, nil
	case models.CompoundRuleDepositorFlag:
		return models.CompoundRuleDepositorFlag, nil
	case models.CompoundRuleCovalentMap:
		return models.CompoundRuleCovalentMap, nil
	case models.CompoundRuleIHMProbe:
		return models.CompoundRuleIHMProbe, nil
	case models.CompoundRuleDepositor:
		return "", fmt.Errorf("the parser gave rule %s, which only a person's edit may give", rule)
	default:
		return "", fmt.Errorf("unknown compound rule from the parser: %s", rule)
	}
}

func glycansFromResult(glycans []cif.Glycan) []models.Glycan {
	result := make([]models.Glycan, 0, len(glycans))
	for _, glycan := range glycans {
		descriptors := make([]models.GlycanDescriptor, 0, len(glycan.Descriptors))
		for _, descriptor := range glycan.Descriptors {
			descriptors = append(descriptors, models.GlycanDescriptor{
				Descriptor:     descriptor.Descriptor,
				Type:           descriptor.Type,
				Program:        descriptor.Program,
				ProgramVersion: descriptor.ProgramVersion,
			})
		}
		result = append(result, models.Glycan{
			EntityID:    glycan.EntityID,
			CompoundIDs: append([]string{}, glycan.CompoundIDs...),
			Descriptors: descriptors,
		})
	}
	return result
}
