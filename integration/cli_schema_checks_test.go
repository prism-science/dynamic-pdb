package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The backend in these tests enforces Schema v0.2.0 (backend/config/local.yml), as
// dev does now and production will.

func Test_should_upload_computed_model_when_manifest_names_run_version_and_metric_software(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	binaryPath := buildCLI(t, root)
	backendBinaryPath := buildBackend(t, root)
	pdbID := integrationPDBID()
	pdbIDLower := strings.ToLower(pdbID)
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, fmt.Sprintf("models/%s_refined.pdb", pdbIDLower), pdbModelWithoutVersion())
	writeFile(t, dataRoot, fmt.Sprintf("scores/%s.json", pdbIDLower), `{"clashscore": 3.5}`)
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), computedModelManifest(dataRoot, `
        run:
          software_version: "4.0"`))

	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	reviewer := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	dataHome := t.TempDir()
	writeConfig(t, dataHome, backend.URL, auth.AccessToken)

	// when
	uploadOutput := runCLI(t, binaryPath, rcsbEnv(dataHome, rcsb.URL()), "upload", "start", manifestPath)

	// then
	assert.Contains(t, uploadOutput, "Uploaded 1 entries, 1 models, 2 artifacts.")
	activateUserRevisions(t, backend.URL, auth.AccessToken, auth.UserID, reviewer.AccessToken)
	entryList := getJSON[entryListResponse](t, backend.URL+"/v1/entries?pdb_id="+pdbID, auth.AccessToken)
	entryInfo := entryForUser(t, entryList.Items, auth.UserID)
	models := getJSON[modelListResponse](t, backend.URL+"/v1/entries/"+entryInfo.ID+"/models", auth.AccessToken)
	require.Len(t, models.Items, 1)
	model := models.Items[0]
	artifacts := getJSON[artifactListResponse](t, backend.URL+"/v1/entries/"+entryInfo.ID+"/models/"+model.ID+"/artifacts", auth.AccessToken)
	require.Len(t, artifacts.Runs, 1)
	run := artifacts.Runs[0]
	assert.Equal(t, "PROLSQ", run.Name, "the program name comes from the coordinates file header")
	require.NotNil(t, run.SoftwareVersion)
	assert.Equal(t, "4.0", *run.SoftwareVersion, "the version comes from the manifest's run block")
	assert.Equal(t, 1, countRunDirections(artifacts.Relations, "input"))
	assert.Equal(t, 1, countRunDirections(artifacts.Relations, "output"))
	assertMetricRun(t, model.Metrics, "r_free", run.ID)
	clashscore := findMetric(t, model.Metrics, "clashscore")
	assert.Equal(t, "MolProbity", stringValue(clashscore.SoftwareName))
	assert.Equal(t, "4.5.2", stringValue(clashscore.SoftwareVersion))
	assert.Nil(t, clashscore.RunID)
	assert.Equal(t, []string{pdbIDLower + "_refined.pdb"}, s3.uploadFilenames())
}

func Test_should_stop_before_uploading_when_computed_model_has_no_run(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	binaryPath := buildCLI(t, root)
	backendBinaryPath := buildBackend(t, root)
	pdbID := integrationPDBID()
	pdbIDLower := strings.ToLower(pdbID)
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, fmt.Sprintf("models/%s_refined.pdb", pdbIDLower), pdbModelWithoutVersion())
	writeFile(t, dataRoot, fmt.Sprintf("scores/%s.json", pdbIDLower), `{"clashscore": 3.5}`)
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")
	writeFile(t, filepath.Dir(manifestPath), filepath.Base(manifestPath), computedModelManifest(dataRoot, ""))

	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	dataHome := t.TempDir()
	writeConfig(t, dataHome, backend.URL, auth.AccessToken)

	// when
	firstOutput := runCLIError(t, binaryPath, rcsbEnv(dataHome, rcsb.URL()), "upload", "start", manifestPath)

	// then
	assert.Contains(t, firstOutput, pdbID+" would not pass the schema checks, so nothing of it was uploaded")
	assert.Contains(t, firstOutput, `run "PROLSQ" has no software version`)
	assert.Contains(t, firstOutput, "set run.software_version")
	assert.Empty(t, s3.uploadFilenames(), "no file may reach storage")
	stateContents, err := os.ReadFile(uploadStatePath(manifestPath))
	if err == nil {
		assert.NotContains(t, string(stateContents), `"event":"entry_uploading"`, "the entry must not be left unfinished")
	} else {
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	entryList := getJSON[entryListResponse](t, backend.URL+"/v1/entries?pdb_id="+pdbID, auth.AccessToken)
	for _, entry := range entryList.Items {
		assert.NotEqual(t, auth.UserID, entry.CreatedBy, "no entry may be created")
	}
}

// pdbModelWithoutVersion names its program without a version, as old PROLSQ files do.
func pdbModelWithoutVersion() string {
	return "REMARK   1 " + uuid.NewString() + "\n" +
		"REMARK   3   PROGRAM     : PROLSQ\n" +
		"REMARK   3   FREE R VALUE                     : 0.243\n" +
		"ATOM      1  N   ALA A   1      11.104  13.207   9.447  1.00 20.00           N\n" +
		"ATOM      2  CA  ALA A   1      12.104  13.207   9.447  1.00 20.00           C\n"
}

// computedModelManifest is a hand-written manifest with one computed model: local
// coordinates, structure factors from RCSB, r_free from the coordinates file and a
// clashscore whose extraction names MolProbity. runBlock is appended to the model.
func computedModelManifest(dataRoot string, runBlock string) string {
	return fmt.Sprintf(`version: 1
data_root: %s
filter:
  include: []
  skip: []
entries:
  - pdb_id: "{{ pdb_id }}"
    title: "{{ pdb_id }}"
    metadata:
      title:
        - source:
            rcsb:
              pdb_id: "{{ pdb_id }}"
              resource: entry
          extract:
            json:
              field: struct.title
      method:
        - source:
            rcsb:
              pdb_id: "{{ pdb_id }}"
              resource: entry
          extract:
            json:
              field: exptl[0].method
    models:
      - id: refined
        title: Refined model
        model_type: Single Conformer
        purpose: Refinement
        artifacts:
          - id: coordinates
            source:
              files:
                - models/{{ pdb_id }}_refined.pdb
            level: L2
          - id: structure_factors_1
            source:
              rcsb:
                pdb_id: "{{ pdb_id }}"
                file: "{{ pdb_id }}-sf.cif"
            level: L1
        metrics:
          r_free:
            - source:
                artifact: coordinates
              extract:
                pdb:
                  field: REMARK 3 FREE R VALUE
          clashscore:
            - source:
                files:
                  - scores/{{ pdb_id }}.json
              extract:
                json:
                  field: clashscore
              software:
                name: MolProbity
                version: 4.5.2%s
`, dataRoot, runBlock)
}

func rcsbEnv(dataHome string, rcsbURL string) []string {
	return []string{
		"XDG_DATA_HOME=" + dataHome,
		"DYNAMIC_PDB_RCSB_DATA_URL=" + rcsbURL,
		"DYNAMIC_PDB_RCSB_FILES_URL=" + rcsbURL,
		"DYNAMIC_PDB_RCSB_WWW_URL=" + rcsbURL,
		"DYNAMIC_PDB_RCSB_CDN_URL=" + rcsbURL,
	}
}
