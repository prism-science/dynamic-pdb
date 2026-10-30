package httpapi

import (
	"fmt"

	domainmodels "dynamic-pdb/backend/internal/models"
)

// compoundSourcesFromModel converts an entry revision's decision record for a response. A record
// that was never derived stays null.
func compoundSourcesFromModel(record *domainmodels.CompoundSources) (*CompoundSources, error) {
	if record == nil {
		return nil, nil
	}
	ligands, err := compoundSourceGroupFromModel(record.Ligands)
	if err != nil {
		return nil, fmt.Errorf("ligands: %w", err)
	}
	cofactors, err := compoundSourceGroupFromModel(record.Cofactors)
	if err != nil {
		return nil, fmt.Errorf("cofactors: %w", err)
	}
	excluded, err := compoundSourceGroupFromModel(record.Excluded)
	if err != nil {
		return nil, fmt.Errorf("excluded: %w", err)
	}
	return &CompoundSources{Ligands: ligands, Cofactors: cofactors, Excluded: excluded}, nil
}

func compoundSourceGroupFromModel(group map[string]domainmodels.CompoundSource) (map[string]CompoundSource, error) {
	result := make(map[string]CompoundSource, len(group))
	for id, source := range group {
		rule, err := compoundRuleFromModel(source.Rule)
		if err != nil {
			return nil, fmt.Errorf("compound %s: %w", id, err)
		}
		result[id] = CompoundSource{
			Rule:         rule,
			ListVersion:  nonEmptyStringPtr(source.ListVersion),
			ReviewReason: nonEmptyStringPtr(source.ReviewReason),
		}
	}
	return result, nil
}

func compoundRuleFromModel(rule domainmodels.CompoundRule) (CompoundSourceRule, error) {
	switch rule {
	case domainmodels.CompoundRuleList:
		return CompoundSourceRuleList, nil
	case domainmodels.CompoundRuleMetalc:
		return CompoundSourceRuleMetalc, nil
	case domainmodels.CompoundRuleBranched:
		return CompoundSourceRuleBranched, nil
	case domainmodels.CompoundRuleMacrolide:
		return CompoundSourceRuleMacrolide, nil
	case domainmodels.CompoundRuleCCDType:
		return CompoundSourceRuleCcdType, nil
	case domainmodels.CompoundRuleDepositorFlag:
		return CompoundSourceRuleDepositorFlag, nil
	case domainmodels.CompoundRuleCovalentMap:
		return CompoundSourceRuleCovalentMap, nil
	case domainmodels.CompoundRuleIHMProbe:
		return CompoundSourceRuleIhmProbe, nil
	case domainmodels.CompoundRuleDepositor:
		return CompoundSourceRuleDepositor, nil
	default:
		return "", fmt.Errorf("unknown compound rule: %s", rule)
	}
}

// glycanDescriptorsFromModel converts an entry revision's glycans for a response. Glycans that
// were never read stay null; an empty list means the file has no branched entity.
func glycanDescriptorsFromModel(glycans *[]domainmodels.Glycan) *GlycanDescriptors {
	if glycans == nil {
		return nil
	}
	result := make(GlycanDescriptors, 0, len(*glycans))
	for _, glycan := range *glycans {
		descriptors := make([]GlycanDescriptor, 0, len(glycan.Descriptors))
		for _, descriptor := range glycan.Descriptors {
			descriptors = append(descriptors, GlycanDescriptor{
				Descriptor:     descriptor.Descriptor,
				Type:           descriptor.Type,
				Program:        nonEmptyStringPtr(descriptor.Program),
				ProgramVersion: nonEmptyStringPtr(descriptor.ProgramVersion),
			})
		}
		result = append(result, Glycan{
			EntityId:    glycan.EntityID,
			CompoundIds: append([]string{}, glycan.CompoundIDs...),
			Descriptors: descriptors,
		})
	}
	return &result
}

// nonEmptyStringPtr returns nil for an empty string, so an optional response value is left out.
func nonEmptyStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
