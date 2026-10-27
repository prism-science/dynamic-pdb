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

func Test_should_use_default_checksum_settings_when_config_has_no_checksum_section(t *testing.T) {
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
	assert.Equal(t, 4, cfg.Checksum.Concurrency)
	assert.Equal(t, 2, cfg.Checksum.PerHostConcurrency)
	assert.Equal(t, 50, cfg.Checksum.BatchSize)
	assert.Equal(t, 0, cfg.Checksum.MaxFilesPerRun)
	assert.Equal(t, 50*time.Minute, cfg.Checksum.ClaimFor)
	assert.Equal(t, 20*time.Minute, cfg.Checksum.FileTimeout)
	assert.True(t, cfg.Checksum.CopyExternal)
	assert.Equal(t, int64(1<<30), cfg.Checksum.CopyLimitBytes)
	assert.Equal(t, "dynamic-pdb", cfg.CDN.S3.Bucket)
}

func Test_should_take_checksum_settings_from_environment_when_variables_are_set(t *testing.T) {
	// given
	writeTestConfig(t, `
db:
  host: localhost
`)
	t.Setenv("DYNAMIC_PDB_CHECKSUM_MAX_FILES_PER_RUN", "1000")
	t.Setenv("DYNAMIC_PDB_CHECKSUM_COPY_EXTERNAL", "false")

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, 1000, cfg.Checksum.MaxFilesPerRun)
	assert.False(t, cfg.Checksum.CopyExternal)
}

func Test_should_read_bucket_and_cloudfront_when_production_config_read(t *testing.T) {
	// given
	t.Chdir(filepath.Join("..", ".."))

	// when
	cfg, err := readConfig("production")

	// then
	require.NoError(t, err)
	assert.Equal(t, "dynamic-pdb-data", cfg.CDN.S3.Bucket)
	assert.Equal(t, "https://files.dynamicpdb.com", cfg.CDN.CloudFront.BaseURL)
}

func Test_should_return_local_env_when_dynamic_pdb_env_is_empty(t *testing.T) {
	// given
	t.Setenv("DYNAMIC_PDB_ENV", "")

	// when
	env := envFromEnvironment()

	// then
	assert.Equal(t, "local", env)
}
