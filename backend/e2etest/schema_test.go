package e2etest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

// localDatabaseDSN reaches the shared local database, in no particular schema.
const localDatabaseDSN = "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password sslmode=disable"

// migrationsGlob matches the backend's structure migrations, from this package's directory.
const migrationsGlob = "../migrations/db/public/structure/V*.sql"

var (
	// testSchemaConnectionParams put a session in the schema this run works in. Other worktrees
	// run these tests against the same local database at the same time, and an active PDB ID is
	// unique, so each run keeps its rows to a schema of its own. TestMain sets them.
	testSchemaConnectionParams string
	// rawTestDatabaseDSN reaches the run's schema outside any server's pool. TestMain sets it.
	rawTestDatabaseDSN string
)

// createTestSchema creates a schema with the backend's structure migrations applied and returns
// its name and a function that drops it. When it fails, it drops the schema itself. A run that
// stops before TestMain drops the schema leaves it behind; its name starts with e2etest_run_.
func createTestSchema(ctx context.Context) (schema string, drop func() error, err error) {
	admin, err := sql.Open("postgres", localDatabaseDSN)
	if err != nil {
		return "", nil, fmt.Errorf("open database: %w", err)
	}
	schema = "e2etest_run_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	drop = func() error {
		_, dropErr := admin.ExecContext(context.Background(), fmt.Sprintf(`drop schema "%s" cascade`, schema))
		if dropErr != nil {
			dropErr = fmt.Errorf("drop schema %s: %w", schema, dropErr)
		}
		return errors.Join(dropErr, admin.Close())
	}
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`create schema "%s"`, schema)); err != nil {
		return "", nil, errors.Join(fmt.Errorf("create schema %s: %w", schema, err), admin.Close())
	}
	if err := migrateSchema(ctx, schema); err != nil {
		return "", nil, errors.Join(err, drop())
	}
	return schema, drop, nil
}

// migrateSchema applies the backend's structure migrations, in order, to schema.
func migrateSchema(ctx context.Context, schema string) (err error) {
	conn, err := sql.Open("postgres", localDatabaseDSN+" search_path="+schema)
	if err != nil {
		return fmt.Errorf("open schema %s: %w", schema, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close schema %s: %w", schema, closeErr))
		}
	}()
	paths, err := filepath.Glob(migrationsGlob)
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(paths) == 0 {
		return fmt.Errorf("list migrations: none match %s", migrationsGlob)
	}
	sort.Strings(paths)
	for _, path := range paths {
		migration, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration: %w", err)
		}
		if _, err := conn.ExecContext(ctx, string(migration)); err != nil {
			return fmt.Errorf("apply %s to schema %s: %w", filepath.Base(path), schema, err)
		}
	}
	return nil
}
