package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	integrationRunEnv = "DYNAMIC_PDB_RUN_INTEGRATION"
	jsonAPIMediaType  = "application/vnd.api+json"
	jwtSecret         = "sample-secret"
	jwtIssuer         = "dynamic-pdb-backend"
)

func Test_should_initialize_and_upload_manifest_from_cli(t *testing.T) {
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
	writeFile(t, dataRoot, fmt.Sprintf("Rerefined/final_model/%s_020.pdb", pdbIDLower), integrationPDBModelTextWithNonce())
	writeFile(t, dataRoot, fmt.Sprintf("Rerefined/final_model/%s_020.log", pdbIDLower), "LOG\n")
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")

	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()

	// when
	initOutput := runCLI(
		t,
		binaryPath,
		nil,
		"upload",
		"manifest",
		"init",
		dataRoot,
		"--out",
		manifestPath,
		"--include-rcsb-model",
	)
	dataHome := t.TempDir()
	writeConfig(t, dataHome, backend.URL, auth.AccessToken)
	uploadOutput := runCLI(t, binaryPath, []string{
		"XDG_DATA_HOME=" + dataHome,
		"DYNAMIC_PDB_RCSB_DATA_URL=" + rcsb.URL(),
		"DYNAMIC_PDB_RCSB_FILES_URL=" + rcsb.URL(),
		"DYNAMIC_PDB_RCSB_WWW_URL=" + rcsb.URL(),
		"DYNAMIC_PDB_RCSB_CDN_URL=" + rcsb.URL(),
	}, "upload", "start", manifestPath)

	// then
	assert.Contains(t, initOutput, "PDB IDs: 1")
	assert.Contains(t, initOutput, "Local files: 2")
	assert.Contains(t, uploadOutput, "Uploaded 1 entries, 2 models, 6 artifacts.")
	activateUserRevisions(t, backend.URL, auth.AccessToken, auth.UserID)

	entryList := getJSON[entryListResponse](t, backend.URL+"/v1/entries", auth.AccessToken)
	entryInfo := entryForUser(t, entryList.Items, auth.UserID)

	entry := getJSON[entryDocumentResponse](t, backend.URL+"/v1/entries/"+entryInfo.ID, auth.AccessToken).Entry()
	require.NotNil(t, entry.Title)
	assert.Equal(t, "example structure", *entry.Title)
	assert.Equal(t, "X-ray crystallography", entry.Metadata["method"])
	assert.NotContains(t, entry.Metadata, "organism")
	assert.Equal(t, 1.4, entry.Metadata["resolution"])
	assert.Equal(t, "P 21 21 21", entry.Metadata["space_group"])
	externalRefs, ok := entry.Metadata["external_refs"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, pdbID, externalRefs["pdb"])
	var dataSyncScheduledAt time.Time
	require.NoError(t, database.Conn.QueryRowContext(
		t.Context(),
		`select scheduled_at from data_sync_jobs where entry_id = $1 and model_id is null`,
		entry.ID,
	).Scan(&dataSyncScheduledAt))
	assert.WithinRange(
		t,
		dataSyncScheduledAt,
		time.Now().Add(6*24*time.Hour),
		time.Now().Add(15*24*time.Hour),
	)
	require.NotNil(t, entry.ThumbnailImageURL)
	assert.Contains(t, *entry.ThumbnailImageURL, pdbIDLower+"_assembly-1.jpeg")
	require.Len(t, entry.ProteinSequences, 1)
	assert.Equal(t, "ACDE", entry.ProteinSequences[0].Sequence)

	entryArtifacts := getJSON[artifactListResponse](t, backend.URL+"/v1/entries/"+entry.ID+"/artifacts", auth.AccessToken)
	assertArtifactNames(t, entryArtifacts.Items, []string{pdbIDLower + ".fasta"})

	models := getJSON[modelListResponse](t, backend.URL+"/v1/entries/"+entry.ID+"/models", auth.AccessToken)
	require.Len(t, models.Items, 2)
	sort.Slice(models.Items, func(i, j int) bool {
		return stringValue(models.Items[i].Title) < stringValue(models.Items[j].Title)
	})

	assertMetric(t, models.Items[0].Metrics, "r_free", 0.21)
	assertMetric(t, models.Items[1].Metrics, "r_free", 0.243)
	for _, model := range models.Items {
		artifacts := getJSON[artifactListResponse](t, backend.URL+"/v1/entries/"+entry.ID+"/models/"+model.ID+"/artifacts", auth.AccessToken)
		names := artifactNames(artifacts.Items)
		if stringValue(model.Title) == "Deposited model" {
			assert.Equal(t, []any{"Nelson, R.", "Sawaya, M.R."}, model.Metadata["authors"])
			assert.Equal(t, "Howard Hughes Medical Institute, UCLA, USA.", model.Metadata["affiliation"])
			assert.Equal(t, 1383.0, model.Metadata["atom_count"])
			assert.Equal(t, 164.0, model.Metadata["modeled_residues"])
			assert.Equal(t, 1.0, model.Metadata["unique_protein_chains"])
			assert.Equal(t, []any{"ATP"}, model.Metadata["ligands"])
			assert.Equal(t, []string{pdbIDLower + "-sf.cif", pdbIDLower + ".cif"}, names)
			require.Len(t, artifacts.Runs, 1)
			assert.Equal(t, "REFMAC", artifacts.Runs[0].Name)
			require.NotNil(t, artifacts.Runs[0].SoftwareVersion)
			assert.Equal(t, "5.2.0005", *artifacts.Runs[0].SoftwareVersion)
			assert.Equal(t, 1, countRunDirections(artifacts.Relations, "output"))
			assert.Equal(t, 1, countRunDirections(artifacts.Relations, "input"))
		} else {
			assert.Equal(t, 4.0, model.Metadata["atom_count"])
			assert.Equal(t, 2.0, model.Metadata["modeled_residues"])
			assert.Equal(t, 1.0, model.Metadata["unique_protein_chains"])
			assert.Equal(t, []any{"ATP"}, model.Metadata["ligands"])
		}
		if slices.Contains(names, pdbIDLower+"_020.log") {
			assert.Equal(t, []string{pdbIDLower + "-sf.cif", pdbIDLower + "_020.log", pdbIDLower + "_020.pdb"}, names)
			require.Len(t, artifacts.Runs, 1)
			assert.Equal(t, "PHENIX", artifacts.Runs[0].Name)
			require.NotNil(t, artifacts.Runs[0].SoftwareVersion)
			assert.Equal(t, "2.0_5824", *artifacts.Runs[0].SoftwareVersion)
			assert.Equal(t, 2, countRunDirections(artifacts.Relations, "output"))
			assert.Equal(t, 1, countRunDirections(artifacts.Relations, "input"))
		}
	}

	uploadedFilenames := s3.uploadFilenames()
	assert.Equal(t, 1, countString(uploadedFilenames, pdbIDLower+"_assembly-1.jpeg"))
	assert.Equal(t, 0, countString(uploadedFilenames, pdbIDLower+".fasta"))
	assert.Equal(t, 0, countString(uploadedFilenames, pdbIDLower+".cif"))
	assert.Equal(t, 0, countString(uploadedFilenames, pdbIDLower+"-sf.cif"))
	assert.Equal(t, 1, countString(uploadedFilenames, pdbIDLower+"_020.pdb"))
	assert.Equal(t, 1, countString(uploadedFilenames, pdbIDLower+"_020.log"))
	assert.False(t, slices.Contains(uploadedFilenames, pdbIDLower+"_020.mtz"))

	paths := rcsb.paths()
	assert.Contains(t, paths, "/rest/v1/core/entry/"+pdbID)
	assert.Contains(t, paths, "/download/"+pdbID+".cif")
	assert.Contains(t, paths, fmt.Sprintf("/images/structures/%s/%s/%s_assembly-1.jpeg", pdbIDLower[1:3], pdbIDLower, pdbIDLower))
	assert.Equal(t, 0, countString(paths, "/download/"+pdbID+"-sf.cif"))
	assert.Contains(t, paths, "/fasta/entry/"+pdbID)
}

func Test_should_stop_restart_when_previous_upload_left_unfinished_entry_state(t *testing.T) {
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
	dataRoot := t.TempDir()
	writeFile(t, dataRoot, "Rerefined/final_model/9zzz_020.pdb", integrationPDBModelText())
	manifestPath := filepath.Join(t.TempDir(), "dynamic-pdb.manifest.yaml")

	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	brokenRCSB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "RCSB is down", http.StatusInternalServerError)
	}))
	defer brokenRCSB.Close()
	dataHome := t.TempDir()
	writeConfig(t, dataHome, backend.URL, auth.AccessToken)
	env := []string{
		"XDG_DATA_HOME=" + dataHome,
		"DYNAMIC_PDB_RCSB_DATA_URL=" + brokenRCSB.URL,
		"DYNAMIC_PDB_RCSB_FILES_URL=" + brokenRCSB.URL,
		"DYNAMIC_PDB_RCSB_WWW_URL=" + brokenRCSB.URL,
		"DYNAMIC_PDB_RCSB_CDN_URL=" + brokenRCSB.URL,
	}

	// when
	runCLI(t, binaryPath, nil, "upload", "manifest", "init", dataRoot, "--out", manifestPath)
	firstOutput := runCLIError(t, binaryPath, env, "upload", "start", manifestPath)
	secondOutput := runCLIError(t, binaryPath, env, "upload", "start", manifestPath)

	// then
	statePath := uploadStatePath(manifestPath)
	stateContents, err := os.ReadFile(statePath)
	require.NoError(t, err)
	stateLines := strings.Split(strings.TrimSpace(string(stateContents)), "\n")
	require.Len(t, stateLines, 1)
	assert.Contains(t, stateLines[0], `"event":"entry_uploading"`)
	assert.Contains(t, stateLines[0], `"pdb_id":"9ZZZ"`)
	assert.NotContains(t, string(stateContents), `"event":"entry_completed"`)
	assert.Contains(t, firstOutput, "unexpected HTTP status 500")
	assert.Contains(t, secondOutput, "unfinished entries 9ZZZ")
	assert.Contains(t, secondOutput, "fix the failed upload and remove those entries")

	entryList := getJSON[entryListResponse](t, backend.URL+"/v1/entries", auth.AccessToken)
	for _, entry := range entryList.Items {
		assert.NotEqual(t, "9ZZZ", stringValue(entry.Title))
	}
}

type integrationDB struct {
	Conn *sql.DB
}

func setupDB(t *testing.T) integrationDB {
	t.Helper()
	conn, err := sql.Open("postgres", dbDSN("sslmode=disable"))
	require.NoError(t, err)
	require.NoError(t, conn.Ping())
	t.Cleanup(func() {
		assert.NoError(t, conn.Close())
	})

	return integrationDB{Conn: conn}
}

type testAuth struct {
	UserID      string
	AccessToken string
}

func seedUserAndIssueToken(t *testing.T, database integrationDB) testAuth {
	t.Helper()
	userID := uuid.New()
	now := time.Now().UTC()
	_, err := database.Conn.Exec(
		`insert into users(id, source, external_ref, email, display_name, avatar_url, created_at, updated_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userID,
		"github",
		userID.String(),
		"integration@example.com",
		"Integration User",
		"",
		now,
		now,
	)
	require.NoError(t, err)
	result, err := database.Conn.Exec(
		`insert into user_roles(user_id, role_id, granted_by)
		 select $1, roles.id, $1
		 from roles
		 where roles.key = 'reviewer'`,
		userID,
	)
	require.NoError(t, err)
	assignedRoles, err := result.RowsAffected()
	require.NoError(t, err)
	require.Equal(t, int64(1), assignedRoles)

	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		Issuer:    jwtIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtSecret))
	require.NoError(t, err)
	return testAuth{UserID: userID.String(), AccessToken: token}
}

func dbDSN(connectionParams string) string {
	return "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password " + connectionParams
}

type backendProcess struct {
	URL string
}

func startBackend(t *testing.T, root string, binaryPath string, s3URL string) backendProcess {
	t.Helper()
	addr := freeAddress(t)
	ctx, cancel := context.WithCancel(t.Context())
	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Dir = filepath.Join(root, "backend")
	cmd.Env = append(os.Environ(),
		"DYNAMIC_PDB_ENV=local",
		"DYNAMIC_PDB_SERVER_ADDR="+addr,
		"DYNAMIC_PDB_DB_CONNECTION_PARAMS=sslmode=disable",
		"DYNAMIC_PDB_CDN_S3_ENDPOINT="+s3URL,
		"DYNAMIC_PDB_CDN_S3_ACCESS_KEY_ID=AKIAEXAMPLE",
		"DYNAMIC_PDB_CDN_S3_SECRET_ACCESS_KEY=secret",
		"AWS_EC2_METADATA_DISABLED=true",
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(output.String())
		}
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
		}
	})

	url := "http://" + addr
	require.Eventually(t, func() bool {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			t.Log(output.String())
			return false
		}
		response, err := http.Get(url + "/readyz")
		if err != nil {
			return false
		}
		defer func() {
			assert.NoError(t, response.Body.Close())
		}()
		return response.StatusCode == http.StatusOK
	}, 15*time.Second, 100*time.Millisecond, output.String())
	return backendProcess{URL: url}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, listener.Close())
	}()
	return listener.Addr().String()
}

type s3Stub struct {
	server *httptest.Server
	mu     sync.Mutex
	paths  []string
	bodies map[string][]byte
}

func newS3Stub(t *testing.T) *s3Stub {
	t.Helper()
	stub := &s3Stub{bodies: make(map[string][]byte)}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.handle))
	return stub
}

func (s *s3Stub) URL() string {
	return s.server.URL
}

func (s *s3Stub) Close() {
	s.server.Close()
}

func (s *s3Stub) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.Contains(r.URL.RawQuery, "uploads"):
		s.recordPath(r.URL.Path)
		writeXML(w, `<CreateMultipartUploadResult><UploadId>upload-id</UploadId></CreateMultipartUploadResult>`)
	case r.Method == http.MethodPut && r.URL.Query().Get("uploadId") != "":
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read upload body", http.StatusInternalServerError)
			return
		}
		closeErr := r.Body.Close()
		if closeErr != nil {
			http.Error(w, "close upload body", http.StatusInternalServerError)
			return
		}
		s.recordBody(r.URL.Path, body)
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && r.URL.Query().Get("uploadId") != "":
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "read complete body", http.StatusInternalServerError)
			return
		}
		closeErr := r.Body.Close()
		if closeErr != nil {
			http.Error(w, "close complete body", http.StatusInternalServerError)
			return
		}
		writeXML(w, `<CompleteMultipartUploadResult></CompleteMultipartUploadResult>`)
	default:
		http.NotFound(w, r)
	}
}

func (s *s3Stub) recordPath(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, value)
}

func (s *s3Stub) recordBody(key string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies[key] = append([]byte(nil), body...)
}

func (s *s3Stub) uploadFilenames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	filenames := make([]string, 0, len(s.paths))
	for _, value := range s.paths {
		filenames = append(filenames, path.Base(value))
	}
	sort.Strings(filenames)
	return filenames
}

type rcsbStub struct {
	server *httptest.Server
	mu     sync.Mutex
	pdbID  string
	nonce  string
	seen   []string
}

func newRCSBStub(t *testing.T, pdbID string) *rcsbStub {
	t.Helper()
	stub := &rcsbStub{pdbID: pdbID, nonce: uuid.NewString()}
	stub.server = httptest.NewServer(http.HandlerFunc(stub.handle))
	return stub
}

func (s *rcsbStub) URL() string {
	return s.server.URL
}

func (s *rcsbStub) Close() {
	s.server.Close()
}

func (s *rcsbStub) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.seen = append(s.seen, r.URL.Path)
	s.mu.Unlock()

	pdbIDLower := strings.ToLower(s.pdbID)
	switch r.URL.Path {
	case "/rest/v1/core/entry/" + s.pdbID:
		writeJSON(w, map[string]any{
			"struct": map[string]any{"title": "example structure"},
			"exptl":  []map[string]any{{"method": "X-RAY DIFFRACTION"}},
			"rcsb_entry_info": map[string]any{
				"resolution_combined":                     []float64{1.4},
				"deposited_atom_count":                    1383,
				"deposited_modeled_polymer_monomer_count": 164,
				"deposited_polymer_entity_instance_count": 1,
				"nonpolymer_bound_components":             []string{"ATP", "HOH"},
			},
			"symmetry": map[string]any{"space_group_name_H_M": "P 21 21 21"},
			"rcsb_entry_container_identifiers": map[string]any{
				"polymer_entity_ids": []string{"1"},
			},
			"refine": []map[string]any{{"ls_R_factor_R_free": 0.21, "ls_R_factor_R_work": 0.18}},
			"audit_author": []map[string]any{
				{"name": "Nelson, R."},
				{"name": "Sawaya, M.R."},
			},
			"pubmed": map[string]any{"rcsb_pubmed_affiliation_info": []string{"Howard Hughes Medical Institute, UCLA, USA."}},
		})
	case "/rest/v1/core/polymer_entity/" + s.pdbID + "/1":
		writeJSON(w, map[string]any{
			"rcsb_entity_source_organism": []map[string]any{{"ncbi_scientific_name": "Homo sapiens"}},
		})
	case "/download/" + s.pdbID + ".cif":
		writeText(w, `data_`+pdbIDLower+`
loop_
_software.name
_software.classification
_software.version
_software.citation_id
_software.pdbx_ordinal
REFMAC refinement 5.2.0005 ? 1
`+s.nonce+"\n")
	case "/download/" + s.pdbID + "-sf.cif":
		writeText(w, "data_"+pdbIDLower+"_sf\n")
	case "/fasta/entry/" + s.pdbID:
		writeText(w, ">"+pdbIDLower+"\nACDE\n")
	case fmt.Sprintf("/images/structures/%s/%s/%s_assembly-1.jpeg", pdbIDLower[1:3], pdbIDLower, pdbIDLower):
		writeText(w, "jpeg")
	case "/graphql":
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"entry": map[string]any{
					"audit_author": []map[string]any{
						{"name": "Nelson, R."},
						{"name": "Sawaya, M.R."},
					},
					"rcsb_primary_citation": map[string]any{"rcsb_authors": []string{"Citation, A."}},
					"pubmed":                map[string]any{"rcsb_pubmed_affiliation_info": []string{"Howard Hughes Medical Institute, UCLA, USA."}},
				},
			},
		})
	default:
		http.NotFound(w, r)
	}
}

func (s *rcsbStub) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

type entryListResponse struct {
	Items []entryInfo `json:"items"`
}

func (r *entryListResponse) UnmarshalJSON(data []byte) error {
	items, err := jsonAPICollectionAttributes[entryInfo](data)
	if err != nil {
		return err
	}
	r.Items = items
	return nil
}

type entryRevisionGroupListResponse struct {
	Items []entryRevisionGroupResponse `json:"items"`
}

func (r *entryRevisionGroupListResponse) UnmarshalJSON(data []byte) error {
	items, err := jsonAPICollectionAttributes[entryRevisionGroupResponse](data)
	if err != nil {
		return err
	}
	r.Items = items
	return nil
}

type entryRevisionGroupResponse struct {
	EntryRevisions []entryRevisionSummaryResponse `json:"entry_revisions"`
	ModelRevisions []modelRevisionSummaryResponse `json:"model_revisions"`
}

type entryRevisionSummaryResponse struct {
	ID      string `json:"id"`
	EntryID string `json:"entry_id"`
}

type modelRevisionSummaryResponse struct {
	ID      string `json:"id"`
	EntryID string `json:"entry_id"`
	ModelID string `json:"model_id"`
}

type entryInfo struct {
	ID        string  `json:"id"`
	Title     *string `json:"title"`
	CreatedBy string  `json:"created_by"`
}

type entryResponse struct {
	ID                string            `json:"id"`
	Title             *string           `json:"title"`
	ThumbnailImageURL *string           `json:"thumbnail_image_url"`
	Metadata          map[string]any    `json:"metadata"`
	ProteinSequences  []proteinSequence `json:"protein_sequences"`
}

type entryDocumentResponse struct {
	Data     entryResourceResponse             `json:"data"`
	Included []proteinSequenceResourceResponse `json:"included"`
}

func (r entryDocumentResponse) Entry() entryResponse {
	sequences := make([]proteinSequence, 0, len(r.Included))
	for _, resource := range r.Included {
		sequences = append(sequences, proteinSequence{Sequence: resource.Attributes.Sequence})
	}
	return entryResponse{
		ID:                r.Data.ID,
		Title:             r.Data.Attributes.Title,
		ThumbnailImageURL: r.Data.Attributes.ThumbnailImageURL,
		Metadata:          r.Data.Attributes.Metadata,
		ProteinSequences:  sequences,
	}
}

type entryResourceResponse struct {
	ID         string                  `json:"id"`
	Attributes entryAttributesResponse `json:"attributes"`
}

type entryAttributesResponse struct {
	Title             *string        `json:"title"`
	ThumbnailImageURL *string        `json:"thumbnail_image_url"`
	Metadata          map[string]any `json:"metadata"`
}

type proteinSequenceResourceResponse struct {
	Attributes proteinSequenceAttributesResponse `json:"attributes"`
}

type proteinSequenceAttributesResponse struct {
	Sequence string `json:"sequence"`
}

type proteinSequence struct {
	Sequence string `json:"sequence"`
}

type modelListResponse struct {
	Items []modelResponse `json:"items"`
}

func (r *modelListResponse) UnmarshalJSON(data []byte) error {
	items, err := jsonAPICollectionAttributes[modelResponse](data)
	if err != nil {
		return err
	}
	r.Items = items
	return nil
}

type modelResponse struct {
	ID       string           `json:"id"`
	Title    *string          `json:"title"`
	Metadata map[string]any   `json:"metadata"`
	Metrics  []metricResponse `json:"metrics"`
}

type metricResponse struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
}

type artifactListResponse struct {
	Items     []artifactResponse    `json:"items"`
	Runs      []runResponse         `json:"runs"`
	Relations []runArtifactResponse `json:"relations"`
}

func (r *artifactListResponse) UnmarshalJSON(data []byte) error {
	var document struct {
		Data []struct {
			Attributes artifactResponse `json:"attributes"`
		} `json:"data"`
		Meta struct {
			Runs      []runResponse         `json:"runs"`
			Relations []runArtifactResponse `json:"relations"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	r.Items = make([]artifactResponse, 0, len(document.Data))
	for _, resource := range document.Data {
		r.Items = append(r.Items, resource.Attributes)
	}
	r.Runs = document.Meta.Runs
	r.Relations = document.Meta.Relations
	return nil
}

type artifactResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type runResponse struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	SoftwareVersion *string `json:"software_version"`
}

type runArtifactResponse struct {
	RunID      string `json:"run_id"`
	ArtifactID string `json:"artifact_id"`
	Direction  string `json:"direction"`
}

func jsonAPICollectionAttributes[T any](data []byte) ([]T, error) {
	var document struct {
		Data []struct {
			Attributes T `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	items := make([]T, 0, len(document.Data))
	for _, resource := range document.Data {
		items = append(items, resource.Attributes)
	}
	return items, nil
}

func getJSON[T any](t *testing.T, url string, token string) T {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", jsonAPIMediaType)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, response.Body.Close())
	}()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var result T
	require.NoError(t, json.Unmarshal(body, &result))
	return result
}

func patchJSON(t *testing.T, url string, token string, body any, expectedStatus int) {
	t.Helper()
	var payload bytes.Buffer
	require.NoError(t, json.NewEncoder(&payload).Encode(body))
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, url, &payload)
	require.NoError(t, err)
	request.Header.Set("Accept", jsonAPIMediaType)
	request.Header.Set("Content-Type", jsonAPIMediaType)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, response.Body.Close())
	}()
	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, expectedStatus, response.StatusCode, string(responseBody))
}

func activateUserRevisions(t *testing.T, backendURL string, token string, userID string) {
	t.Helper()
	groups := getJSON[entryRevisionGroupListResponse](
		t,
		backendURL+"/v1/users/"+userID+"/entries/revisions?state=in_review",
		token,
	)
	for _, group := range groups.Items {
		for _, revision := range group.EntryRevisions {
			patchJSON(
				t,
				fmt.Sprintf("%s/v1/entries/%s/revisions/%s", backendURL, revision.EntryID, revision.ID),
				token,
				map[string]any{"state": "active"},
				http.StatusOK,
			)
		}
		for _, revision := range group.ModelRevisions {
			patchJSON(
				t,
				fmt.Sprintf(
					"%s/v1/entries/%s/models/%s/revisions/%s",
					backendURL,
					revision.EntryID,
					revision.ModelID,
					revision.ID,
				),
				token,
				map[string]any{"state": "active"},
				http.StatusOK,
			)
		}
	}
}

func entryForUser(t *testing.T, entries []entryInfo, userID string) entryInfo {
	t.Helper()
	for _, entry := range entries {
		if entry.CreatedBy == userID {
			return entry
		}
	}
	require.Failf(t, "missing entry", "entry created by %s was not found in %#v", userID, entries)
	return entryInfo{}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Dir(filepath.Dir(filename))
}

func buildCLI(t *testing.T, root string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "dynamic-pdb")
	cmd := exec.Command(goCommand(), "build", "-o", binaryPath, "./cli/cmd/dynamic-pdb")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".tmp", "go-build-cache"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return binaryPath
}

func buildBackend(t *testing.T, root string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "dynamic-pdb-backend")
	cmd := exec.Command(goCommand(), "build", "-o", binaryPath, "./backend/cmd/server")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".tmp", "go-build-cache"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return binaryPath
}

func runCLI(t *testing.T, binaryPath string, env []string, args ...string) string {
	t.Helper()
	ctx := t.Context()
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = append(os.Environ(), env...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	require.NoError(t, err, output.String())
	return output.String()
}

func runCLIError(t *testing.T, binaryPath string, env []string, args ...string) string {
	t.Helper()
	ctx := t.Context()
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = append(os.Environ(), env...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	require.Error(t, err, output.String())
	return output.String()
}

func uploadStatePath(manifestPath string) string {
	extension := filepath.Ext(manifestPath)
	if extension == "" {
		return manifestPath + ".upload.jsonl"
	}
	return strings.TrimSuffix(manifestPath, extension) + ".upload.jsonl"
}

func goCommand() string {
	if configured := strings.TrimSpace(os.Getenv("GO")); configured != "" {
		return configured
	}
	if _, err := os.Stat("/Users/Denis/sdk/go1.26.3/bin/go"); err == nil {
		return "/Users/Denis/sdk/go1.26.3/bin/go"
	}
	return "go"
}

func writeConfig(t *testing.T, xdgDataHome string, serverURL string, accessToken string) {
	t.Helper()
	configPath := filepath.Join(xdgDataHome, "dynamic-pdb", "config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))
	contents := fmt.Sprintf(`[server]
url = %q

[auth]
access_token = %q
expires_at = %q
`, serverURL, accessToken, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	require.NoError(t, os.WriteFile(configPath, []byte(contents), 0o600))
}

func writeFile(t *testing.T, root string, relativePath string, contents string) {
	t.Helper()
	filePath := filepath.Join(root, filepath.FromSlash(relativePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o755))
	require.NoError(t, os.WriteFile(filePath, []byte(contents), 0o644))
}

func integrationPDBModelText() string {
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

func integrationPDBModelTextWithNonce() string {
	return "REMARK   1 " + uuid.NewString() + "\n" + integrationPDBModelText()
}

func integrationPDBID() string {
	compactUUID := strings.ReplaceAll(uuid.NewString(), "-", "")
	return "9" + strings.ToUpper(compactUUID[:3])
}

func assertArtifactNames(t *testing.T, artifacts []artifactResponse, names []string) {
	t.Helper()
	assert.Equal(t, names, artifactNames(artifacts))
}

func artifactNames(artifacts []artifactResponse) []string {
	names := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		names = append(names, artifact.Name)
	}
	sort.Strings(names)
	return names
}

func assertMetric(t *testing.T, metrics []metricResponse, key string, value float64) {
	t.Helper()
	for _, metric := range metrics {
		if metric.Key == key {
			assert.InDelta(t, value, metric.Value, 0.000001)
			return
		}
	}
	assert.Failf(t, "missing metric", "metric %q was not found in %#v", key, metrics)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func countRunDirections(values []runArtifactResponse, target string) int {
	count := 0
	for _, value := range values {
		if value.Direction == target {
			count++
		}
	}
	return count
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeText(w http.ResponseWriter, text string) {
	_, err := fmt.Fprint(w, text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeXML(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/xml")
	if _, err := io.WriteString(w, text); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
