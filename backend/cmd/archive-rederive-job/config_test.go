package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T, contents string) {
	t.Helper()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.Mkdir(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "test.yml"), []byte(contents), 0o644))
	t.Chdir(tmpDir)
}

func Test_should_use_default_settings_when_config_has_no_archive_rederive_section(t *testing.T) {
	// given
	writeTestConfig(t, `
db:
  host: localhost
`)

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.ArchiveRederive.MaxEntriesPerRun)
	assert.Equal(t, 200, cfg.ArchiveRederive.PageSize)
	assert.Equal(t, 4, cfg.ArchiveRederive.DownloadConcurrency)
	assert.Equal(t, 50*time.Minute, cfg.ArchiveRederive.RunFor)
	assert.False(t, cfg.ArchiveRederive.DryRun)
	assert.Equal(t, "localhost", cfg.DB.Host)
}

func Test_should_take_settings_from_environment_when_variables_are_set(t *testing.T) {
	// given
	// The environment overrides only keys viper knows, so the timeout is in the file, as in the
	// backend's own configs.
	writeTestConfig(t, `
db:
  host: localhost
  statement_timeout: 10s
`)
	t.Setenv("DYNAMIC_PDB_ARCHIVE_REDERIVE_MAX_ENTRIES_PER_RUN", "5000")
	t.Setenv("DYNAMIC_PDB_ARCHIVE_REDERIVE_DRY_RUN", "true")
	t.Setenv("DYNAMIC_PDB_DB_STATEMENT_TIMEOUT", "30s")

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, 5000, cfg.ArchiveRederive.MaxEntriesPerRun)
	assert.True(t, cfg.ArchiveRederive.DryRun)
	assert.Equal(t, 30*time.Second, cfg.DB.StatementTimeout)
}

func Test_should_read_production_config(t *testing.T) {
	// given
	t.Chdir(filepath.Join("..", ".."))

	// when
	cfg, err := readConfig("production")

	// then
	require.NoError(t, err)
	assert.Equal(t, 200, cfg.ArchiveRederive.PageSize)
}

func Test_should_return_local_env_when_dynamic_pdb_env_is_empty(t *testing.T) {
	// given
	t.Setenv("DYNAMIC_PDB_ENV", "")

	// when
	env := envFromEnvironment()

	// then
	assert.Equal(t, "local", env)
}
