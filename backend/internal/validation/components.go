package validation

import (
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

const reservedMetadataKeyPrefix = "__"

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Artifact struct {
	ID        uuid.UUID
	Name      string
	Level     models.ArtifactLevel
	Type      models.ArtifactType
	Format    *string
	SizeBytes *int64
	SHA256    *string
	Metadata  map[string]any
}

type Metric struct {
	ID    uuid.UUID
	Key   models.MetricKey
	Value *float64
}

type Run struct {
	ID        uuid.UUID
	Name      string
	Artifacts []RunArtifact
}

type RunArtifact struct {
	ArtifactID uuid.UUID
	Direction  models.RunArtifactDirection
}

// ValidateArtifact checks one artifact. The file-format vocabulary is open; only its shape is checked.
func ValidateArtifact(pointer string, artifact Artifact) []FieldError {
	var fieldErrors []FieldError
	if artifact.ID == uuid.Nil {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "id"), CodeRequired, "artifact id is required"))
	}
	if strings.TrimSpace(artifact.Name) == "" {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "name"), CodeRequired, "artifact name is required"))
	}
	if artifact.Level == "" {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "level"), CodeRequired, "artifact level is required"))
	} else if err := artifact.Level.Validate(); err != nil {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "level"), CodeNotInSchema, "%v", err))
	}
	if artifact.Type == "" {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "type"), CodeRequired, "artifact type is required"))
	} else if err := artifact.Type.Validate(); err != nil {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "type"), CodeNotInSchema, "%v", err))
	}
	if artifact.Format != nil && *artifact.Format != strings.ToLower(*artifact.Format) {
		fieldErrors = append(fieldErrors, fieldError(
			pointerJoin(pointer, "format"), CodeInvalidFormat, "artifact format %q must be lowercase", *artifact.Format,
		))
	}
	if artifact.SizeBytes != nil && *artifact.SizeBytes < 0 {
		fieldErrors = append(fieldErrors, fieldError(
			pointerJoin(pointer, "size_bytes"), CodeOutOfRange, "artifact size_bytes must not be negative",
		))
	}
	if artifact.SHA256 != nil && !sha256Pattern.MatchString(*artifact.SHA256) {
		fieldErrors = append(fieldErrors, fieldError(
			pointerJoin(pointer, "sha256"), CodeInvalidFormat, "artifact sha256 must be 64 lowercase hex characters",
		))
	}
	for _, key := range sortedKeys(artifact.Metadata) {
		if strings.HasPrefix(key, reservedMetadataKeyPrefix) {
			fieldErrors = append(fieldErrors, fieldError(
				pointerJoin(pointer, "metadata", key), CodeReservedKey,
				"artifact metadata keys starting with %q are reserved", reservedMetadataKeyPrefix,
			))
		}
	}
	return fieldErrors
}

// ValidateMetric checks one metric. Duplicate keys are checked by the model validators.
func ValidateMetric(pointer string, metric Metric) []FieldError {
	var fieldErrors []FieldError
	if metric.ID == uuid.Nil {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "id"), CodeRequired, "metric id is required"))
	}
	if metric.Key == "" {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "key"), CodeRequired, "metric key is required"))
	} else if err := metric.Key.Validate(); err != nil {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "key"), CodeNotInSchema, "%v", err))
	}
	if metric.Value == nil {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "value"), CodeRequired, "metric value is required"))
	}
	return fieldErrors
}

// ValidateRun checks one run. When modelArtifactIDs is non-nil every run artifact must be one of them;
// pass nil when the model revision's artifact set is not known from the request.
func ValidateRun(pointer string, run Run, modelArtifactIDs map[uuid.UUID]struct{}) []FieldError {
	var fieldErrors []FieldError
	if run.ID == uuid.Nil {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "id"), CodeRequired, "run id is required"))
	}
	if strings.TrimSpace(run.Name) == "" {
		fieldErrors = append(fieldErrors, fieldError(pointerJoin(pointer, "name"), CodeRequired, "run name is required"))
	}
	for index, runArtifact := range run.Artifacts {
		artifactPointer := pointerJoin(pointer, "artifacts", index)
		if runArtifact.ArtifactID == uuid.Nil {
			fieldErrors = append(fieldErrors, fieldError(
				pointerJoin(artifactPointer, "artifact_id"), CodeRequired, "run artifact artifact_id is required",
			))
		} else if modelArtifactIDs != nil {
			if _, ok := modelArtifactIDs[runArtifact.ArtifactID]; !ok {
				fieldErrors = append(fieldErrors, fieldError(
					pointerJoin(artifactPointer, "artifact_id"), CodeUnknownReference,
					"run artifact %s is not an artifact of this model revision", runArtifact.ArtifactID,
				))
			}
		}
		if runArtifact.Direction == "" {
			fieldErrors = append(fieldErrors, fieldError(
				pointerJoin(artifactPointer, "direction"), CodeRequired, "run artifact direction is required",
			))
		} else if err := runArtifact.Direction.Validate(); err != nil {
			fieldErrors = append(fieldErrors, fieldError(pointerJoin(artifactPointer, "direction"), CodeNotInSchema, "%v", err))
		}
	}
	return fieldErrors
}

// ValidateUniProtMapping checks one polymer entity UniProt mapping.
func ValidateUniProtMapping(pointer string, mapping models.PolymerEntityUniProtReference) []FieldError {
	var fieldErrors []FieldError
	if strings.TrimSpace(mapping.Accession) == "" {
		fieldErrors = append(fieldErrors, fieldError(
			pointerJoin(pointer, "accession"), CodeRequired, "UniProt mapping accession is required",
		))
	}
	if mapping.Source == "" {
		return append(fieldErrors, fieldError(pointerJoin(pointer, "source"), CodeRequired, "UniProt mapping source is required"))
	}
	requiresRelease, err := mapping.Source.RequiresRelease()
	if err != nil {
		return append(fieldErrors, fieldError(pointerJoin(pointer, "source"), CodeNotInSchema, "%v", err))
	}
	if requiresRelease && (mapping.UniProtRelease == nil || strings.TrimSpace(*mapping.UniProtRelease) == "") {
		fieldErrors = append(fieldErrors, fieldError(
			pointerJoin(pointer, "unp_release"), CodeRequired,
			"UniProt mapping from source %s requires unp_release", mapping.Source,
		))
	}
	return fieldErrors
}

func validateArtifacts(pointer string, artifacts []Artifact) []FieldError {
	var fieldErrors []FieldError
	for index, artifact := range artifacts {
		fieldErrors = append(fieldErrors, ValidateArtifact(pointerJoin(pointer, index), artifact)...)
	}
	return fieldErrors
}

func validateMetrics(pointer string, metrics []Metric) []FieldError {
	var fieldErrors []FieldError
	seenKeys := make(map[models.MetricKey]int, len(metrics))
	for index, metric := range metrics {
		metricPointer := pointerJoin(pointer, index)
		fieldErrors = append(fieldErrors, ValidateMetric(metricPointer, metric)...)
		if metric.Key == "" {
			continue
		}
		if firstIndex, seen := seenKeys[metric.Key]; seen {
			fieldErrors = append(fieldErrors, fieldError(
				pointerJoin(metricPointer, "key"), CodeDuplicate,
				"metric key %s is already set by metric %d of this model revision", metric.Key, firstIndex,
			))
			continue
		}
		seenKeys[metric.Key] = index
	}
	return fieldErrors
}

func validateRuns(pointer string, runs []Run, modelArtifactIDs map[uuid.UUID]struct{}) []FieldError {
	var fieldErrors []FieldError
	for index, run := range runs {
		fieldErrors = append(fieldErrors, ValidateRun(pointerJoin(pointer, index), run, modelArtifactIDs)...)
	}
	return fieldErrors
}

func artifactIDs(artifacts []Artifact) map[uuid.UUID]struct{} {
	ids := make(map[uuid.UUID]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		ids[artifact.ID] = struct{}{}
	}
	return ids
}

func sortedKeys[K ~string, V any](values map[K]V) []K {
	keys := make([]K, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
