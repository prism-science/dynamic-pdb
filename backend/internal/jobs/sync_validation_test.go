package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

func Test_should_accept_entry_update_when_rcsb_values_follow_schema(t *testing.T) {
	// given
	revision := models.EntryRevision{Title: new("T4 lysozyme"), Metadata: models.EntryMetadata{Resolution: new(1.8)}}
	polymerEntities := []models.PolymerEntity{{Metadata: models.PolymerEntityMetadata{
		UniProtMappings: []models.PolymerEntityUniProtReference{
			{Accession: "P00720", Source: models.UniProtReferenceSourceSIFTS, UniProtRelease: new("2026_04")},
			{Accession: "P00720", Source: models.UniProtReferenceSourceStructRef},
		},
	}}}

	// when
	fieldErrors := entryUpdateErrors(revision, polymerEntities)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_accept_entry_update_when_rcsb_mutation_text_is_free_form(t *testing.T) {
	// given
	revision := models.EntryRevision{Title: new("T4 lysozyme")}
	polymerEntities := []models.PolymerEntity{{Metadata: models.PolymerEntityMetadata{
		Mutations: []string{"ENGINEERED MUTATION", "DELETION", "A(-3)G"},
	}}}

	// when
	fieldErrors := entryUpdateErrors(revision, polymerEntities)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_entry_update_errors_when_rcsb_values_break_schema(t *testing.T) {
	// given
	revision := models.EntryRevision{Metadata: models.EntryMetadata{Resolution: new(-1.0)}}
	polymerEntities := []models.PolymerEntity{
		{Metadata: models.PolymerEntityMetadata{}},
		{Metadata: models.PolymerEntityMetadata{
			UniProtMappings: []models.PolymerEntityUniProtReference{
				{Accession: "P00720", Source: models.UniProtReferenceSourceSIFTS},
			},
		}},
	}

	// when
	fieldErrors := entryUpdateErrors(revision, polymerEntities)

	// then
	assert.Equal(t, []validation.Pointer{
		"/title",
		"/resolution",
		"/polymer_entities/1/uniprot_mappings/0/unp_release",
	}, pointersOf(fieldErrors))
}

func Test_should_accept_model_update_when_rcsb_metrics_follow_schema(t *testing.T) {
	// given
	details := rcsb.EntryDetails{
		Refinements:        []rcsb.EntryRefinement{{RFree: new(0.22), RWork: new(0.19)}},
		ValidationGeometry: []rcsb.EntryValidationGeometry{{Clashscore: new(3.1), RamachandranOutliersPercent: new(0.4)}},
	}

	// when
	fieldErrors := modelUpdateErrors(models.ModelRevision{}, modelMetricsFromRCSB(details))

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_report_model_update_errors_when_rcsb_values_break_schema(t *testing.T) {
	// given
	revision := models.ModelRevision{Metadata: models.ModelMetadata{UnmodeledFraction: new(1.2)}}
	metrics := []models.Metric{{Key: "resolution", Value: 2.0}}

	// when
	fieldErrors := modelUpdateErrors(revision, metrics)

	// then
	assert.Equal(t, []validation.Pointer{"/metadata/unmodeled_fraction", "/metrics/0/key"}, pointersOf(fieldErrors))
}

func Test_should_pass_validation_mode_to_sync_strategies_when_job_created(t *testing.T) {
	// given
	database := &db.DB{}

	// when
	job, err := NewDataSyncJob(database, rcsb.NewClient(), sifts.NewClient(), nil, validation.ModeEnforce)

	// then
	require.NoError(t, err)
	require.Len(t, job.strategies, 2)
	entryStrategy, ok := job.strategies[0].(*entrySyncStrategy)
	require.True(t, ok)
	assert.Equal(t, validation.ModeEnforce, entryStrategy.validationMode)
	modelStrategy, ok := job.strategies[1].(*modelSyncStrategy)
	require.True(t, ok)
	assert.Equal(t, validation.ModeEnforce, modelStrategy.validationMode)
}

func Test_should_reject_data_sync_job_when_validation_mode_unknown(t *testing.T) {
	// when
	job, err := NewDataSyncJob(&db.DB{}, rcsb.NewClient(), sifts.NewClient(), nil, validation.Mode("warn"))

	// then
	require.EqualError(t, err, "validation mode: unknown validation mode: warn")
	assert.Nil(t, job)
}

func pointersOf(fieldErrors []validation.FieldError) []validation.Pointer {
	pointers := make([]validation.Pointer, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		pointers = append(pointers, fieldError.Pointer)
	}
	return pointers
}
