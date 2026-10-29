package integration

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_record_checksums_of_stored_and_external_files_when_checksum_job_runs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	jobBinaryPath := buildChecksumJob(t, root)
	// The job checks every due artifact, so it runs in a schema of its own instead of the
	// shared local database.
	database, schemaEnv := setupMigratedSchema(t, root)
	auth := seedUserAndIssueToken(t, database)
	reviewer := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	pdbID := integrationPDBID()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), schemaEnv)
	entryID, entryRevisionID := seedEntryRevision(t, database, auth.UserID)

	storedContent := "data_stored\n"
	stored := seedStoredArtifact(t, database, s3, entryID, entryRevisionID, auth.UserID, storedContent, sha256Hex(storedContent))
	changed := seedStoredArtifact(t, database, s3, entryID, entryRevisionID, auth.UserID, "data_changed\n", sha256Hex("data_declared\n"))
	externalPath := "/download/" + pdbID + "-sf.cif"
	external := seedArtifact(t, database, entryRevisionID, auth.UserID, rcsb.URL()+externalPath, nil)
	apiContent := "data_uploaded\n"
	apiEntryID, uploaded := createEntryWithStoredArtifact(t, backend.URL, auth.AccessToken, s3, apiContent)
	assert.Equal(t, "pending", checksumStatus(t, database, uploaded), "the API adds the checksum row with the artifact")

	// when
	output := runChecksumJob(t, root, jobBinaryPath, s3.URL(), schemaEnv)

	// then
	checksums := artifactChecksums(t, backend.URL+"/v1/entries/"+entryID+"/artifacts", auth.AccessToken)
	require.Contains(t, checksums, stored, output)
	assert.Equal(t, "verified", checksums[stored].Status, output)
	assert.Equal(t, sha256Hex(storedContent), stringValue(checksums[stored].SHA256))
	require.NotNil(t, checksums[stored].SizeBytes)
	assert.Equal(t, int64(len(storedContent)), *checksums[stored].SizeBytes)
	assert.NotNil(t, checksums[stored].CheckedAt)
	assert.Nil(t, checksums[stored].StoredURI)
	assert.Equal(t, "own_storage", stringValue(checksums[stored].CopyStatus))
	var storedCopyStatus string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select copy_status from artifact_checksums where artifact_id = $1`, stored).Scan(&storedCopyStatus))
	assert.Equal(t, "own_storage", storedCopyStatus)

	assert.Equal(t, "mismatch", checksums[changed].Status, output)
	assert.Equal(t, sha256Hex("data_changed\n"), stringValue(checksums[changed].SHA256))
	assert.Equal(t, "own_storage", stringValue(checksums[changed].CopyStatus), "a file in our bucket never needed a copy")

	// RCSB is stubbed on 127.0.0.1, which the job must refuse to reach.
	assert.Equal(t, "failed", checksums[external].Status, output)
	assert.Nil(t, checksums[external].CopyStatus)
	assert.NotContains(t, rcsb.paths(), externalPath)
	var lastError string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select last_error from artifact_checksums where artifact_id = $1`, external).Scan(&lastError))
	assert.Contains(t, lastError, "address is not allowed")

	activateUserRevisions(t, backend.URL, auth.AccessToken, auth.UserID, reviewer.AccessToken)
	apiChecksums := artifactChecksums(t, backend.URL+"/v1/entries/"+apiEntryID+"/artifacts", auth.AccessToken)
	require.Contains(t, apiChecksums, uploaded, output)
	assert.Equal(t, "verified", apiChecksums[uploaded].Status, output)
	assert.Equal(t, sha256Hex(apiContent), stringValue(apiChecksums[uploaded].SHA256))
	assert.Equal(t, "own_storage", stringValue(apiChecksums[uploaded].CopyStatus))
}

// createEntryWithStoredArtifact creates an entry through the API with one file stored in the S3
// stub, the way a client registers an upload, and returns the entry and artifact ids.
func createEntryWithStoredArtifact(t *testing.T, backendURL string, token string, s3 *s3Stub, content string) (string, uuid.UUID) {
	t.Helper()
	artifactID := uuid.New()
	objectPath := "/dynamic-pdb/uploads/artifacts/" + artifactID.String() + "/uploaded.cif"
	s3.recordBody(objectPath, []byte(content))
	body := map[string]any{
		"entry": map[string]any{
			"title":  "checksum api entry",
			"method": "X-ray crystallography",
			"artifacts": []map[string]any{{
				"id":         artifactID.String(),
				"name":       "uploaded.cif",
				"level":      "L0",
				"type":       "other",
				"format":     "cif",
				"uri":        s3.URL() + objectPath,
				"sha256":     sha256Hex(content),
				"size_bytes": len(content),
				"metadata":   map[string]any{},
			}},
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
	return document.Data.Attributes.EntryID, artifactID
}

func checksumStatus(t *testing.T, database integrationDB, artifactID uuid.UUID) string {
	t.Helper()
	var status string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select status from artifact_checksums where artifact_id = $1`, artifactID).Scan(&status))
	return status
}

func buildChecksumJob(t *testing.T, root string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "dynamic-pdb-checksum-job")
	cmd := exec.Command(goCommand(), "build", "-o", binaryPath, "./backend/cmd/checksum-job")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".tmp", "go-build-cache"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return binaryPath
}

func runChecksumJob(t *testing.T, root string, binaryPath string, s3URL string, extraEnv []string) string {
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

// setupMigratedSchema creates a schema with every migration applied and returns a connection
// to it and the environment that points the backend and the job at it.
func setupMigratedSchema(t *testing.T, root string) (integrationDB, []string) {
	t.Helper()
	schemaName := "integration_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	admin, err := sql.Open("postgres", dbDSN("sslmode=disable"))
	require.NoError(t, err)
	_, err = admin.ExecContext(t.Context(), fmt.Sprintf(`create schema "%s"`, schemaName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := admin.Exec(fmt.Sprintf(`drop schema "%s" cascade`, schemaName))
		assert.NoError(t, dropErr)
		assert.NoError(t, admin.Close())
	})

	connectionParams := "sslmode=disable search_path=" + schemaName
	conn, err := sql.Open("postgres", dbDSN(connectionParams))
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })

	paths, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "db", "public", "structure", "V*.sql"))
	require.NoError(t, err)
	sort.Strings(paths)
	for _, path := range paths {
		migration, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = conn.ExecContext(t.Context(), string(migration))
		require.NoError(t, err, "apply %s", filepath.Base(path))
	}
	return integrationDB{Conn: conn}, []string{"DYNAMIC_PDB_DB_CONNECTION_PARAMS=" + connectionParams}
}

func seedEntryRevision(t *testing.T, database integrationDB, createdBy string) (string, uuid.UUID) {
	t.Helper()
	entryID := "entry-" + uuid.NewString()
	revisionID := uuid.New()
	now := time.Now().UTC()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		_, err := transaction.ExecContext(t.Context(),
			`insert into entries(id, state, created_by, created_at) values ($1, 'active', $2, $3)`,
			entryID, createdBy, now)
		require.NoError(t, err)
		_, err = transaction.ExecContext(t.Context(),
			`insert into entry_revisions(
			   id, entry_id, revision_number, state, entry_state, published_at, title,
			   metadata, created_by, created_at, updated_at
			 ) values ($1, $2, 1, 'active', 'active', $3, 'checksum test entry', '{}'::jsonb, $4, $3, $3)`,
			revisionID, entryID, now, createdBy)
		require.NoError(t, err)
	})
	return entryID, revisionID
}

// seedArtifact inserts an artifact without a checksum row, as code from before the checksum
// table does, so the job's first step has to add the row.
func seedArtifact(
	t *testing.T,
	database integrationDB,
	entryRevisionID uuid.UUID,
	createdBy string,
	uri string,
	declaredSHA256 *string,
) uuid.UUID {
	t.Helper()
	artifactID := uuid.New()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		_, err := transaction.ExecContext(t.Context(),
			`insert into artifacts(id, name, level, type, uri, sha256, created_by)
			 values ($1, $2, 'L1', 'model', $3, $4, $5)`,
			artifactID, "checksum-"+artifactID.String()[:8]+".cif", uri, declaredSHA256, createdBy)
		require.NoError(t, err)
		_, err = transaction.ExecContext(t.Context(),
			`insert into entry_revision_artifacts(entry_revision_id, artifact_id) values ($1, $2)`,
			entryRevisionID, artifactID)
		require.NoError(t, err)
	})
	return artifactID
}

// seedStoredArtifact stores content in the S3 stub and inserts an artifact that points at it.
func seedStoredArtifact(
	t *testing.T,
	database integrationDB,
	s3 *s3Stub,
	entryID string,
	entryRevisionID uuid.UUID,
	createdBy string,
	content string,
	declaredSHA256 string,
) uuid.UUID {
	t.Helper()
	objectPath := "/dynamic-pdb/" + entryID + "/artifacts/" + uuid.NewString() + "/model.cif"
	s3.recordBody(objectPath, []byte(content))
	return seedArtifact(t, database, entryRevisionID, createdBy, s3.URL()+objectPath, &declaredSHA256)
}

type artifactChecksumResponse struct {
	Status     string     `json:"status"`
	SHA256     *string    `json:"sha256"`
	SizeBytes  *int64     `json:"size_bytes"`
	StoredURI  *string    `json:"stored_uri"`
	CopyStatus *string    `json:"copy_status"`
	CheckedAt  *time.Time `json:"checked_at"`
}

func artifactChecksums(t *testing.T, url string, token string) map[uuid.UUID]artifactChecksumResponse {
	t.Helper()
	var document struct {
		Data []struct {
			Attributes struct {
				ID       uuid.UUID                `json:"id"`
				Checksum artifactChecksumResponse `json:"checksum"`
			} `json:"attributes"`
		} `json:"data"`
	}
	body := getJSON[json.RawMessage](t, url, token)
	require.NoError(t, json.Unmarshal(body, &document))
	checksums := make(map[uuid.UUID]artifactChecksumResponse, len(document.Data))
	for _, resource := range document.Data {
		checksums[resource.Attributes.ID] = resource.Attributes.Checksum
	}
	return checksums
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
