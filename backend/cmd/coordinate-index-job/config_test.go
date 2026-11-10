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

func Test_should_use_default_settings_when_config_has_no_coordinate_index_section(t *testing.T) {
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
	assert.Equal(t, 0, cfg.CoordinateIndex.MaxFilesPerRun)
	assert.Equal(t, 4*time.Minute, cfg.CoordinateIndex.ClaimFor)
	assert.Equal(t, 10*time.Minute, cfg.CoordinateIndex.FileTimeout)
	assert.Equal(t, 48*time.Hour, cfg.CoordinateIndex.RecentWindow)
	assert.False(t, cfg.CoordinateIndex.Fill)
	assert.Equal(t, 2000, cfg.CoordinateIndex.FillBatchSize)
	assert.False(t, cfg.CoordinateIndex.ArchiveFiles)
	assert.Equal(t, "localhost", cfg.DB.Host)
}

func Test_should_read_the_switches_from_the_section_and_the_environment_when_both_set_them(t *testing.T) {
	// given: the section turns archive files on; the CronJob's environment turns the fill on and
	// caps the run.
	writeTestConfig(t, `
db:
  host: localhost
coordinate_index:
  archive_files: true
`)
	t.Setenv("DYNAMIC_PDB_COORDINATE_INDEX_FILL", "true")
	t.Setenv("DYNAMIC_PDB_COORDINATE_INDEX_MAX_FILES_PER_RUN", "500")

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.True(t, cfg.CoordinateIndex.ArchiveFiles)
	assert.True(t, cfg.CoordinateIndex.Fill)
	assert.Equal(t, 500, cfg.CoordinateIndex.MaxFilesPerRun)
}

func Test_should_turn_archive_files_on_in_dev_and_production_only_when_the_repository_configs_are_read(t *testing.T) {
	for env, want := range map[string]bool{"local": false, "dev": true, "production": true} {
		t.Run(env, func(t *testing.T) {
			// given
			t.Chdir("../..")

			// when
			cfg, err := readConfig(env)

			// then
			require.NoError(t, err)
			assert.Equal(t, want, cfg.CoordinateIndex.ArchiveFiles)
			assert.False(t, cfg.CoordinateIndex.Fill, "the fill is turned on in deploy/values.yaml only")
		})
	}
}
