package frontend

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"dynamic-pdb/cli/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_clear_saved_auth_when_logout_succeeds(t *testing.T) {
	// given
	xdgDataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	dataHome := filepath.Join(xdgDataHome, "dynamic-pdb")
	require.NoError(t, config.Save(dataHome, config.Config{
		Auth: config.Auth{
			TokenType:   "Bearer",
			AccessToken: "jwt-token",
			ExpiresAt:   time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
			Login:       "octocat",
		},
	}))
	var stdout bytes.Buffer

	// when
	err := Logout(&stdout)
	cfg, loadErr := config.Load(dataHome)

	// then
	require.NoError(t, err)
	require.NoError(t, loadErr)
	assert.Contains(t, stdout.String(), "Logged out.")
	assert.Empty(t, cfg.Auth.AccessToken)
	assert.Empty(t, cfg.Auth.Login)
	assert.True(t, cfg.Auth.ExpiresAt.IsZero())
}
