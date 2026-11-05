package models

import (
	"fmt"
	"maps"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// FieldSourceKind names where the value of a revision field came from.
type FieldSourceKind string

const (
	// FieldSourceRCSB means the archive sync copied the value from RCSB.
	FieldSourceRCSB FieldSourceKind = "rcsb"
	// FieldSourceDepositor means a user set or changed the value; the archive sync never
	// overwrites it.
	FieldSourceDepositor FieldSourceKind = "depositor"
	// FieldSourceFile means a user sent the value as read from a file they uploaded with the
	// revision; ArtifactID and SHA256 name the file. Like a depositor's value, the archive
	// sync never overwrites it.
	FieldSourceFile FieldSourceKind = "file"
)

// Validate reports a kind this package does not define, so a stored source of an unknown
// kind fails where it is read instead of being taken for one of the known kinds.
func (kind FieldSourceKind) Validate() error {
	switch kind {
	case FieldSourceRCSB, FieldSourceDepositor, FieldSourceFile:
		return nil
	default:
		return fmt.Errorf("unknown field source: %q", kind)
	}
}

// FieldSource records where the value of one revision field came from. For archive values,
// Accession is the PDB ID and Release the archive version, such as 4.3. For file values,
// ArtifactID and SHA256 name the uploaded file the value was read from.
type FieldSource struct {
	Source      FieldSourceKind `json:"source"`
	Accession   *string         `json:"accession,omitempty"`
	Release     *string         `json:"release,omitempty"`
	RetrievedAt *time.Time      `json:"retrieved_at,omitempty"`
	ArtifactID  *uuid.UUID      `json:"artifact_id,omitempty"`
	SHA256      *string         `json:"sha256,omitempty"`
}

// FieldPointer is a JSON pointer to a revision field the archive sync writes, such as /title.
// Metrics are addressed by key, as /metrics/<key>.
type FieldPointer string

// FieldSources maps the archive-synced fields of a revision to where their values came from.
// A field without a key was not typed by a depositor, so the archive sync may write it.
// Revisions stored before sources were recorded have nil FieldSources: they were imported
// from the archive, so none of their fields count as typed by a depositor.
type FieldSources map[FieldPointer]FieldSource

// IsUserSupplied reports whether a user set or changed the field, typed or read from a file
// they uploaded, so the archive sync must keep it.
func (sources FieldSources) IsUserSupplied(pointer FieldPointer) bool {
	source, ok := sources[pointer]
	if !ok {
		return false
	}
	switch source.Source {
	case FieldSourceDepositor, FieldSourceFile:
		return true
	case FieldSourceRCSB:
		return false
	default:
		// Stored sources are checked on read (FieldSourceKind.Validate), so only a source
		// built in code can get here.
		panic(fmt.Sprintf("unknown field source %q at %s", source.Source, pointer))
	}
}

// Validate reports the first source of an unknown kind.
func (sources FieldSources) Validate() error {
	for pointer, source := range sources {
		if err := source.Source.Validate(); err != nil {
			return fmt.Errorf("field source at %s: %w", pointer, err)
		}
	}
	return nil
}

// ModelSourceFields are the model revision fields whose sources are recorded: the fields
// the archive sync writes and the archive metrics.
func ModelSourceFields() []FieldPointer {
	fields := append([]FieldPointer(nil), ModelArchiveFields...)
	fields = append(fields, ModelFieldRelatedEntries)
	for _, key := range ArchiveMetricKeys {
		fields = append(fields, ArchiveMetricField(key))
	}
	return fields
}

// Entry revision fields the archive sync writes.
const (
	EntryFieldTitle           FieldPointer = "/title"
	EntryFieldDetails         FieldPointer = "/metadata/details"
	EntryFieldResolution      FieldPointer = "/metadata/resolution"
	EntryFieldMethod          FieldPointer = "/metadata/method"
	EntryFieldSpaceGroup      FieldPointer = "/metadata/space_group"
	EntryFieldCrystallography FieldPointer = "/metadata/crystallography"
	EntryFieldLigands         FieldPointer = "/metadata/ligands"
	EntryFieldCofactors       FieldPointer = "/metadata/cofactors"
)

// Model revision fields the archive sync writes, apart from the archive metrics.
const (
	ModelFieldAuthors             FieldPointer = "/metadata/authors"
	ModelFieldDetails             FieldPointer = "/metadata/details"
	ModelFieldAffiliation         FieldPointer = "/metadata/affiliation"
	ModelFieldAtomCount           FieldPointer = "/metadata/atom_count"
	ModelFieldModeledResidues     FieldPointer = "/metadata/modeled_residues"
	ModelFieldUniqueProteinChains FieldPointer = "/metadata/unique_protein_chains"
	ModelFieldUnmodeledFraction   FieldPointer = "/metadata/unmodeled_fraction"
	ModelFieldLigands             FieldPointer = "/metadata/ligands"
	ModelFieldResidueData         FieldPointer = "/metadata/residue_data"
	// ModelFieldRelatedEntries is synced only on models that name the archive as their origin,
	// so it is not one of ModelArchiveFields.
	ModelFieldRelatedEntries FieldPointer = "/metadata/related_entries"
)

// EntryArchiveFields are the entry revision fields the archive sync writes.
var EntryArchiveFields = []FieldPointer{
	EntryFieldTitle,
	EntryFieldDetails,
	EntryFieldResolution,
	EntryFieldMethod,
	EntryFieldSpaceGroup,
	EntryFieldCrystallography,
	EntryFieldLigands,
	EntryFieldCofactors,
}

// ModelArchiveFields are the model revision fields the archive sync writes, apart from the
// archive metrics.
var ModelArchiveFields = []FieldPointer{
	ModelFieldAuthors,
	ModelFieldDetails,
	ModelFieldAffiliation,
	ModelFieldAtomCount,
	ModelFieldModeledResidues,
	ModelFieldUniqueProteinChains,
	ModelFieldUnmodeledFraction,
	ModelFieldLigands,
	ModelFieldResidueData,
}

// ArchiveMetricKeys are the metrics the archive sync copies from RCSB onto Deposited models.
var ArchiveMetricKeys = []MetricKey{
	MetricKeyRFree,
	MetricKeyRWork,
	MetricKeyRamachandranOutliers,
	MetricKeyClashscore,
}

// ArchiveMetricField addresses one archive metric of a model revision.
func ArchiveMetricField(key MetricKey) FieldPointer {
	return FieldPointer("/metrics/" + string(key))
}

// EntryRevisionFieldSources returns the sources of an entry revision a user creates from
// parent, or as the first revision when parent is nil.
func EntryRevisionFieldSources(parent *EntryRevision, revision EntryRevision) FieldSources {
	if parent == nil {
		return depositorFieldSources(nil, nil, entryFieldValues(revision))
	}
	return depositorFieldSources(parent.FieldSources, entryFieldValues(*parent), entryFieldValues(revision))
}

// ArchiveEntryFieldSources marks each field of a new entry revision whose value equals the
// archive's with source, so the archive sync keeps that value current. Every other field
// keeps its source in sources. archive holds the archive's values, shaped the way a request
// carries them.
func ArchiveEntryFieldSources(
	sources FieldSources,
	revision EntryRevision,
	archive EntryRevision,
	source FieldSource,
) FieldSources {
	archiveValues := entryFieldValues(archive)
	marked := make(FieldSources, len(sources))
	maps.Copy(marked, sources)
	for pointer, value := range entryFieldValues(revision) {
		if !isUnset(value) && reflect.DeepEqual(value, archiveValues[pointer]) {
			marked[pointer] = source
		}
	}
	return marked
}

// ModelRevisionFieldSources returns the sources of a model revision a user creates from
// parent, or as the first revision when parent is nil. Metrics are the revision's metrics;
// only the archive metrics among them have sources.
func ModelRevisionFieldSources(
	parent *ModelRevision,
	parentMetrics []Metric,
	revision ModelRevision,
	metrics []Metric,
) FieldSources {
	if parent == nil {
		return depositorFieldSources(nil, nil, modelFieldValues(revision, metrics))
	}
	return depositorFieldSources(
		parent.FieldSources,
		modelFieldValues(*parent, parentMetrics),
		modelFieldValues(revision, metrics),
	)
}

// ArchiveModelFieldSources marks each field of a new model revision whose value equals the
// archive's with source, so the archive sync keeps that value current. Every other field keeps
// its source in sources.
func ArchiveModelFieldSources(
	sources FieldSources,
	revision ModelRevision,
	metrics []Metric,
	archive ModelRevision,
	archiveMetrics []Metric,
	source FieldSource,
) FieldSources {
	archiveValues := modelFieldValues(archive, archiveMetrics)
	marked := make(FieldSources, len(sources))
	maps.Copy(marked, sources)
	for pointer, value := range modelFieldValues(revision, metrics) {
		if !isUnset(value) && reflect.DeepEqual(value, archiveValues[pointer]) {
			marked[pointer] = source
		}
	}
	return marked
}

// depositorFieldSources marks the fields a user set (no parent) or changed (with a parent) as
// the depositor's; unchanged fields keep the parent's source, if it has one.
func depositorFieldSources(
	parentSources FieldSources,
	parentValues map[FieldPointer]any,
	values map[FieldPointer]any,
) FieldSources {
	sources := FieldSources{}
	for pointer, value := range values {
		if parentValues == nil {
			if !isUnset(value) {
				sources[pointer] = FieldSource{Source: FieldSourceDepositor}
			}
			continue
		}
		if !reflect.DeepEqual(parentValues[pointer], value) {
			sources[pointer] = FieldSource{Source: FieldSourceDepositor}
			continue
		}
		if source, ok := parentSources[pointer]; ok {
			sources[pointer] = source
		}
	}
	return sources
}

func entryFieldValues(revision EntryRevision) map[FieldPointer]any {
	return map[FieldPointer]any{
		EntryFieldTitle:           revision.Title,
		EntryFieldDetails:         revision.Metadata.Details,
		EntryFieldResolution:      revision.Metadata.Resolution,
		EntryFieldMethod:          revision.Metadata.Method,
		EntryFieldSpaceGroup:      revision.Metadata.SpaceGroup,
		EntryFieldCrystallography: revision.Metadata.Crystallography,
		EntryFieldLigands:         revision.Metadata.Ligands,
		EntryFieldCofactors:       revision.Metadata.Cofactors,
	}
}

func modelFieldValues(revision ModelRevision, metrics []Metric) map[FieldPointer]any {
	values := map[FieldPointer]any{
		ModelFieldAuthors:             revision.Metadata.Authors,
		ModelFieldDetails:             revision.Metadata.Details,
		ModelFieldAffiliation:         revision.Metadata.Affiliation,
		ModelFieldAtomCount:           revision.Metadata.AtomCount,
		ModelFieldModeledResidues:     revision.Metadata.ModeledResidues,
		ModelFieldUniqueProteinChains: revision.Metadata.UniqueProteinChains,
		ModelFieldUnmodeledFraction:   revision.Metadata.UnmodeledFraction,
		ModelFieldLigands:             revision.Metadata.Ligands,
		ModelFieldResidueData:         revision.Metadata.ResidueData,
		ModelFieldRelatedEntries:      revision.Metadata.RelatedEntries,
	}
	for _, key := range ArchiveMetricKeys {
		values[ArchiveMetricField(key)] = metricValue(metrics, key)
	}
	return values
}

// metricValue returns the value of the metric with key, or nil when the list has none.
func metricValue(metrics []Metric, key MetricKey) *float64 {
	for _, metric := range metrics {
		if metric.Key == key {
			return new(metric.Value)
		}
	}
	return nil
}

// isUnset reports whether a field value is a nil pointer or a nil list. An empty list is set:
// it means known to be empty.
func isUnset(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map:
		return reflected.IsNil()
	default:
		return false
	}
}
