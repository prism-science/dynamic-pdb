package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"math/rand/v2"
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
	backend := startBackend(t, root, backendBinaryPath, s3.URL())

	// The integration database outlives a run, so each run's sequences are random and only
	// similar to each other, which keeps earlier runs out of the source's similar entries.
	sourceSequence := randomProteinSequence(40)
	similarSequence := withResidueReplaced(sourceSequence, len(sourceSequence)-1)
	secondSimilarSequence := withResidueReplaced(sourceSequence, len(sourceSequence)-2)
	sourceEntryID := createProteinEntry(t, backend.URL, auth.AccessToken, "mmseqs-source", sourceSequence)
	similarEntryID := createProteinEntry(t, backend.URL, auth.AccessToken, "mmseqs-similar", similarSequence)
	secondSimilarEntryID := createProteinEntry(t, backend.URL, auth.AccessToken, "mmseqs-similar-2", secondSimilarSequence)
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
	assert.Equal(t, similarSequence, match.SimilarSequence.Sequence)
	assert.NotEmpty(t, match.Metadata["qaln"])
	assert.NotEmpty(t, match.Metadata["taln"])
	assert.NotEmpty(t, match.Metadata["fident"])
	assert.NotEmpty(t, match.Metadata["qcov"])
	assert.NotEmpty(t, match.Metadata["tcov"])

	walkedIDs := walkSimilarEntryIDsByLinks(t, backend.URL+"/v1/entries/"+sourceEntryID+"/similar-entries?limit=1", auth.AccessToken)
	assert.Equal(t, similarEntryIDs(similarEntries.Items), walkedIDs, "one entry per page walks the same list in the same order")
	assert.Contains(t, walkedIDs, similarEntryID)
	assert.Contains(t, walkedIDs, secondSimilarEntryID)
}

func randomProteinSequence(length int) string {
	const aminoAcids = "ACDEFGHIKLMNPQRSTVWY"
	var builder strings.Builder
	for range length {
		builder.WriteByte(aminoAcids[rand.IntN(len(aminoAcids))])
	}
	return builder.String()
}

// withResidueReplaced returns sequence with the residue at index swapped for a different one.
func withResidueReplaced(sequence string, index int) string {
	replacement := byte('W')
	if sequence[index] == replacement {
		replacement = 'Y'
	}
	return sequence[:index] + string(replacement) + sequence[index+1:]
}

// walkSimilarEntryIDsByLinks follows links.next from firstURL and returns the similar
// entry IDs of every page in order.
func walkSimilarEntryIDsByLinks(t *testing.T, firstURL string, token string) []string {
	t.Helper()
	const maxPages = 100
	ids := make([]string, 0)
	nextURL := firstURL
	for range maxPages {
		page := getJSON[similarEntryCursorPageResponse](t, nextURL, token)
		for _, item := range page.Data {
			ids = append(ids, item.Attributes.Entry.ID)
		}
		if page.Links == nil {
			return ids
		}
		nextURL = page.Links.Next
	}
	require.FailNow(t, "similar entries did not reach their last page", "after %d pages", maxPages)
	return nil
}

type similarEntryCursorPageResponse struct {
	Data []struct {
		Attributes similarEntryResponse `json:"attributes"`
	} `json:"data"`
	Links *struct {
		Next string `json:"next"`
	} `json:"links"`
}

func similarEntryIDs(entries []similarEntryResponse) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.Entry.ID)
	}
	return ids
}

func buildMMseqsJobImage(t *testing.T, root string) string {
	t.Helper()

	image := "dynamic-pdb-mmseqs-job-integration:" + strings.ReplaceAll(uuid.NewString(), "-", "")
	cmd := exec.Command(
		dockerCommand(),
		"build",
		"--build-context",
		"lib=lib",
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

func createProteinEntry(t *testing.T, backendURL string, token string, title string, sequence string) string {
	t.Helper()

	artifactID := uuid.NewString()
	body := map[string]any{
		"entry": map[string]any{
			"title":  title,
			"method": "X-ray crystallography",
			"artifacts": []map[string]any{
				{
					"id":     artifactID,
					"name":   title + ".fasta",
					"level":  "L0",
					"type":   "other",
					"format": "fasta",
					"uri":    "s3://dynamic-pdb/" + artifactID + ".fasta",
					"metadata": map[string]any{
						"records": []map[string]any{
							{
								"header":   title + ":A",
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
	var document struct {
		Data struct {
			Attributes struct {
				EntryID string `json:"entry_id"`
			} `json:"attributes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(responseBody, &document))
	require.NotEmpty(t, document.Data.Attributes.EntryID)
	return document.Data.Attributes.EntryID
}

func postJSON(t *testing.T, url string, token string, body any) *http.Response {
	t.Helper()

	var payload bytes.Buffer
	require.NoError(t, json.NewEncoder(&payload).Encode(body))

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, &payload)
	require.NoError(t, err)
	request.Header.Set("Accept", jsonAPIMediaType)
	request.Header.Set("Content-Type", jsonAPIMediaType)
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

func (r *similarEntryListResponse) UnmarshalJSON(data []byte) error {
	items, err := jsonAPICollectionAttributes[similarEntryResponse](data)
	if err != nil {
		return err
	}
	r.Items = items
	return nil
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
