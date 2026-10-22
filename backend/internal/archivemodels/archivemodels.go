// Package archivemodels converts values mapped from RCSB by lib/archive into backend
// models, so the sync jobs and the lookup endpoint build identical values.
package archivemodels

import (
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
)

func EntryMetadata(mapping archive.EntryMapping) models.EntryMetadata {
	return models.EntryMetadata{
		Details:         mapping.Details,
		Resolution:      mapping.Resolution,
		Method:          StructureMethod(mapping.Method),
		SpaceGroup:      mapping.SpaceGroup,
		Crystallography: Crystallography(mapping.Crystallography),
	}
}

func StructureMethod(method *string) *models.StructureMethod {
	if method == nil {
		return nil
	}
	structureMethod := models.StructureMethod(*method)
	return &structureMethod
}

func Crystallography(crystallography *archive.Crystallography) *models.EntryCrystallography {
	if crystallography == nil {
		return nil
	}
	crystals := make([]models.EntryCrystal, 0, len(crystallography.Crystals))
	for _, sourceCrystal := range crystallography.Crystals {
		crystal := models.EntryCrystal{ID: sourceCrystal.ID}
		if sourceCrystal.Growth != nil {
			crystal.Growth = &models.EntryCrystalGrowth{
				PH:                sourceCrystal.Growth.PH,
				TemperatureKelvin: sourceCrystal.Growth.TemperatureKelvin,
			}
		}
		for _, sourceDiffraction := range sourceCrystal.Diffractions {
			crystal.Diffractions = append(crystal.Diffractions, models.EntryDiffraction{
				ID:                sourceDiffraction.ID,
				TemperatureKelvin: sourceDiffraction.TemperatureKelvin,
			})
		}
		crystals = append(crystals, crystal)
	}
	return &models.EntryCrystallography{Crystals: crystals}
}

func ModelMetadata(mapping archive.ModelMapping) models.ModelMetadata {
	return models.ModelMetadata{
		Details:             mapping.Details,
		Authors:             mapping.Authors,
		Affiliation:         mapping.Affiliation,
		AtomCount:           mapping.AtomCount,
		ModeledResidues:     mapping.ModeledResidues,
		UniqueProteinChains: mapping.UniqueProteinChains,
		UnmodeledFraction:   mapping.UnmodeledFraction,
		Ligands:             mapping.Ligands,
	}
}

func Metrics(mapping archive.ModelMapping) []models.Metric {
	metrics := make([]models.Metric, 0, len(mapping.Metrics))
	for _, metric := range mapping.Metrics {
		metrics = append(metrics, models.Metric{Key: models.MetricKey(metric.Key), Value: metric.Value})
	}
	return metrics
}

// PolymerEntityMetadata converts a polymer entity. Alignment-only UniProt mappings
// are left out unless includeAlignments is set, because the sync does not store them.
func PolymerEntityMetadata(mapping archive.PolymerEntityMapping, includeAlignments bool) models.PolymerEntityMetadata {
	var sourceOrganisms []models.PolymerEntityOrganism
	for _, sourceOrganism := range mapping.SourceOrganisms {
		sourceOrganisms = append(sourceOrganisms, models.PolymerEntityOrganism{
			ScientificName: sourceOrganism.ScientificName,
			NCBITaxonomyID: sourceOrganism.NCBITaxonomyID,
		})
	}
	uniProtMappings := uniProtReferences(nil, mapping.UniProtMappings)
	if includeAlignments {
		uniProtMappings = uniProtReferences(uniProtMappings, mapping.AlignmentMappings)
	}
	return models.PolymerEntityMetadata{
		LabelEntityID:   mapping.LabelEntityID,
		Description:     mapping.Description,
		SourceOrganisms: sourceOrganisms,
		Construct:       mapping.Construct,
		Mutations:       mapping.Mutations,
		UniProtMappings: uniProtMappings,
	}
}

func uniProtReferences(
	references []models.PolymerEntityUniProtReference,
	mappings []archive.UniProtMapping,
) []models.PolymerEntityUniProtReference {
	for _, mapping := range mappings {
		references = append(references, models.PolymerEntityUniProtReference{
			Accession:      mapping.Accession,
			Source:         models.UniProtReferenceSource(mapping.Source),
			UniProtRelease: mapping.UniProtRelease,
		})
	}
	return references
}

func ResidueData(residues []archive.ResidueData) []models.ResidueData {
	converted := make([]models.ResidueData, 0, len(residues))
	for _, residue := range residues {
		converted = append(converted, models.ResidueData{
			LabelAsymID:     residue.LabelAsymID,
			LabelSeqID:      residue.LabelSeqID,
			LabelCompID:     residue.LabelCompID,
			AuthAsymID:      residue.AuthAsymID,
			AuthSeqID:       residue.AuthSeqID,
			PDBxPDBInsCode:  residue.PDBxPDBInsCode,
			UniProtPosition: residue.UniProtPosition,
			RSCC:            residue.RSCC,
			BIso:            residue.BIso,
			Occupancy:       residue.Occupancy,
		})
	}
	return converted
}
