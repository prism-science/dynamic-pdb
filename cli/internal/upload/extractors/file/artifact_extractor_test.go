package file

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/manifest"
)

func Test_should_extract_local_artifact_when_file_exists(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	contents := []byte("MODEL\n")
	writeTestFile(t, dataRoot, "models/5amf_model.pdb", contents)
	extractor := NewArtifactExtractor(dataRoot)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{Files: []string{"models/{{ pdb_id }}_model.pdb"}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf_model.pdb", artifact.Filename)
	assert.Equal(t, int64(len(contents)), artifact.Size)
	assert.Equal(t, "pdb", artifact.Format)
	assert.Equal(t, testSHA256(contents), artifact.SHA256)
	assert.Equal(t, filepath.Join(dataRoot, "models", "5amf_model.pdb"), artifact.LocalPath)
	assert.Empty(t, artifact.Contents)
}

func Test_should_build_lowercase_and_uppercase_pdb_id_file_sources(t *testing.T) {
	// when
	sources := pdbIDFileSources("models/{{ pdb_id }}_model.pdb", "5AMF")

	// then
	assert.Equal(t, []string{"models/5amf_model.pdb", "models/5AMF_model.pdb"}, sources)
}

func Test_should_extract_zip_artifact_when_pdb_id_path_is_uppercase(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	contents := []byte("MODEL\n")
	writeTestZip(t, dataRoot, "models.zip", map[string][]byte{
		"models/5AMF_model.pdb": contents,
	})
	extractor := NewArtifactExtractor(dataRoot)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5amf", manifest.Artifact{
		Source: manifest.Source{Files: []string{"models.zip#models/{{ pdb_id }}_model.pdb"}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5AMF_model.pdb", artifact.Filename)
	assert.Equal(t, filepath.Join(dataRoot, "models.zip")+"#models/5AMF_model.pdb", artifact.LocalPath)
}

func Test_should_extract_zip_artifact_when_entry_exists(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	contents := []byte("MTZ\n")
	writeTestZip(t, dataRoot, "models.zip", map[string][]byte{
		"models/5amf_model.mtz": contents,
	})
	extractor := NewArtifactExtractor(dataRoot)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{Files: []string{"models.zip#models/{{ pdb_id }}_model.mtz"}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf_model.mtz", artifact.Filename)
	assert.Equal(t, int64(len(contents)), artifact.Size)
	assert.Equal(t, "mtz", artifact.Format)
	assert.Equal(t, testSHA256(contents), artifact.SHA256)
	assert.Equal(t, filepath.Join(dataRoot, "models.zip")+"#models/5amf_model.mtz", artifact.LocalPath)
	assert.Equal(t, contents, artifact.Contents)
}

func Test_should_try_next_file_source_when_first_file_is_missing(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "fallback/5amf_model.cif", []byte("data_5amf\n"))
	extractor := NewArtifactExtractor(dataRoot)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{Files: []string{
			"missing/{{ pdb_id }}.pdb",
			"fallback/{{ pdb_id }}_model.cif",
		}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf_model.cif", artifact.Filename)
	assert.Equal(t, "cif", artifact.Format)
}

func Test_should_skip_local_artifact_when_file_is_empty(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "models/5amf_model.pdb", []byte{})
	extractor := NewArtifactExtractor(dataRoot)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{Files: []string{"models/{{ pdb_id }}_model.pdb"}},
	})

	// then
	require.NoError(t, err)
	require.False(t, ok)
	assert.Empty(t, artifact)
}

func Test_should_skip_zip_artifact_when_entry_is_empty(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestZip(t, dataRoot, "models.zip", map[string][]byte{
		"models/5amf_model.pdb": {},
	})
	extractor := NewArtifactExtractor(dataRoot)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{Files: []string{"models.zip#models/{{ pdb_id }}_model.pdb"}},
	})

	// then
	require.NoError(t, err)
	require.False(t, ok)
	assert.Empty(t, artifact)
}

func Test_should_try_next_file_source_when_first_file_is_empty(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeTestFile(t, dataRoot, "models/5amf_model.pdb", []byte{})
	writeTestFile(t, dataRoot, "fallback/5amf_model.cif", []byte("data_5amf\n"))
	extractor := NewArtifactExtractor(dataRoot)

	// when
	artifact, ok, err := extractor.Extract(context.Background(), "5AMF", manifest.Artifact{
		Source: manifest.Source{Files: []string{
			"models/{{ pdb_id }}_model.pdb",
			"fallback/{{ pdb_id }}_model.cif",
		}},
	})

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "5amf_model.cif", artifact.Filename)
}

func writeTestFile(t *testing.T, root string, relativePath string, contents []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, contents, 0o644))
}

func writeTestZip(t *testing.T, root string, relativePath string, entries map[string][]byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	file, err := os.Create(path)
	require.NoError(t, err)
	writer := zip.NewWriter(file)
	for name, contents := range entries {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write(contents)
		require.NoError(t, err)
	}
	closeZipErr := writer.Close()
	closeFileErr := file.Close()
	require.NoError(t, closeZipErr)
	require.NoError(t, closeFileErr)
}

func testSHA256(contents []byte) string {
	hash := sha256.Sum256(contents)
	return hex.EncodeToString(hash[:])
}
