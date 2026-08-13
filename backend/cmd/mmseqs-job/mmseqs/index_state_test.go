package mmseqs

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_nil_index_state_when_state_file_does_not_exist(t *testing.T) {
	// given
	cacheDir := t.TempDir()

	// when
	state, err := ReadIndexState(cacheDir)

	// then
	require.NoError(t, err)
	assert.Nil(t, state)
}

func Test_should_write_and_read_index_state_when_latest_run_published(t *testing.T) {
	// given
	cacheDir := t.TempDir()
	runID := uuid.New()
	updatedAt := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)

	// when
	err := WriteIndexState(cacheDir, MMseqsIndexState{
		LatestCompletedRunID: &runID,
		UpdatedAt:            updatedAt,
	})

	// then
	require.NoError(t, err)
	state, err := ReadIndexState(cacheDir)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, state.LatestCompletedRunID)
	assert.Equal(t, runID, *state.LatestCompletedRunID)
	assert.Equal(t, updatedAt, state.UpdatedAt)
}
