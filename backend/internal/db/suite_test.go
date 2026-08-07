package db_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"dynamic-pdb/backend/internal/db"
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

	migrationPaths, err := schemaMigrationPaths()
	if err != nil {
		if dropErr := dropDBTestSchema(adminDB, schemaName); dropErr != nil {
			return nil, "", fmt.Errorf("list schema migrations: %v; drop schema: %w", err, dropErr)
		}
		if closeErr := adminDB.Close(); closeErr != nil {
			return nil, "", fmt.Errorf("list schema migrations: %v; close admin database: %w", err, closeErr)
		}
		return nil, "", fmt.Errorf("list schema migrations: %w", err)
	}
	for _, migrationPath := range migrationPaths {
		migrationSQL, err := os.ReadFile(migrationPath)
		if err != nil {
			if dropErr := dropDBTestSchema(adminDB, schemaName); dropErr != nil {
				return nil, "", fmt.Errorf("read schema migration %s: %v; drop schema: %w", migrationPath, err, dropErr)
			}
			if closeErr := adminDB.Close(); closeErr != nil {
				return nil, "", fmt.Errorf("read schema migration %s: %v; close admin database: %w", migrationPath, err, closeErr)
			}
			return nil, "", fmt.Errorf("read schema migration %s: %w", migrationPath, err)
		}
		if _, err := migrationDB.Exec(string(migrationSQL)); err != nil {
			if dropErr := dropDBTestSchema(adminDB, schemaName); dropErr != nil {
				return nil, "", fmt.Errorf("apply schema migration %s: %v; drop schema: %w", migrationPath, err, dropErr)
			}
			if closeErr := adminDB.Close(); closeErr != nil {
				return nil, "", fmt.Errorf("apply schema migration %s: %v; close admin database: %w", migrationPath, err, closeErr)
			}
			return nil, "", fmt.Errorf("apply schema migration %s: %w", migrationPath, err)
		}
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

func schemaMigrationPaths() ([]string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Glob("../../migrations/db/public/structure/V*.sql")
	}
	pattern := filepath.Join(
		filepath.Dir(filename),
		"..",
		"..",
		"migrations",
		"db",
		"public",
		"structure",
		"V*.sql",
	)
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob schema migrations: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}
