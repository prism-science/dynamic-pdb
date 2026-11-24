package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_share_the_downloads_limits_with_the_server_when_a_profile_is_read(t *testing.T) {
	for _, profile := range []string{"local", "dev", "production"} {
		t.Run(profile, func(t *testing.T) {
			// given the backend's own config folder
			t.Chdir("../..")

			// when
			cfg, err := readConfig(profile)

			// then
			require.NoError(t, err)
			assert.Equal(t, 10*time.Minute, cfg.Worker.Lease)
			assert.Equal(t, time.Minute, cfg.Worker.Heartbeat)
			assert.Equal(t, "downloads/", cfg.Worker.ArchivePrefix)
			assert.Equal(t, 3, cfg.Worker.MaxAttempts)
			assert.Equal(t, 3, cfg.Worker.FileReadAttempts)
			assert.Equal(t, 5, cfg.Worker.MaxConsecutiveReadFailures)
			assert.Equal(t, 20*time.Minute, cfg.Worker.FileTimeout)
			assert.Equal(t, 4*time.Hour, cfg.Worker.JobTimeout)
			assert.Equal(t, 7*24*time.Hour, cfg.Worker.ArchiveTTL, "the worker offers archives as long as the API says")
			assert.Equal(t, int64(10_000_000_000), cfg.Downloads.MaxArchiveBytes)
		})
	}
}

func Test_should_use_the_scratch_folder_from_env_var_when_it_is_set(t *testing.T) {
	// given
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "test.yml"), []byte("db:\n  host: localhost\n"), 0o644))
	t.Chdir(dir)
	t.Setenv("DYNAMIC_PDB_ARCHIVE_WORKER_SCRATCH_DIR", "/scratch")

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "/scratch", cfg.Worker.ScratchDir)
}

func Test_should_refuse_to_start_when_a_downloads_limit_is_not_positive(t *testing.T) {
	// given
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config", "test.yml"),
		[]byte("downloads:\n  archive_ttl: 0s\n"), 0o644))
	t.Chdir(dir)

	// when
	_, err := readConfig("test")

	// then
	require.ErrorContains(t, err, "downloads.archive_ttl must be positive")
}
