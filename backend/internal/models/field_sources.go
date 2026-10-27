package models

import (
	"reflect"
	"time"
)

// FieldSourceKind names where the value of a revision field came from.
type FieldSourceKind string

const (
	// FieldSourceRCSB means the archive sync copied the value from RCSB.
	FieldSourceRCSB FieldSourceKind = "rcsb"
	// FieldSourceDepositor means a user set or changed the value; the archive sync never
	// overwrites it.
	FieldSourceDepositor FieldSourceKind = "depositor"
)

// FieldSource records where the value of one revision field came from. For archive values,
// Accession is the PDB ID and Release the archive version, such as 4.3.
type FieldSource struct {
	Source      FieldSourceKind `json:"source"`
	Accession   *string         `json:"accession,omitempty"`
	Release     *string         `json:"release,omitempty"`
	RetrievedAt *time.Time      `json:"retrieved_at,omitempty"`
}

// FieldPointer is a JSON pointer to a revision field the archive sync writes, such as /title.
// Metrics are addressed by key, as /metrics/<key>.
type FieldPointer string

// FieldSources maps the archive-synced fields of a revision to where their values came from.
// A field without a key was not typed by a depositor, so the archive sync may write it.
// Revisions stored before sources were recorded have nil FieldSources: they were imported
// from the archive, so none of their fields count as typed by a depositor.
type FieldSources map[FieldPointer]FieldSource

// IsDepositor reports whether a user set or changed the field, so the archive sync must keep it.
func (sources FieldSources) IsDepositor(pointer FieldPointer) bool {
	source, ok := sources[pointer]
	return ok && source.Source == FieldSourceDepositor
}

// Entry revision fields the archive sync writes.
const (
	EntryFieldTitle           FieldPointer = "/title"
	EntryFieldDetails         FieldPointer = "/metadata/details"
	EntryFieldResolution      FieldPointer = "/metadata/resolution"
	EntryFieldMethod          FieldPointer = "/metadata/method"
	EntryFieldSpaceGroup      FieldPointer = "/metadata/space_group"
	EntryFieldCrystallography FieldPointer = "/metadata/crystallography"
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
)

// EntryArchiveFields are the entry revision fields the archive sync writes.
var EntryArchiveFields = []FieldPointer{
	EntryFieldTitle,
	EntryFieldDetails,
	EntryFieldResolution,
	EntryFieldMethod,
	EntryFieldSpaceGroup,
	EntryFieldCrystallography,
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
