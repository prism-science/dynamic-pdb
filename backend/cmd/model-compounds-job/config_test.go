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

func Test_should_use_default_compound_settings_when_config_has_no_compounds_section(t *testing.T) {
	// given
	writeTestConfig(t, `
db:
  host: localhost
cdn:
  s3:
    bucket: dynamic-pdb
`)

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.Compounds.MaxFilesPerRun)
	assert.Equal(t, 4*time.Minute, cfg.Compounds.ClaimFor)
	assert.Equal(t, 10*time.Minute, cfg.Compounds.FileTimeout)
	assert.Equal(t, 48*time.Hour, cfg.Compounds.RecentWindow)
	assert.False(t, cfg.Compounds.HistoricalFill)
	assert.Equal(t, 2000, cfg.Compounds.FillBatchSize)
	assert.Equal(t, "localhost", cfg.DB.Host)
}

func Test_should_read_compound_settings_from_the_environment_when_they_are_set(t *testing.T) {
	// given
	writeTestConfig(t, "db:\n  host: localhost\n")
	t.Setenv("DYNAMIC_PDB_COMPOUNDS_MAX_FILES_PER_RUN", "25")
	t.Setenv("DYNAMIC_PDB_COMPOUNDS_FILE_TIMEOUT", "2m")

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, 25, cfg.Compounds.MaxFilesPerRun)
	assert.Equal(t, 2*time.Minute, cfg.Compounds.FileTimeout)
}
