package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_recalculate_protein_sequence_similarities_from_mmseqs_job(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	mmseqsJobImage := buildMMseqsJobImage(t, root)
	cacheDir := t.TempDir()

	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL(), auth.UserID)

	sourceEntryID := uuid.NewString()
	similarEntryID := uuid.NewString()
	createProteinEntry(t, backend.URL, auth.AccessToken, sourceEntryID, "mmseqs-source", "ACDEFGHIKLMNPQRSTVWYACDEFGHIKLMNPQRSTVWY")
	createProteinEntry(t, backend.URL, auth.AccessToken, similarEntryID, "mmseqs-similar", "ACDEFGHIKLMNPQRSTVWYACDEFGHIKLMNPQRSTVWF")
	activateUserRevisions(t, backend.URL, auth.AccessToken, auth.UserID)

	// when
	runMMseqsJobContainer(t, root, mmseqsJobImage, cacheDir)
	runMMseqsJobContainer(t, root, mmseqsJobImage, cacheDir)

	// then
	similarEntries := getJSON[similarEntryListResponse](t, backend.URL+"/v1/entries/"+sourceEntryID+"/similar-entries", auth.AccessToken)
	similarEntry := similarEntryByID(t, similarEntries.Items, similarEntryID)
	assert.NotZero(t, similarEntry.Score)

	require.NotEmpty(t, similarEntry.Matches)
	match := similarEntry.Matches[0]
	assert.Equal(t, "mmseqs2", match.Tool)
	assert.NotZero(t, match.Score)
	assert.Equal(t, "ACDEFGHIKLMNPQRSTVWYACDEFGHIKLMNPQRSTVWF", match.SimilarSequence.Sequence)
	assert.NotEmpty(t, match.Metadata["qaln"])
	assert.NotEmpty(t, match.Metadata["taln"])
	assert.NotEmpty(t, match.Metadata["fident"])
	assert.NotEmpty(t, match.Metadata["qcov"])
	assert.NotEmpty(t, match.Metadata["tcov"])
}

func buildMMseqsJobImage(t *testing.T, root string) string {
	t.Helper()

	image := "dynamic-pdb-mmseqs-job-integration:" + strings.ReplaceAll(uuid.NewString(), "-", "")
	cmd := exec.Command(
		dockerCommand(),
		"build",
		"-f",
		"backend/cmd/mmseqs-job/Dockerfile",
		"-t",
		image,
		"backend",
	)
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Run(), output.String())
	t.Cleanup(func() {
		removeCmd := exec.Command(dockerCommand(), "rmi", "-f", image)
		removeCmd.Dir = root
		removeOutput, err := removeCmd.CombinedOutput()
		if err != nil {
			t.Logf("remove mmseqs job image failed: %v: %s", err, string(removeOutput))
		}
	})
	return image
}

func runMMseqsJobContainer(t *testing.T, root string, image string, cacheDir string) {
	t.Helper()

	cmd := exec.Command(
		dockerCommand(),
		"run",
		"--rm",
		"--add-host=host.docker.internal:host-gateway",
		"-v",
		cacheDir+":/app/.tmp/mmseqs",
		"-e",
		"DYNAMIC_PDB_ENV=local",
		"-e",
		"DYNAMIC_PDB_DB_HOST=host.docker.internal",
		"-e",
		"DYNAMIC_PDB_DB_PORT=35432",
		"-e",
		"DYNAMIC_PDB_DB_NAME=dynamic_pdb_local",
		"-e",
		"DYNAMIC_PDB_DB_USERNAME=postgres",
		"-e",
		"DYNAMIC_PDB_DB_PASSWORD=password",
		"-e",
		"DYNAMIC_PDB_DB_CONNECTION_PARAMS=sslmode=disable",
		"-e",
		"DYNAMIC_PDB_MMSEQS_CACHE_DIR=/app/.tmp/mmseqs",
		image,
	)
	cmd.Dir = root
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Run(), output.String())
}

func createProteinEntry(t *testing.T, backendURL string, token string, entryID string, name string, sequence string) {
	t.Helper()

	artifactID := uuid.NewString()
	body := map[string]any{
		"entry": map[string]any{
			"id":   entryID,
			"name": name,
			"artifacts": []map[string]any{
				{
					"id":     artifactID,
					"name":   name + ".fasta",
					"level":  "L0",
					"format": "fasta",
					"uri":    "s3://dynamic-pdb/" + artifactID + ".fasta",
					"metadata": map[string]any{
						"records": []map[string]any{
							{
								"header":   name + ":A",
								"sequence": sequence,
							},
						},
					},
				},
			},
		},
	}

	response := postJSON(t, backendURL+"/v1/entries", token, body)
	defer func() {
		assert.NoError(t, response.Body.Close())
	}()
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(responseBody))
}

func postJSON(t *testing.T, url string, token string, body any) *http.Response {
	t.Helper()

	var payload bytes.Buffer
	require.NoError(t, json.NewEncoder(&payload).Encode(body))

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, &payload)
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	return response
}

func dockerCommand() string {
	if configured := strings.TrimSpace(os.Getenv("DOCKER")); configured != "" {
		return configured
	}
	return "docker"
}

type similarEntryListResponse struct {
	Items []similarEntryResponse `json:"items"`
}

type similarEntryResponse struct {
	Entry   entryInfo                 `json:"entry"`
	Score   float64                   `json:"score"`
	Matches []similarityMatchResponse `json:"matches"`
}

type similarityMatchResponse struct {
	SimilarSequence proteinSequence `json:"similar_sequence"`
	Score           float64         `json:"score"`
	Tool            string          `json:"tool"`
	Metadata        map[string]any  `json:"metadata"`
}

func similarEntryByID(t *testing.T, entries []similarEntryResponse, entryID string) similarEntryResponse {
	t.Helper()
	for _, entry := range entries {
		if entry.Entry.ID == entryID {
			return entry
		}
	}
	require.Failf(t, "missing similar entry", "entry %s was not found in %#v", entryID, entries)
	return similarEntryResponse{}
}
