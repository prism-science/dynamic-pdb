package integration

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mmcifWithGlycerol is a small mmCIF file modeling one glycerol.
const mmcifWithGlycerol = "data_TEST\nloop_\n_entity.id\n_entity.type\n1 polymer\n2 non-polymer\n#\n" +
	"loop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n" +
	"_atom_site.label_entity_id\n_atom_site.label_seq_id\n_atom_site.auth_seq_id\n_atom_site.auth_asym_id\n" +
	"ATOM ALA A 1 1 1 A\nHETATM GOL B 2 . 301 A\n#\n"

func Test_should_store_the_compounds_of_uploaded_model_files_when_the_model_compounds_job_runs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: two models added through the API, each with a file in our bucket: an mmCIF file
	// and a map, which is no structure file.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	jobBinaryPath := buildModelCompoundsJob(t, root)
	// The job reads every due file, so it runs in a schema of its own.
	database, schemaEnv := setupMigratedSchema(t, root)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), schemaEnv)
	entryID, _ := seedEntryRevision(t, database, auth.UserID)
	readable := createModelWithStoredFile(t, backend.URL, auth.AccessToken, s3, entryID, "model.cif", "mmcif", mmcifWithGlycerol)
	unreadable := createModelWithStoredFile(t, backend.URL, auth.AccessToken, s3, entryID, "map.ccp4", "ccp4", "not a structure")
	assert.Equal(t, "pending", modeledCompoundStatus(t, database, readable), "the API adds the set with the artifact")

	// when
	output := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), schemaEnv)

	// then
	assert.Equal(t, "done", modeledCompoundStatus(t, database, readable), output)
	rows, err := database.Conn.QueryContext(t.Context(),
		`select ccd_id || ' ' || auth_asym_id || ' ' || auth_seq_id || ' ' || source
		 from modeled_compound_records where artifact_id = $1 order by ordinal`, readable)
	require.NoError(t, err)
	defer func() { assert.NoError(t, rows.Close()) }()
	copies := []string{}
	for rows.Next() {
		var copied string
		require.NoError(t, rows.Scan(&copied))
		copies = append(copies, copied)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"GOL A 301 atom_site"}, copies)

	assert.Equal(t, "failed", modeledCompoundStatus(t, database, unreadable), output)
	var lastError string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select last_error from modeled_compound_sets where artifact_id = $1`, unreadable).Scan(&lastError))
	assert.Equal(t, `open the structure: declared format: unknown model file format "ccp4"`, lastError)
	assert.Contains(t, output, "model compounds job finished")
}

// createModelWithStoredFile adds a model through the API whose model file is stored in the
// S3 stub, the way a client registers an upload, and returns the artifact id.
func createModelWithStoredFile(
	t *testing.T,
	backendURL string,
	token string,
	s3 *s3Stub,
	entryID string,
	name string,
	format string,
	content string,
) uuid.UUID {
	t.Helper()
	artifactID := uuid.New()
	objectPath := "/dynamic-pdb/uploads/artifacts/" + artifactID.String() + "/" + name
	s3.recordBody(objectPath, []byte(content))
	body := map[string]any{
		"model": map[string]any{
			"title": "model compounds " + name,
			// A Deposited model needs no run recording how it was computed.
			"metadata": map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{{
				"id":         artifactID.String(),
				"name":       name,
				"level":      "L0",
				"type":       "model",
				"format":     format,
				"uri":        s3.URL() + objectPath,
				"sha256":     sha256Hex(content),
				"size_bytes": len(content),
				"metadata":   map[string]any{},
			}},
		},
	}
	response := postJSON(t, backendURL+"/v1/entries/"+entryID+"/models", token, body)
	defer func() {
		assert.NoError(t, response.Body.Close())
	}()
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(responseBody))
	return artifactID
}

func modeledCompoundStatus(t *testing.T, database integrationDB, artifactID uuid.UUID) string {
	t.Helper()
	var status string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select status from modeled_compound_sets where artifact_id = $1`, artifactID).Scan(&status))
	return status
}

func buildModelCompoundsJob(t *testing.T, root string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "dynamic-pdb-model-compounds-job")
	cmd := exec.Command(goCommand(), "build", "-o", binaryPath, "./backend/cmd/model-compounds-job")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".tmp", "go-build-cache"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return binaryPath
}

func runModelCompoundsJob(t *testing.T, root string, binaryPath string, s3URL string, extraEnv []string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binaryPath)
	cmd.Dir = filepath.Join(root, "backend")
	cmd.Env = append(os.Environ(),
		"DYNAMIC_PDB_ENV=local",
		"DYNAMIC_PDB_CDN_S3_ENDPOINT="+s3URL,
		"DYNAMIC_PDB_CDN_S3_ACCESS_KEY_ID=AKIAEXAMPLE",
		"DYNAMIC_PDB_CDN_S3_SECRET_ACCESS_KEY=secret",
		"AWS_EC2_METADATA_DISABLED=true",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Run(), output.String())
	return output.String()
}
