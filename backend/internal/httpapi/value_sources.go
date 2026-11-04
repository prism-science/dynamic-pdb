package httpapi

import (
	"slices"
	"strings"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

// A model's value_sources name the uploaded file each value was read from. The website
// fills a new model's R-factors and authors from the header of its coordinate file; with
// value_sources those values are recorded with source `file` and the file's SHA-256 instead
// of `depositor`.
//
// Only the model's own coordinate file (the primary artifact) may be named. Ticket 3 forbids
// assigning an archive model's scores to a submitted model, and an archive mmCIF reaches a
// deposit as an input of one of its runs, never as its primary artifact.

// modelValueSources checks the value_sources of a model create request and returns the file
// sources of its valid entries, keyed like field_sources. sources are the request's sources
// without value_sources: a pointer in them is a field the request sets. Invalid entries get
// an error and no source, so in log_only mode they are logged and the value stays the
// depositor's.
func modelValueSources(
	at validation.Pointer,
	data CreateModelData,
	sources domainmodels.FieldSources,
) (domainmodels.FieldSources, []validation.FieldError) {
	if data.ValueSources == nil {
		return nil, nil
	}
	at = at.Field("value_sources")
	known := domainmodels.ModelSourceFields()
	primary, primaryProblem := primaryStructureArtifact(data)

	fileSources := domainmodels.FieldSources{}
	var fieldErrors []validation.FieldError
	// Sorted, so the errors come in the same order every time.
	for _, key := range sortedKeys(*data.ValueSources) {
		source := (*data.ValueSources)[key]
		entryAt := at.Field(key)
		pointer := domainmodels.FieldPointer(key)
		var problem string
		switch {
		case !slices.Contains(known, pointer):
			problem = "is not a field whose source is recorded"
		case !hasSource(sources, pointer):
			problem = "names a field this request does not set"
		case data.PrimaryArtifactId == nil || source.ArtifactId != *data.PrimaryArtifactId:
			problem = "must be the request's primary artifact: a model's values are read only from its own coordinate file"
		case primaryProblem != "":
			problem = primaryProblem
		}
		if problem != "" {
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: entryAt.Field("artifact_id"),
				Code:    validation.CodeInvalidValue,
				Detail:  problem,
			})
			continue
		}
		artifactID := primary.Id
		fileSources[pointer] = domainmodels.FieldSource{
			Source:     domainmodels.FieldSourceFile,
			ArtifactID: &artifactID,
			SHA256:     new(strings.TrimSpace(*primary.Sha256)),
		}
	}
	return fileSources, fieldErrors
}

// primaryStructureArtifact returns the request's primary artifact, or why no value can be
// read from it: it is not among the request's artifacts, it is not a coordinate file, or it
// has no SHA-256 to name it by.
func primaryStructureArtifact(data CreateModelData) (CreateArtifactRequest, string) {
	if data.PrimaryArtifactId == nil || data.Artifacts == nil {
		return CreateArtifactRequest{}, "the primary artifact is not among this request's artifacts"
	}
	index := slices.IndexFunc(*data.Artifacts, func(artifact CreateArtifactRequest) bool {
		return artifact.Id == *data.PrimaryArtifactId
	})
	if index < 0 {
		return CreateArtifactRequest{}, "the primary artifact is not among this request's artifacts"
	}
	artifact := (*data.Artifacts)[index]
	if artifact.Format == nil || !isStructureFormat(*artifact.Format) {
		return CreateArtifactRequest{}, "the primary artifact is not a PDB or mmCIF coordinate file"
	}
	if artifact.Sha256 == nil || strings.TrimSpace(*artifact.Sha256) == "" {
		return CreateArtifactRequest{}, "the primary artifact has no sha256"
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

// withFileSources returns sources with each file source in place of the field's source.
func withFileSources(sources domainmodels.FieldSources, fileSources domainmodels.FieldSources) domainmodels.FieldSources {
	if len(fileSources) == 0 {
		return sources
	}
	merged := make(domainmodels.FieldSources, len(sources)+len(fileSources))
	for pointer, source := range sources {
		merged[pointer] = source
	}
	for pointer, source := range fileSources {
		merged[pointer] = source
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
	fileSources, fieldErrors := modelValueSources(at, data, sources)
	return withFileSources(sources, fileSources), fieldErrors
}
