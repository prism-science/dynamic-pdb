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
	assert.False(t, cfg.Compounds.ArchiveFiles)
	assert.False(t, cfg.Compounds.ArchiveFill)
	assert.Equal(t, "localhost", cfg.DB.Host)
}

func Test_should_read_the_archive_switches_from_the_compounds_section_when_the_config_sets_them(t *testing.T) {
	// given
	writeTestConfig(t, `
db:
  host: localhost
compounds:
  archive_files: true
  archive_fill: true
`)

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.True(t, cfg.Compounds.ArchiveFiles)
	assert.True(t, cfg.Compounds.ArchiveFill)
}

func Test_should_parse_the_compounds_section_when_the_repository_configs_are_read(t *testing.T) {
	for _, env := range []string{"local", "dev", "production"} {
		t.Run(env, func(t *testing.T) {
			// given: the config files the job image carries.
			t.Chdir(filepath.Join("..", ".."))

			// when
			_, err := readConfig(env)

			// then
			require.NoError(t, err)
		})
	}
}

func Test_should_keep_the_archive_switches_off_when_the_local_config_is_read(t *testing.T) {
	// given: the config local runs and the tests use, which reads uploads only.
	t.Chdir(filepath.Join("..", ".."))

	// when
	cfg, err := readConfig("local")

	// then
	require.NoError(t, err)
	assert.False(t, cfg.Compounds.ArchiveFiles)
	assert.False(t, cfg.Compounds.ArchiveFill)
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
