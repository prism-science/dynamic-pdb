package upload

import (
	"archive/zip"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/cli/internal/dynamicpdbapi"
	"dynamic-pdb/cli/internal/rcsb"
	extractorapi "dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
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
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 2, summary.Models)
	assert.Equal(t, 6, summary.Artifacts)
	assert.Equal(t, uploadStatePath(manifestPath), summary.StatePath)
	require.Len(t, dynamicPDBClient.entries, 1)
	entry := dynamicPDBClient.entries[0]
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
	require.NotNil(t, entry.Artifacts[0].URI)
	assert.Equal(t, "https://www.rcsb.test/fasta/entry/5AMF", *entry.Artifacts[0].URI)
	assert.Empty(t, entry.Models)
	models := modelRequests(dynamicPDBClient.models)
	require.Len(t, models, 2)
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
	assert.Equal(t, "5amf_model.log", models[1].Artifacts[1].Name)
	assert.Equal(t, "5amf_model.mtz", models[1].Artifacts[2].Name)
	assert.Equal(t, "5amf-sf.cif", models[0].Artifacts[1].Name)
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
	require.NotNil(t, entry.ID)
	entryState := state.Entries["5AMF"]
	assert.Equal(t, entryStatusCompleted, entryState.Status)
	assert.Equal(t, *entry.ID, entryState.EntryID)
	assert.Equal(t, modelIDs(models), entryState.UploadedModelIDs)
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
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

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

func Test_should_download_structure_factors_from_rcsb_when_manifest_points_to_rcsb(t *testing.T) {
	// given
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "models/5amf_model.pdb", "MODEL\n")
	writeFile(t, dataRoot, "models/5amf_model.log", "LOG\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeRemoteMTZManifest(t, manifestPath, dataRoot)
	dynamicPDBClient := &fakeDynamicPDBClient{}
	rcsbClient := &trackingFakeRCSB{}

	// when
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

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
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Skipped)
	require.Len(t, dynamicPDBClient.entries, 1)
	assert.Equal(t, "5AMF", dynamicPDBClient.entries[0].Name)
	state := readState(t, summary.StatePath)
	assert.NotEmpty(t, state.Entries["5AMF"].EntryID)
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
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Models)
	assert.Empty(t, dynamicPDBClient.entries)
	require.Len(t, dynamicPDBClient.models, 1)
	assert.Equal(t, existingEntryID, dynamicPDBClient.models[0].entryID)
	state := readState(t, summary.StatePath)
	assert.Equal(t, existingEntryID, state.Entries["5AMF"].EntryID)
	assert.Equal(t, modelIDs([]dynamicpdbapi.CreateModelRequest{dynamicPDBClient.models[0].request}), state.Entries["5AMF"].UploadedModelIDs)
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
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, dynamicPDBClient.createEntryAttempts)
	require.Len(t, dynamicPDBClient.models, 1)
	assert.Equal(t, existingEntryID, dynamicPDBClient.models[0].entryID)
	state := readState(t, summary.StatePath)
	assert.Equal(t, existingEntryID, state.Entries["5AMF"].EntryID)
	assert.Equal(t, modelIDs([]dynamicpdbapi.CreateModelRequest{dynamicPDBClient.models[0].request}), state.Entries["5AMF"].UploadedModelIDs)
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
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Entries)
	assert.Equal(t, 1, summary.Skipped)
	require.Len(t, dynamicPDBClient.entries, 1)
	assert.Equal(t, "6ABC", dynamicPDBClient.entries[0].Name)
	updatedState := readState(t, statePath)
	assert.Equal(t, entryStatusCompleted, updatedState.Entries["5AMF"].Status)
	assert.Equal(t, entryStatusCompleted, updatedState.Entries["6ABC"].Status)
	assert.Equal(t, "entry-5amf", updatedState.Entries["5AMF"].EntryID)
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
	summary, err := New(dynamicPDBClient, rcsbClient, NoopProgress{}).Upload(context.Background(), manifestPath)

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unfinished entries 5AMF")
	assert.Contains(t, err.Error(), "fix the failed upload and remove those entries")
	assert.Empty(t, summary)
	assert.Empty(t, dynamicPDBClient.entries)
	assert.Empty(t, dynamicPDBClient.models)
}

func Test_should_parse_metrics_from_mmcif_artifact_when_metrics_source_references_artifact(t *testing.T) {
	// given
	artifactPath := filepath.Join(t.TempDir(), "5amf.cif")
	require.NoError(t, os.WriteFile(artifactPath, []byte("_refine.ls_r_factor_r_free 0.244\n_refine.ls_r_factor_r_work 0.198\n"), 0o644))
	metrics := manifest.Metrics{
		"r_free": {
			Source: manifest.Source{Artifact: "coordinates"},
			Extract: manifest.Extract{
				MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_free"},
			},
		},
		"r_work": {
			Source: manifest.Source{Artifact: "coordinates"},
			Extract: manifest.Extract{
				MMCIF: &manifest.ExtractRule{Field: "_refine.ls_R_factor_R_work"},
			},
		},
	}

	// when
	requests, err := New(&fakeDynamicPDBClient{}, fakeRCSB{}, NoopProgress{}).modelMetrics(
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

type fakeDynamicPDBClient struct {
	existingEntries     []dynamicpdbapi.Entry
	listResponses       [][]dynamicpdbapi.Entry
	listCalls           int
	createEntryError    error
	createEntryAttempts int
	entries             []dynamicpdbapi.CreateEntryRequest
	models              []createdModel
	uploads             []dynamicpdbapi.CreateFileUploadRequest
	completed           []dynamicpdbapi.CompleteFileUploadRequest
}

type createdModel struct {
	entryID string
	request dynamicpdbapi.CreateModelRequest
}

func (b *fakeDynamicPDBClient) ListEntries(_ context.Context, params dynamicpdbapi.ListEntriesParams) ([]dynamicpdbapi.Entry, error) {
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

func (b *fakeDynamicPDBClient) CreateEntry(_ context.Context, request dynamicpdbapi.CreateEntryRequest) error {
	b.createEntryAttempts++
	if b.createEntryError != nil {
		return b.createEntryError
	}
	b.entries = append(b.entries, request)
	return nil
}

func (b *fakeDynamicPDBClient) CreateModel(_ context.Context, entryID string, request dynamicpdbapi.CreateModelRequest) error {
	b.models = append(b.models, createdModel{entryID: entryID, request: request})
	return nil
}

func (b *fakeDynamicPDBClient) CreateFileUpload(
	_ context.Context,
	request dynamicpdbapi.CreateFileUploadRequest,
) (dynamicpdbapi.FileUploadGrantResponse, error) {
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
	b.completed = append(b.completed, request)
	return nil
}

func (b *fakeDynamicPDBClient) PutUploadPart(_ context.Context, _ string, body io.Reader, _ int64) (string, error) {
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
			Contents: []byte(`data_5amf
loop_
_software.name
_software.classification
_software.version
_software.citation_id
_software.pdbx_ordinal
REFMAC refinement 5.2.0005 ? 1
`),
		}, nil
	case "5amf-sf.cif":
		return rcsb.Artifact{
			Filename: "5amf-sf.cif",
			Format:   "structure_factors_cif",
			URI:      "https://files.rcsb.test/download/5AMF-sf.cif",
			Contents: []byte("structure factors\n"),
		}, nil
	default:
		return rcsb.Artifact{}, assert.AnError
	}
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
	contents := `version: 1
data_root: ` + dataRoot + `
entries:
  - pdb_id: '{{ pdb_id }}'
    name: '{{ pdb_id }}'
    metadata:
      title:
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: entry
        extract:
          json:
            field: struct.title
      method:
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: entry
        extract:
          json:
            field: exptl[0].method
      organism:
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: polymer_entity
        extract:
          json:
            field: rcsb_entity_source_organism.ncbi_scientific_name
      resolution:
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: entry
        extract:
          json:
            field: rcsb_entry_info.resolution_combined[0]
      space_group:
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: entry
        extract:
          json:
            field: symmetry.space_group_name_H_M
    preview_image:
      source:
        rcsb:
          pdb_id: '{{ pdb_id }}'
          file: '{{ pdb_id }}_assembly-1.jpeg'
    artifacts:
      - id: fasta
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: fasta
        level: L1
    models:
      - id: model_1
        name: Deposited model
        model_type: ""
        purpose: ""
        metadata:
          atom_count:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: rcsb_entry_info.deposited_atom_count
          modeled_residues:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: rcsb_entry_info.deposited_modeled_polymer_monomer_count
          unique_protein_chains:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: rcsb_entry_info.deposited_polymer_entity_instance_count
          ligands:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: rcsb_entry_info.nonpolymer_bound_components
          authors:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: audit_author.name
          affiliation:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: pubmed.rcsb_pubmed_affiliation_info
        artifacts:
          - id: coordinates
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                file: '{{ pdb_id }}.cif'
            level: L2
          - id: structure_factors_1
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                file: '{{ pdb_id }}-sf.cif'
            level: L1
        metrics:
          r_free:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_free
          r_work:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_work
      - id: model_2
        name: Rerefined model
        model_type: ""
        purpose: ""
        metadata:
          atom_count:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: atom_count
          modeled_residues:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: modeled_residues
          unique_protein_chains:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: unique_protein_chains
          ligands:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: ligands
        artifacts:
          - id: coordinates
            source:
              files:
                - models/{{ pdb_id }}_model.pdb
            level: L2
          - id: log_1
            source:
              files:
                - models/{{ pdb_id }}_model.log
          - id: mtz_1
            source:
              files:
                - models/{{ pdb_id }}_model.mtz
            level: L1
        metrics:
          r_free:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 FREE R VALUE
          r_work:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 R VALUE WORKING SET
`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func writeRemoteMTZManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	contents := `version: 1
data_root: ` + dataRoot + `
entries:
  - pdb_id: '{{ pdb_id }}'
    name: '{{ pdb_id }}'
    metadata:
      title:
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: entry
        extract:
          json:
            field: struct.title
    preview_image:
      source:
        rcsb:
          pdb_id: '{{ pdb_id }}'
          file: '{{ pdb_id }}_assembly-1.jpeg'
    artifacts:
      - id: fasta
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: fasta
        level: L1
    models:
      - id: model_1
        name: Deposited model
        model_type: ""
        purpose: ""
        artifacts:
          - id: coordinates
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                file: '{{ pdb_id }}.cif'
            level: L2
          - id: structure_factors_1
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                file: '{{ pdb_id }}-sf.cif'
            level: L1
        metrics:
          r_free:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_free
          r_work:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_work
      - id: model_2
        name: Rerefined model
        model_type: ""
        purpose: ""
        artifacts:
          - id: coordinates
            source:
              files:
                - models/{{ pdb_id }}_model.pdb
            level: L2
          - id: log_1
            source:
              files:
                - models/{{ pdb_id }}_model.log
          - id: structure_factors_1
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                file: '{{ pdb_id }}-sf.cif'
            level: L1
        metrics:
          r_free:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 FREE R VALUE
          r_work:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 R VALUE WORKING SET
`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func writeZipManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	contents := `version: 1
data_root: ` + dataRoot + `
entries:
  - pdb_id: '{{ pdb_id }}'
    name: '{{ pdb_id }}'
    metadata:
      title:
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: entry
        extract:
          json:
            field: struct.title
    preview_image:
      source:
        rcsb:
          pdb_id: '{{ pdb_id }}'
          file: '{{ pdb_id }}_assembly-1.jpeg'
    artifacts:
      - id: fasta
        source:
          rcsb:
            pdb_id: '{{ pdb_id }}'
            resource: fasta
        level: L1
    models:
      - id: model_1
        name: Deposited model
        model_type: ""
        purpose: ""
        artifacts:
          - id: coordinates
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                file: '{{ pdb_id }}.cif'
            level: L2
          - id: structure_factors_1
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                file: '{{ pdb_id }}-sf.cif'
            level: L1
        metrics:
          r_free:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_free
          r_work:
            source:
              rcsb:
                pdb_id: '{{ pdb_id }}'
                resource: entry
            extract:
              json:
                field: refine[0].ls_R_factor_R_work
      - id: model_2
        name: Rerefined model
        model_type: ""
        purpose: ""
        artifacts:
          - id: coordinates
            source:
              files:
                - models.zip#models/{{ pdb_id }}_model.pdb
            level: L2
          - id: log_1
            source:
              files:
                - models.zip#models/{{ pdb_id }}_model.log
          - id: mtz_1
            source:
              files:
                - models.zip#models/{{ pdb_id }}_model.mtz
            level: L1
        metrics:
          r_free:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 FREE R VALUE
          r_work:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 R VALUE WORKING SET
`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func writeFilterManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	contents := `version: 1
data_root: ` + dataRoot + `
filter:
  include:
    - 5AMF
  skip: []
entries:
  - pdb_id: '{{ pdb_id }}'
    name: '{{ pdb_id }}'
    models:
      - id: model_1
        name: Uploaded model
        model_type: ""
        purpose: ""
        artifacts:
          - id: coordinates
            source:
              files:
                - models/{{ pdb_id }}_model.pdb
            level: L2
        metrics:
          r_free:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 FREE R VALUE
          r_work:
            source:
              artifact: coordinates
            extract:
              pdb:
                field: REMARK 3 R VALUE WORKING SET
`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func writeSimpleManifest(t *testing.T, path string, dataRoot string) {
	t.Helper()
	contents := `version: 1
data_root: ` + dataRoot + `
entries:
  - pdb_id: '{{ pdb_id }}'
    name: '{{ pdb_id }}'
    models:
      - id: model_1
        name: Uploaded model
        model_type: ""
        purpose: ""
        artifacts:
          - id: coordinates
            source:
              files:
                - models/{{ pdb_id }}_model.pdb
            level: L2
`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
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

func modelRequests(models []createdModel) []dynamicpdbapi.CreateModelRequest {
	requests := make([]dynamicpdbapi.CreateModelRequest, 0, len(models))
	for _, model := range models {
		requests = append(requests, model.request)
	}
	return requests
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
