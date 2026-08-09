package frontend

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_not_panic_when_upload_progress_has_no_artifacts(t *testing.T) {
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

func Test_should_update_upload_progress_for_artifact(t *testing.T) {
	// given
	var stdout bytes.Buffer
	progress := newUploadProgress(&stdout)

	// when
	startErr := progress.Start(1)
	artifactStartErr := progress.ArtifactStarted("1yjo", "coordinates")
	nextArtifactStartErr := progress.ArtifactStarted("1yjo", "log_1")
	artifactDoneErr := progress.ArtifactDone()
	entryDoneErr := progress.EntryDone("1yjo", "entry-id", 1, 1)
	finishErr := progress.Finish()

	// then
	require.NoError(t, startErr)
	require.NoError(t, artifactStartErr)
	require.NoError(t, nextArtifactStartErr)
	require.NoError(t, artifactDoneErr)
	require.NoError(t, entryDoneErr)
	require.NoError(t, finishErr)
	assert.Contains(t, stdout.String(), "uploading 1yjo coordinates\n")
	assert.Contains(t, stdout.String(), "\x1b[1A\r\x1b[2Kuploading 1yjo log_1\n")
	assert.False(t, strings.Contains(stdout.String(), "\rupload 1yjo coordinates"))
}
