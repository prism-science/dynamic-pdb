package db_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
)

var testDB *db.DB

func TestMain(m *testing.M) {
	adminDB, testSchema, err := setupDBTestSchema()
	if err != nil {
		log.Fatalf("db tests: setup schema: %v", err)
	}

	testDB, err = db.NewDB(db.Config{
		Host:               "localhost",
		Port:               35432,
		Name:               "dynamic_pdb_local",
		Username:           "postgres",
		Password:           "password",
		ConnectionParams:   "sslmode=disable search_path=" + testSchema,
		MaxOpenConnections: 1,
	})
	if err != nil {
		if cleanupErr := dropDBTestSchema(adminDB, testSchema); cleanupErr != nil {
			log.Printf("db tests: drop schema after connect failed: %v", cleanupErr)
		}
		if closeErr := adminDB.Close(); closeErr != nil {
			log.Printf("db tests: close admin connection after connect failed: %v", closeErr)
		}
		log.Fatalf("db tests: connect: %v", err)
	}
	code := m.Run()
	if err := testDB.Close(); err != nil {
		log.Printf("db tests: close: %v", err)
		code = 1
	}
	if err := dropDBTestSchema(adminDB, testSchema); err != nil {
		log.Printf("db tests: drop schema: %v", err)
		code = 1
	}
	if err := adminDB.Close(); err != nil {
		log.Printf("db tests: close admin connection: %v", err)
		code = 1
	}
	os.Exit(code)
}

func setupDBTestSchema() (*sqlx.DB, string, error) {
	schemaName := "db_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	adminDB, err := sqlx.Connect("postgres", dbTestDSN("sslmode=disable"))
	if err != nil {
		return nil, "", fmt.Errorf("connect admin database: %w", err)
	}

	if _, err := adminDB.Exec(fmt.Sprintf(`create schema "%s"`, schemaName)); err != nil {
		if closeErr := adminDB.Close(); closeErr != nil {
			return nil, "", fmt.Errorf("create schema: %v; close admin database: %w", err, closeErr)
		}
		return nil, "", fmt.Errorf("create schema: %w", err)
	}

	migrationDB, err := sqlx.Connect("postgres", dbTestDSN("sslmode=disable search_path="+schemaName))
	if err != nil {
		if dropErr := dropDBTestSchema(adminDB, schemaName); dropErr != nil {
			return nil, "", fmt.Errorf("connect migration database: %v; drop schema: %w", err, dropErr)
		}
		if closeErr := adminDB.Close(); closeErr != nil {
			return nil, "", fmt.Errorf("connect migration database: %v; close admin database: %w", err, closeErr)
		}
		return nil, "", fmt.Errorf("connect migration database: %w", err)
	}
	defer func() {
		if err := migrationDB.Close(); err != nil {
			log.Printf("db tests: close migration connection: %v", err)
		}
	}()

	migrationSQL, err := os.ReadFile(initialSchemaMigrationPath())
	if err != nil {
		if dropErr := dropDBTestSchema(adminDB, schemaName); dropErr != nil {
			return nil, "", fmt.Errorf("read initial schema migration: %v; drop schema: %w", err, dropErr)
		}
		if closeErr := adminDB.Close(); closeErr != nil {
			return nil, "", fmt.Errorf("read initial schema migration: %v; close admin database: %w", err, closeErr)
		}
		return nil, "", fmt.Errorf("read initial schema migration: %w", err)
	}
	if _, err := migrationDB.Exec(string(migrationSQL)); err != nil {
		if dropErr := dropDBTestSchema(adminDB, schemaName); dropErr != nil {
			return nil, "", fmt.Errorf("apply initial schema migration: %v; drop schema: %w", err, dropErr)
		}
		if closeErr := adminDB.Close(); closeErr != nil {
			return nil, "", fmt.Errorf("apply initial schema migration: %v; close admin database: %w", err, closeErr)
		}
		return nil, "", fmt.Errorf("apply initial schema migration: %w", err)
	}

	return adminDB, schemaName, nil
}

func dropDBTestSchema(adminDB *sqlx.DB, schemaName string) error {
	if _, err := adminDB.Exec(fmt.Sprintf(`drop schema "%s" cascade`, schemaName)); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}
	return nil
}

func dbTestDSN(connectionParams string) string {
	return "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password " + connectionParams
}

func initialSchemaMigrationPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "../../migrations/db/public/structure/V20260803043058__initial_schema.sql"
	}
	return filepath.Join(
		filepath.Dir(filename),
		"..",
		"..",
		"migrations",
		"db",
		"public",
		"structure",
		"V20260803043058__initial_schema.sql",
	)
}

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

func Test_should_return_existing_user_when_create_hits_external_ref_conflict(t *testing.T) {
	// given
	originalID := uuid.New()
	externalRef := uuid.NewString()
	originalTime := time.Now().UTC()
	original, err := testDB.Users.Create(context.Background(), models.User{
		ID:          originalID,
		ExternalRef: types.ExternalRef{Source: "github", Value: externalRef},
		Email:       "old@example.com",
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
		CreatedAt:   newTime,
		UpdatedAt:   newTime,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, originalID, result.ID)
	assert.Equal(t, "old@example.com", result.Email)
	assert.Equal(t, original.CreatedAt.Unix(), result.CreatedAt.Unix())
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
