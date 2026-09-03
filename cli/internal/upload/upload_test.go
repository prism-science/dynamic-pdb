package upload

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"dynamic-pdb/cli/internal/dynamicpdbapi"
	extractorapi "dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/rcsb"
)

func Test_should_upload_entries_from_manifest(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", pdbModelText())
	writeFile(t, dataRoot, "models/5amf_model.log", "LOG\n")
	writeFile(t, dataRoot, "models/5amf_model.mtz", "MTZ\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 2, summary.Models)
	assert.Equal(t, 6, summary.Artifacts)
	assert.Equal(t, uploadStatePath(manifestPath), summary.StatePath)
	require.Len(t, dynamicPDBClient.entries, 1)
	entryRequest := dynamicPDBClient.entries[0]
	entry := entryRequest.Entry
	assert.Equal(t, "5AMF", entry.Name)
	require.NotNil(t, entry.Description)
	assert.Equal(t, "example structure", *entry.Description)
	assert.Equal(t, "example structure", entry.Metadata["title"])
	assert.Equal(t, "Homo sapiens", entry.Metadata["organism"])
	assert.Equal(t, "X-ray crystallography", entry.Metadata["method"])
	assert.Equal(t, "P 21 21 21", entry.Metadata["space_group"])
	externalRefs, ok := entry.Metadata["external_refs"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "5AMF", externalRefs["pdb"])
	require.NotNil(t, entry.ThumbnailImageURL)
	assert.Contains(t, *entry.ThumbnailImageURL, "https://cdn.example.test/")
	assert.Contains(t, *entry.ThumbnailImageURL, "/5amf_assembly-1.jpeg")
	assert.Len(t, entry.Artifacts, 1)
	assert.Equal(t, "fasta", entry.Artifacts[0].Type)
	require.NotNil(t, entry.Artifacts[0].URI)
	assert.Equal(t, "https://www.rcsb.test/fasta/entry/5AMF", *entry.Artifacts[0].URI)
	assert.Len(t, entryRequest.ModelOperations, 2)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 2)
	require.NotNil(t, models[1].IdempotencyKey)
	require.NotNil(t, models[1].Artifacts[0].SHA256)
	assert.Equal(t, *models[1].Artifacts[0].SHA256, *models[1].IdempotencyKey)
	assert.Equal(t, []string{"Nelson, R.", "Sawaya, M.R."}, models[0].Metadata["authors"])
	assert.Equal(t, "Howard Hughes Medical Institute, UCLA, USA.", models[0].Metadata["affiliation"])
	assert.Equal(t, 1383, models[0].Metadata["atom_count"])
	assert.Equal(t, 164, models[0].Metadata["modeled_residues"])
	assert.Equal(t, 1, models[0].Metadata["unique_protein_chains"])
	assert.Equal(t, []string{"ATP"}, models[0].Metadata["ligands"])
	assert.NotContains(t, models[1].Metadata, "authors")
	assert.NotContains(t, models[1].Metadata, "affiliation")
	assert.Equal(t, 4, models[1].Metadata["atom_count"])
	assert.Equal(t, 2, models[1].Metadata["modeled_residues"])
	assert.Equal(t, 1, models[1].Metadata["unique_protein_chains"])
	assert.Equal(t, []string{"ATP"}, models[1].Metadata["ligands"])
	assert.Len(t, models[1].Artifacts, 3)
	assert.Equal(t, "model", models[1].Artifacts[0].Type)
	assert.Equal(t, "other", models[1].Artifacts[1].Type)
	assert.Equal(t, "other", models[1].Artifacts[2].Type)
	assert.Equal(t, "5amf_model.log", models[1].Artifacts[1].Name)
	assert.Equal(t, "5amf_model.mtz", models[1].Artifacts[2].Name)
	assert.Equal(t, "5amf-sf.cif", models[0].Artifacts[1].Name)
	assert.Equal(t, "model", models[0].Artifacts[0].Type)
	assert.Equal(t, "structure_factors", models[0].Artifacts[1].Type)
	require.NotNil(t, models[0].Artifacts[0].URI)
	assert.Equal(t, "https://files.rcsb.test/download/5AMF.cif", *models[0].Artifacts[0].URI)
	require.NotNil(t, models[0].Artifacts[1].URI)
	assert.Equal(t, "https://files.rcsb.test/download/5AMF-sf.cif", *models[0].Artifacts[1].URI)
	require.Len(t, models[0].Runs, 1)
	assert.Equal(t, "REFMAC", models[0].Runs[0].Name)
	require.NotNil(t, models[0].Runs[0].SoftwareVersion)
	assert.Equal(t, "5.2.0005", *models[0].Runs[0].SoftwareVersion)
	assertRunArtifactDirections(t, models[0].Runs[0].Artifacts, []string{"output", "input"})
	require.Len(t, models[1].Runs, 1)
	assert.Equal(t, "PHENIX", models[1].Runs[0].Name)
	require.NotNil(t, models[1].Runs[0].SoftwareVersion)
	assert.Equal(t, "2.0_5824", *models[1].Runs[0].SoftwareVersion)
	assertRunArtifactDirections(t, models[1].Runs[0].Artifacts, []string{"output", "output", "input"})
	require.Len(t, models[0].Metrics, 2)
	assert.Equal(t, "r_free", models[0].Metrics[0].Key)
	assert.Equal(t, 0.21, models[0].Metrics[0].Value)
	require.Len(t, models[1].Metrics, 2)
	assert.Equal(t, "r_free", models[1].Metrics[0].Key)
	assert.Equal(t, 0.243, models[1].Metrics[0].Value)
	assert.Len(t, dynamicPDBClient.uploads, 4)
	assert.Len(t, dynamicPDBClient.completed, 4)
	assert.Equal(t, 1, countString(uploadFilenames(dynamicPDBClient.uploads), "5amf_assembly-1.jpeg"))
	assert.Equal(t, 0, countString(uploadFilenames(dynamicPDBClient.uploads), "5amf-sf.cif"))
	assert.Equal(t, 1, countString(uploadFilenames(dynamicPDBClient.uploads), "5amf_model.mtz"))
	state := readState(t, summary.StatePath)
	entryState := state.Entries["5AMF"]
	assert.Equal(t, entryStatusCompleted, entryState.Status)
	assert.Equal(t, "dpdb_test0001", entryState.EntryID)
	assert.Equal(t, createdModelIDs(dynamicPDBClient.models), entryState.UploadedModelIDs)
	assert.Equal(t, append(artifactIDs(entry.Artifacts), append(artifactIDs(models[0].Artifacts), artifactIDs(models[1].Artifacts)...)...), entryState.UploadedArtifactIDs)
	assert.Equal(t, append(runIDs(models[0].Runs), runIDs(models[1].Runs)...), entryState.UploadedRunIDs)
	assert.Equal(t, append(metricIDs(models[0].Metrics), metricIDs(models[1].Metrics)...), entryState.UploadedMetricIDs)
	stateContents, err := os.ReadFile(summary.StatePath)
	require.NoError(t, err)
	stateLines := strings.Split(strings.TrimSpace(string(stateContents)), "\n")
	require.Len(t, stateLines, 2)
	assert.Contains(t, stateLines[0], `"event":"entry_uploading"`)
	assert.Contains(t, stateLines[1], `"event":"entry_completed"`)
}

func Test_should_upload_entries_from_zip_sources(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeZipFile(t, dataRoot, "models.zip", map[string]string{
		"models/5amf_model.pdb": "REMARK   3   R VALUE     (WORKING SET) : 0.191\nREMARK   3   FREE R VALUE                     : 0.243\nMODEL\n",
		"models/5amf_model.log": "LOG\n",
		"models/5amf_model.mtz": "MTZ\n",
	})
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeZipManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 2, summary.Models)
	assert.Equal(t, 6, summary.Artifacts)
	assert.FileExists(t, summary.StatePath)
	require.Len(t, dynamicPDBClient.entries, 1)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 2)
	require.Len(t, models[1].Metrics, 2)
	assert.Equal(t, 0.243, models[1].Metrics[0].Value)
	assert.Equal(t, 1, countString(uploadFilenames(dynamicPDBClient.uploads), "5amf_model.pdb"))
	assert.Equal(t, 1, countString(uploadFilenames(dynamicPDBClient.uploads), "5amf_model.log"))
	assert.Equal(t, 1, countString(uploadFilenames(dynamicPDBClient.uploads), "5amf_model.mtz"))
}

func Test_should_link_structure_factors_from_rcsb_when_manifest_points_to_rcsb(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/5amf_model.log", "LOG\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeRemoteMTZManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := &trackingFakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 2, summary.Models)
	assert.Equal(t, 6, summary.Artifacts)
	assert.FileExists(t, summary.StatePath)
	assert.Len(t, dynamicPDBClient.uploads, 3)
	assert.Equal(t, 0, countString(uploadFilenames(dynamicPDBClient.uploads), "5amf-sf.cif"))
	assert.NotContains(t, uploadFilenames(dynamicPDBClient.uploads), "5amf_model.mtz")
	assert.Equal(t, 2, countString(rcsbClient.files, "5amf-sf.cif"))
}

func Test_should_skip_non_deposited_model_when_coordinates_file_is_missing(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.log", "LOG\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeRemoteMTZManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := &trackingFakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Models)
	assert.Equal(t, 3, summary.Artifacts)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 1)
	assert.Equal(t, "Deposited model", models[0].Name)
	assert.Equal(t, 1, countString(rcsbClient.files, "5amf-sf.cif"))
	assert.NotContains(t, uploadFilenames(dynamicPDBClient.uploads), "5amf_model.pdb")
}

func Test_should_skip_non_deposited_model_when_coordinates_zip_entry_is_empty(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeZipFile(t, dataRoot, "models.zip", map[string]string{
		"models/5amf_model.pdb": "",
		"models/5amf_model.log": "LOG\n",
		"models/5amf_model.mtz": "MTZ\n",
	})
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeZipManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Models)
	assert.Equal(t, 3, summary.Artifacts)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 1)
	assert.Equal(t, "Deposited model", models[0].Name)
	assert.NotContains(t, uploadFilenames(dynamicPDBClient.uploads), "5amf_model.pdb")
	assert.NotContains(t, uploadFilenames(dynamicPDBClient.uploads), "5amf_model.log")
	assert.NotContains(t, uploadFilenames(dynamicPDBClient.uploads), "5amf_model.mtz")
}

func Test_should_upload_only_included_pdb_ids_when_filter_include_is_set(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/6abc_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeFilterManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Skipped)
	require.Len(t, dynamicPDBClient.entries, 1)
	assert.Equal(t, "5AMF", dynamicPDBClient.entries[0].Entry.Name)
	state := readState(t, summary.StatePath)
	assert.NotEmpty(t, state.Entries["5AMF"].EntryID)
}

func Test_should_override_manifest_include_when_upload_include_option_is_set(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/6abc_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeFilterManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(
		context.Background(),
		manifestPath,
		Options{Include: []string{"6ABC"}, OverrideInclude: true},
	)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Skipped)
	require.Len(t, dynamicPDBClient.entries, 1)
	assert.Equal(t, "6ABC", dynamicPDBClient.entries[0].Entry.Name)
}

func Test_should_override_manifest_skip_when_upload_skip_option_is_set(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/6abc_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	uploadManifest := simpleManifest(dataRoot)
	uploadManifest.Filter.Skip = []string{"5AMF"}
	writeTestManifest(t, manifestPath, uploadManifest)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(
		context.Background(),
		manifestPath,
		Options{Skip: []string{"6ABC"}, OverrideSkip: true},
	)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Skipped)
	require.Len(t, dynamicPDBClient.entries, 1)
	assert.Equal(t, "5AMF", dynamicPDBClient.entries[0].Entry.Name)
}

func Test_should_add_models_to_existing_entry_when_pdb_id_already_exists(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeFilterManifest(t, manifestPath, dataRoot)
	existingEntryID := "entry-5amf"
	dynamicPDBClient := &fakeDynamicPDBClient{
		existingEntries: []dynamicpdbapi.Entry{
			{
				ID:   existingEntryID,
				Name: "Existing 5AMF",
				Metadata: map[string]any{
					"external_refs": map[string]any{"pdb": "5AMF"},
				},
			},
		},
	}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Models)
	assert.Empty(t, dynamicPDBClient.entries)
	require.Len(t, dynamicPDBClient.models, 1)
	assert.Equal(t, existingEntryID, dynamicPDBClient.models[0].entryID)
	require.NotNil(t, dynamicPDBClient.models[0].model.IdempotencyKey)
	require.NotNil(t, dynamicPDBClient.models[0].model.Artifacts[0].SHA256)
	assert.Equal(t, *dynamicPDBClient.models[0].model.Artifacts[0].SHA256, *dynamicPDBClient.models[0].model.IdempotencyKey)
	state := readState(t, summary.StatePath)
	assert.Equal(t, existingEntryID, state.Entries["5AMF"].EntryID)
	assert.Equal(t, createdModelIDs(dynamicPDBClient.models), state.Entries["5AMF"].UploadedModelIDs)
}

func Test_should_add_models_to_existing_entry_when_create_entry_hits_pdb_ref_conflict(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeFilterManifest(t, manifestPath, dataRoot)
	existingEntryID := "entry-5amf"
	dynamicPDBClient := &fakeDynamicPDBClient{
		listResponses: [][]dynamicpdbapi.Entry{
			{},
			{
				{
					ID:   existingEntryID,
					Name: "Existing 5AMF",
					Metadata: map[string]any{
						"external_refs": map[string]any{"pdb": "5AMF"},
					},
				},
			},
		},
		createEntryError: &dynamicpdbapi.Error{
			Status:  http.StatusConflict,
			Code:    "ENTRY_PDB_REF_EXISTS",
			Message: "entry with this PDB reference already exists",
		},
	}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, dynamicPDBClient.createEntryAttempts)
	require.Len(t, dynamicPDBClient.models, 1)
	assert.Equal(t, existingEntryID, dynamicPDBClient.models[0].entryID)
	state := readState(t, summary.StatePath)
	assert.Equal(t, existingEntryID, state.Entries["5AMF"].EntryID)
	assert.Equal(t, createdModelIDs(dynamicPDBClient.models), state.Entries["5AMF"].UploadedModelIDs)
}

func Test_should_resume_from_completed_entries_in_upload_state(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/6abc_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeSimpleManifest(t, manifestPath, dataRoot)
	statePath := uploadStatePath(manifestPath)
	state := newUploadState()
	state.Entries["5AMF"] = EntryState{
		Status:  entryStatusCompleted,
		EntryID: "entry-5amf",
	}
	require.NoError(t, writeUploadState(statePath, state))
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Skipped)
	require.Len(t, dynamicPDBClient.entries, 1)
	assert.Equal(t, "6ABC", dynamicPDBClient.entries[0].Entry.Name)
	updatedState := readState(t, statePath)
	assert.Equal(t, entryStatusCompleted, updatedState.Entries["5AMF"].Status)
	assert.Equal(t, entryStatusCompleted, updatedState.Entries["6ABC"].Status)
	assert.Equal(t, "entry-5amf", updatedState.Entries["5AMF"].EntryID)
}

func Test_should_use_manifest_artifact_format_when_set(t *testing.T) {
	// given
	artifact := manifest.Artifact{
		ID:     "starting_structure",
		Format: "structure_factors_cif",
	}
	payload := extractorapi.Artifact{Format: "cif"}

	// when
	format := artifactFormat(artifact, payload)
	direction, ok := runArtifactDirection(uploadedArtifactRef{
		ManifestID: artifact.ID,
		Format:     format,
	})

	// then
	assert.Equal(t, "structure_factors_cif", format)
	require.True(t, ok)
	assert.Equal(t, "input", direction)
}

func Test_should_upload_entries_in_parallel_when_concurrency_is_greater_than_one(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/6abc_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/7def_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeSimpleManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}
	uploader := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 2)

	// when
	summary, err := uploader.Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 3, summary.Entries)
	assert.Equal(t, 3, summary.Models)
	assert.Equal(t, 3, summary.Artifacts)
	assert.Len(t, dynamicPDBClient.entries, 3)
	state := readState(t, summary.StatePath)
	assert.Equal(t, entryStatusCompleted, state.Entries["5AMF"].Status)
	assert.Equal(t, entryStatusCompleted, state.Entries["6ABC"].Status)
	assert.Equal(t, entryStatusCompleted, state.Entries["7DEF"].Status)
}

func Test_should_stop_when_upload_state_has_uploading_entry(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeSimpleManifest(t, manifestPath, dataRoot)
	state := newUploadState()
	state.Entries["5AMF"] = EntryState{Status: entryStatusUploading}
	require.NoError(t, writeUploadState(uploadStatePath(manifestPath), state))
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := fakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}, 1).Upload(context.Background(), manifestPath)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unfinished entries 5AMF")
	assert.Contains(t, err.Error(), "fix the failed upload and remove those entries")
	assert.Empty(t, summary)
	assert.Empty(t, dynamicPDBClient.entries)
	assert.Empty(t, dynamicPDBClient.models)
}

func Test_should_retry_upload_part_when_transient_upload_error_happens(t *testing.T) {
	// given
	dynamicPDBClient := &fakeDynamicPDBClient{
		putUploadPartErrors: []error{errors.New("read: connection reset by peer")},
	}
	uploader := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1)

	// when
	uploadedURL, err := uploader.uploadPayload(
		context.Background(),
		"entry-id",
		nil,
		"artifact-id",
		"model.pdb",
		int64(len("MODEL\n")),
		"",
		[]byte("MODEL\n"),
	)

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.test/artifact-id/model.pdb", uploadedURL)
	assert.Equal(t, 2, dynamicPDBClient.putUploadPartCalls)
	assert.Len(t, dynamicPDBClient.completed, 1)
}

func Test_should_retry_upload_part_when_s3_request_timeout_happens(t *testing.T) {
	// given
	dynamicPDBClient := &fakeDynamicPDBClient{
		putUploadPartErrors: []error{&dynamicpdbapi.Error{
			Status:  http.StatusBadRequest,
			Code:    "RequestTimeout",
			Message: "Your socket connection to the server was not read from or written to within the timeout period.",
		}},
	}
	uploader := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1)

	// when
	uploadedURL, err := uploader.uploadPayload(
		context.Background(),
		"entry-id",
		nil,
		"artifact-id",
		"model.pdb",
		int64(len("MODEL\n")),
		"",
		[]byte("MODEL\n"),
	)

	// then
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.test/artifact-id/model.pdb", uploadedURL)
	assert.Equal(t, 2, dynamicPDBClient.putUploadPartCalls)
	assert.Len(t, dynamicPDBClient.completed, 1)
}

func Test_should_upload_multipart_parts_in_parallel_when_part_concurrency_is_greater_than_one(t *testing.T) {
	// given
	dynamicPDBClient := &fakeDynamicPDBClient{}
	uploader := New(dynamicPDBClient, fakeRCSB{}, NoopProgress{}, 1, 4)
	parts := []dynamicpdbapi.FileUploadPart{
		{PartNumber: 2, URL: "https://upload.example.test/part-2"},
		{PartNumber: 1, URL: "https://upload.example.test/part-1"},
	}

	// when
	completedParts, err := uploader.uploadParts(
		context.Background(),
		parts,
		[]byte("MODEL DATA"),
		"",
		int64(len("MODEL DATA")),
		5,
	)

	// then
	require.NoError(t, err)
	require.Len(t, completedParts, 2)
	assert.Equal(t, 1, completedParts[0].PartNumber)
	assert.Equal(t, 2, completedParts[1].PartNumber)
	assert.Equal(t, 2, dynamicPDBClient.putUploadPartCalls)
}

func Test_should_parse_metrics_from_mmcif_artifact_when_metrics_source_references_artifact(t *testing.T) {
	// given
	artifactPath := filepath.Join(t.TempDir(), "5amf.cif")
	require.NoError(t, os.WriteFile(artifactPath, []byte("_refine.ls_r_factor_r_free 0.244\n_refine.ls_r_factor_r_work 0.198\n"), 0o644))
	metrics := manifest.Metrics{
		"r_free": {
			{
				Source: manifest.Source{Artifact: "coordinates"},
				Extract: manifest.Extract{
					MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"},
				},
			},
		},
		"r_work": {
			{
				Source: manifest.Source{Artifact: "coordinates"},
				Extract: manifest.Extract{
					MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_work"},
				},
			},
		},
	}

	// when
	requests, err := New(&fakeDynamicPDBClient{}, fakeRCSB{}, NoopProgress{}, 1).modelMetrics(
		context.Background(),
		"",
		"5amf",
		metrics,
		map[string]extractorapi.Artifact{
			"coordinates": {
				Filename:  "5amf.cif",
				LocalPath: artifactPath,
			},
		},
	)

	// then
	require.NoError(t, err)
	require.Len(t, requests, 2)
	assert.Equal(t, "r_free", requests[0].Key)
	assert.Equal(t, 0.244, requests[0].Value)
	assert.Equal(t, "r_work", requests[1].Key)
	assert.Equal(t, 0.198, requests[1].Value)
}

func Test_should_use_next_field_source_when_previous_source_has_no_value(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "metadata.csv", "pdb_id,title\n6ABC,\n")
	metadata := manifest.EntryMetadata{
		"title": {
			{
				Source: manifest.Source{Files: []string{"metadata.csv"}},
				Extract: manifest.Extract{CSV: &manifest.ExtractRule{
					Column: "title",
					Where:  &manifest.ExtractRule{Column: "pdb_id", Equals: "{{ pdb_id }}"},
				}},
			},
			{
				Source:  manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: "entry"}},
				Extract: manifest.Extract{JSON: &manifest.ExtractRule{Field: "struct.title"}},
			},
		},
	}

	// when
	values, err := New(&fakeDynamicPDBClient{}, fakeRCSB{}, NoopProgress{}, 1).entryMetadata(
		context.Background(),
		dataRoot,
		metadata,
		"5AMF",
	)

	// then
	require.NoError(t, err)
	assert.Equal(t, "example structure", values["title"])
}

func Test_should_prefer_manifest_artifact_name_over_filename(t *testing.T) {
	// given
	artifact := manifest.Artifact{
		ID:   "coordinates",
		Name: "Human name",
	}
	payload := extractorapi.Artifact{Filename: "5amf.cif"}

	// when
	name, err := artifactName(artifact, payload)

	// then
	require.NoError(t, err)
	assert.Equal(t, "Human name", name)
}

func Test_should_return_error_when_artifact_has_no_name_or_filename(t *testing.T) {
	// given
	artifact := manifest.Artifact{ID: "coordinates"}
	payload := extractorapi.Artifact{}

	// when
	_, err := artifactName(artifact, payload)

	// then
	require.Error(t, err)
}

func Test_should_split_semicolon_ligands_when_model_metadata_is_canonicalized(t *testing.T) {
	// given
	value := "CL;BME,HOH"

	// when
	metadataValue := toCanonicalModelMetadataValue("ligands", value)

	// then
	assert.Equal(t, []string{"CL", "BME"}, metadataValue)
}

func Test_should_parse_entry_resolution_string_when_metadata_is_canonicalized(t *testing.T) {
	// given
	value := "1.30"

	// when
	metadataValue := toCanonicalMetadataValue("resolution", value)

	// then
	assert.Equal(t, 1.30, metadataValue)
}

type fakeDynamicPDBClient struct {
	mutex               sync.Mutex
	existingEntries     []dynamicpdbapi.Entry
	listResponses       [][]dynamicpdbapi.Entry
	listCalls           int
	createEntryError    error
	createEntryAttempts int
	putUploadPartErrors []error
	putUploadPartCalls  int
	entries             []dynamicpdbapi.CreateEntryRequest
	models              []createdModel
	uploads             []dynamicpdbapi.CreateFileUploadRequest
	completed           []dynamicpdbapi.CompleteFileUploadRequest
}

type createdModel struct {
	entryID string
	modelID string
	model   dynamicpdbapi.CreateModelData
}

func (b *fakeDynamicPDBClient) ListEntries(_ context.Context, params dynamicpdbapi.ListEntriesParams) ([]dynamicpdbapi.Entry, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	if len(b.listResponses) > 0 {
		index := b.listCalls
		if index >= len(b.listResponses) {
			index = len(b.listResponses) - 1
		}
		b.listCalls++
		return b.listResponses[index], nil
	}
	wanted := make(map[string]struct{}, len(params.PDBIDs))
	for _, pdbID := range params.PDBIDs {
		wanted[canonicalPDBID(pdbID)] = struct{}{}
	}
	entries := make([]dynamicpdbapi.Entry, 0)
	for _, entry := range b.existingEntries {
		pdbID, ok := entryPDBID(entry)
		if !ok {
			continue
		}
		if _, ok := wanted[pdbID]; ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (b *fakeDynamicPDBClient) CreateEntry(
	_ context.Context,
	request dynamicpdbapi.CreateEntryRequest,
) (dynamicpdbapi.CreateEntryResult, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.createEntryAttempts++
	if b.createEntryError != nil {
		return dynamicpdbapi.CreateEntryResult{}, b.createEntryError
	}
	b.entries = append(b.entries, request)
	entryID := "dpdb_test0001"
	modelResults := make([]dynamicpdbapi.CreateEntryModelResult, 0, len(request.ModelOperations))
	for index, operation := range request.ModelOperations {
		modelID := fmt.Sprintf("%s_m_%03d", entryID, index+1)
		b.models = append(b.models, createdModel{
			entryID: entryID,
			modelID: modelID,
			model: dynamicpdbapi.CreateModelData{
				Name:              operation.Data.Name,
				Description:       operation.Data.Description,
				ThumbnailImageURL: operation.Data.ThumbnailImageURL,
				Metadata:          operation.Data.Metadata,
				IdempotencyKey:    operation.Data.IdempotencyKey,
				PrimaryArtifactID: operation.Data.PrimaryArtifactID,
				Artifacts:         operation.Data.Artifacts,
				Runs:              operation.Data.Runs,
				Metrics:           operation.Data.Metrics,
			},
		})
		modelResults = append(modelResults, dynamicpdbapi.CreateEntryModelResult{ModelID: modelID})
	}
	return dynamicpdbapi.CreateEntryResult{EntryID: entryID, ModelResults: modelResults}, nil
}

func (b *fakeDynamicPDBClient) CreateModel(
	_ context.Context,
	entryID string,
	request dynamicpdbapi.CreateModelRequest,
) (dynamicpdbapi.CreateModelResult, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	modelID := fmt.Sprintf("%s_m_%03d", entryID, len(b.models)+1)
	b.models = append(b.models, createdModel{entryID: entryID, modelID: modelID, model: request.Model})
	return dynamicpdbapi.CreateModelResult{ModelID: modelID}, nil
}

func (b *fakeDynamicPDBClient) CreateFileUpload(
	_ context.Context,
	request dynamicpdbapi.CreateFileUploadRequest,
) (dynamicpdbapi.FileUploadGrantResponse, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.uploads = append(b.uploads, request)
	return dynamicpdbapi.FileUploadGrantResponse{
		Key:       request.ArtifactID + "/" + request.Filename,
		UploadID:  "upload-id",
		ObjectURL: "https://cdn.example.test/" + request.ArtifactID + "/" + request.Filename,
		PartSize:  request.Size,
		Parts: []dynamicpdbapi.FileUploadPart{
			{PartNumber: 1, URL: "https://upload.example.test/part"},
		},
	}, nil
}

func (b *fakeDynamicPDBClient) CompleteFileUpload(
	_ context.Context,
	request dynamicpdbapi.CompleteFileUploadRequest,
) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.completed = append(b.completed, request)
	return nil
}

func (b *fakeDynamicPDBClient) PutUploadPart(_ context.Context, _ string, body io.Reader, _ int64) (string, error) {
	b.mutex.Lock()
	b.putUploadPartCalls++
	index := b.putUploadPartCalls - 1
	var uploadErr error
	if index < len(b.putUploadPartErrors) {
		uploadErr = b.putUploadPartErrors[index]
	}
	b.mutex.Unlock()
	if uploadErr != nil {
		return "", uploadErr
	}
	_, err := io.Copy(io.Discard, body)
	if err != nil {
		return "", err
	}
	return `"etag"`, nil
}

func assertRunArtifactDirections(t *testing.T, artifacts []dynamicpdbapi.CreateRunArtifactRequest, directions []string) {
	t.Helper()
	require.Len(t, artifacts, len(directions))
	for index, direction := range directions {
		assert.Equal(t, direction, artifacts[index].Direction)
	}
}

type fakeRCSB struct{}

func (fakeRCSB) GetEntry(_ context.Context, _ string) (map[string]any, error) {
	return map[string]any{
		"struct": map[string]any{"title": "example structure"},
		"exptl":  []any{map[string]any{"method": "X-RAY DIFFRACTION"}},
		"symmetry": map[string]any{
			"space_group_name_H_M": "P 21 21 21",
		},
		"rcsb_entry_info": map[string]any{
			"resolution_combined":                     []any{1.5},
			"deposited_atom_count":                    float64(1383),
			"deposited_modeled_polymer_monomer_count": float64(164),
			"deposited_polymer_entity_instance_count": float64(1),
			"nonpolymer_bound_components":             []any{"ATP"},
		},
		"rcsb_entry_container_identifiers": map[string]any{
			"polymer_entity_ids": []any{"1"},
		},
		"audit_author": []any{
			map[string]any{"name": "Nelson, R."},
			map[string]any{"name": "Sawaya, M.R."},
		},
		"pubmed": map[string]any{
			"rcsb_pubmed_affiliation_info": []any{"Howard Hughes Medical Institute, UCLA, USA."},
		},
		"refine": []any{
			map[string]any{
				"ls_R_factor_R_free": float64(0.21),
				"ls_R_factor_R_work": float64(0.18),
			},
		},
	}, nil
}

func (fakeRCSB) GetPolymerEntity(_ context.Context, _ string, _ string) (map[string]any, error) {
	return map[string]any{
		"rcsb_entity_source_organism": []any{
			map[string]any{"ncbi_scientific_name": "Homo sapiens"},
		},
	}, nil
}

type trackingFakeRCSB struct {
	files []string
}

func (f *trackingFakeRCSB) GetEntry(ctx context.Context, pdbID string) (map[string]any, error) {
	return fakeRCSB{}.GetEntry(ctx, pdbID)
}

func (f *trackingFakeRCSB) GetPolymerEntity(ctx context.Context, pdbID string, entityID string) (map[string]any, error) {
	return fakeRCSB{}.GetPolymerEntity(ctx, pdbID, entityID)
}

func (f *trackingFakeRCSB) GetFile(ctx context.Context, pdbID string, file string) (rcsb.Artifact, error) {
	f.files = append(f.files, file)
	return fakeRCSB{}.GetFile(ctx, pdbID, file)
}

func (f *trackingFakeRCSB) DownloadFile(ctx context.Context, pdbID string, file string) (rcsb.Artifact, error) {
	f.files = append(f.files, file)
	return fakeRCSB{}.DownloadFile(ctx, pdbID, file)
}

func (f *trackingFakeRCSB) GetImage(ctx context.Context, pdbID string, file string) (rcsb.Artifact, error) {
	return fakeRCSB{}.GetImage(ctx, pdbID, file)
}

func (f *trackingFakeRCSB) GetFASTA(ctx context.Context, pdbID string) (rcsb.Artifact, error) {
	return fakeRCSB{}.GetFASTA(ctx, pdbID)
}

func (fakeRCSB) GetFile(_ context.Context, _ string, file string) (rcsb.Artifact, error) {
	switch file {
	case "5amf.cif":
		return rcsb.Artifact{
			Filename: "5amf.cif",
			Format:   "cif",
			URI:      "https://files.rcsb.test/download/5AMF.cif",
		}, nil
	case "5amf-sf.cif":
		return rcsb.Artifact{
			Filename: "5amf-sf.cif",
			Format:   "structure_factors_cif",
			URI:      "https://files.rcsb.test/download/5AMF-sf.cif",
		}, nil
	default:
		return rcsb.Artifact{}, assert.AnError
	}
}

func (fakeRCSB) DownloadFile(ctx context.Context, pdbID string, file string) (rcsb.Artifact, error) {
	artifact, err := fakeRCSB{}.GetFile(ctx, pdbID, file)
	if err != nil {
		return rcsb.Artifact{}, err
	}
	switch file {
	case "5amf.cif":
		artifact.Contents = []byte(`data_5amf
loop_
_software.name
_software.classification
_software.version
_software.citation_id
_software.pdbx_ordinal
REFMAC refinement 5.2.0005 ? 1
`)
	case "5amf-sf.cif":
		artifact.Contents = []byte("structure factors\n")
	}
	return artifact, nil
}

func (fakeRCSB) GetImage(_ context.Context, _ string, file string) (rcsb.Artifact, error) {
	switch file {
	case "5amf_assembly-1.jpeg":
		return rcsb.Artifact{
			Filename: "5amf_assembly-1.jpeg",
			Contents: []byte("image\n"),
		}, nil
	default:
		return rcsb.Artifact{}, assert.AnError
	}
}

func (fakeRCSB) GetFASTA(_ context.Context, _ string) (rcsb.Artifact, error) {
	return rcsb.Artifact{
		Filename: "5amf.fasta",
		Format:   "fasta",
		URI:      "https://www.rcsb.test/fasta/entry/5AMF",
		Contents: []byte(">5amf\nACDE\n"),
	}, nil
}

func writeManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	writeTestManifest(t, path, testManifest(dataRoot, "models/{{ pdb_id }}_model.pdb", "models/{{ pdb_id }}_model.log", "models/{{ pdb_id }}_model.mtz"))
}

func writeRemoteMTZManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	uploadManifest := testManifest(dataRoot, "models/{{ pdb_id }}_model.pdb", "models/{{ pdb_id }}_model.log", "")
	uploadManifest.Entries[0].Metadata = manifest.EntryMetadata{"title": rcsbJSONExtraction("entry", "struct.title")}
	uploadManifest.Entries[0].Models[1].Artifacts = []manifest.Artifact{
		fileArtifact("coordinates", "models/{{ pdb_id }}_model.pdb", "L2"),
		fileArtifact("log_1", "models/{{ pdb_id }}_model.log", ""),
		rcsbFileArtifact("structure_factors_1", "{{ pdb_id }}-sf.cif", "L1"),
	}
	writeTestManifest(t, path, uploadManifest)
}

func writeZipManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	uploadManifest := testManifest(dataRoot, "models.zip#models/{{ pdb_id }}_model.pdb", "models.zip#models/{{ pdb_id }}_model.log", "models.zip#models/{{ pdb_id }}_model.mtz")
	uploadManifest.Entries[0].Metadata = manifest.EntryMetadata{"title": rcsbJSONExtraction("entry", "struct.title")}
	writeTestManifest(t, path, uploadManifest)
}

func writeFilterManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	uploadManifest := simpleManifest(dataRoot)
	uploadManifest.Filter.Include = []string{"5AMF"}
	writeTestManifest(t, path, uploadManifest)
}

func writeSimpleManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	writeTestManifest(t, path, simpleManifest(dataRoot))
}

func writeTestManifest(t *testing.T, path string, uploadManifest manifest.Manifest) {
	t.Helper()
	contents, err := yaml.Marshal(uploadManifest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, contents, 0o644))
}

func testManifest(dataRoot string, coordinateSource string, logSource string, mtzSource string) manifest.Manifest {
	entry := testEntry()
	entry.Metadata = manifest.EntryMetadata{
		"title":       rcsbJSONExtraction("entry", "struct.title"),
		"method":      rcsbJSONExtraction("entry", "exptl[0].method"),
		"organism":    rcsbJSONExtraction("polymer_entity", "rcsb_entity_source_organism.ncbi_scientific_name"),
		"resolution":  rcsbJSONExtraction("entry", "rcsb_entry_info.resolution_combined[0]"),
		"space_group": rcsbJSONExtraction("entry", "symmetry.space_group_name_H_M"),
	}
	entry.Models = []manifest.ModelPattern{
		depositedModel(),
		localModel("Rerefined model", coordinateSource, logSource, mtzSource),
	}
	return manifest.Manifest{
		Version:  1,
		DataRoot: dataRoot,
		Filter:   manifest.Filter{Include: []string{}, Skip: []string{}},
		Entries:  []manifest.Entry{entry},
	}
}

func simpleManifest(dataRoot string) manifest.Manifest {
	entry := manifest.Entry{
		PDBID: "{{ pdb_id }}",
		Name:  "{{ pdb_id }}",
		Models: []manifest.ModelPattern{
			{
				ID:        "model_1",
				Name:      "Uploaded model",
				ModelType: "",
				Purpose:   "",
				Artifacts: []manifest.Artifact{
					fileArtifact("coordinates", "models/{{ pdb_id }}_model.pdb", "L2"),
				},
				Metrics: pdbRefinementMetrics(),
			},
		},
	}
	return manifest.Manifest{
		Version:  1,
		DataRoot: dataRoot,
		Filter:   manifest.Filter{Include: []string{}, Skip: []string{}},
		Entries:  []manifest.Entry{entry},
	}
}

func testEntry() manifest.Entry {
	return manifest.Entry{
		PDBID:        "{{ pdb_id }}",
		Name:         "{{ pdb_id }}",
		PreviewImage: &manifest.EntryPreviewImage{Source: rcsbFileSource("{{ pdb_id }}_assembly-1.jpeg")},
		Artifacts: []manifest.Artifact{
			{
				ID:     "fasta",
				Source: rcsbResourceSource("fasta"),
				Level:  "L1",
			},
		},
	}
}

func depositedModel() manifest.ModelPattern {
	return manifest.ModelPattern{
		ID:        "model_1",
		Name:      "Deposited model",
		ModelType: "Deposited",
		Purpose:   "Reference",
		Metadata: manifest.ModelMetadata{
			"atom_count":            rcsbJSONExtraction("entry", "rcsb_entry_info.deposited_atom_count"),
			"modeled_residues":      rcsbJSONExtraction("entry", "rcsb_entry_info.deposited_modeled_polymer_monomer_count"),
			"unique_protein_chains": rcsbJSONExtraction("entry", "rcsb_entry_info.deposited_polymer_entity_instance_count"),
			"ligands":               rcsbJSONExtraction("entry", "rcsb_entry_info.nonpolymer_bound_components"),
			"authors":               rcsbJSONExtraction("entry", "audit_author.name"),
			"affiliation":           rcsbJSONExtraction("entry", "pubmed.rcsb_pubmed_affiliation_info"),
		},
		Artifacts: []manifest.Artifact{
			rcsbFileArtifact("coordinates", "{{ pdb_id }}.cif", "L2"),
			rcsbFileArtifact("structure_factors_1", "{{ pdb_id }}-sf.cif", "L1"),
		},
		Metrics: manifest.Metrics{
			"r_free": rcsbJSONExtraction("entry", "refine[0].ls_R_factor_R_free"),
			"r_work": rcsbJSONExtraction("entry", "refine[0].ls_R_factor_R_work"),
		},
	}
}

func localModel(name string, coordinateSource string, logSource string, mtzSource string) manifest.ModelPattern {
	artifacts := []manifest.Artifact{fileArtifact("coordinates", coordinateSource, "L2")}
	if logSource != "" {
		artifacts = append(artifacts, fileArtifact("log_1", logSource, ""))
	}
	if mtzSource != "" {
		artifacts = append(artifacts, fileArtifact("mtz_1", mtzSource, "L1"))
	}
	return manifest.ModelPattern{
		ID:        "model_2",
		Name:      name,
		ModelType: "",
		Purpose:   "",
		Metadata: manifest.ModelMetadata{
			"atom_count":            pdbArtifactExtraction("atom_count"),
			"modeled_residues":      pdbArtifactExtraction("modeled_residues"),
			"unique_protein_chains": pdbArtifactExtraction("unique_protein_chains"),
			"ligands":               pdbArtifactExtraction("ligands"),
		},
		Artifacts: artifacts,
		Metrics:   pdbRefinementMetrics(),
	}
}

func fileArtifact(id string, source string, level string) manifest.Artifact {
	return manifest.Artifact{
		ID:     id,
		Source: manifest.Source{Files: []string{source}},
		Level:  level,
	}
}

func rcsbFileArtifact(id string, file string, level string) manifest.Artifact {
	return manifest.Artifact{
		ID:     id,
		Source: rcsbFileSource(file),
		Level:  level,
	}
}

func rcsbFileSource(file string) manifest.Source {
	return manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", File: file}}
}

func rcsbResourceSource(resource string) manifest.Source {
	return manifest.Source{RCSB: &manifest.RCSBSource{PDBID: "{{ pdb_id }}", Resource: resource}}
}

func rcsbJSONExtraction(resource string, field string) []manifest.FieldExtraction {
	return []manifest.FieldExtraction{
		{
			Source: rcsbResourceSource(resource),
			Extract: manifest.Extract{
				JSON: &manifest.ExtractRule{Field: field},
			},
		},
	}
}

func pdbArtifactExtraction(field string) []manifest.FieldExtraction {
	return []manifest.FieldExtraction{
		{
			Source: manifest.Source{Artifact: "coordinates"},
			Extract: manifest.Extract{
				PDB: &manifest.ExtractRule{Field: field},
			},
		},
	}
}

func pdbRefinementMetrics() manifest.Metrics {
	return manifest.Metrics{
		"r_free": pdbArtifactExtraction("REMARK 3 FREE R VALUE"),
		"r_work": pdbArtifactExtraction("REMARK 3 R VALUE WORKING SET"),
	}
}

func writeFile(t *testing.T, root string, relativePath string, contents string) {
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

func pdbModelText() string {
	return "REMARK   3   PROGRAM     : PHENIX (2.0_5824: ???)\n" +
		"REMARK   3   R VALUE     (WORKING SET) : 0.191\n" +
		"REMARK   3   FREE R VALUE                     : 0.243\n" +
		"ATOM      1  N   ALA A   1      11.104  13.207   9.447  1.00 20.00           N\n" +
		"ATOM      2  CA  ALA A   1      12.104  13.207   9.447  1.00 20.00           C\n" +
		"ATOM      3  N   GLY A   2      13.104  13.207   9.447  1.00 20.00           N\n" +
		"HETATM    4  C1  ATP B 101      14.104  13.207   9.447  1.00 20.00           C\n" +
		"HETATM    5  O   HOH B 201      15.104  13.207   9.447  1.00 20.00           O\n" +
		"HETATM    6  H1  ATP B 101      16.104  13.207   9.447  1.00 20.00           H\n"
}

func uploadFilenames(uploadRequests []dynamicpdbapi.CreateFileUploadRequest) []string {
	filenames := make([]string, 0, len(uploadRequests))
	for _, request := range uploadRequests {
		filenames = append(filenames, request.Filename)
	}
	return filenames
}

func modelRequests(models []createdModel) []dynamicpdbapi.CreateModelData {
	requests := make([]dynamicpdbapi.CreateModelData, 0, len(models))
	for _, model := range models {
		requests = append(requests, model.model)
	}
	return requests
}

func createdModelIDs(models []createdModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.modelID)
	}
	return ids
}

func countString(values []string, target string) int {
	count := 0
	for _, value := range values {
		if value == target {
			count++
		}
	}
	return count
}

func readState(t *testing.T, path string) State {
	t.Helper()
	state, err := readUploadState(path)
	require.NoError(t, err)
	return state
}
