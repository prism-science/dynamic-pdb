package manifest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_sampleworks_builder_should_create_manifest_from_sampleworks_folder(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeSampleWorksRun(t, dataRoot, "results/rf3/rf3/1VME_0.25occA_0.75occB")

	// when
	uploadManifest, stats, err := SampleWorksManifestBuilder{}.Build(dataRoot)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, stats.PDBIDs)
	assert.Equal(t, 5, stats.LocalFiles)
	require.Len(t, uploadManifest.Entries, 1)
	assert.Equal(t, filepath.ToSlash(dataRoot), uploadManifest.DataRoot)
	require.Len(t, uploadManifest.Entries[0].Models, 1)

	model := uploadManifest.Entries[0].Models[0]
	assert.Equal(t, "sampleworks_025occa_075occb", model.ID)
	assert.Equal(t, "Sampleworks 0.25occA 0.75occB", model.Title)
	assert.Equal(t, "Single Conformer", model.ModelType)
	assert.Equal(t, "Refinement", model.Purpose)
	require.Len(t, model.Artifacts, 3)
	assert.Equal(t, []string{"results/rf3/rf3/{{ pdb_id }}_0.25occA_0.75occB/refined.cif"}, model.Artifacts[0].Source.Files)
	assert.Equal(t, "coordinates", model.Artifacts[0].ID)
	assert.Empty(t, model.Artifacts[0].Name)
	assert.Equal(t, "L2", model.Artifacts[0].Level)
	assert.Equal(t, "starting_structure", model.Artifacts[1].ID)
	assert.Empty(t, model.Artifacts[1].Name)
	assert.Equal(t, "L1", model.Artifacts[1].Level)
	assert.Equal(t, "structure_factors_cif", model.Artifacts[1].Format)
	assert.Equal(t, []string{sampleWorksInputPattern}, model.Artifacts[1].Source.Files)
	assert.Equal(t, "log_1", model.Artifacts[2].ID)
	assert.Empty(t, model.Artifacts[2].Name)
	assert.Contains(t, model.Metrics, "r_free")
	assert.Contains(t, model.Metrics, "r_work")
}

func Test_sampleworks_builder_should_create_one_model_per_detected_pattern(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.01")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.1")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/2A26_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/2A26_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.01")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/2A26_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.1")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/3HYN_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/3HYN_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.01")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/3HYN_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.1")

	// when
	uploadManifest, stats, err := SampleWorksManifestBuilder{}.Build(dataRoot)

	// then
	require.NoError(t, err)
	assert.Equal(t, 3, stats.PDBIDs)
	assert.Equal(t, 45, stats.LocalFiles)
	require.Len(t, uploadManifest.Entries, 1)
	require.Len(t, uploadManifest.Entries[0].Models, 3)
	assert.Equal(t, "sampleworks_ens1_gw00", uploadManifest.Entries[0].Models[0].ID)
	assert.Equal(t, "sampleworks_ens1_gw001", uploadManifest.Entries[0].Models[1].ID)
	assert.Equal(t, "sampleworks_ens1_gw01", uploadManifest.Entries[0].Models[2].ID)
	require.Len(t, uploadManifest.Entries[0].Models[0].Artifacts, 3)
	assert.Equal(t, []string{"rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0/refined.cif"}, uploadManifest.Entries[0].Models[0].Artifacts[0].Source.Files)
	assert.Equal(t, "log_1", uploadManifest.Entries[0].Models[0].Artifacts[2].ID)
	assert.Equal(t, []string{"rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0/run.log"}, uploadManifest.Entries[0].Models[0].Artifacts[2].Source.Files)
}

func Test_sampleworks_builder_should_include_parent_folder_when_leaf_model_label_is_not_unique(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/other_guidance/ens1")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/2A26_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/other_guidance/ens1")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/2A26_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1")

	// when
	uploadManifest, _, err := SampleWorksManifestBuilder{}.Build(dataRoot)

	// then
	require.NoError(t, err)
	require.Len(t, uploadManifest.Entries, 1)
	require.Len(t, uploadManifest.Entries[0].Models, 2)
	assert.Equal(t, "sampleworks_other_guidance_ens1", uploadManifest.Entries[0].Models[0].ID)
	assert.Equal(t, "Sampleworks other_guidance ens1", uploadManifest.Entries[0].Models[0].Title)
	assert.Equal(t, "sampleworks_pure_guidance_ens1", uploadManifest.Entries[0].Models[1].ID)
	assert.Equal(t, "Sampleworks pure_guidance ens1", uploadManifest.Entries[0].Models[1].Title)
}

func Test_sampleworks_builder_should_reject_non_sampleworks_folder(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")

	// when
	_, _, err := SampleWorksManifestBuilder{}.Build(dataRoot)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not look like Sampleworks")
}

func Test_init_sampleworks_should_write_manifest(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeSampleWorksRun(t, dataRoot, "2A26_1.0occB")
	outputPath := filepath.Join(dataRoot, SampleWorksDefaultFilename)

	// when
	writtenPath, uploadManifest, stats, err := InitSampleWorks(dataRoot, "")

	// then
	require.NoError(t, err)
	assert.Equal(t, outputPath, writtenPath)
	assert.FileExists(t, writtenPath)
	assert.Equal(t, 1, stats.PDBIDs)
	require.Len(t, uploadManifest.Entries[0].Models, 1)
	assert.Equal(t, "Sampleworks 1.0occB", uploadManifest.Entries[0].Models[0].Title)
}
