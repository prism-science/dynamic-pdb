package models

import (
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.True(t, sources.IsUserSupplied(EntryFieldResolution))
}

func Test_should_record_no_sources_when_user_revision_of_imported_parent_changes_no_synced_field(t *testing.T) {
	// given
	parent := EntryRevision{Title: new("Archive title")}
	revision := parent
	revision.ThumbnailImageURL = new("https://example.org/thumbnail.png")

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
	depositor := sources.IsUserSupplied(EntryFieldTitle)

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

func Test_should_mark_changed_lists_depositor_when_user_creates_entry_revision(t *testing.T) {
	// given
	ligands := []string{"NAG"}
	parent := EntryRevision{
		Metadata:     EntryMetadata{Ligands: &ligands},
		FieldSources: FieldSources{EntryFieldLigands: archiveSource("1ABC")},
	}
	changedCofactors := []string{"ZN"}
	revision := EntryRevision{Metadata: EntryMetadata{Ligands: &ligands, Cofactors: &changedCofactors}}

	// when
	sources := EntryRevisionFieldSources(&parent, revision)

	// then
	assert.Equal(t, FieldSources{
		EntryFieldLigands:   archiveSource("1ABC"),
		EntryFieldCofactors: {Source: FieldSourceDepositor},
	}, sources)
}

func Test_should_treat_file_values_as_user_supplied_and_archive_values_as_synced(t *testing.T) {
	// given
	artifactID := uuid.New()
	sources := FieldSources{
		ArchiveMetricField(MetricKeyRWork): {Source: FieldSourceFile, ArtifactID: &artifactID, SHA256: new("abc")},
		ModelFieldAuthors:                  {Source: FieldSourceDepositor},
		ModelFieldAtomCount:                {Source: FieldSourceRCSB},
	}

	// when / then
	assert.True(t, sources.IsUserSupplied(ArchiveMetricField(MetricKeyRWork)))
	assert.True(t, sources.IsUserSupplied(ModelFieldAuthors))
	assert.False(t, sources.IsUserSupplied(ModelFieldAtomCount))
	assert.False(t, sources.IsUserSupplied(ModelFieldLigands))
	assert.Panics(t, func() {
		FieldSources{ModelFieldDetails: {Source: "guess"}}.IsUserSupplied(ModelFieldDetails)
	})
}

func Test_should_reject_stored_field_source_of_unknown_kind(t *testing.T) {
	// given
	known := FieldSources{
		EntryFieldTitle:      {Source: FieldSourceRCSB},
		EntryFieldDetails:    {Source: FieldSourceDepositor},
		EntryFieldResolution: {Source: FieldSourceFile},
	}
	unknown := FieldSources{EntryFieldTitle: {Source: "guess"}}

	// when / then
	require.NoError(t, known.Validate())
	require.ErrorContains(t, unknown.Validate(), `field source at /title: unknown field source: "guess"`)
}

func Test_should_list_model_fields_and_archive_metrics_as_fields_with_sources(t *testing.T) {
	// when
	fields := ModelSourceFields()

	// then
	assert.Contains(t, fields, ModelFieldAuthors)
	assert.Contains(t, fields, ArchiveMetricField(MetricKeyRFree))
	assert.Contains(t, fields, ArchiveMetricField(MetricKeyRWork))
	assert.NotContains(t, fields, ArchiveMetricField(MetricKeyRSCC))
	assert.Len(t, fields, len(ModelArchiveFields)+len(ArchiveMetricKeys))
}
