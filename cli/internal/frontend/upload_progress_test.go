package frontend

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_not_panic_when_upload_progress_has_no_entries(t *testing.T) {
	// given
	var stdout bytes.Buffer
	progress := newUploadProgress(&stdout)

	// when
	err := progress.Start(0)
	finishErr := progress.Finish()

	// then
	require.NoError(t, err)
	require.NoError(t, finishErr)
}

func Test_should_update_upload_progress_for_entry_without_artifact_lines(t *testing.T) {
	// given
	var stdout bytes.Buffer
	progress := newUploadProgress(&stdout)

	// when
	startErr := progress.Start(1)
	entryDoneErr := progress.EntryDone("1yjo", "entry-id", 1, 1)
	finishErr := progress.Finish()

	// then
	require.NoError(t, startErr)
	require.NoError(t, entryDoneErr)
	require.NoError(t, finishErr)
	assert.NotContains(t, stdout.String(), "uploading 1yjo coordinates")
	assert.NotContains(t, stdout.String(), "uploading 1yjo log_1")
	assert.Contains(t, stdout.String(), "uploaded 1yjo entry_id=entry-id models=1 artifacts=1")
}
