package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
)

const mutationsMigrationPrefix = "V20261023090200__"

func Test_should_not_create_revision_when_rcsb_data_is_unchanged_after_mutations_migration(t *testing.T) {
	// given
	cases := []struct {
		name   string
		stored string
		rcsb   string
	}{
		{"substitutions only", `["C54T","C97A"]`, "C54T, C97A"},
		{"non substitutions only", `["ENGINEERED MUTATION","DELETION"]`, "ENGINEERED MUTATION, DELETION"},
		{"mixed", `["C54T","ENGINEERED MUTATION"]`, "C54T, ENGINEERED MUTATION"},
		{"mixed with semicolons", `["C54T","DELETION","K7R"]`, "C54T;DELETION;K7R"},
		{"duplicates", `["C54T","C54T","X"]`, "C54T, C54T, X"},
		{"empty list", `[]`, ""},
		{"string of substitutions", `"C54T, C97A"`, "C54T, C97A"},
		{"string with non substitution", `"C54T;DELETION,A(-3)G, K7R"`, "C54T;DELETION,A(-3)G, K7R"},
		{"empty string", `""`, ""},
	}
	database := newDatabaseMigratedThroughMutations(t)
	sequenceID := uuid.New()
	entityIDs := make([]uuid.UUID, len(cases))
	for index, testCase := range cases {
		entityIDs[index] = uuid.New()
		_, err := database.Exec(
			`insert into polymer_entities(id, protein_sequence_id, metadata) values ($1, $2, jsonb_build_object('label_entity_id', '1')) `,
			entityIDs[index], sequenceID)
		require.NoError(t, err, testCase.name)
		_, err = database.Exec(`update polymer_entities set metadata = metadata || jsonb_build_object('mutations', $2::jsonb) where id = $1`,
			entityIDs[index], testCase.stored)
		require.NoError(t, err, testCase.name)
	}
	applyMutationsMigration(t, database)

	for index, testCase := range cases {
		var storedMetadata models.PolymerEntityMetadata
		var storedJSON []byte
		require.NoError(t, database.Get(&storedJSON, `select metadata from polymer_entities where id = $1`, entityIDs[index]))
		require.NoError(t, storedMetadata.UnmarshalJSON(storedJSON), testCase.name)
		desired := polymerEntityMetadataFromRCSB(rcsb.PolymerEntityDetails{
			Entity:      rcsb.PolymerEntityData{Mutation: testCase.rcsb},
			Identifiers: rcsb.PolymerEntityContainerIdentifiers{EntityID: "1"},
		}, "fallback", nil)

		// when
		same := polymerEntitiesHaveSameData(
			[]models.PolymerEntity{{ProteinSequenceID: sequenceID, Metadata: storedMetadata}},
			[]polymerEntitySnapshot{{ProteinSequenceID: sequenceID, Metadata: desired}},
		)

		// then
		assert.True(t, same, "%s: stored %s", testCase.name, storedJSON)
		for _, mutation := range storedMetadata.Mutations {
			assert.True(t, models.IsMutationSubstitution(mutation), "%s: %q", testCase.name, mutation)
		}
	}
}

func newDatabaseMigratedThroughMutations(t *testing.T) *sqlx.DB {
	t.Helper()
	schemaName := "jobs_migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	adminDatabase, err := sqlx.Connect("postgres", "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password sslmode=disable")
	require.NoError(t, err)
	_, err = adminDatabase.Exec(fmt.Sprintf(`create schema "%s"`, schemaName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := adminDatabase.Exec(fmt.Sprintf(`drop schema "%s" cascade`, schemaName))
		assert.NoError(t, dropErr)
		assert.NoError(t, adminDatabase.Close())
	})
	database, err := sqlx.Connect("postgres", "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password sslmode=disable search_path="+schemaName)
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })

	for _, path := range migrationPaths(t) {
		if strings.HasPrefix(filepath.Base(path), mutationsMigrationPrefix) {
			break
		}
		migrationSQL, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = database.Exec(string(migrationSQL))
		require.NoError(t, err, "apply %s", filepath.Base(path))
	}
	_, err = database.Exec(`set session_replication_role = replica`)
	require.NoError(t, err)
	return database
}

func applyMutationsMigration(t *testing.T, database *sqlx.DB) {
	t.Helper()
	for _, path := range migrationPaths(t) {
		if strings.HasPrefix(filepath.Base(path), mutationsMigrationPrefix) {
			migrationSQL, err := os.ReadFile(path)
			require.NoError(t, err)
			_, err = database.Exec(string(migrationSQL))
			require.NoError(t, err, "apply %s", filepath.Base(path))
			return
		}
	}
	require.Fail(t, "mutations migration not found")
}

func migrationPaths(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "migrations", "db", "public", "structure", "V*.sql"))
	require.NoError(t, err)
	sort.Strings(paths)
	return paths
}
