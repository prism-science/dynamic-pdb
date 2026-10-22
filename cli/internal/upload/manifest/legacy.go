package manifest

import "dynamic-pdb/lib/cif"

const (
	modelFieldLigands        = "ligands"
	modelFieldCofactors      = "cofactors"
	modelFieldCompoundReview = "extensions." + cif.FieldCompoundReview
)

// WithCoordinateCofactors returns model metadata that also reads cofactors and
// the compound review flags from the coordinates, when it reads ligands from
// them but has no cofactors entry. Manifests written before cofactors had their
// own field list HEM and metal ions under ligands; the extractor now moves them
// to cofactors, so without this they would be dropped from such a manifest. The
// second result is true when entries were added. The input is not modified.
func (metadata ModelMetadata) WithCoordinateCofactors() (ModelMetadata, bool) {
	if _, hasCofactors := metadata[modelFieldCofactors]; hasCofactors {
		return metadata, false
	}
	var cofactors []FieldExtraction
	var review []FieldExtraction
	for _, ligands := range metadata[modelFieldLigands] {
		if ligands.Source.Artifact != artifactCoordinatesSourceValue {
			continue
		}
		cofactors = append(cofactors, withStructureField(ligands, modelFieldCofactors))
		review = append(review, withStructureField(ligands, cif.FieldCompoundReview))
	}
	if len(cofactors) == 0 {
		return metadata, false
	}
	upgraded := make(ModelMetadata, len(metadata)+2)
	for key, fields := range metadata {
		upgraded[key] = fields
	}
	upgraded[modelFieldCofactors] = cofactors
	if _, hasReview := upgraded[modelFieldCompoundReview]; !hasReview {
		upgraded[modelFieldCompoundReview] = review
	}
	return upgraded, true
}

// withStructureField copies a PDB or mmCIF extraction onto another field of the
// same file. Rules of other formats are not copied.
func withStructureField(extraction FieldExtraction, field string) FieldExtraction {
	copied := FieldExtraction{Source: extraction.Source}
	if extraction.Extract.PDB != nil {
		copied.Extract.PDB = &ExtractRule{Field: field}
	}
	if extraction.Extract.MMCIF != nil {
		copied.Extract.MMCIF = &ExtractRule{Field: field}
	}
	return copied
}
