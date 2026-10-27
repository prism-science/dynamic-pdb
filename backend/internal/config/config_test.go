package config_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/config"
	"dynamic-pdb/backend/internal/validation"
)

const sampleYAML = `
server:
  public_base_url: https://api.example.com/api/
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
cdn:
  s3:
    endpoint: http://localhost:9000
    region: us-west-2
    bucket: dynamic-pdb-test
    access_key_id: file-access-key
    secret_access_key: file-secret-key
    upload_max_file_size: 1073741824
    upload_url_ttl: 15m
  cloudfront:
    base_url: https://files.example.com
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
	assert.Equal(t, "https://api.example.com/api", cfg.Server.PublicBaseURL, "trailing slash is trimmed")
	assert.Equal(t, "dynamic_pdb_local", cfg.DB.Name)
	assert.Equal(t, "http://localhost:9000", cfg.CDN.S3.Endpoint)
	assert.Equal(t, "us-west-2", cfg.CDN.S3.Region)
	assert.Equal(t, "dynamic-pdb-test", cfg.CDN.S3.Bucket)
	assert.Equal(t, int64(1073741824), cfg.CDN.S3.UploadMaxFileSize)
	assert.Equal(t, 15*time.Minute, cfg.CDN.S3.UploadURLTTL)
	assert.Equal(t, "https://files.example.com", cfg.CDN.CloudFront.BaseURL)
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
	t.Setenv("DYNAMIC_PDB_CDN_S3_BUCKET", "env-bucket")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "env-bucket", cfg.CDN.S3.Bucket)
}

func Test_should_default_rate_limit_when_config_does_not_set_it(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, 100, cfg.Server.RateLimit.Requests)
	assert.Equal(t, 10*time.Second, cfg.Server.RateLimit.Window)
	assert.Equal(t, 1000, cfg.Server.RateLimit.InternalRequests)
	assert.Empty(t, cfg.Server.RateLimit.TrustedProxies)
}

func Test_should_default_validation_mode_to_log_only_when_config_does_not_set_it(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, validation.ModeLogOnly, cfg.Validation.Mode)
}

func Test_should_override_validation_mode_from_env_var(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_VALIDATION_MODE", "enforce")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, validation.ModeEnforce, cfg.Validation.Mode)
}

func Test_should_reject_config_when_validation_mode_is_unknown(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_VALIDATION_MODE", "warn")

	// when
	_, err := config.ReadFromFile("test")

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "validation.mode: unknown validation mode: warn")
}

func Test_should_override_rate_limit_from_env_vars(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_SERVER_RATE_LIMIT_REQUESTS", "5000")
	t.Setenv("DYNAMIC_PDB_SERVER_RATE_LIMIT_WINDOW", "1m")
	t.Setenv("DYNAMIC_PDB_SERVER_RATE_LIMIT_INTERNAL_REQUESTS", "20000")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, 5000, cfg.Server.RateLimit.Requests)
	assert.Equal(t, time.Minute, cfg.Server.RateLimit.Window)
	assert.Equal(t, 20000, cfg.Server.RateLimit.InternalRequests)
}

func Test_should_reject_config_when_rate_limit_requests_is_not_positive(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_SERVER_RATE_LIMIT_REQUESTS", "0")

	// when
	_, err := config.ReadFromFile("test")

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "server.rate_limit.requests must be positive")
}

func Test_should_reject_config_when_rate_limit_internal_requests_is_not_positive(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_SERVER_RATE_LIMIT_INTERNAL_REQUESTS", "0")

	// when
	_, err := config.ReadFromFile("test")

	// then
	require.Error(t, err)
	assert.ErrorContains(t, err, "server.rate_limit.internal_requests must be positive")
}

func Test_should_reject_config_when_trusted_proxy_is_not_cidr(t *testing.T) {
	for _, value := range []string{"10.42.0.1", "10.42.0.0/33", "ingress"} {
		t.Run(value, func(t *testing.T) {
			// given
			t.Chdir(writeConfig(t, "test", strings.Replace(sampleYAML, "server:\n", "server:\n  rate_limit:\n    trusted_proxies:\n      - "+value+"\n", 1)))

			// when
			_, err := config.ReadFromFile("test")

			// then
			require.Error(t, err)
			assert.ErrorContains(t, err, "server.rate_limit.trusted_proxies")
		})
	}
}

func Test_should_override_public_base_url_from_env_var(t *testing.T) {
	// given
	t.Chdir(writeSampleConfig(t))
	t.Setenv("DYNAMIC_PDB_SERVER_PUBLIC_BASE_URL", "https://dev.example.com/api")

	// when
	cfg, err := config.ReadFromFile("test")

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://dev.example.com/api", cfg.Server.PublicBaseURL)
}

func Test_should_reject_config_when_public_base_url_is_not_absolute_http_url(t *testing.T) {
	for _, value := range []string{" ", "/api", "ftp://example.com/api", "https://example.com/api?x=1"} {
		t.Run(value, func(t *testing.T) {
			// given
			t.Chdir(writeSampleConfig(t))
			t.Setenv("DYNAMIC_PDB_SERVER_PUBLIC_BASE_URL", value)

			// when
			_, err := config.ReadFromFile("test")

			// then
			require.Error(t, err)
			assert.ErrorContains(t, err, "server.public_base_url")
		})
	}
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
server:
  public_base_url: https://api.example.com/api/
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

func Test_should_read_local_config_profile(t *testing.T) {
	// given
	t.Chdir(writeConfig(t, "local", `
server:
  public_base_url: https://api.example.com/api/
auth:
  allowed_orgs:
    - Astera-org
  github:
    client_id: local-github-client-id
    client_secret: local-github-client-secret
  jwt:
    secret: sample-secret
    issuer: dynamic-pdb-backend
    ttl: 24h
db:
  host: localhost
  port: 5432
  name: dynamic_pdb_local
  username: postgres
  password: password
  connection_params: sslmode=disable
cdn:
  s3:
    endpoint: http://localhost:9000
    region: us-west-2
    bucket: dynamic-pdb
    access_key_id: local-access-key
    secret_access_key: local-secret-key
    upload_max_file_size: 1073741824
    upload_url_ttl: 15m
`))

	// when
	cfg, err := config.ReadFromFile("local")

	// then
	require.NoError(t, err)
	assert.Equal(t, "local-github-client-id", cfg.Auth.GitHub.ClientID)
	assert.Equal(t, "local-github-client-secret", cfg.Auth.GitHub.ClientSecret)
	assert.Equal(t, "sample-secret", cfg.Auth.JWT.Secret)
	assert.Equal(t, "dynamic_pdb_local", cfg.DB.Name)
	assert.Equal(t, "dynamic-pdb", cfg.CDN.S3.Bucket)
	assert.Equal(t, int64(1073741824), cfg.CDN.S3.UploadMaxFileSize)
	assert.Equal(t, 15*time.Minute, cfg.CDN.S3.UploadURLTTL)
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
	assert.Equal(t, "https://dynamicpdb.com/api", cfg.Server.PublicBaseURL)
	assert.Equal(t, "us-west-1", cfg.CDN.S3.Region)
	assert.Equal(t, "dynamic-pdb-data", cfg.CDN.S3.Bucket)
	assert.Equal(t, int64(1073741824), cfg.CDN.S3.UploadMaxFileSize)
	assert.Equal(t, 15*time.Minute, cfg.CDN.S3.UploadURLTTL)
	assert.Equal(t, "https://files.dynamicpdb.com", cfg.CDN.CloudFront.BaseURL)
	assert.Equal(t, validation.ModeLogOnly, cfg.Validation.Mode)
	assertClusterTrustedProxies(t, cfg.Server.RateLimit)
}

func Test_should_read_repository_dev_config_with_auth_secrets_from_env_vars(t *testing.T) {
	// given
	t.Chdir(filepath.Join("..", ".."))
	t.Setenv("DYNAMIC_PDB_AUTH_GITHUB_CLIENT_SECRET", "env-github-secret")
	t.Setenv("DYNAMIC_PDB_AUTH_JWT_SECRET", "env-jwt-secret")

	// when
	cfg, err := config.ReadFromFile("dev")

	// then
	require.NoError(t, err)
	assert.Equal(t, "env-github-secret", cfg.Auth.GitHub.ClientSecret)
	assert.Equal(t, "env-jwt-secret", cfg.Auth.JWT.Secret)
	assert.Equal(t, "https://dev.dynamicpdb.com/api", cfg.Server.PublicBaseURL)
	assert.Equal(t, "us-west-1", cfg.CDN.S3.Region)
	assert.Equal(t, "dev-dynamic-pdb-data", cfg.CDN.S3.Bucket)
	assert.Equal(t, int64(1073741824), cfg.CDN.S3.UploadMaxFileSize)
	assert.Equal(t, 15*time.Minute, cfg.CDN.S3.UploadURLTTL)
	assert.Equal(t, "https://dev-files.dynamicpdb.com", cfg.CDN.CloudFront.BaseURL)
	assert.Equal(t, validation.ModeLogOnly, cfg.Validation.Mode)
	assertClusterTrustedProxies(t, cfg.Server.RateLimit)
}

// assertClusterTrustedProxies checks that a deployed profile trusts the pod network and the
// AWS VPC, the two networks between the load balancer and the server.
func assertClusterTrustedProxies(t *testing.T, rateLimit config.RateLimitConfig) {
	t.Helper()
	prefixes, err := rateLimit.TrustedProxyPrefixes()
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{
		netip.MustParsePrefix("10.42.0.0/16"),
		netip.MustParsePrefix("10.7.0.0/16"),
	}, prefixes)
}

func Test_should_enforce_schema_validation_when_repository_local_config_is_read(t *testing.T) {
	// given
	t.Chdir(filepath.Join("..", ".."))

	// when
	cfg, err := config.ReadFromFile("local")

	// then
	require.NoError(t, err)
	assert.Equal(t, validation.ModeEnforce, cfg.Validation.Mode)
}
