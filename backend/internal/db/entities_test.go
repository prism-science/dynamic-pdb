package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_create_and_list_model_entity_payload_when_entities_repository_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "model-entity-entry", time.Now().UTC())
	experiment := createDBTestExperiment(t, entry.ID, "model experiment", time.Now().UTC())
	level := models.EntityLevelL2
	affiliation := "Department of Chemistry, Boston University"
	entity := models.Entity{
		ID:           uuid.New(),
		EntryID:      entry.ID,
		ExperimentID: &experiment.ID,
		Type:         models.EntityTypeModel,
		Level:        &level,
		Name:         "qFit model " + uuid.NewString(),
		Payload: models.ModelPayload{
			FileURL:     "s3://dynamic-pdb/models/qfit.cif",
			Authors:     []string{"Hendrickson, W.A.", "Teeter, M.M."},
			Affiliation: &affiliation,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// when
	created, err := testDB.Entities.Create(context.Background(), entity)
	require.NoError(t, err)
	got := listSingleEntityByEntry(t, entry.ID)

	// then
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, entry.ID, got.EntryID)
	require.NotNil(t, got.ExperimentID)
	assert.Equal(t, experiment.ID, *got.ExperimentID)
	require.NotNil(t, got.Level)
	assert.Equal(t, level, *got.Level)
	payload, err := got.Model()
	require.NoError(t, err)
	assert.Equal(t, "s3://dynamic-pdb/models/qfit.cif", payload.FileURL)
	assert.Equal(t, []string{"Hendrickson, W.A.", "Teeter, M.M."}, payload.Authors)
	require.NotNil(t, payload.Affiliation)
	assert.Equal(t, affiliation, *payload.Affiliation)
}

func Test_should_create_and_list_data_entity_metadata_when_entities_repository_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "data-entity-entry", time.Now().UTC())
	size := int64(385)
	entity := models.Entity{
		ID:      uuid.New(),
		EntryID: entry.ID,
		Type:    models.EntityTypeData,
		Name:    "fasta " + uuid.NewString(),
		Payload: models.DataPayload{
			FileURL: "https://www.rcsb.org/fasta/entry/5GY3/download",
			Type:    "fasta",
			Size:    &size,
			Metadata: map[string]any{
				"length": 310,
				"chains": 1,
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// when
	created, err := testDB.Entities.Create(context.Background(), entity)
	require.NoError(t, err)
	got := listSingleEntityByEntry(t, entry.ID)

	// then
	assert.Equal(t, created.ID, got.ID)
	payload, err := got.Data()
	require.NoError(t, err)
	assert.Equal(t, "https://www.rcsb.org/fasta/entry/5GY3/download", payload.FileURL)
	assert.Equal(t, "fasta", payload.Type)
	require.NotNil(t, payload.Size)
	assert.Equal(t, size, *payload.Size)
	assert.Equal(t, float64(310), payload.Metadata["length"])
	assert.Equal(t, float64(1), payload.Metadata["chains"])
}

func Test_should_create_and_list_program_entity_payload_when_entities_repository_called(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "program-entity-entry", time.Now().UTC())
	entity := models.Entity{
		ID:      uuid.New(),
		EntryID: entry.ID,
		Type:    models.EntityTypeProgram,
		Name:    "qFit " + uuid.NewString(),
		Payload: models.ProgramPayload{
			Name:        "qFit",
			Version:     "4.0.0",
			Description: "Multiconformer model builder",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// when
	created, err := testDB.Entities.Create(context.Background(), entity)
	require.NoError(t, err)
	got := listSingleEntityByEntry(t, entry.ID)

	// then
	assert.Equal(t, created.ID, got.ID)
	payload, err := got.Program()
	require.NoError(t, err)
	assert.Equal(t, "qFit", payload.Name)
	assert.Equal(t, "4.0.0", payload.Version)
	assert.Equal(t, "Multiconformer model builder", payload.Description)
}

func Test_should_list_entities_matching_entry_experiment_type_and_level_filters(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "list-entities-entry", time.Now().UTC())
	otherEntry := createDBTestEntry(t, "list-entities-other-entry", time.Now().UTC())
	experiment := createDBTestExperiment(t, entry.ID, "filtered experiment", time.Now().UTC())
	otherExperiment := createDBTestExperiment(t, entry.ID, "other experiment", time.Now().UTC().Add(time.Second))
	foreignExperiment := createDBTestExperiment(t, otherEntry.ID, "foreign experiment", time.Now().UTC())
	levelL0 := models.EntityLevelL0
	levelL2 := models.EntityLevelL2
	levelL3 := models.EntityLevelL3

	_ = createDBTestEntity(t, entry.ID, nil, models.EntityTypeData, &levelL0, "entry data")
	matching := createDBTestEntity(t, entry.ID, &experiment.ID, models.EntityTypeModel, &levelL2, "matching model")
	_ = createDBTestEntity(t, entry.ID, &experiment.ID, models.EntityTypeMetrics, &levelL3, "wrong type")
	_ = createDBTestEntity(t, entry.ID, &otherExperiment.ID, models.EntityTypeModel, &levelL2, "wrong experiment")
	_ = createDBTestEntity(t, otherEntry.ID, &foreignExperiment.ID, models.EntityTypeModel, &levelL2, "wrong entry")
	types := []models.EntityType{models.EntityTypeModel}
	levels := []models.EntityLevel{models.EntityLevelL2}

	// when
	got, err := testDB.Entities.List(context.Background(), db.EntityFilters{
		EntryID:      &entry.ID,
		ExperimentID: &experiment.ID,
		Types:        types,
		Levels:       levels,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, matching.ID, got[0].ID)
	assert.Equal(t, models.EntityTypeModel, got[0].Type)
	require.NotNil(t, got[0].Level)
	assert.Equal(t, models.EntityLevelL2, *got[0].Level)
}

func Test_should_return_error_when_entities_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Entities.List(context.Background(), db.EntityFilters{Limit: &limit})

	// then
	require.Error(t, err)
}

func listSingleEntityByEntry(t *testing.T, entryID uuid.UUID) models.Entity {
	t.Helper()

	entities, err := testDB.Entities.List(context.Background(), db.EntityFilters{EntryID: &entryID})
	require.NoError(t, err)
	require.Len(t, entities, 1)
	return entities[0]
}
