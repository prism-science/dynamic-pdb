package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"dynamic-pdb/cli/internal/paths"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_return_defaults_when_config_file_does_not_exist(t *testing.T) {
	// given
	dataHome := t.TempDir()

	// when
	cfg, err := Load(dataHome)

	// then
	require.NoError(t, err)
	assert.Equal(t, DefaultServerURL, cfg.ServerURL())
	assert.Equal(t, DefaultGitHubClientID, cfg.GitHubClientID())
}

func Test_should_round_trip_auth_when_config_saved_and_loaded(t *testing.T) {
	// given
	dataHome := t.TempDir()
	expiresAt := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	want := Config{
		Server: Server{URL: "https://api.example.test/"},
		GitHub: GitHub{ClientID: "github-client-id"},
		Auth: Auth{
			TokenType:   "Bearer",
			AccessToken: "jwt-token",
			ExpiresAt:   expiresAt,
			Name:        "Octo Cat",
			Email:       "octocat@example.test",
			Login:       "octocat",
		},
	}

	// when
	err := Save(dataHome, want)
	require.NoError(t, err)
	got, err := Load(dataHome)
	require.NoError(t, err)
	fileInfo, err := os.Stat(paths.ConfigPath(dataHome))

	// then
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
	assert.Equal(t, "https://api.example.test", got.Server.URL)
	assert.Equal(t, want.GitHub, got.GitHub)
	assert.Equal(t, want.Auth, got.Auth)
}

func Test_should_preserve_server_when_auth_updated(t *testing.T) {
	// given
	dataHome := t.TempDir()
	err := Save(dataHome, Config{Server: Server{URL: "https://api.example.test"}})
	require.NoError(t, err)

	// when
	err = Update(dataHome, func(cfg *Config) error {
		cfg.Auth = Auth{AccessToken: "jwt-token"}
		return nil
	})
	require.NoError(t, err)
	got, err := Load(dataHome)

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.test", got.Server.URL)
	assert.Equal(t, "Bearer", got.Auth.TokenType)
	assert.Equal(t, "jwt-token", got.Auth.AccessToken)
	assert.FileExists(t, filepath.Join(dataHome, "config.toml"))
}

func Test_should_clear_auth_without_removing_server_and_github_config(t *testing.T) {
	// given
	dataHome := t.TempDir()
	expiresAt := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	err := Save(dataHome, Config{
		Server: Server{URL: "https://api.example.test/"},
		GitHub: GitHub{ClientID: "github-client-id"},
		Auth: Auth{
			TokenType:   "Bearer",
			AccessToken: "jwt-token",
			ExpiresAt:   expiresAt,
			Name:        "Octo Cat",
			Email:       "octocat@example.test",
			Login:       "octocat",
		},
	})
	require.NoError(t, err)

	// when
	err = ClearAuth(dataHome)
	require.NoError(t, err)
	got, err := Load(dataHome)

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.test", got.Server.URL)
	assert.Equal(t, GitHub{ClientID: "github-client-id"}, got.GitHub)
	assert.Empty(t, got.Auth.AccessToken)
	assert.Empty(t, got.Auth.Login)
	assert.True(t, got.Auth.ExpiresAt.IsZero())
}
