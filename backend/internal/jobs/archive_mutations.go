package jobs

import (
	"context"
	"slices"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// archiveMutations is what the entry sync read from the archive's mmCIF (researcher answer Q3):
// engineered mutations in UniProt numbering, per polymer entity.
type archiveMutations struct {
	sets []cif.MutationSet
	// complete is false when the file could not be parsed or was not read to its coordinates;
	// then sets is empty and the entities keep their stored mutations.
	complete bool
}

// readArchiveMutations reads the mutation sets from the head of the entry's RCSB mmCIF. A failed
// download is an error, so the job retries; a file that cannot be read completely leaves the
// mutations as they are.
func (s *entrySyncStrategy) readArchiveMutations(ctx context.Context, pdbID string) (archiveMutations, error) {
	file, err := readArchiveFile(ctx, s.rcsbClient, s.logger, pdbID)
	if err != nil {
		return archiveMutations{}, err
	}
	return file.mutations, nil
}

// applyArchiveMutations returns desired with each polymer entity's mutations taken from the
// archive's mmCIF. An entity the file has no mutation set for keeps the mutations the RCSB JSON
// gave it. When the file could not be read completely, entities keep their stored mutations.
func applyArchiveMutations(
	desired []models.PolymerEntity,
	stored []models.PolymerEntity,
	mutations archiveMutations,
) []models.PolymerEntity {
	result := append([]models.PolymerEntity(nil), desired...)
	for index := range result {
		metadata := &result[index].Metadata
		label := stringValue(metadata.LabelEntityID)
		if !mutations.complete {
			if storedEntity, ok := polymerEntityWithLabel(stored, label); ok {
				metadata.Mutations = storedEntity.Metadata.Mutations
				metadata.MutationsRaw = storedEntity.Metadata.MutationsRaw
				metadata.MutationsStatus = storedEntity.Metadata.MutationsStatus
			}
			continue
		}
		sets := mutationSetsOfEntity(mutations.sets, label)
		if len(sets) == 0 {
			continue
		}
		metadata.Mutations, metadata.MutationsRaw, metadata.MutationsStatus = mutationsFromSets(sets)
	}
	return result
}

// mutationsFromSets merges an entity's mutation sets, one per UniProt accession. A list that
// mixes the numbering of several accessions needs review, since the accession is not stored.
func mutationsFromSets(sets []cif.MutationSet) ([]string, *string, *models.MutationsStatus) {
	var mutations []string
	var original string
	var accessions []string
	needsReview := false
	for _, set := range sets {
		mutations = append(mutations, set.Mutations...)
		if original == "" {
			original = set.Original
		}
		if set.Accession != "" && !slices.Contains(accessions, set.Accession) {
			accessions = append(accessions, set.Accession)
		}
		needsReview = needsReview || set.NeedsReview
	}
	status := models.MutationsStatusNone
	switch {
	case needsReview || len(accessions) > 1:
		status = models.MutationsStatusNeedsReview
	case len(mutations) > 0:
		status = models.MutationsStatusParsed
	}
	return mutations, optionalString(original), &status
}

func mutationSetsOfEntity(sets []cif.MutationSet, labelEntityID string) []cif.MutationSet {
	var result []cif.MutationSet
	for _, set := range sets {
		if set.EntityID != "" && set.EntityID == labelEntityID {
			result = append(result, set)
		}
	}
	return result
}

func polymerEntityWithLabel(entities []models.PolymerEntity, labelEntityID string) (models.PolymerEntity, bool) {
	for _, entity := range entities {
		if stringValue(entity.Metadata.LabelEntityID) == labelEntityID {
			return entity, true
		}
	}
	return models.PolymerEntity{}, false
}
