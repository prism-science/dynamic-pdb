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
	"dynamic-pdb/backend/internal/types"
)

func Test_should_insert_user_and_return_persisted_record_when_create_called(t *testing.T) {
	// given
	now := time.Now().UTC()
	user := models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "github", Value: uuid.NewString()},
		Email:       "user@example.com",
		DisplayName: "User Name",
		AvatarURL:   "https://avatars.example/u.png",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// when
	created, err := testDB.Users.Create(context.Background(), user)

	// then
	require.NoError(t, err)
	assert.Equal(t, user.ID, created.ID)
	assert.Equal(t, user.ExternalRef, created.ExternalRef)
	assert.Equal(t, user.Email, created.Email)
	assert.Equal(t, user.DisplayName, created.DisplayName)
	assert.Equal(t, user.AvatarURL, created.AvatarURL)
	assert.Equal(t, now.Unix(), created.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), created.UpdatedAt.Unix())
}

func Test_should_return_empty_strings_when_create_called_without_optional_fields(t *testing.T) {
	// given
	now := time.Now().UTC()

	// when
	created, err := testDB.Users.Create(context.Background(), models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "github", Value: uuid.NewString()},
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, "", created.Email)
	assert.Equal(t, "", created.DisplayName)
	assert.Equal(t, "", created.AvatarURL)
}

func Test_should_update_profile_fields_when_create_hits_external_ref_conflict(t *testing.T) {
	// given
	originalID := uuid.New()
	externalRef := uuid.NewString()
	originalTime := time.Now().UTC()
	_, err := testDB.Users.Create(context.Background(), models.User{
		ID:          originalID,
		ExternalRef: types.ExternalRef{Source: "github", Value: externalRef},
		Email:       "old@example.com",
		DisplayName: "Old Name",
		AvatarURL:   "https://avatars.example/old.png",
		CreatedAt:   originalTime,
		UpdatedAt:   originalTime,
	})
	require.NoError(t, err)

	// when
	newTime := originalTime.Add(time.Hour)
	result, err := testDB.Users.Create(context.Background(), models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "github", Value: externalRef},
		Email:       "new@example.com",
		DisplayName: "New Name",
		AvatarURL:   "https://avatars.example/new.png",
		CreatedAt:   newTime,
		UpdatedAt:   newTime,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, originalID, result.ID)
	assert.Equal(t, "new@example.com", result.Email)
	assert.Equal(t, "New Name", result.DisplayName)
	assert.Equal(t, "https://avatars.example/new.png", result.AvatarURL)
	assert.Equal(t, originalTime.Unix(), result.CreatedAt.Unix())
	assert.Equal(t, newTime.Unix(), result.UpdatedAt.Unix())
}

func Test_should_keep_existing_email_when_create_conflict_has_empty_email(t *testing.T) {
	// given
	originalID := uuid.New()
	externalRef := uuid.NewString()
	originalTime := time.Now().UTC()
	_, err := testDB.Users.Create(context.Background(), models.User{
		ID:          originalID,
		ExternalRef: types.ExternalRef{Source: "github", Value: externalRef},
		Email:       "known@example.com",
		DisplayName: "Old Name",
		CreatedAt:   originalTime,
		UpdatedAt:   originalTime,
	})
	require.NoError(t, err)

	// when
	newTime := originalTime.Add(time.Hour)
	result, err := testDB.Users.Create(context.Background(), models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "github", Value: externalRef},
		DisplayName: "New Name",
		CreatedAt:   newTime,
		UpdatedAt:   newTime,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, originalID, result.ID)
	assert.Equal(t, "known@example.com", result.Email)
	assert.Equal(t, "New Name", result.DisplayName)
	assert.Equal(t, originalTime.Unix(), result.CreatedAt.Unix())
	assert.Equal(t, newTime.Unix(), result.UpdatedAt.Unix())
}

func Test_should_return_user_when_get_finds_row(t *testing.T) {
	// given
	now := time.Now().UTC()
	user := models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "github", Value: uuid.NewString()},
		Email:       "user@example.com",
		DisplayName: "User Name",
		AvatarURL:   "https://avatars.example/u.png",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	created, err := testDB.Users.Create(context.Background(), user)
	require.NoError(t, err)

	// when
	got, err := testDB.Users.Get(context.Background(), created.ID)

	// then
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, user.ExternalRef, got.ExternalRef)
	assert.Equal(t, user.Email, got.Email)
	assert.Equal(t, user.DisplayName, got.DisplayName)
	assert.Equal(t, user.AvatarURL, got.AvatarURL)
	assert.Equal(t, created.CreatedAt.Unix(), got.CreatedAt.Unix())
}

func Test_should_return_not_found_error_when_get_misses(t *testing.T) {
	// given / when
	_, err := testDB.Users.Get(context.Background(), uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrUserNotFound)
}

func Test_should_create_system_user_when_database_migrated(t *testing.T) {
	// given
	systemUserID, err := uuid.Parse(models.SystemUserID)
	require.NoError(t, err)

	// when
	user, err := testDB.Users.Get(context.Background(), systemUserID)

	// then
	require.NoError(t, err)
	assert.Equal(t, "system", user.ExternalRef.Source)
	assert.Equal(t, "dynamic-pdb", user.ExternalRef.Value)
	assert.Equal(t, "Dynamic PDB", user.DisplayName)
}
