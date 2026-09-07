package manifest

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_create_template_manifest_from_data_folder(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "Rerefined/final_model/5amf_020.pdb", "MODEL\n")
	writeFile(t, dataRoot, "Rerefined/final_model/5amf_020.log", "ok\n")
	writeFile(t, dataRoot, "Rerefined/final_model/5amf_020.mtz", "mtz\n")
	writeFile(t, dataRoot, "Rerefined/final_model/110l_020.pdb", "MODEL\n")
	writeFile(t, dataRoot, "qFit/qfit_cif/5amf_qFit_010.cif", "data_5amf\n")
	writeFile(t, dataRoot, "qFit/qfit_cif/5amf_qFit_010.log", "ok\n")
	writeFile(t, dataRoot, "qFit/qfit_cif/5amf_qFit_010.mtz", "mtz\n")
	writeFile(t, dataRoot, "qFit/qfit_PDBs/5amf_qFit_010.pdb", "MODEL\n")
	writeFile(t, dataRoot, "qFit/qfit_PDBs/5amf_qFit_010.log", "ok\n")
	writeFile(t, dataRoot, "qFit/qfit_PDBs/5amf_qFit_010.mtz", "mtz\n")
	writeFile(t, dataRoot, "qFit/qfit_PDBs/7abc_qFit_010.pdb", "MODEL\n")
	writeFile(t, dataRoot, "Ensemble_Refinement/1_3/5amf_final_ensemble.pdb", "MODEL\n")
	writeFile(t, dataRoot, DefaultFilename, "old manifest\n")
	outputPath := filepath.Join(t.TempDir(), DefaultFilename)

	// when
	writtenPath, generated, stats, err := Init(Options{
		DataRoot:   dataRoot,
		OutputPath: outputPath,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, outputPath, writtenPath)
	assert.FileExists(t, outputPath)
	assert.Equal(t, 3, stats.PDBIDs)
	assert.Equal(t, 12, stats.LocalFiles)
	require.Len(t, generated.Entries, 1)
	assert.Len(t, generated.Entries[0].Models, 3)

	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.NotContains(t, string(contents), "inventory:")
	assert.Contains(t, string(contents), "filter:")
	assert.Contains(t, string(contents), "include: []")
	assert.Contains(t, string(contents), "skip: []")
	assert.NotContains(t, string(contents), "exceptions:")
	assert.Contains(t, string(contents), "title: '{{ pdb_id }}'")
	assert.Contains(t, string(contents), "id: model_1")
	assert.Contains(t, string(contents), "id: model_2")
	assert.Contains(t, string(contents), "id: model_3")
	assert.NotContains(t, string(contents), "id: model_4")
	assert.NotContains(t, string(contents), "id: deposited")
	assert.NotContains(t, string(contents), "id: rerefined")
	assert.NotContains(t, string(contents), "id: qfit")
	assert.NotContains(t, string(contents), "id: ensemble")
	assert.Contains(t, string(contents), "qFit/qfit_cif/{{ pdb_id }}_qFit_010.cif")
	assert.NotContains(t, string(contents), "strategy:")
	assert.NotContains(t, string(contents), "sources:")
	assert.NotContains(t, string(contents), "prefer:")
	assert.NotContains(t, string(contents), "confidence:")
	assert.NotContains(t, string(contents), "fetch:")
	assert.NotContains(t, string(contents), "external_ref:")
	assert.NotContains(t, string(contents), "on_existing:")
	assert.NotContains(t, string(contents), "on_missing:")
	assert.NotContains(t, string(contents), "required:")
	assert.NotContains(t, string(contents), "support:")
	assert.NotContains(t, string(contents), "kind:")
	assert.NotContains(t, string(contents), "Multiconformer")
	assert.NotContains(t, string(contents), "Model Building")
	assert.NotContains(t, string(contents), "purpose: Refinement")
	assert.NotContains(t, string(contents), "model_type: Ensemble")
	assert.NotContains(t, string(contents), "title: Deposited model")
	assert.Contains(t, string(contents), "r_free")
	assert.Contains(t, string(contents), "r_work")
	assert.GreaterOrEqual(t, strings.Count(string(contents), "pdb_id: '{{ pdb_id }}'"), 3)
	assert.Contains(t, string(contents), "resource: entry")
	assert.NotContains(t, string(contents), "resource: polymer_entity")
	assert.Contains(t, string(contents), "resource: fasta")
	assert.NotContains(t, string(contents), "file: '{{ pdb_id }}.cif'")
	assert.Contains(t, string(contents), "file: '{{ pdb_id }}-sf.cif'")
	assert.Contains(t, string(contents), "file: '{{ pdb_id }}_assembly-1.jpeg'")
	assert.Contains(t, string(contents), "artifact: coordinates")
	assert.Contains(t, string(contents), "files:")
	assert.Contains(t, string(contents), "- Rerefined/final_model/{{ pdb_id }}_020.pdb")
	assert.Contains(t, string(contents), "model_type: \"\"")
	assert.Contains(t, string(contents), "purpose: \"\"")
	assert.NotContains(t, string(contents), "metrics: []")
	assert.NotContains(t, string(contents), "runs:")
	assert.NotContains(t, string(contents), "rcsb://")
	assert.NotContains(t, string(contents), "/metadata")
	assert.NotContains(t, string(contents), "/metrics")
	assert.NotContains(t, string(contents), "structure-factors.cif")
	assert.NotContains(t, string(contents), "coordinates.cif")
	assert.NotContains(t, string(contents), "/fasta")
	assert.NotContains(t, string(contents), "format:")
	assert.Contains(t, string(contents), "preview_image:")
	assert.NotContains(t, string(contents), "id: preview_image")
	assert.NotContains(t, string(contents), "name: Preview image")
	assert.NotContains(t, string(contents), "name: Deposited FASTA")
	assert.NotContains(t, string(contents), "name: Structure factors")
	assert.Contains(t, string(contents), "id: fasta")
	assert.Contains(t, string(contents), "id: structure_factors_1")
	assert.Contains(t, string(contents), "id: log_1")
	assert.Contains(t, string(contents), "id: log_2")
	assert.Contains(t, string(contents), "id: mtz_1")
	assert.Contains(t, string(contents), "id: mtz_2")
	assert.Contains(t, string(contents), "- Rerefined/final_model/{{ pdb_id }}_020.mtz")
	assert.Contains(t, string(contents), "- qFit/qfit_cif/{{ pdb_id }}_qFit_010.log")
	assert.Contains(t, string(contents), "- qFit/qfit_PDBs/{{ pdb_id }}_qFit_010.mtz")
	assert.Contains(t, string(contents), "id: coordinates")
	assert.Contains(t, string(contents), "level: L2")
	assert.Less(t,
		strings.Index(string(contents), "- qFit/qfit_cif/{{ pdb_id }}_qFit_010.cif"),
		strings.Index(string(contents), "- qFit/qfit_PDBs/{{ pdb_id }}_qFit_010.pdb"),
	)
}

func Test_should_include_rcsb_model_when_requested(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "Rerefined/final_model/5amf_020.pdb", "MODEL\n")
	outputPath := filepath.Join(t.TempDir(), DefaultFilename)

	// when
	writtenPath, generated, stats, err := Init(Options{
		DataRoot:         dataRoot,
		OutputPath:       outputPath,
		IncludeRCSBModel: true,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, outputPath, writtenPath)
	assert.Equal(t, 1, stats.PDBIDs)
	assert.Equal(t, 1, stats.LocalFiles)
	require.Len(t, generated.Entries, 1)
	require.Len(t, generated.Entries[0].Models, 2)
	assert.Equal(t, "model_1", generated.Entries[0].Models[0].ID)
	assert.Equal(t, "Deposited model", generated.Entries[0].Models[0].Title)
	assert.Equal(t, "model_2", generated.Entries[0].Models[1].ID)

	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "title: Deposited model")
	assert.Contains(t, string(contents), "file: '{{ pdb_id }}.cif'")
	assert.Contains(t, string(contents), "- Rerefined/final_model/{{ pdb_id }}_020.pdb")
}

func Test_should_create_template_manifest_from_zip_entries(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeZipFile(t, dataRoot, "models.zip", map[string]string{
		"models/5amf_model.pdb": "MODEL\n",
		"models/5amf_model.log": "ok\n",
		"models/5amf_model.mtz": "mtz\n",
	})
	outputPath := filepath.Join(t.TempDir(), DefaultFilename)

	// when
	_, _, stats, err := Init(Options{
		DataRoot:   dataRoot,
		OutputPath: outputPath,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, stats.PDBIDs)
	assert.Equal(t, 3, stats.LocalFiles)

	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "- models.zip#models/{{ pdb_id }}_model.pdb")
	assert.Contains(t, string(contents), "- models.zip#models/{{ pdb_id }}_model.log")
	assert.Contains(t, string(contents), "- models.zip#models/{{ pdb_id }}_model.mtz")
}

func Test_should_create_metric_extract_for_coordinate_file_format(t *testing.T) {
	// given
	pdbDataRoot := t.TempDir()
	writeFile(t, pdbDataRoot, "models/5amf_model.pdb", "MODEL\n")
	pdbOutputPath := filepath.Join(t.TempDir(), DefaultFilename)
	cifDataRoot := t.TempDir()
	writeFile(t, cifDataRoot, "models/5amf_model.cif", "data_5amf\n")
	cifOutputPath := filepath.Join(t.TempDir(), DefaultFilename)

	// when
	_, _, _, pdbErr := Init(Options{DataRoot: pdbDataRoot, OutputPath: pdbOutputPath})
	_, _, _, cifErr := Init(Options{DataRoot: cifDataRoot, OutputPath: cifOutputPath})

	// then
	require.NoError(t, pdbErr)
	require.NoError(t, cifErr)
	pdbContents, err := os.ReadFile(pdbOutputPath)
	require.NoError(t, err)
	assert.Contains(t, string(pdbContents), "field: REMARK 3 FREE R VALUE")
	assert.NotContains(t, string(pdbContents), "_refine.ls_R_factor_R_free")
	cifContents, err := os.ReadFile(cifOutputPath)
	require.NoError(t, err)
	assert.Contains(t, string(cifContents), "field: _refine.ls_R_factor_R_free")
	assert.NotContains(t, string(cifContents), "REMARK 3 FREE R VALUE")
}

func Test_should_overwrite_manifest_when_output_file_already_exists(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "5amf.pdb", "MODEL\n")
	outputPath := filepath.Join(t.TempDir(), DefaultFilename)
	writeFile(t, filepath.Dir(outputPath), DefaultFilename, "existing\n")

	// when
	writtenPath, _, _, err := Init(Options{DataRoot: dataRoot, OutputPath: outputPath})

	// then
	require.NoError(t, err)
	assert.Equal(t, outputPath, writtenPath)
	contents, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "version: 1")
	assert.NotContains(t, string(contents), "existing")
}

func Test_should_reject_metric_values_when_manifest_loaded(t *testing.T) {
	// given
	manifestPath := filepath.Join(t.TempDir(), DefaultFilename)
	contents := `version: 1
data_root: /tmp/data
entries:
  - pdb_id: '{{ pdb_id }}'
    title: '{{ pdb_id }}'
    models:
      - id: model_1
        title: ""
        model_type: ""
        purpose: ""
        artifacts: []
        metrics:
          - key: r_free
            value: "0.21"
`
	require.NoError(t, os.WriteFile(manifestPath, []byte(contents), 0o644))

	// when
	_, err := Read(manifestPath)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot unmarshal")
}

func writeFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func writeZipFile(t *testing.T, root string, relativePath string, entries map[string]string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	file, err := os.Create(path)
	require.NoError(t, err)
	writer := zip.NewWriter(file)
	for name, contents := range entries {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(contents))
		require.NoError(t, err)
	}
	closeZipErr := writer.Close()
	closeFileErr := file.Close()
	require.NoError(t, closeZipErr)
	require.NoError(t, closeFileErr)
}
