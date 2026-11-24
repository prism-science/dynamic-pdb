package jobs

import (
	"fmt"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// fileConformers turns a read's conformer counts into what is stored for the file. Ensembles get
// no counts: a file with more than one model frame, or one a model uses as an Ensemble
// (ensembleType, as the save transaction sees it). The model-frame rule wins when both hold.
func fileConformers(counts cif.ConformerCounts, ensembleType bool) (models.FileConformers, error) {
	conformers := models.FileConformers{
		Version:            cif.ResidueCountVersion,
		ReadAsEnsembleType: ensembleType,
		Counts:             []models.ResidueConformerCount{},
	}
	switch {
	case counts.MultipleModelFrames:
		conformers.Scope = models.ConformerScopeEnsembleModelFrames
		return conformers, nil
	case ensembleType:
		conformers.Scope = models.ConformerScopeEnsembleModelType
		return conformers, nil
	default:
		conformers.Scope = models.ConformerScopeCounted
	}
	residues, multiConformer := counts.Residues, counts.MultiConformerResidues
	conformers.Residues = &residues
	conformers.MultiConformerResidues = &multiConformer
	if counts.Key != "" {
		key, err := residueKey(counts.Key)
		if err != nil {
			return models.FileConformers{}, err
		}
		conformers.Key = &key
	}
	for _, row := range counts.Rows {
		conformers.Counts = append(conformers.Counts, models.ResidueConformerCount{
			ResidueType:    row.ResidueType,
			ConformerCount: row.ConformerCount,
			Residues:       row.Residues,
		})
	}
	return conformers, nil
}

func residueKey(key cif.ResidueKey) (models.ResidueKey, error) {
	switch key {
	case cif.ResidueKeyLabel:
		return models.ResidueKeyLabel, nil
	case cif.ResidueKeyAuthor:
		return models.ResidueKeyAuthor, nil
	case cif.ResidueKeyMixed:
		return models.ResidueKeyMixed, nil
	default:
		return "", fmt.Errorf("unknown residue key %q", key)
	}
}
