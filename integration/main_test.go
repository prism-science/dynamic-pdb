package integration

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// runSchema is the schema this test process works in. TestMain creates it from the backend's
// migrations and drops it at the end. Other worktrees run these tests against the same local
// database at the same time, and every backend a test starts syncs every due data-sync job it
// can see, so each run keeps its rows, jobs and PDB IDs to a schema of its own.
var runSchema string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() || os.Getenv(integrationRunEnv) != "1" {
		// Every test skips, so none needs the database.
		os.Exit(m.Run())
	}
	os.Exit(runInOwnSchema(m))
}

// runInOwnSchema creates runSchema, runs the tests in it and drops it.
func runInOwnSchema(m *testing.M) int {
	ctx := context.Background()
	root, err := sourceRepoRoot()
	if err != nil {
		log.Printf("integration: %v", err)
		return 1
	}
	admin, err := sql.Open("postgres", dbDSN("sslmode=disable"))
	if err != nil {
		log.Printf("integration: open database: %v", err)
		return 1
	}
	defer func() {
		if err := admin.Close(); err != nil {
			log.Printf("integration: close database: %v", err)
		}
	}()

	schema := newSchemaName("integration_run_")
	if _, err := admin.ExecContext(ctx, fmt.Sprintf(`create schema "%s"`, schema)); err != nil {
		log.Printf("integration: create schema %s: %v", schema, err)
		return 1
	}
	defer func() {
		if _, err := admin.ExecContext(ctx, fmt.Sprintf(`drop schema "%s" cascade`, schema)); err != nil {
			log.Printf("integration: drop schema %s: %v", schema, err)
		}
	}()
	if err := migrateSchema(ctx, root, schema); err != nil {
		log.Printf("integration: %v", err)
		return 1
	}

	runSchema = schema
	return m.Run()
}

// runConnectionParams are the connection parameters of runSchema, for test connections and for
// every backend or job process a test starts.
func runConnectionParams() string {
	if runSchema == "" {
		panic("integration: TestMain has not created the run's schema")
	}
	return schemaConnectionParams(runSchema)
}

// schemaConnectionParams are the connection parameters that put a session in schema.
func schemaConnectionParams(schema string) string {
	return "sslmode=disable search_path=" + schema
}

// newSchemaName returns prefix and a random suffix, a schema name no other run uses.
func newSchemaName(prefix string) string {
	return prefix + strings.ReplaceAll(uuid.NewString(), "-", "_")
}

// migrateSchema applies the backend's structure migrations, in order, to schema, which must exist.
func migrateSchema(ctx context.Context, root string, schema string) (err error) {
	conn, err := sql.Open("postgres", dbDSN(schemaConnectionParams(schema)))
	if err != nil {
		return fmt.Errorf("open schema %s: %w", schema, err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close schema %s: %w", schema, closeErr))
		}
	}()
	paths, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "db", "public", "structure", "V*.sql"))
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(paths) == 0 {
		return errors.New("list migrations: none found")
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

// sourceRepoRoot returns the repository root, found from this file's path.
func sourceRepoRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("find repository root: no caller information")
	}
	return filepath.Dir(filepath.Dir(filename)), nil
}
