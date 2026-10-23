package models

import (
	"fmt"
	"strconv"
	"strings"
)

// The lists below are the controlled vocabularies of Dynamic PDB Schema v0.2.0
// (docs/schema/dynamic-pdb-schema-v0.2.0.md), in the schema's order. Each Validate
// method accepts exactly the values in its list.

var ModelPurposes = []ModelPurpose{ModelPurposeModelBuilding, ModelPurposeRefinement, ModelPurposeReference}

func (purpose ModelPurpose) Validate() error {
	switch purpose {
	case ModelPurposeModelBuilding, ModelPurposeRefinement, ModelPurposeReference:
		return nil
	default:
		return unknownValueError("model purpose", purpose, ModelPurposes)
	}
}

var StructureModelTypes = []StructureModelType{
	StructureModelTypeSingleConformer,
	StructureModelTypeMulticonformer,
	StructureModelTypeEnsemble,
	StructureModelTypeDeposited,
}

func (modelType StructureModelType) Validate() error {
	switch modelType {
	case StructureModelTypeSingleConformer,
		StructureModelTypeMulticonformer,
		StructureModelTypeEnsemble,
		StructureModelTypeDeposited:
		return nil
	default:
		return unknownValueError("model type", modelType, StructureModelTypes)
	}
}

var MetricKeys = []MetricKey{
	MetricKeyRWork,
	MetricKeyRFree,
	MetricKeyCC,
	MetricKeyRSCC,
	MetricKeyClashscore,
	MetricKeyRamachandranOutliers,
	MetricKeySideChainOutliers,
	MetricKeyMolProbityScore,
	MetricKeyWilsonB,
	MetricKeyRMSF,
}

func (key MetricKey) Validate() error {
	switch key {
	case MetricKeyRWork,
		MetricKeyRFree,
		MetricKeyCC,
		MetricKeyRSCC,
		MetricKeyClashscore,
		MetricKeyRamachandranOutliers,
		MetricKeySideChainOutliers,
		MetricKeyMolProbityScore,
		MetricKeyWilsonB,
		MetricKeyRMSF:
		return nil
	default:
		return unknownValueError("metric key", key, MetricKeys)
	}
}

var ArtifactLevels = []ArtifactLevel{ArtifactLevelL0, ArtifactLevelL1, ArtifactLevelL2, ArtifactLevelL3}

func (level ArtifactLevel) Validate() error {
	switch level {
	case ArtifactLevelL0, ArtifactLevelL1, ArtifactLevelL2, ArtifactLevelL3:
		return nil
	default:
		return unknownValueError("artifact level", level, ArtifactLevels)
	}
}

var ArtifactTypes = []ArtifactType{ArtifactTypeModel, ArtifactTypeStructureFactors, ArtifactTypeFASTA, ArtifactTypeOther}

func (artifactType ArtifactType) Validate() error {
	switch artifactType {
	case ArtifactTypeModel, ArtifactTypeStructureFactors, ArtifactTypeFASTA, ArtifactTypeOther:
		return nil
	default:
		return unknownValueError("artifact type", artifactType, ArtifactTypes)
	}
}

var RunArtifactDirections = []RunArtifactDirection{RunArtifactDirectionInput, RunArtifactDirectionOutput}

func (direction RunArtifactDirection) Validate() error {
	switch direction {
	case RunArtifactDirectionInput, RunArtifactDirectionOutput:
		return nil
	default:
		return unknownValueError("run artifact direction", direction, RunArtifactDirections)
	}
}

var UniProtReferenceSources = []UniProtReferenceSource{
	UniProtReferenceSourceSIFTS,
	UniProtReferenceSourceStructRef,
	UniProtReferenceSourceAlignment,
}

func (source UniProtReferenceSource) Validate() error {
	switch source {
	case UniProtReferenceSourceSIFTS, UniProtReferenceSourceStructRef, UniProtReferenceSourceAlignment:
		return nil
	default:
		return unknownValueError("UniProt source", source, UniProtReferenceSources)
	}
}

// RequiresUniProtRelease reports whether a mapping from this source must name the
// UniProt release it was made against.
func (source UniProtReferenceSource) RequiresUniProtRelease() (bool, error) {
	switch source {
	case UniProtReferenceSourceSIFTS, UniProtReferenceSourceAlignment:
		return true, nil
	case UniProtReferenceSourceStructRef:
		return false, nil
	default:
		return false, unknownValueError("UniProt source", source, UniProtReferenceSources)
	}
}

var MutationsStatuses = []MutationsStatus{MutationsStatusParsed, MutationsStatusNeedsReview, MutationsStatusNone}

func (status MutationsStatus) Validate() error {
	switch status {
	case MutationsStatusParsed, MutationsStatusNeedsReview, MutationsStatusNone:
		return nil
	default:
		return unknownValueError("mutations status", status, MutationsStatuses)
	}
}

var ModelSources = []ModelSource{ModelSourcePDB, ModelSourceEXT}

func (source ModelSource) Validate() error {
	switch source {
	case ModelSourcePDB, ModelSourceEXT:
		return nil
	default:
		return unknownValueError("model external reference", source, ModelSources)
	}
}

var EntrySources = []EntrySource{EntrySourcePDB}

func (source EntrySource) Validate() error {
	switch source {
	case EntrySourcePDB:
		return nil
	default:
		return unknownValueError("entry external reference", source, EntrySources)
	}
}

func unknownValueError[T ~string](kind string, value T, allowed []T) error {
	quoted := make([]string, 0, len(allowed))
	for _, allowedValue := range allowed {
		quoted = append(quoted, strconv.Quote(string(allowedValue)))
	}
	return fmt.Errorf("%s %q is not one of %s", kind, value, strings.Join(quoted, ", "))
}
