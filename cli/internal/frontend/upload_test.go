package frontend

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_show_upload_help_when_no_upload_subcommand_given(t *testing.T) {
	// given
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Upload(context.Background(), nil, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "dynamic-pdb upload manifest init <data-folder>")
	assert.Empty(t, stderr.String())
}

func Test_should_create_manifest_when_upload_manifest_init_called(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataRoot, "5amf_020.pdb"), []byte("MODEL\n"), 0o644))
	outputPath := filepath.Join(t.TempDir(), "manifest.yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Upload(context.Background(), []string{"manifest", "init", dataRoot, "--out", outputPath}, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "Wrote "+outputPath)
	assert.Empty(t, stderr.String())
	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "PDB IDs: 1")
	assert.NotContains(t, string(contents), "inventory:")
	assert.Contains(t, string(contents), "metadata:")
	assert.Contains(t, string(contents), "pdb_id: '{{ pdb_id }}'")
	assert.Contains(t, string(contents), "resource: entry")
	assert.Contains(t, string(contents), "files:")
	assert.Contains(t, string(contents), "- '{{ pdb_id }}_020.pdb'")
	assert.NotContains(t, string(contents), "rcsb: '{{ pdb_id }}/metadata'")
	assert.NotContains(t, string(contents), "rcsb: '{{ pdb_id }}/coordinates.cif'")
	assert.NotContains(t, string(contents), "rcsb: '{{ pdb_id }}/fasta'")
	assert.NotContains(t, string(contents), "fetch:")
	assert.NotContains(t, string(contents), "external_ref:")
}

func Test_should_fail_upload_start_when_user_is_not_authenticated(t *testing.T) {
	// given
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Upload(context.Background(), []string{"start", "manifest.yaml"}, &stdout, &stderr)

	// then
	assert.Equal(t, 1, exitCode)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "run `dynamic-pdb login`")
}

func Test_should_fail_when_upload_subcommand_is_unknown(t *testing.T) {
	// given
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Upload(context.Background(), []string{"check"}, &stdout, &stderr)

	// then
	assert.Equal(t, 2, exitCode)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), `unknown subcommand "check"`)
}
