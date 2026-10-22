package validation

import (
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

const (
	createEntryPointer     = "/entry"
	modelOperationsPointer = "/model_operations"
	modelPointer           = "/model"
)

type Entry struct {
	Title      string
	Method     *string
	Resolution *float64
	Artifacts  []Artifact
}

type CreateEntry struct {
	Entry  Entry
	Models []Model
}

type Model struct {
	Title     string
	Metadata  models.ModelMetadata
	Artifacts []Artifact
	Runs      []Run
	Metrics   []Metric
}

type EntryChange struct {
	Title      Field[string]
	Method     Field[string]
	Resolution Field[float64]
}

// ModelChange holds the fields of a model revision change. Artifacts, runs and metrics replace the
// whole set when present.
type ModelChange struct {
	Title     Field[string]
	Metadata  Field[models.ModelMetadata]
	Artifacts Field[[]Artifact]
	Runs      Field[[]Run]
	Metrics   Field[[]Metric]
}

// ValidateCreateEntry checks a new entry and the models created with it.
func ValidateCreateEntry(request CreateEntry) []FieldError {
	entry := request.Entry
	var fieldErrors []FieldError
	if strings.TrimSpace(entry.Title) == "" {
		fieldErrors = append(fieldErrors, fieldError(
			pointerJoin(createEntryPointer, "title"), CodeRequired, "entry title is required",
		))
	}
	fieldErrors = append(fieldErrors, validateMethod(pointerJoin(createEntryPointer, "method"), entry.Method)...)
	fieldErrors = append(fieldErrors, validateResolution(pointerJoin(createEntryPointer, "resolution"), entry.Resolution)...)
	fieldErrors = append(fieldErrors, validateArtifacts(pointerJoin(createEntryPointer, "artifacts"), entry.Artifacts)...)
	for index, model := range request.Models {
		fieldErrors = append(fieldErrors, validateModel(pointerJoin(modelOperationsPointer, index, "data"), model)...)
	}
	return fieldErrors
}

// ValidateCreateModel checks a new model.
func ValidateCreateModel(model Model) []FieldError {
	return validateModel(modelPointer, model)
}

// ValidateEntryRevisionChange checks only the entry fields present in the change.
func ValidateEntryRevisionChange(change EntryChange) []FieldError {
	var fieldErrors []FieldError
	if change.Title.Present && (change.Title.Value == nil || strings.TrimSpace(*change.Title.Value) == "") {
		fieldErrors = append(fieldErrors, fieldError(
			pointerJoin(createEntryPointer, "title"), CodeRequired, "entry title is required",
		))
	}
	if change.Method.Present {
		fieldErrors = append(fieldErrors, validateMethod(pointerJoin(createEntryPointer, "method"), change.Method.Value)...)
	}
	if change.Resolution.Present {
		fieldErrors = append(fieldErrors, validateResolution(
			pointerJoin(createEntryPointer, "resolution"), change.Resolution.Value,
		)...)
	}
	return fieldErrors
}

// ValidateModelRevisionChange checks only the model fields present in the change. Run artifacts are
// checked against the model's artifacts only when the change replaces them.
func ValidateModelRevisionChange(change ModelChange) []FieldError {
	var fieldErrors []FieldError
	if change.Title.Present && (change.Title.Value == nil || strings.TrimSpace(*change.Title.Value) == "") {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(modelPointer, "title"), CodeRequired, "model title is required"))
	}
	if change.Metadata.Present && change.Metadata.Value != nil {
		fieldErrors = append(fieldErrors, validateModelMetadata(pointerJoin(modelPointer, "metadata"), *change.Metadata.Value)...)
	}
	var modelArtifactIDs map[uuid.UUID]struct{}
	if change.Artifacts.Present && change.Artifacts.Value != nil {
		fieldErrors = append(fieldErrors, validateArtifacts(pointerJoin(modelPointer, "artifacts"), *change.Artifacts.Value)...)
		modelArtifactIDs = artifactIDs(*change.Artifacts.Value)
	}
	if change.Runs.Present && change.Runs.Value != nil {
		fieldErrors = append(fieldErrors, validateRuns(pointerJoin(modelPointer, "runs"), *change.Runs.Value, modelArtifactIDs)...)
	}
	if change.Metrics.Present && change.Metrics.Value != nil {
		fieldErrors = append(fieldErrors, validateMetrics(pointerJoin(modelPointer, "metrics"), *change.Metrics.Value)...)
	}
	return fieldErrors
}

func validateModel(pointer string, model Model) []FieldError {
	var fieldErrors []FieldError
	if strings.TrimSpace(model.Title) == "" {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "title"), CodeRequired, "model title is required"))
	}
	fieldErrors = append(fieldErrors, validateModelMetadata(pointerJoin(pointer, "metadata"), model.Metadata)...)
	fieldErrors = append(fieldErrors, validateArtifacts(pointerJoin(pointer, "artifacts"), model.Artifacts)...)
	fieldErrors = append(fieldErrors, validateRuns(pointerJoin(pointer, "runs"), model.Runs, artifactIDs(model.Artifacts))...)
	fieldErrors = append(fieldErrors, validateMetrics(pointerJoin(pointer, "metrics"), model.Metrics)...)
	return fieldErrors
}

func validateModelMetadata(pointer string, metadata models.ModelMetadata) []FieldError {
	var fieldErrors []FieldError
	for _, source := range sortedKeys(metadata.ExternalRefs) {
		if err := source.Validate(); err != nil {
			fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "external_refs", source), CodeNotInSchema, "%v", err))
		}
	}
	if metadata.Purpose != nil {
		if err := metadata.Purpose.Validate(); err != nil {
			fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "purpose"), CodeNotInSchema, "%v", err))
		}
	}
	if metadata.ModelType != nil {
		if err := metadata.ModelType.Validate(); err != nil {
			fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "model_type"), CodeNotInSchema, "%v", err))
		}
	}
	return fieldErrors
}

func validateMethod(pointer string, method *string) []FieldError {
	if method == nil || strings.TrimSpace(*method) == "" {
		return []FieldError{fieldError(pointer, CodeRequired, "entry method is required")}
	}
	return nil
}

func validateResolution(pointer string, resolution *float64) []FieldError {
	if resolution != nil && *resolution <= 0 {
		return []FieldError{fieldError(pointer, CodeOutOfRange, "entry resolution must be positive, got %v", *resolution)}
	}
	return nil
}
