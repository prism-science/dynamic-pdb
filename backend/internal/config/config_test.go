package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/config"
)

const sampleYAML = `
auth:
  allowed_orgs:
    - org-a
    - org-b
  github:
    client_id: file-client-id
    client_secret: file-client-secret
  jwt:
    secret: file-secret
    issuer: dynamic-pdb-test
    ttl: 24h
db:
  host: localhost
  port: 35432
  name: dynamic_pdb_local
  username: postgres
  password: password
  connection_params: sslmode=disable
s3:
  region: us-west-2
  bucket: dynamic-pdb-test
  access_key_id: file-access-key
  secret_access_key: file-secret-key
`

func writeSampleConfig(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.Mkdir(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "test.yml"), []byte(sampleYAML), 0o644))
	return tmpDir
}

func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.Mkdir(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, name+".yml"), []byte(content), 0o644))
	return tmpDir
}

func Test_should_read_config_from_yaml_file(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, []string{"org-a", "org-b"}, cfg.Auth.AllowedOrgs)
	assert.Equal(t, "file-client-id", cfg.Auth.GitHub.ClientID)
	assert.Equal(t, "file-client-secret", cfg.Auth.GitHub.ClientSecret)
	assert.Equal(t, "file-secret", cfg.Auth.JWT.Secret)
	assert.Equal(t, "dynamic-pdb-test", cfg.Auth.JWT.Issuer)
	assert.Equal(t, 24*time.Hour, cfg.Auth.JWT.TTL)
	assert.Equal(t, "dynamic_pdb_local", cfg.DB.Name)
	assert.Equal(t, "us-west-2", cfg.S3.Region)
	assert.Equal(t, "dynamic-pdb-test", cfg.S3.Bucket)
}

func Test_should_override_jwt_secret_from_env_var(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_AUTH_JWT_SECRET", "env-secret")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "env-secret", cfg.Auth.JWT.Secret)
}

func Test_should_override_github_client_secret_from_env_var(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_AUTH_GITHUB_CLIENT_SECRET", "env-github-secret")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "env-github-secret", cfg.Auth.GitHub.ClientSecret)
}

func Test_should_override_s3_bucket_from_env_var(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_S3_BUCKET", "env-bucket")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "env-bucket", cfg.S3.Bucket)
}

func Test_should_reject_config_when_auth_secrets_are_missing(t *testing.T) {
	// given
	t.Chdir(writeConfig(t, "test", `
auth:
  allowed_orgs:
    - org-a
  github:
    client_id: file-client-id
    client_secret: ""
  jwt:
    secret: ""
    issuer: dynamic-pdb-backend
    ttl: 24h
`))

	// when
	_, err := config.ReadFromFile("test")

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "auth.github.client_secret is required")
}

func Test_should_reject_config_when_jwt_secret_is_missing(t *testing.T) {
	// given
	t.Chdir(writeConfig(t, "test", `
auth:
  allowed_orgs:
    - org-a
  github:
    client_id: file-client-id
    client_secret: ""
  jwt:
    secret: ""
    issuer: dynamic-pdb-backend
    ttl: 24h
`))
	t.Setenv("DYNAMIC_PDB_AUTH_GITHUB_CLIENT_SECRET", "env-github-secret")

	// when
	_, err := config.ReadFromFile("test")

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "auth.jwt.secret is required")
}

func Test_should_accept_auth_secrets_from_env_vars(t *testing.T) {
	// given
	t.Chdir(writeConfig(t, "test", `
auth:
  allowed_orgs:
    - org-a
  github:
    client_id: file-client-id
    client_secret: ""
  jwt:
    secret: ""
    issuer: dynamic-pdb-backend
    ttl: 24h
`))
	t.Setenv("DYNAMIC_PDB_AUTH_GITHUB_CLIENT_SECRET", "env-github-secret")
	t.Setenv("DYNAMIC_PDB_AUTH_JWT_SECRET", "env-jwt-secret")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "env-github-secret", cfg.Auth.GitHub.ClientSecret)
	assert.Equal(t, "env-jwt-secret", cfg.Auth.JWT.Secret)
}

func Test_should_read_repository_local_config(t *testing.T) {
	// given
	t.Chdir(filepath.Join("..", ".."))

	// when
	cfg, err := config.ReadFromFile("local")

	// then
	require.NoError(t, err)
	assert.Equal(t, "local-github-client-id", cfg.Auth.GitHub.ClientID)
	assert.Equal(t, "local-github-client-secret", cfg.Auth.GitHub.ClientSecret)
	assert.Equal(t, "sample-secret", cfg.Auth.JWT.Secret)
	assert.Equal(t, "dynamic_pdb_local", cfg.DB.Name)
	assert.Equal(t, "dynamic-pdb", cfg.S3.Bucket)
}

func Test_should_reject_repository_production_config_without_auth_secret_env_vars(t *testing.T) {
	// given
	t.Chdir(filepath.Join("..", ".."))
	t.Setenv("DYNAMIC_PDB_AUTH_GITHUB_CLIENT_SECRET", "")
	t.Setenv("DYNAMIC_PDB_AUTH_JWT_SECRET", "")

	// when
	_, err := config.ReadFromFile("production")

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "auth.github.client_secret is required")
}

func Test_should_read_repository_production_config_with_auth_secrets_from_env_vars(t *testing.T) {
	// given
	t.Chdir(filepath.Join("..", ".."))
	t.Setenv("DYNAMIC_PDB_AUTH_GITHUB_CLIENT_SECRET", "env-github-secret")
	t.Setenv("DYNAMIC_PDB_AUTH_JWT_SECRET", "env-jwt-secret")

	// when
	cfg, err := config.ReadFromFile("production")

	// then
	require.NoError(t, err)
	assert.Equal(t, "env-github-secret", cfg.Auth.GitHub.ClientSecret)
	assert.Equal(t, "env-jwt-secret", cfg.Auth.JWT.Secret)
}
