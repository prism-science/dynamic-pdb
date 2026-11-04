package httpapi

import (
	"slices"
	"strings"

	"github.com/google/uuid"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

// A create request's value_sources name the uploaded file each value was read from. The
// deposition forms fill values from the header and coordinates of a structure file the
// depositor uploads; with value_sources those values are recorded with source `file` and the
// file's SHA-256 instead of `depositor`.
//
// A model's values may name only the model's own coordinate file (the primary artifact).
// Ticket 3 forbids assigning an archive model's scores to a submitted model, and an archive
// mmCIF reaches a deposit as an input of one of its runs, never as its primary artifact. An
// entry has no primary artifact, so its values may name any structure file among its own.

// valueSourceRule says which fields a request's value_sources may name and which artifact of
// the request a value may be read from.
type valueSourceRule struct {
	fields []domainmodels.FieldPointer
	// artifact returns the request artifact named by id, or why a value cannot be read from it.
	artifact func(id uuid.UUID) (CreateArtifactRequest, string)
}

// fileValueSources checks valueSources and returns the file sources of its valid entries,
// keyed like field_sources. sources are the request's sources without value_sources: a
// pointer in them is a field the request sets. Invalid entries get an error and no source, so
// in log_only mode they are logged and the value keeps its source.
func fileValueSources(
	at validation.Pointer,
	valueSources *ValueSources,
	sources domainmodels.FieldSources,
	rule valueSourceRule,
) (domainmodels.FieldSources, []validation.FieldError) {
	if valueSources == nil {
		return nil, nil
	}
	at = at.Field("value_sources")
	fileSources := domainmodels.FieldSources{}
	var fieldErrors []validation.FieldError
	// Sorted, so the errors come in the same order every time.
	for _, key := range sortedKeys(*valueSources) {
		pointer := domainmodels.FieldPointer(key)
		var artifact CreateArtifactRequest
		var problem string
		switch {
		case !slices.Contains(rule.fields, pointer):
			problem = "is not a field whose source is recorded"
		case !hasSource(sources, pointer):
			problem = "names a field this request does not set"
		default:
			artifact, problem = rule.artifact((*valueSources)[key].ArtifactId)
		}
		if problem != "" {
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: at.Field(key).Field("artifact_id"),
				Code:    validation.CodeInvalidValue,
				Detail:  problem,
			})
			continue
		}
		artifactID := artifact.Id
		fileSources[pointer] = domainmodels.FieldSource{
			Source:     domainmodels.FieldSourceFile,
			ArtifactID: &artifactID,
			SHA256:     new(strings.TrimSpace(*artifact.Sha256)),
		}
	}
	return fileSources, fieldErrors
}

// modelValueSourceRule: any field whose source a model records, read from the request's
// primary artifact only.
func modelValueSourceRule(data CreateModelData) valueSourceRule {
	return valueSourceRule{
		fields: domainmodels.ModelSourceFields(),
		artifact: func(id uuid.UUID) (CreateArtifactRequest, string) {
			if data.PrimaryArtifactId == nil || id != *data.PrimaryArtifactId {
				return CreateArtifactRequest{}, "must be the request's primary artifact: a model's values are read only from its own coordinate file"
			}
			return structureArtifact(data.Artifacts, id, "the primary artifact")
		},
	}
}

// entryValueSourceRule: any field whose source an entry records, read from one of the
// request's entry artifacts.
func entryValueSourceRule(data CreateEntryData) valueSourceRule {
	return valueSourceRule{
		fields: domainmodels.EntryArchiveFields,
		artifact: func(id uuid.UUID) (CreateArtifactRequest, string) {
			return structureArtifact(data.Artifacts, id, "the artifact")
		},
	}
}

// structureArtifact returns the artifact of the request with id, or why no value can be read
// from it: it is not among the request's artifacts, it is not a coordinate file, or it has no
// SHA-256 to name it by. name says which artifact the problem is about.
func structureArtifact(artifacts *[]CreateArtifactRequest, id uuid.UUID, name string) (CreateArtifactRequest, string) {
	if artifacts == nil {
		return CreateArtifactRequest{}, name + " is not among this request's artifacts"
	}
	index := slices.IndexFunc(*artifacts, func(artifact CreateArtifactRequest) bool {
		return artifact.Id == id
	})
	if index < 0 {
		return CreateArtifactRequest{}, name + " is not among this request's artifacts"
	}
	artifact := (*artifacts)[index]
	if artifact.Format == nil || !isStructureFormat(*artifact.Format) {
		return CreateArtifactRequest{}, name + " is not a PDB or mmCIF coordinate file"
	}
	if artifact.Sha256 == nil || strings.TrimSpace(*artifact.Sha256) == "" {
		return CreateArtifactRequest{}, name + " has no sha256"
	}
	return artifact, ""
}

func isStructureFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "pdb", "cif", "mmcif":
		return true
	default:
		return false
	}
}

func hasSource(sources domainmodels.FieldSources, pointer domainmodels.FieldPointer) bool {
	_, ok := sources[pointer]
	return ok
}

// withFileSources returns sources with each file source in place of the field's depositor
// source. A field the server recorded as the archive's keeps that source: the archive sync
// then keeps the value current.
func withFileSources(sources domainmodels.FieldSources, fileSources domainmodels.FieldSources) domainmodels.FieldSources {
	if len(fileSources) == 0 {
		return sources
	}
	merged := make(domainmodels.FieldSources, len(sources))
	for pointer, source := range sources {
		merged[pointer] = source
	}
	for pointer, source := range fileSources {
		if merged[pointer].Source == domainmodels.FieldSourceDepositor {
			merged[pointer] = source
		}
	}
	return merged
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// newModelRevisionSources returns the field sources of a model's first revision: the fields
// the request sets are the depositor's, except those value_sources reads from the model's
// file. Errors are value_sources entries that cannot be recorded.
func newModelRevisionSources(
	at validation.Pointer,
	data CreateModelData,
	metadata domainmodels.ModelMetadata,
) (domainmodels.FieldSources, []validation.FieldError) {
	sources := domainmodels.ModelRevisionFieldSources(
		nil, nil, domainmodels.ModelRevision{Metadata: metadata}, metricsFromRequests(data.Metrics),
	)
	fileSources, fieldErrors := fileValueSources(at, data.ValueSources, sources, modelValueSourceRule(data))
	return withFileSources(sources, fileSources), fieldErrors
}

// newEntryRevisionSources applies an entry create request's value_sources to the sources of
// its first revision. sources already hold the depositor's and the archive's sources.
func newEntryRevisionSources(
	at validation.Pointer,
	data CreateEntryData,
	sources domainmodels.FieldSources,
) (domainmodels.FieldSources, []validation.FieldError) {
	fileSources, fieldErrors := fileValueSources(at, data.ValueSources, sources, entryValueSourceRule(data))
	return withFileSources(sources, fileSources), fieldErrors
}
