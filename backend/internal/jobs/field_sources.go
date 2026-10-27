package jobs

import (
	"slices"
	"time"

	"dynamic-pdb/backend/internal/models"
)

// keepDepositorEntryFields puts the current values of the fields a depositor typed into the
// revision built from the archive, so the sync neither reports them as changed nor overwrites
// them. Every other field keeps the archive's value.
func keepDepositorEntryFields(current models.EntryRevision, desired *models.EntryRevision) {
	sources := current.FieldSources
	if sources.IsDepositor(models.EntryFieldTitle) {
		desired.Title = current.Title
	}
	if sources.IsDepositor(models.EntryFieldDetails) {
		desired.Metadata.Details = current.Metadata.Details
	}
	if sources.IsDepositor(models.EntryFieldResolution) {
		desired.Metadata.Resolution = current.Metadata.Resolution
	}
	if sources.IsDepositor(models.EntryFieldMethod) {
		desired.Metadata.Method = current.Metadata.Method
	}
	if sources.IsDepositor(models.EntryFieldSpaceGroup) {
		desired.Metadata.SpaceGroup = current.Metadata.SpaceGroup
	}
	if sources.IsDepositor(models.EntryFieldCrystallography) {
		desired.Metadata.Crystallography = current.Metadata.Crystallography
	}
}

// keepDepositorModelFields is keepDepositorEntryFields for model revisions. Archive metrics are
// kept by leaving their keys out of syncedArchiveMetricKeys.
func keepDepositorModelFields(current models.ModelRevision, desired *models.ModelRevision) {
	sources := current.FieldSources
	if sources.IsDepositor(models.ModelFieldAuthors) {
		desired.Metadata.Authors = current.Metadata.Authors
	}
	if sources.IsDepositor(models.ModelFieldDetails) {
		desired.Metadata.Details = current.Metadata.Details
	}
	if sources.IsDepositor(models.ModelFieldAffiliation) {
		desired.Metadata.Affiliation = current.Metadata.Affiliation
	}
	if sources.IsDepositor(models.ModelFieldAtomCount) {
		desired.Metadata.AtomCount = current.Metadata.AtomCount
	}
	if sources.IsDepositor(models.ModelFieldModeledResidues) {
		desired.Metadata.ModeledResidues = current.Metadata.ModeledResidues
	}
	if sources.IsDepositor(models.ModelFieldUniqueProteinChains) {
		desired.Metadata.UniqueProteinChains = current.Metadata.UniqueProteinChains
	}
	if sources.IsDepositor(models.ModelFieldUnmodeledFraction) {
		desired.Metadata.UnmodeledFraction = current.Metadata.UnmodeledFraction
	}
	if sources.IsDepositor(models.ModelFieldLigands) {
		desired.Metadata.Ligands = current.Metadata.Ligands
	}
	if sources.IsDepositor(models.ModelFieldResidueData) {
		desired.Metadata.ResidueData = current.Metadata.ResidueData
	}
}

// syncedArchiveMetricKeys returns the archive metrics the sync may write: all of them except
// those a depositor typed.
func syncedArchiveMetricKeys(sources models.FieldSources) []models.MetricKey {
	keys := make([]models.MetricKey, 0, len(models.ArchiveMetricKeys))
	for _, key := range models.ArchiveMetricKeys {
		if !sources.IsDepositor(models.ArchiveMetricField(key)) {
			keys = append(keys, key)
		}
	}
	return keys
}

// isDepositedModel reports whether a model revision is the archive's own model. Archive values
// describe that model only, so the sync never copies them onto any other model.
func isDepositedModel(revision models.ModelRevision) bool {
	return revision.Metadata.ModelType != nil &&
		*revision.Metadata.ModelType == models.StructureModelTypeDeposited
}

// archiveEntryFieldSources returns the sources of an entry revision the sync creates: fields a
// depositor typed keep their source, and every other synced field comes from the RCSB entry.
func archiveEntryFieldSources(
	current models.FieldSources,
	pdbID string,
	retrievedAt time.Time,
) models.FieldSources {
	return archiveFieldSources(current, models.EntryArchiveFields, pdbID, retrievedAt)
}

// archiveModelFieldSources is archiveEntryFieldSources for model revisions, where the synced
// archive metrics also come from the RCSB entry.
func archiveModelFieldSources(
	current models.FieldSources,
	pdbID string,
	retrievedAt time.Time,
	metricKeys []models.MetricKey,
) models.FieldSources {
	fields := slices.Clone(models.ModelArchiveFields)
	for _, key := range metricKeys {
		fields = append(fields, models.ArchiveMetricField(key))
	}
	sources := archiveFieldSources(current, fields, pdbID, retrievedAt)
	for _, key := range models.ArchiveMetricKeys {
		pointer := models.ArchiveMetricField(key)
		if current.IsDepositor(pointer) {
			sources[pointer] = current[pointer]
		}
	}
	return sources
}

func archiveFieldSources(
	current models.FieldSources,
	fields []models.FieldPointer,
	pdbID string,
	retrievedAt time.Time,
) models.FieldSources {
	sources := make(models.FieldSources, len(fields))
	for _, pointer := range fields {
		if current.IsDepositor(pointer) {
			sources[pointer] = current[pointer]
			continue
		}
		sources[pointer] = models.FieldSource{
			Source:      models.FieldSourceRCSB,
			Accession:   new(pdbID),
			RetrievedAt: new(retrievedAt),
		}
	}
	return sources
}
