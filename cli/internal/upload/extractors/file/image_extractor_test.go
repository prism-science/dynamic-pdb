package file

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_extract_local_image_when_file_exists(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	contents := []byte("image\n")
	writeTestFile(t, dataRoot, "images/5amf.jpeg", contents)
	extractor := NewImageExtractor(dataRoot)

	// when
	image, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Source{
		Files: []string{"images/{{ pdb_id }}.jpeg"},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf.jpeg", image.Filename)
	assert.Equal(t, int64(len(contents)), image.Size)
	assert.Equal(t, filepath.Join(dataRoot, "images", "5amf.jpeg"), image.LocalPath)
	assert.Empty(t, image.Contents)
}

func Test_should_extract_zip_image_when_entry_exists(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	contents := []byte("image\n")
	writeTestZip(t, dataRoot, "images.zip", map[string][]byte{
		"images/5amf.jpeg": contents,
	})
	extractor := NewImageExtractor(dataRoot)

	// when
	image, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Source{
		Files: []string{"images.zip#images/{{ pdb_id }}.jpeg"},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf.jpeg", image.Filename)
	assert.Equal(t, int64(len(contents)), image.Size)
	assert.Equal(t, filepath.Join(dataRoot, "images.zip")+"#images/5amf.jpeg", image.LocalPath)
	assert.Equal(t, contents, image.Contents)
}

func Test_should_return_no_image_when_local_image_is_missing(t *testing.T) {
	// given
	extractor := NewImageExtractor(t.TempDir())

	// when
	_, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Source{
		Files: []string{"missing/{{ pdb_id }}.jpeg"},
	})

	// then
	require.NoError(t, err)
	assert.False(t, ok)
}

func Test_should_return_error_when_local_image_stat_fails(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	path := filepath.Join(dataRoot, "images")
	require.NoError(t, os.WriteFile(path, []byte("not a directory"), 0o644))
	extractor := NewImageExtractor(dataRoot)

	// when
	_, _, err := extractor.Extract(context.Background(), "5AMF", manifest.Source{
		Files: []string{"images/{{ pdb_id }}.jpeg"},
	})

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stat image source")
}
