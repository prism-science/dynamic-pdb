package paths

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_use_xdg_data_home_when_environment_variable_is_set(t *testing.T) {
	// given
	xdgDataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdgDataHome)

	// when
	dataHome, err := DataHome()

	// then
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(xdgDataHome, "dynamic-pdb"), dataHome)
}
