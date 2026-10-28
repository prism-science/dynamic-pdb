package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_keep_depositor_title_when_archive_title_changes(t *testing.T) {
	// given
	current := models.EntryRevision{
		Title:        new("Depositor title"),
		Metadata:     models.EntryMetadata{SpaceGroup: new("P 1")},
		FieldSources: models.FieldSources{models.EntryFieldTitle: {Source: models.FieldSourceDepositor}},
	}
	desired := models.EntryRevision{
		Title:    new("Archive title"),
		Metadata: models.EntryMetadata{SpaceGroup: new("P 1")},
	}

	// when
	keepDepositorEntryFields(current, &desired)
	entryChanged, _ := compareEntryUpdate(current, desired, nil, nil)

	// then
	assert.Equal(t, "Depositor title", *desired.Title)
	assert.False(t, entryChanged)
}

func Test_should_fill_empty_resolution_from_archive_when_depositor_left_it_blank(t *testing.T) {
	// given
	current := models.EntryRevision{
		Title:        new("Depositor title"),
		FieldSources: models.FieldSources{models.EntryFieldTitle: {Source: models.FieldSourceDepositor}},
	}
	desired := models.EntryRevision{
		Title:    new("Archive title"),
		Metadata: models.EntryMetadata{Resolution: new(1.8)},
	}

	// when
	keepDepositorEntryFields(current, &desired)
	entryChanged, _ := compareEntryUpdate(current, desired, nil, nil)

	// then
	assert.Equal(t, 1.8, *desired.Metadata.Resolution)
	assert.True(t, entryChanged)
}

func Test_should_update_every_synced_entry_field_when_revision_predates_sources(t *testing.T) {
	// given
	current := models.EntryRevision{Title: new("Imported title")}
	desired := models.EntryRevision{Title: new("Archive title")}

	// when
	keepDepositorEntryFields(current, &desired)

	// then
	assert.Equal(t, "Archive title", *desired.Title)
}

func Test_should_record_rcsb_source_for_synced_entry_fields_and_keep_depositor_sources(t *testing.T) {
	// given
	retrievedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	current := models.FieldSources{models.EntryFieldTitle: {Source: models.FieldSourceDepositor}}

	source := archiveRevisionSource{pdbID: "1ABC", version: models.ArchiveVersion{Ordinal: 9, Version: "4.3"}}

	// when
	sources := archiveEntryFieldSources(current, source, retrievedAt)

	// then
	require.Len(t, sources, len(models.EntryArchiveFields))
	assert.Equal(t, models.FieldSource{Source: models.FieldSourceDepositor}, sources[models.EntryFieldTitle])
	assert.Equal(t, models.FieldSource{
		Source:      models.FieldSourceRCSB,
		Accession:   new("1ABC"),
		Release:     new("4.3"),
		RetrievedAt: &retrievedAt,
	}, sources[models.EntryFieldResolution])
}

func Test_should_sync_model_only_when_it_is_deposited(t *testing.T) {
	// given
	deposited := models.StructureModelTypeDeposited
	singleConformer := models.StructureModelTypeSingleConformer

	// when
	syncsDeposited := isDepositedModel(models.ModelRevision{Metadata: models.ModelMetadata{ModelType: &deposited}})
	syncsSingleConformer := isDepositedModel(models.ModelRevision{Metadata: models.ModelMetadata{ModelType: &singleConformer}})
	syncsUntyped := isDepositedModel(models.ModelRevision{})

	// then
	assert.True(t, syncsDeposited)
	assert.False(t, syncsSingleConformer)
	assert.False(t, syncsUntyped)
}

func Test_should_keep_depositor_metric_when_archive_metric_changes(t *testing.T) {
	// given
	current := models.ModelRevision{FieldSources: models.FieldSources{
		models.ArchiveMetricField(models.MetricKeyRFree): {Source: models.FieldSourceDepositor},
	}}
	currentMetrics := []models.Metric{
		{Key: models.MetricKeyRFree, Value: 0.25},
		{Key: models.MetricKeyRWork, Value: 0.18},
	}
	desiredMetrics := []models.Metric{
		{Key: models.MetricKeyRFree, Value: 0.21},
		{Key: models.MetricKeyRWork, Value: 0.18},
	}

	// when
	keys := syncedArchiveMetricKeys(current.FieldSources)
	_, metricsChanged := compareModelUpdate(current, current, currentMetrics, desiredMetrics, keys)

	// then
	assert.NotContains(t, keys, models.MetricKeyRFree)
	assert.Contains(t, keys, models.MetricKeyRWork)
	assert.False(t, metricsChanged)
}

func Test_should_keep_depositor_atom_count_when_archive_count_changes(t *testing.T) {
	// given
	current := models.ModelRevision{
		Metadata:     models.ModelMetadata{AtomCount: new(1200), Authors: []string{"Doe, J."}},
		FieldSources: models.FieldSources{models.ModelFieldAtomCount: {Source: models.FieldSourceDepositor}},
	}
	desired := models.ModelRevision{Metadata: models.ModelMetadata{AtomCount: new(1383), Authors: []string{"Nelson, R."}}}

	// when
	keepDepositorModelFields(current, &desired)

	// then
	assert.Equal(t, 1200, *desired.Metadata.AtomCount)
	assert.Equal(t, []string{"Nelson, R."}, desired.Metadata.Authors)
}

func Test_should_record_rcsb_source_for_synced_model_fields_and_metrics(t *testing.T) {
	// given
	retrievedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	current := models.FieldSources{
		models.ModelFieldLigands:                         {Source: models.FieldSourceDepositor},
		models.ArchiveMetricField(models.MetricKeyRFree): {Source: models.FieldSourceDepositor},
	}

	// when
	source := archiveRevisionSource{pdbID: "5AMF", version: models.ArchiveVersion{Ordinal: 3, Version: "2.1"}}
	sources := archiveModelFieldSources(current, source, retrievedAt, syncedArchiveMetricKeys(current))

	// then
	require.Len(t, sources, len(models.ModelArchiveFields)+len(models.ArchiveMetricKeys))
	assert.True(t, sources.IsDepositor(models.ModelFieldLigands))
	assert.True(t, sources.IsDepositor(models.ArchiveMetricField(models.MetricKeyRFree)))
	assert.Equal(t, models.FieldSourceRCSB, sources[models.ModelFieldAtomCount].Source)
	assert.Equal(t, models.FieldSourceRCSB, sources[models.ArchiveMetricField(models.MetricKeyClashscore)].Source)
	assert.Equal(t, new("5AMF"), sources[models.ArchiveMetricField(models.MetricKeyClashscore)].Accession)
}

func Test_should_keep_null_field_sources_when_title_comes_from_removal_record(t *testing.T) {
	// given: NULL sources, the mark of an archive import
	var current models.FieldSources

	// when
	sources := removalTitleFieldSources(current, "8PYQ", time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))

	// then
	assert.Nil(t, sources)
}

func Test_should_record_rcsb_title_source_without_release_when_title_comes_from_removal_record(t *testing.T) {
	// given
	current := models.FieldSources{models.EntryFieldDetails: {Source: models.FieldSourceDepositor}}
	retrievedAt := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

	// when
	sources := removalTitleFieldSources(current, "8PYQ", retrievedAt)

	// then
	assert.Equal(t, models.FieldSources{
		models.EntryFieldDetails: {Source: models.FieldSourceDepositor},
		models.EntryFieldTitle: {
			Source:      models.FieldSourceRCSB,
			Accession:   new("8PYQ"),
			RetrievedAt: new(retrievedAt),
		},
	}, sources)
	assert.NotContains(t, current, models.EntryFieldTitle)
}
