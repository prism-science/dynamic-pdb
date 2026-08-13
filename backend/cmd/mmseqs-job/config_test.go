package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_mmseqs_job_db_config_when_backend_auth_secrets_are_missing(t *testing.T) {
	// given
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.Mkdir(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "test.yml"), []byte(`
auth:
  github:
    client_secret: ""
  jwt:
    secret: ""
db:
  host: localhost
  port: 35432
  name: dynamic_pdb_local
  username: postgres
  password: password
  connection_params: sslmode=disable
`), 0o644))
	t.Chdir(tmpDir)

	// when
	cfg, err := readConfig("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "localhost", cfg.DB.Host)
	assert.Equal(t, 35432, cfg.DB.Port)
	assert.Equal(t, "dynamic_pdb_local", cfg.DB.Name)
	assert.Equal(t, "postgres", cfg.DB.Username)
	assert.Equal(t, "password", cfg.DB.Password)
	assert.Equal(t, "sslmode=disable", cfg.DB.ConnectionParams)
}
