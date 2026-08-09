package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"dynamic-pdb/cli/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_show_login_command_when_no_command_is_given(t *testing.T) {
	// given
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := execute(context.Background(), nil, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "login")
	assert.Empty(t, stderr.String())
}

func Test_should_fail_when_unknown_command_is_given(t *testing.T) {
	// given
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := execute(context.Background(), []string{"upload"}, &stdout, &stderr)

	// then
	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stderr.String(), "unknown command")
}

func Test_should_clear_auth_when_logout_command_is_called(t *testing.T) {
	// given
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	configHome := filepath.Join(dataHome, "dynamic-pdb")
	err := config.Save(configHome, config.Config{
		Server: config.Server{URL: "http://localhost:8080"},
		GitHub: config.GitHub{ClientID: "github-client-id"},
		Auth: config.Auth{
			TokenType:   "Bearer",
			AccessToken: "jwt-token",
			ExpiresAt:   time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
			Login:       "octocat",
		},
	})
	require.NoError(t, err)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := execute(context.Background(), []string{"logout"}, &stdout, &stderr)
	got, err := config.Load(configHome)

	// then
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "Logged out.\n", stdout.String())
	assert.Empty(t, stderr.String())
	assert.Equal(t, "http://localhost:8080", got.Server.URL)
	assert.Equal(t, "github-client-id", got.GitHub.ClientID)
	assert.Empty(t, got.Auth.AccessToken)
	assert.Empty(t, got.Auth.Login)
}
