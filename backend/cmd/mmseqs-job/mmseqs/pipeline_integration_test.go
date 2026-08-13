package mmseqs

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

func Test_should_recalculate_protein_sequence_similarities_when_mmseqs_pipeline_run(t *testing.T) {
	// given
	ctx := context.Background()
	database := setupPipelineTestDB(t)
	cacheDir := t.TempDir()
	mmseqsBinary := createFakeMMseqsBinary(t)
	commands, err := NewCommands(mmseqsBinary)
	require.NoError(t, err)

	now := time.Now().UTC()
	entryRevision := createPipelineTestEntryRevision(t, database, "mmseqs-pipeline-entry", now)
	artifact := createPipelineTestArtifact(t, database, entryRevision.CreatedBy, "mmseqs-pipeline-fasta", now)
	records := []models.FASTARecord{
		{Header: "first", Sequence: "ACDEFGHIKLMNPQRSTVWY"},
		{Header: "second", Sequence: "ACDEFGHIKLMNPQRSTVWF"},
	}
	require.NoError(t, database.ProteinSequences.Create(ctx, entryRevision.ID, artifact.ID, records))

	pipeline, err := NewPipeline(database, commands, nil, cacheDir)
	require.NoError(t, err)

	// when
	err = pipeline.Run(ctx)

	// then
	require.NoError(t, err)
	sequences, err := database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{EntryRevisionID: &entryRevision.ID})
	require.NoError(t, err)
	require.Len(t, sequences, 2)
	assert.Equal(t, models.ProteinSequenceProcessingStateProcessed, sequences[0].ProcessingState)
	assert.Equal(t, models.ProteinSequenceProcessingStateProcessed, sequences[1].ProcessingState)

	similarities, err := database.ProteinSequenceSimilarities.List(ctx, db.ProteinSequenceSimilarityFilters{
		SourceSequenceID: &sequences[0].ID,
	})
	require.NoError(t, err)
	require.Len(t, similarities, 1)
	assert.Equal(t, sequences[1].ID, similarities[0].SimilarSequenceID)
	assert.Equal(t, "mmseqs2", similarities[0].Tool)
	assert.Equal(t, 1.0, similarities[0].Score)
	assert.Equal(t, 0.9, similarities[0].Metadata["fident"])
	assert.Equal(t, "ACDEFGHIKLMNPQRSTVWY", similarities[0].Metadata["qaln"])
	assert.Equal(t, "ACDEFGHIKLMNPQRSTVWF", similarities[0].Metadata["taln"])

	succeededState := models.ProteinSequenceSimilarityRunStateSucceeded
	runs, err := database.ProteinSequenceSimilarities.ListRuns(ctx, db.ProteinSequenceSimilarityRunFilters{State: &succeededState})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, PipelineModeBootstrap, runMode(runs[0]))

	state, err := ReadIndexState(cacheDir)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, state.LatestCompletedRunID)
	assert.Equal(t, runs[0].ID, *state.LatestCompletedRunID)
	assert.FileExists(t, filepath.Join(cacheDir, "runs", runs[0].ID.String(), "similarity-index", "index"))

	// when
	err = pipeline.Run(ctx)

	// then
	require.NoError(t, err)
	runsAfterSecondRun, err := database.ProteinSequenceSimilarities.ListRuns(ctx, db.ProteinSequenceSimilarityRunFilters{State: &succeededState})
	require.NoError(t, err)
	assert.Len(t, runsAfterSecondRun, 1)
}

func setupPipelineTestDB(t *testing.T) *db.DB {
	t.Helper()

	adminDB, schemaName := setupPipelineTestSchema(t)
	database, err := db.NewDB(db.Config{
		Host:               "localhost",
		Port:               35432,
		Name:               "dynamic_pdb_local",
		Username:           "postgres",
		Password:           "password",
		ConnectionParams:   "sslmode=disable search_path=" + schemaName,
		MaxOpenConnections: 1,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, database.Close())
		require.NoError(t, dropPipelineTestSchema(adminDB, schemaName))
		require.NoError(t, adminDB.Close())
	})
	return database
}

func setupPipelineTestSchema(t *testing.T) (*sqlx.DB, string) {
	t.Helper()

	schemaName := "mmseqs_pipeline_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	adminDB, err := sqlx.Connect("postgres", pipelineTestDSN("sslmode=disable"))
	require.NoError(t, err)

	_, err = adminDB.Exec(fmt.Sprintf(`create schema "%s"`, schemaName))
	require.NoError(t, err)

	migrationDB, err := sqlx.Connect("postgres", pipelineTestDSN("sslmode=disable search_path="+schemaName))
	require.NoError(t, err)
	defer func() {
		if err := migrationDB.Close(); err != nil {
			log.Printf("close pipeline migration database: %v", err)
		}
	}()

	migrationPaths := pipelineTestMigrationPaths(t)
	for _, migrationPath := range migrationPaths {
		migrationSQL, err := os.ReadFile(migrationPath)
		require.NoError(t, err)
		_, err = migrationDB.Exec(string(migrationSQL))
		require.NoError(t, err, "apply migration %s", migrationPath)
	}
	return adminDB, schemaName
}

func dropPipelineTestSchema(adminDB *sqlx.DB, schemaName string) error {
	if _, err := adminDB.Exec(fmt.Sprintf(`drop schema "%s" cascade`, schemaName)); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}
	return nil
}

func pipelineTestDSN(connectionParams string) string {
	return "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password " + connectionParams
}

func pipelineTestMigrationPaths(t *testing.T) []string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	pattern := filepath.Join(
		filepath.Dir(filename),
		"..",
		"..",
		"..",
		"migrations",
		"db",
		"public",
		"structure",
		"V*.sql",
	)
	paths, err := filepath.Glob(pattern)
	require.NoError(t, err)
	sort.Strings(paths)
	return paths
}

func createPipelineTestUser(t *testing.T, database *db.DB) uuid.UUID {
	t.Helper()

	now := time.Now().UTC()
	user, err := database.Users.Create(context.Background(), models.User{
		ID: uuid.New(),
		ExternalRef: types.ExternalRef{
			Source: "test",
			Value:  uuid.NewString(),
		},
		Email:       "mmseqs-pipeline-" + uuid.NewString() + "@example.com",
		DisplayName: "MMseqs Pipeline Test User",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	return user.ID
}

func createPipelineTestEntryRevision(t *testing.T, database *db.DB, name string, createdAt time.Time) *models.EntryRevision {
	t.Helper()

	createdBy := createPipelineTestUser(t, database)
	revision, err := database.Entries.Create(context.Background(), models.EntryRevision{
		ID:        uuid.New(),
		EntryID:   uuid.New(),
		State:     models.RevisionStatePending,
		Name:      name + "-" + uuid.NewString(),
		CreatedBy: createdBy,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return revision
}

func createPipelineTestArtifact(
	t *testing.T,
	database *db.DB,
	createdBy uuid.UUID,
	name string,
	createdAt time.Time,
) *models.Artifact {
	t.Helper()

	format := "fasta"
	artifact, err := database.Artifacts.Create(context.Background(), models.Artifact{
		ID:        uuid.New(),
		Name:      name + "-" + uuid.NewString(),
		Level:     models.ArtifactLevelL2,
		Format:    &format,
		Metadata:  models.FASTAMetadata{},
		CreatedBy: createdBy,
		CreatedAt: createdAt,
	})
	require.NoError(t, err)
	return artifact
}

func createFakeMMseqsBinary(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mmseqs")
	script := `#!/bin/sh
set -eu
command="$1"
shift
case "$command" in
  version)
    echo "fake-mmseqs"
    ;;
  createdb)
    fasta_path="$1"
    database_path="$2"
    mkdir -p "$(dirname "$database_path")"
    : > "$database_path"
    awk '/^>/ { sub(/^>/, ""); print $1 }' "$fasta_path" > "$database_path.ids"
    cp "$database_path.ids" "$database_path.index"
    ;;
  createindex)
    database_path="$1"
    tmp_dir="$2"
    mkdir -p "$tmp_dir"
    : > "$database_path.lookup"
    ;;
  concatdbs)
    first_database_path="$1"
    second_database_path="$2"
    output_database_path="$3"
    mkdir -p "$(dirname "$output_database_path")"
    : > "$output_database_path"
    cat "$first_database_path.ids" "$second_database_path.ids" > "$output_database_path.ids"
    cp "$output_database_path.ids" "$output_database_path.index"
    ;;
  easy-search)
    query_fasta_path="$1"
    target_database_path="$2"
    result_path="$3"
    tmp_dir="$4"
    mkdir -p "$(dirname "$result_path")" "$tmp_dir"
    awk '/^>/ { sub(/^>/, ""); print $1 }' "$query_fasta_path" > "$tmp_dir/query.ids"
    : > "$result_path"
    while IFS= read -r query_id; do
      while IFS= read -r target_id; do
        printf '%s\t%s\t0.9\t1\t1\t1e-20\t100\t20\t1\t20\t1\t20\tACDEFGHIKLMNPQRSTVWY\tACDEFGHIKLMNPQRSTVWF\n' "$query_id" "$target_id" >> "$result_path"
      done < "$target_database_path.ids"
    done < "$tmp_dir/query.ids"
    ;;
  *)
    echo "unexpected fake mmseqs command: $command" >&2
    exit 1
    ;;
esac
`
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}
