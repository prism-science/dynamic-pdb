package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func createDBTestEntry(t *testing.T, name string, createdAt time.Time) *models.Entry {
	t.Helper()

	entry, err := testDB.Entries.Create(context.Background(), models.Entry{
		ID:        uuid.New(),
		Name:      name + "-" + uuid.NewString(),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return entry
}

func createDBTestExperiment(t *testing.T, entryID uuid.UUID, name string, createdAt time.Time) *models.Experiment {
	t.Helper()

	experiment, err := testDB.Experiments.Create(context.Background(), models.Experiment{
		ID:        uuid.New(),
		EntryID:   entryID,
		Name:      name + "-" + uuid.NewString(),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return experiment
}

func createDBTestEntity(
	t *testing.T,
	entryID uuid.UUID,
	experimentID *uuid.UUID,
	entityType models.EntityType,
	level *models.EntityLevel,
	name string,
) *models.Entity {
	t.Helper()

	now := time.Now().UTC()
	entity, err := testDB.Entities.Create(context.Background(), models.Entity{
		ID:           uuid.New(),
		EntryID:      entryID,
		ExperimentID: experimentID,
		Type:         entityType,
		Level:        level,
		Name:         name + "-" + uuid.NewString(),
		Payload:      dbTestPayload(entityType),
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	require.NoError(t, err)
	return entity
}

func dbTestPayload(entityType models.EntityType) any {
	switch entityType {
	case models.EntityTypeData:
		return models.DataPayload{FileURL: "s3://dynamic-pdb/test/data.fasta", Type: "fasta"}
	case models.EntityTypeModel:
		return models.ModelPayload{FileURL: "s3://dynamic-pdb/test/model.cif"}
	case models.EntityTypeMetrics:
		value := 0.92
		return models.MetricsPayload{CC: &value}
	case models.EntityTypeProgram:
		return models.ProgramPayload{Name: "phenix.refine", Version: "1.21.2"}
	default:
		return models.DataPayload{FileURL: "s3://dynamic-pdb/test/file.dat"}
	}
}
