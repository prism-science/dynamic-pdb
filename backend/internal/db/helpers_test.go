package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
)

func ptr[T any](value T) *T {
	return &value
}

func createDBTestUser(t *testing.T) uuid.UUID {
	t.Helper()

	now := time.Now().UTC()
	user, err := testDB.Users.Create(context.Background(), models.User{
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

func createDBTestEntryRevision(t *testing.T, name string, createdAt time.Time) *models.EntryRevision {
	t.Helper()

	createdBy := createDBTestUser(t)
	revision, err := testDB.Entries.Create(context.Background(), models.EntryRevision{
		ID:        uuid.New(),
		EntryID:   "entry-" + uuid.NewString(),
		State:     models.RevisionStatePending,
		Name:      name + "-" + uuid.NewString(),
		CreatedBy: createdBy,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return revision
}

func createDBTestActiveEntryRevision(t *testing.T, name string, createdAt time.Time) *models.EntryRevision {
	t.Helper()

	createdBy := createDBTestUser(t)
	revision, err := testDB.Entries.Create(context.Background(), models.EntryRevision{
		ID:         uuid.New(),
		EntryID:    "entry-" + uuid.NewString(),
		State:      models.RevisionStateInReview,
		EntryState: models.EntryStateActive,
		Name:       name + "-" + uuid.NewString(),
		CreatedBy:  createdBy,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	})
	require.NoError(t, err)

	active, err := testDB.Entries.ActivateRevision(context.Background(), revision.EntryID, revision.ID)
	require.NoError(t, err)
	return active
}

func createDBTestModelRevision(
	t *testing.T,
	entryID string,
	name string,
	createdAt time.Time,
) *models.ModelRevision {
	t.Helper()

	createdBy := createDBTestUser(t)
	revision, err := testDB.Models.Create(context.Background(), entryID, models.ModelRevision{
		ID:        uuid.New(),
		ModelID:   "model-" + uuid.NewString(),
		State:     models.RevisionStatePending,
		Name:      name + "-" + uuid.NewString(),
		CreatedBy: createdBy,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return revision
}

func createDBTestActiveModelRevision(
	t *testing.T,
	entryID string,
	name string,
	createdAt time.Time,
) *models.ModelRevision {
	t.Helper()

	createdBy := createDBTestUser(t)
	revision, err := testDB.Models.Create(context.Background(), entryID, models.ModelRevision{
		ID:         uuid.New(),
		ModelID:    "model-" + uuid.NewString(),
		State:      models.RevisionStateInReview,
		ModelState: models.ModelStateActive,
		Name:       name + "-" + uuid.NewString(),
		CreatedBy:  createdBy,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	})
	require.NoError(t, err)

	active, err := testDB.Models.ActivateRevision(context.Background(), revision.ID)
	require.NoError(t, err)
	return active
}

func createDBTestArtifact(t *testing.T, createdBy uuid.UUID, name string, createdAt time.Time) *models.Artifact {
	t.Helper()

	format := "cif"
	artifact, err := testDB.Artifacts.Create(context.Background(), models.Artifact{
		ID:        uuid.New(),
		Name:      name + "-" + uuid.NewString(),
		Level:     models.ArtifactLevelL2,
		Format:    &format,
		Metadata:  map[string]any{"kind": "test"},
		CreatedBy: createdBy,
		CreatedAt: createdAt,
	})
	require.NoError(t, err)
	return artifact
}
