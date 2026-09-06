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
	assert.Contains(t, stdout.String(), "--include <pdb-id>")
	assert.Contains(t, stdout.String(), "--skip <pdb-id>")
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
	assert.NotContains(t, string(contents), "title: Deposited model")
	assert.NotContains(t, string(contents), "rcsb: '{{ pdb_id }}/metadata'")
	assert.NotContains(t, string(contents), "rcsb: '{{ pdb_id }}/coordinates.cif'")
	assert.NotContains(t, string(contents), "rcsb: '{{ pdb_id }}/fasta'")
	assert.NotContains(t, string(contents), "fetch:")
	assert.NotContains(t, string(contents), "external_ref:")
}

func Test_should_create_manifest_with_rcsb_model_when_upload_manifest_init_flag_is_given(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataRoot, "5amf_020.pdb"), []byte("MODEL\n"), 0o644))
	outputPath := filepath.Join(t.TempDir(), "manifest.yaml")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// when
	exitCode := Upload(context.Background(), []string{"manifest", "init", dataRoot, "--out", outputPath, "--include-rcsb-model"}, &stdout, &stderr)

	// then
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout.String(), "Wrote "+outputPath)
	assert.Empty(t, stderr.String())
	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "title: Deposited model")
	assert.Contains(t, string(contents), "file: '{{ pdb_id }}.cif'")
	assert.Contains(t, string(contents), "- '{{ pdb_id }}_020.pdb'")
}

func Test_should_create_sampleworks_manifest_when_upload_start_points_to_folder(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	runDir := filepath.Join(dataRoot, "results", "rf3", "rf3", "1VME_0.25occA_0.75occB")
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "trajectory"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "job_metadata.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "losses.txt"), []byte("step,loss\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "refined.cif"), []byte("data_1VME\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "run.log"), []byte("ok\n"), 0o644))
	var stdout bytes.Buffer

	// when
	manifestPath, err := resolveUploadManifestPath(dataRoot, &stdout)

	// then
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataRoot, "dynamic-pdb.manifest.yaml"), manifestPath)
	assert.FileExists(t, manifestPath)
	assert.Contains(t, stdout.String(), "Detected Sampleworks data folder")
	contents, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "Sampleworks 0.25occA 0.75occB")
	assert.Contains(t, string(contents), "{{ pdb_id }}_0.25occA_0.75occB/refined.cif")
	assert.Contains(t, string(contents), "format: structure_factors_cif")
	assert.Contains(t, string(contents), "id: log_1")
	assert.NotContains(t, string(contents), "id: job_metadata")
	assert.NotContains(t, string(contents), "id: losses")
	assert.NotContains(t, string(contents), "id: run_log")
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
