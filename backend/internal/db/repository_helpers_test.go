package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
)

func createDBTestUser(t *testing.T) uuid.UUID {
	t.Helper()

	now := time.Now().UTC()
	user, err := testDB.Users.Create(context.Background(), auth.User{
		ID: uuid.New(),
		ExternalRef: types.ExternalRef{
			Source: "test",
			Value:  uuid.NewString(),
		},
		Email:       "user-" + uuid.NewString() + "@example.com",
		DisplayName: "DB Test User",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	return user.ID
}

func createDBTestStructure(t *testing.T, name string, createdAt time.Time) *models.Structure {
	t.Helper()

	createdBy := createDBTestUser(t)
	structure, err := testDB.Structures.Create(context.Background(), models.Structure{
		ID:        uuid.New(),
		CreatedBy: createdBy,
		Name:      name + "-" + uuid.NewString(),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return structure
}

func createDBTestModel(t *testing.T, structureID uuid.UUID, name string, createdAt time.Time) *models.Model {
	t.Helper()

	createdBy := createDBTestUser(t)
	model, err := testDB.Models.Create(context.Background(), models.Model{
		ID:          uuid.New(),
		StructureID: structureID,
		CreatedBy:   createdBy,
		Name:        name + "-" + uuid.NewString(),
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	})
	require.NoError(t, err)
	return model
}

func createDBTestEntity(
	t *testing.T,
	structureID uuid.UUID,
	modelID *uuid.UUID,
	entityType models.EntityType,
	level *models.EntityLevel,
	name string,
) *models.Entity {
	t.Helper()

	now := time.Now().UTC()
	entity, err := testDB.Entities.Create(context.Background(), models.Entity{
		ID:          uuid.New(),
		StructureID: structureID,
		ModelID:     modelID,
		Type:        entityType,
		Level:       level,
		Name:        name + "-" + uuid.NewString(),
		Payload:     dbTestPayload(entityType),
		CreatedAt:   now,
		UpdatedAt:   now,
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
