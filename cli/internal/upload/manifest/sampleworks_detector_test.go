package manifest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_sampleworks_detector_should_return_input_and_output_patterns(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.01")
	writeSampleWorksRun(t, dataRoot, "rf3_smoke/rf3/1VME_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.1")

	// when
	patterns, ok, err := SampleWorksDetector{}.Detect(dataRoot)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, sampleWorksExpectedPatterns(dataRoot), patterns)
}

func Test_sampleworks_detector_should_return_patterns_for_multiple_proteins_with_same_layout(t *testing.T) {
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
	patterns, ok, err := SampleWorksDetector{}.Detect(dataRoot)

	// then
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, sampleWorksExpectedPatterns(dataRoot), patterns)
}

func Test_sampleworks_detector_should_return_false_for_non_sampleworks_folder(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "results/rf3/rf3/1VME_0.25occA_0.75occB/refined.cif", "data_1VME\n")

	// when
	patterns, ok, err := SampleWorksDetector{}.Detect(dataRoot)

	// then
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, patterns)
}

func sampleWorksExpectedPatterns(dataRoot string) []SampleWorksPattern {
	root := filepath.ToSlash(dataRoot)
	return []SampleWorksPattern{
		{
			DensityMapPattern:   sampleWorksInputPattern,
			RefinedModelPattern: root + "/rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0/refined.cif",
			RunLogPattern:       root + "/rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.0/run.log",
		},
		{
			DensityMapPattern:   sampleWorksInputPattern,
			RefinedModelPattern: root + "/rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.01/refined.cif",
			RunLogPattern:       root + "/rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.01/run.log",
		},
		{
			DensityMapPattern:   sampleWorksInputPattern,
			RefinedModelPattern: root + "/rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.1/refined.cif",
			RunLogPattern:       root + "/rf3_smoke/rf3/{{ pdb_id }}_0.25occA_0.75occB/rf3_X-RAY_DIFFRACTION/pure_guidance/ens1_gw0.1/run.log",
		},
	}
}

func writeSampleWorksRun(t *testing.T, root string, dir string) {
	t.Helper()
	writeFile(t, root, dir+"/job_metadata.json", "{}\n")
	writeFile(t, root, dir+"/losses.txt", "step,loss\n")
	writeFile(t, root, dir+"/refined.cif", "data_1VME\n")
	writeFile(t, root, dir+"/run.log", "ok\n")
	writeFile(t, root, dir+"/trajectory/frame_000.cif", "data_1VME\n")
}
