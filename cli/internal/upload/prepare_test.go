package upload

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/upload/manifest"
)

// pdbModelWithoutVersion names its program without a version, as old PROLSQ files do.
const pdbModelWithoutVersion = "REMARK   3   PROGRAM     : PROLSQ\n" +
	"REMARK   3   FREE R VALUE                     : 0.243\n" +
	"ATOM      1  N   ALA A   1      11.104  13.207   9.447  1.00 20.00           N\n"

// pdbModelWithoutProgram names no program.
const pdbModelWithoutProgram = "ATOM      1  N   ALA A   1      11.104  13.207   9.447  1.00 20.00           N\n"

func Test_should_take_run_version_from_manifest_when_coordinates_header_has_none(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelWithoutVersion)
	uploadManifest := simpleManifest(dataRoot)
	uploadManifest.Entries[0].Models[0].Run = &manifest.Run{SoftwareVersion: "4.0"}
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	_, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 1)
	require.Len(t, models[0].Runs, 1)
	run := models[0].Runs[0]
	assert.Equal(t, "PROLSQ", run.Name, "the name still comes from the header")
	require.NotNil(t, run.SoftwareVersion)
	assert.Equal(t, "4.0", *run.SoftwareVersion)
	require.Len(t, models[0].Metrics, 1)
	assert.Equal(t, &run.ID, models[0].Metrics[0].RunID, "a value from the coordinates file links to the run")
}

func Test_should_use_run_input_and_output_lists_when_manifest_sets_them(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelWithoutProgram)
	writeFile(t, dataRoot, "models/5amf_model.log", "LOG\n")
	writeFile(t, dataRoot, "models/5amf_start.pdb", pdbModelWithoutProgram)
	uploadManifest := simpleManifest(dataRoot)
	model := &uploadManifest.Entries[0].Models[0]
	model.Artifacts = []manifest.Artifact{
		fileArtifact("coordinates", "models/{{ pdb_id }}_model.pdb", "L2"),
		fileArtifact("log_1", "models/{{ pdb_id }}_model.log", "L2"),
		fileArtifact("starting_model", "models/{{ pdb_id }}_start.pdb", "L2"),
		rcsbFileArtifact("structure_factors_1", "{{ pdb_id }}-sf.cif", "L1"),
	}
	model.Run = &manifest.Run{
		SoftwareName:    "qFit",
		SoftwareVersion: "2024.1",
		Inputs:          []string{"starting_model", "structure_factors_1"},
		Outputs:         []string{"coordinates"},
	}
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	_, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 1)
	require.Len(t, models[0].Runs, 1)
	assert.Equal(t, "qFit", models[0].Runs[0].Name)
	artifactIDs := map[string]string{}
	for _, artifact := range models[0].Artifacts {
		artifactIDs[artifact.ID] = artifact.Name
	}
	directions := map[string]string{}
	for _, link := range models[0].Runs[0].Artifacts {
		directions[artifactIDs[link.ArtifactID]] = link.Direction
	}
	assert.Equal(t, map[string]string{
		"5amf_model.pdb": "output",
		"5amf_start.pdb": "input",
		"5amf-sf.cif":    "input",
	}, directions, "the log is no output once run.outputs is set")
}

func Test_should_send_metric_software_when_its_extraction_names_it(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelWithoutProgram)
	writeFile(t, dataRoot, "scores.csv", "pdb_id,clashscore\n5amf,3.5\n")
	uploadManifest := simpleManifest(dataRoot)
	uploadManifest.Entries[0].Models[0].Metrics = manifest.Metrics{
		"clashscore": {{
			Source: manifest.Source{Files: []string{"scores.csv"}},
			Extract: manifest.Extract{CSV: &manifest.ExtractRule{
				Column: "clashscore",
				Where:  &manifest.ExtractRule{Column: "pdb_id", Equals: "{{ pdb_id }}"},
			}},
			Software: &manifest.Software{Name: "MolProbity", Version: "4.5.2"},
		}},
	}
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	_, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 1)
	require.Len(t, models[0].Metrics, 1)
	metric := models[0].Metrics[0]
	assert.Equal(t, "clashscore", metric.Key)
	assert.Equal(t, stringPtr("MolProbity"), metric.SoftwareName)
	assert.Equal(t, stringPtr("4.5.2"), metric.SoftwareVersion)
	assert.Nil(t, metric.RunID)
}

func Test_should_stop_before_any_upload_when_computed_model_fails_schema_checks(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelWithoutProgram)
	writeFile(t, dataRoot, "scores.csv", "pdb_id,clashscore\n5amf,3.5\n")
	uploadManifest := simpleManifest(dataRoot)
	model := &uploadManifest.Entries[0].Models[0]
	model.Run = nil
	model.Artifacts = []manifest.Artifact{fileArtifact("coordinates", "models/{{ pdb_id }}_model.pdb", "L2")}
	model.Metrics["clashscore"] = []manifest.FieldExtraction{{
		Source: manifest.Source{Files: []string{"scores.csv"}},
		Extract: manifest.Extract{CSV: &manifest.ExtractRule{
			Column: "clashscore",
			Where:  &manifest.ExtractRule{Column: "pdb_id", Equals: "{{ pdb_id }}"},
		}},
	}}
	uploadManifest.Entries[0].Metadata = nil
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	summary, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	var checkError *EntryCheckError
	require.True(t, errors.As(err, &checkError), "got %v", err)
	assert.Equal(t, "5AMF", checkError.PDBID)
	assert.Equal(t, []string{
		"the entry has no experimental method: no metadata.method extraction of the manifest finds one; " +
			"add one, such as RCSB entry field exptl[0].method",
		`model "model_1": a computed model needs a run, but neither the coordinates file header nor ` +
			"run.software_name names the program that produced it; set run.software_name and run.software_version",
		`model "model_1": the run has no input artifact: no artifact id starts with mtz_ or structure_factors_ ` +
			"and no format is mtz, structure_factors_cif, map, ccp4 or mrc; list the input artifact ids in run.inputs",
		`model "model_1": metric "clashscore" names no software and links to no run; ` +
			"set software.name and software.version on its extraction",
	}, checkError.Problems, "r_free comes from the coordinates file, so it waits for the run")
	assert.Empty(t, dynamicPDBClient.uploads, "no file may be uploaded")
	assert.Empty(t, dynamicPDBClient.entries)
	assert.Empty(t, readState(t, summary.StatePath).Entries, "the entry must not be left as uploading")
}

func Test_should_need_no_run_when_model_is_deposited(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelWithoutProgram)
	uploadManifest := simpleManifest(dataRoot)
	model := &uploadManifest.Entries[0].Models[0]
	model.ModelType = manifest.ModelTypeDeposited
	model.Run = nil
	model.Artifacts = []manifest.Artifact{fileArtifact("coordinates", "models/{{ pdb_id }}_model.pdb", "L2")}
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	_, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 1)
	assert.Empty(t, models[0].Runs)
}

func Test_should_reject_manifest_before_reading_data_when_metric_key_is_unknown(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelText())
	uploadManifest := simpleManifest(dataRoot)
	uploadManifest.Entries[0].Models[0].Metrics["r-free"] = pdbArtifactExtraction("REMARK 3 FREE R VALUE")
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	_, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	var validationError *manifest.ValidationError
	require.True(t, errors.As(err, &validationError), "got %v", err)
	require.Len(t, validationError.Problems, 1)
	assert.Contains(t, validationError.Problems[0], `entries[0].models[0].metrics.r-free: unknown metric key "r-free"`)
	assert.Zero(t, dynamicPDBClient.listCalls, "the API must not be called")
	assert.Empty(t, dynamicPDBClient.uploads)
}

func Test_should_finish_started_entries_and_leave_none_uploading_when_another_entry_fails_checks(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelText())
	writeFile(t, dataRoot, "models/6abc_model.pdb", pdbModelText())
	writeFile(t, dataRoot, "models/7def_model.pdb", pdbModelWithoutVersion)
	uploadManifest := simpleManifest(dataRoot)
	uploadManifest.Entries[0].Models[0].Run = nil
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	// 5AMF and 6ABC are still creating their entries when 7DEF fails its checks.
	dynamicPDBClient := &fakeDynamicPDBClient{createEntryDelay: 300 * time.Millisecond}

	// when
	summary, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 3).Upload(context.Background(), manifestPath)

	// then
	var checkError *EntryCheckError
	require.True(t, errors.As(err, &checkError), "got %v", err)
	assert.Equal(t, "7DEF", checkError.PDBID)
	state := readState(t, summary.StatePath)
	assert.Empty(t, state.uploadingEntries(), "entries already uploading must finish, not be cancelled")
	assert.Equal(t, entryStatusCompleted, state.Entries["5AMF"].Status)
	assert.Equal(t, entryStatusCompleted, state.Entries["6ABC"].Status)
	assert.NotContains(t, state.Entries, "7DEF")
}

func Test_should_leave_no_state_when_rcsb_fails_while_preparing(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelText())
	uploadManifest := simpleManifest(dataRoot)
	uploadManifest.Entries[0].Models[0].Artifacts[1] = rcsbFileArtifact("structure_factors_1", "{{ pdb_id }}-missing.cif", "L1")
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	summary, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.ErrorContains(t, err, "prepare 5amf:")
	assert.Empty(t, dynamicPDBClient.uploads)
	assert.Empty(t, readState(t, summary.StatePath).Entries, "a restart must not be blocked")
}

func Test_should_say_listed_input_has_no_file_when_run_inputs_find_nothing(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelText())
	uploadManifest := simpleManifest(dataRoot)
	model := &uploadManifest.Entries[0].Models[0]
	model.Artifacts = []manifest.Artifact{
		fileArtifact("coordinates", "models/{{ pdb_id }}_model.pdb", "L2"),
		fileArtifact("density", "maps/{{ pdb_id }}.ccp4", "L1"),
	}
	model.Run = &manifest.Run{Inputs: []string{"density"}}
	manifestPath := filepath.Join(t.TempDir(), manifest.DefaultFilename)
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}

	// when
	_, err := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	var checkError *EntryCheckError
	require.True(t, errors.As(err, &checkError), "got %v", err)
	assert.Equal(t, []string{
		`model "model_1": the run has no input artifact: no artifact in run.inputs has a file for this entry`,
	}, checkError.Problems)
	assert.Empty(t, dynamicPDBClient.uploads)
}
