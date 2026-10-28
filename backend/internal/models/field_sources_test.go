package models

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_should_mark_set_fields_depositor_and_leave_unset_fields_without_source_when_entry_is_created(t *testing.T) {
	// given
	revision := EntryRevision{
		Title:    new("Depositor title"),
		Metadata: EntryMetadata{Resolution: new(1.8)},
	}

	// when
	sources := EntryRevisionFieldSources(nil, revision)

	// then
	assert.Equal(t, FieldSources{
		EntryFieldTitle:      {Source: FieldSourceDepositor},
		EntryFieldResolution: {Source: FieldSourceDepositor},
	}, sources)
}

func Test_should_mark_field_depositor_when_user_revision_changes_it(t *testing.T) {
	// given
	parent := EntryRevision{
		Title:        new("Archive title"),
		Metadata:     EntryMetadata{Resolution: new(1.8)},
		FieldSources: FieldSources{EntryFieldTitle: archiveSource("1ABC")},
	}
	revision := parent
	revision.Title = new("Corrected title")

	// when
	sources := EntryRevisionFieldSources(&parent, revision)

	// then
	assert.Equal(t, FieldSources{EntryFieldTitle: {Source: FieldSourceDepositor}}, sources)
}

func Test_should_keep_parent_source_when_user_revision_leaves_field_unchanged(t *testing.T) {
	// given
	parent := EntryRevision{
		Title:    new("Archive title"),
		Metadata: EntryMetadata{SpaceGroup: new("P 1")},
		FieldSources: FieldSources{
			EntryFieldTitle:      archiveSource("1ABC"),
			EntryFieldSpaceGroup: {Source: FieldSourceDepositor},
		},
	}
	revision := parent
	revision.Metadata.Details = new("Depositor details")

	// when
	sources := EntryRevisionFieldSources(&parent, revision)

	// then
	assert.Equal(t, FieldSources{
		EntryFieldTitle:      archiveSource("1ABC"),
		EntryFieldSpaceGroup: {Source: FieldSourceDepositor},
		EntryFieldDetails:    {Source: FieldSourceDepositor},
	}, sources)
}

func Test_should_mark_field_depositor_when_user_revision_clears_it(t *testing.T) {
	// given
	parent := EntryRevision{Metadata: EntryMetadata{Resolution: new(1.8)}}
	revision := parent
	revision.Metadata.Resolution = nil

	// when
	sources := EntryRevisionFieldSources(&parent, revision)

	// then
	assert.True(t, sources.IsDepositor(EntryFieldResolution))
}

func Test_should_record_no_sources_when_user_revision_of_imported_parent_changes_no_synced_field(t *testing.T) {
	// given
	parent := EntryRevision{Title: new("Archive title")}
	revision := parent
	revision.Metadata.Ligands = &[]string{"ATP"}

	// when
	sources := EntryRevisionFieldSources(&parent, revision)

	// then
	assert.NotNil(t, sources)
	assert.Empty(t, sources)
}

func Test_should_mark_changed_archive_metric_depositor_when_user_model_revision_edits_it(t *testing.T) {
	// given
	deposited := StructureModelTypeDeposited
	parent := ModelRevision{Metadata: ModelMetadata{ModelType: &deposited, AtomCount: new(100)}}
	parentMetrics := []Metric{
		{Key: MetricKeyRFree, Value: 0.21},
		{Key: MetricKeyRWork, Value: 0.18},
	}
	revision := parent
	metrics := []Metric{
		{Key: MetricKeyRFree, Value: 0.25},
		{Key: MetricKeyRWork, Value: 0.18},
		{Key: MetricKeyMolProbityScore, Value: 1.4},
	}

	// when
	sources := ModelRevisionFieldSources(&parent, parentMetrics, revision, metrics)

	// then
	assert.Equal(t, FieldSources{ArchiveMetricField(MetricKeyRFree): {Source: FieldSourceDepositor}}, sources)
}

func Test_should_mark_set_model_fields_and_archive_metrics_depositor_when_model_is_created(t *testing.T) {
	// given
	revision := ModelRevision{Metadata: ModelMetadata{
		Authors:   []string{"Doe, J."},
		AtomCount: new(1200),
		Ligands:   []string{},
	}}
	metrics := []Metric{{Key: MetricKeyClashscore, Value: 3.1}}

	// when
	sources := ModelRevisionFieldSources(nil, nil, revision, metrics)

	// then
	assert.Equal(t, FieldSources{
		ModelFieldAuthors:                       {Source: FieldSourceDepositor},
		ModelFieldAtomCount:                     {Source: FieldSourceDepositor},
		ModelFieldLigands:                       {Source: FieldSourceDepositor},
		ArchiveMetricField(MetricKeyClashscore): {Source: FieldSourceDepositor},
	}, sources)
}

func Test_should_treat_missing_sources_as_not_typed_by_depositor_when_revision_predates_sources(t *testing.T) {
	// given
	var sources FieldSources

	// when
	depositor := sources.IsDepositor(EntryFieldTitle)

	// then
	assert.False(t, depositor)
}

func Test_should_compare_exactly_the_archive_fields_when_sources_are_computed(t *testing.T) {
	// given
	entryValues := entryFieldValues(EntryRevision{})
	modelValues := modelFieldValues(ModelRevision{}, nil)

	// when
	modelArchiveFields := append([]FieldPointer{}, ModelArchiveFields...)
	for _, key := range ArchiveMetricKeys {
		modelArchiveFields = append(modelArchiveFields, ArchiveMetricField(key))
	}

	// then
	assert.ElementsMatch(t, EntryArchiveFields, slices.Collect(maps.Keys(entryValues)))
	assert.ElementsMatch(t, modelArchiveFields, slices.Collect(maps.Keys(modelValues)))
}

func archiveSource(pdbID string) FieldSource {
	return FieldSource{Source: FieldSourceRCSB, Accession: new(pdbID)}
}

func Test_should_mark_values_equal_to_the_archive_rcsb_when_entry_is_created(t *testing.T) {
	// given
	revision := EntryRevision{
		Title:    new("Archive title"),
		Metadata: EntryMetadata{Resolution: new(1.8), SpaceGroup: new("P 1")},
	}
	archive := EntryRevision{
		Title:    new("Archive title"),
		Metadata: EntryMetadata{Resolution: new(1.8), SpaceGroup: new("P 21 21 21")},
	}
	source := archiveSource("1ABC")

	// when
	sources := ArchiveEntryFieldSources(EntryRevisionFieldSources(nil, revision), revision, archive, source)

	// then
	assert.Equal(t, FieldSources{
		EntryFieldTitle:      source,
		EntryFieldResolution: source,
		EntryFieldSpaceGroup: {Source: FieldSourceDepositor},
	}, sources)
}

func Test_should_mark_nothing_rcsb_when_entry_leaves_a_value_out_that_the_archive_also_lacks(t *testing.T) {
	// given
	revision := EntryRevision{Title: new("Depositor title")}
	archive := EntryRevision{Title: new("Archive title")}

	// when
	sources := ArchiveEntryFieldSources(
		EntryRevisionFieldSources(nil, revision), revision, archive, archiveSource("1ABC"))

	// then
	assert.Equal(t, FieldSources{EntryFieldTitle: {Source: FieldSourceDepositor}}, sources)
}
