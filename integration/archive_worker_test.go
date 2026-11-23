package integration

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// archiveSeed stores entries, models and files as the API and the checksum job leave them: files
// linked before a revision is published, and checksum results with our copies in the S3 stub.
type archiveSeed struct {
	t        *testing.T
	database integrationDB
	s3       *s3Stub
	userID   string
	clock    time.Time
	content  map[uuid.UUID]string
}

func (a *archiveSeed) tick() time.Time {
	a.clock = a.clock.Add(time.Second)
	return a.clock
}

func (a *archiveSeed) exec(query string, args ...any) {
	a.t.Helper()
	_, err := a.database.Conn.ExecContext(a.t.Context(), query, args...)
	require.NoError(a.t, err, query)
}

// file stores an artifact and its checksum result. where is upload (our bucket), copied (an RCSB
// file with our copy), changed (an RCSB file that changed before it was copied), over_limit (an
// RCSB file over 1 GiB) or pending (not checked yet).
func (a *archiveSeed) file(entryID, name, fileType, level, where, rcsbURL, content string) uuid.UUID {
	a.t.Helper()
	id := uuid.New()
	sha := sha256Hex(content)
	size := int64(len(content))
	objectPath := "/dynamic-pdb/" + entryID + "/artifacts/" + id.String() + "/" + name
	uri := rcsbURL
	if where == "upload" {
		uri = a.s3.URL() + objectPath
	}
	if where == "over_limit" {
		size = 2 << 30
	}
	a.exec(`insert into artifacts (id, name, level, type, uri, sha256, format, size_bytes, created_by, created_at)
	        values ($1, $2, $3, $4, $5, $6, 'cif', $7, $8, $9)`,
		id, name, level, fileType, uri, sha, size, a.userID, a.tick())
	checkedAt := a.tick()
	switch where {
	case "upload":
		a.s3.recordBody(objectPath, []byte(content))
		a.exec(`insert into artifact_checksums (artifact_id, status, sha256, size_bytes, copy_status, checked_at)
		        values ($1, 'verified', $2, $3, 'own_storage', $4)`, id, sha, size, checkedAt)
	case "copied":
		a.s3.recordBody(objectPath, []byte(content))
		a.exec(`insert into artifact_checksums (artifact_id, status, sha256, size_bytes, stored_uri, copy_status, checked_at)
		        values ($1, 'verified', $2, $3, $4, 'copied', $5)`, id, sha, size, a.s3.URL()+objectPath, checkedAt)
	case "changed":
		a.exec(`insert into artifact_checksums (artifact_id, status, sha256, size_bytes, copy_status, checked_at)
		        values ($1, 'mismatch', $2, 99, 'mismatch', $3)`, id, sha256Hex("newer bytes"), checkedAt)
	case "over_limit":
		a.exec(`insert into artifact_checksums (artifact_id, status, sha256, size_bytes, copy_status, checked_at)
		        values ($1, 'verified', $2, $3, 'over_limit', $4)`, id, sha, size, checkedAt)
	case "pending":
		a.exec(`insert into artifact_checksums (artifact_id) values ($1)`, id)
	default:
		require.FailNow(a.t, "unknown seeded file location", where)
	}
	a.content[id] = content
	return id
}

// entry stores an active entry whose published revision has metadata and files.
func (a *archiveSeed) entry(metadata map[string]any, files ...uuid.UUID) string {
	a.t.Helper()
	entryID := "dpdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	revisionID := uuid.New()
	now := a.tick()
	a.exec(`insert into entries (id, state, created_by, created_at) values ($1, 'active', $2, $3)`, entryID, a.userID, now)
	a.exec(`insert into entry_revisions (id, entry_id, revision_number, state, entry_state, published_at, title,
	                                     metadata, created_by, created_at, updated_at)
	        values ($1, $2, 1, 'pending', 'active', $3, 'archive entry', $4, $5, $3, $3)`,
		revisionID, entryID, now, jsonForArchiveTest(a.t, metadata), a.userID)
	for _, file := range files {
		a.exec(`insert into entry_revision_artifacts (entry_revision_id, artifact_id) values ($1, $2)`, revisionID, file)
	}
	a.exec(`update entry_revisions set state = 'active' where id = $1`, revisionID)
	return entryID
}

// archiveModel is a seeded model and its published revision.
type archiveModel struct {
	id         string
	revisionID uuid.UUID
}

// modelRevision stores a published revision of model with its files and one run, as the API does:
// files first, then publishing.
func (a *archiveSeed) modelRevision(
	model archiveModel,
	revisionNumber int,
	metadata map[string]any,
	primary uuid.UUID,
	files []uuid.UUID,
	runInputs []uuid.UUID,
	runOutputs []uuid.UUID,
) {
	a.t.Helper()
	now := a.tick()
	a.exec(`insert into model_revisions (id, model_id, primary_artifact_id, revision_number, state, model_state,
	                                     title, metadata, created_by, created_at, updated_at, published_at)
	        values ($1, $2, $3, $4, 'pending', 'active', 'archive model', $5, $6, $7, $7, $7)`,
		model.revisionID, model.id, primary, revisionNumber, jsonForArchiveTest(a.t, metadata), a.userID, now)
	for _, file := range append([]uuid.UUID{primary}, files...) {
		a.exec(`insert into model_revision_artifacts (model_revision_id, artifact_id) values ($1, $2)
		        on conflict do nothing`, model.revisionID, file)
	}
	if len(runInputs)+len(runOutputs) > 0 {
		runID := uuid.New()
		a.exec(`insert into runs (id, name, software_name, software_version, created_by) values ($1, 'refinement', 'qFit', '3.2', $2)`,
			runID, a.userID)
		for _, file := range runInputs {
			a.exec(`insert into run_artifacts (run_id, artifact_id, direction) values ($1, $2, 'input')`, runID, file)
		}
		for _, file := range runOutputs {
			a.exec(`insert into run_artifacts (run_id, artifact_id, direction) values ($1, $2, 'output')`, runID, file)
		}
		a.exec(`insert into model_revision_runs (model_revision_id, run_id) values ($1, $2)`, model.revisionID, runID)
	}
	a.exec(`update model_revisions set state = 'archived' where model_id = $1 and state = 'active'`, model.id)
	a.exec(`update model_revisions set state = 'active' where id = $1`, model.revisionID)
}

func (a *archiveSeed) model(
	entryID string,
	number int,
	metadata map[string]any,
	primary uuid.UUID,
	files []uuid.UUID,
	runInputs []uuid.UUID,
	runOutputs []uuid.UUID,
) archiveModel {
	a.t.Helper()
	model := archiveModel{id: fmt.Sprintf("%s_m_%03d", entryID, number), revisionID: uuid.New()}
	a.exec(`insert into models (id, entry_id, state, created_by, created_at) values ($1, $2, 'active', $3, $4)`,
		model.id, entryID, a.userID, a.tick())
	a.modelRevision(model, 1, metadata, primary, files, runInputs, runOutputs)
	return model
}

func jsonForArchiveTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func buildArchiveWorker(t *testing.T, root string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "dynamic-pdb-archive-worker")
	cmd := exec.Command(goCommand(), "build", "-o", binaryPath, "./backend/cmd/archive-worker")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".tmp", "go-build-cache"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return binaryPath
}

// runArchiveWorker builds every waiting archive and returns the worker's log once it is idle.
func runArchiveWorker(t *testing.T, root string, binaryPath string, s3URL string, extraEnv []string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binaryPath)
	cmd.Dir = filepath.Join(root, "backend")
	cmd.Env = append(os.Environ(),
		"DYNAMIC_PDB_ENV=local",
		"DYNAMIC_PDB_CDN_S3_ENDPOINT="+s3URL,
		"DYNAMIC_PDB_CDN_S3_ACCESS_KEY_ID=AKIAEXAMPLE",
		"DYNAMIC_PDB_CDN_S3_SECRET_ACCESS_KEY=secret",
		"AWS_EC2_METADATA_DISABLED=true",
		"DYNAMIC_PDB_ARCHIVE_WORKER_EXIT_WHEN_IDLE=true",
		"DYNAMIC_PDB_ARCHIVE_WORKER_SCRATCH_DIR="+t.TempDir(),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Run(), output.String())
	return output.String()
}

// downloadAttributes is what the tests read of GET /v1/downloads/{id}.
type downloadAttributes struct {
	Status        string  `json:"status"`
	ArchiveURL    *string `json:"archive_url"`
	ArchiveSHA256 *string `json:"archive_sha256"`
	ArchiveBytes  *int64  `json:"archive_bytes"`
	ManifestURL   *string `json:"manifest_url"`
	Message       *string `json:"message"`
	Counts        *struct {
		Included        int `json:"included"`
		Linked          int `json:"linked"`
		Unavailable     int `json:"unavailable"`
		ChangedAtSource int `json:"changed_at_source"`
	} `json:"counts"`
}

func createdID(t *testing.T, response *http.Response, status int) string {
	t.Helper()
	defer func() { assert.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, status, response.StatusCode, string(body))
	var document struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &document))
	require.NotEmpty(t, document.Data.ID)
	return document.Data.ID
}

func readDownload(t *testing.T, baseURL string, id string) downloadAttributes {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/v1/downloads/"+id, nil)
	require.NoError(t, err)
	request.Header.Set("Accept", jsonAPIMediaType)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { assert.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var document struct {
		Data struct {
			Attributes downloadAttributes `json:"attributes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &document))
	return document.Data.Attributes
}

// openArchive reads a ready archive from the S3 stub, checks its SHA-256 and returns its entries
// and manifest rows.
func openArchive(t *testing.T, s3 *s3Stub, attributes downloadAttributes) (map[string]string, []string, []map[string]string) {
	t.Helper()
	require.Equal(t, "ready", attributes.Status, attributes.Message)
	require.NotNil(t, attributes.ArchiveURL)
	content, found := s3.body(strings.TrimPrefix(*attributes.ArchiveURL, s3.URL()))
	require.True(t, found, *attributes.ArchiveURL)
	require.Equal(t, sha256Hex(string(content)), *attributes.ArchiveSHA256)
	require.Equal(t, int64(len(content)), *attributes.ArchiveBytes)
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	require.NoError(t, err)
	entries := map[string]string{}
	var names []string
	for _, entry := range reader.File {
		opened, err := entry.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(opened)
		require.NoError(t, err)
		require.NoError(t, opened.Close())
		entries[entry.Name] = string(data)
		names = append(names, entry.Name)
	}
	checkWithPythonZipfile(t, content)
	manifest, found := s3.body(strings.TrimPrefix(*attributes.ManifestURL, s3.URL()))
	require.True(t, found)
	return entries, names, parseArchiveManifest(t, string(manifest))
}

// checkWithPythonZipfile runs Python's zip reader over the archive, a second, independent reader.
func checkWithPythonZipfile(t *testing.T, content []byte) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Log("python3 not found: the archive is checked with Go's zip reader only")
		return
	}
	path := filepath.Join(t.TempDir(), "archive.zip")
	require.NoError(t, os.WriteFile(path, content, 0o600))
	output, err := exec.CommandContext(t.Context(), python, "-I", "-m", "zipfile", "-t", path).CombinedOutput()
	require.NoError(t, err, string(output))
}

func parseArchiveManifest(t *testing.T, text string) []map[string]string {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(text))
	var header []string
	var rows []map[string]string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		if header == nil {
			header = cells
			continue
		}
		require.Len(t, cells, len(header), line)
		row := map[string]string{}
		for index, column := range header {
			row[column] = cells[index]
		}
		rows = append(rows, row)
	}
	require.NoError(t, scanner.Err())
	return rows
}

func manifestStatus(rows []map[string]string, fileID uuid.UUID) (string, string) {
	for _, row := range rows {
		if row["file_id"] == fileID.String() {
			return row["status"], row["path_in_archive"]
		}
	}
	return "", ""
}

func Test_should_build_every_file_choice_when_a_saved_selection_holds_every_model_representation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given the backend and the archive worker on a schema of their own, and an S3 stub
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	workerBinaryPath := buildArchiveWorker(t, root)
	database, schemaEnv := setupMigratedSchema(t, root)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	seed := &archiveSeed{t: t, database: database, s3: s3, userID: auth.UserID,
		clock: time.Now().UTC().Add(-time.Hour), content: map[uuid.UUID]string{}}

	// and entry 1: a Deposited model, a qFit Multiconformer sharing its structure factors, and a
	// re-refinement deposited as an Ensemble (origin archive)
	fasta1 := seed.file("e1", "1abc.fasta", "fasta", "L1", "copied", "https://www.rcsb.org/fasta/entry/1ABC", ">1ABC\nMKV\n")
	entry1 := seed.entry(map[string]any{"external_refs": map[string]any{"pdb": "1ABC"},
		"related_datasets": []map[string]any{{"doi": "10.15785/SBGRID/1", "data_type": "diffraction image data"}}}, fasta1)
	coordinates := seed.file(entry1, "1abc.cif", "model", "L2", "copied", "https://files.rcsb.org/download/1ABC.cif", "data_1ABC v1\n")
	structureFactors := seed.file(entry1, "1abc-sf.cif", "structure_factors", "L1", "copied",
		"https://files.rcsb.org/download/1ABC-sf.cif", "data_r1abcsf\n")
	deposited := seed.model(entry1, 1, map[string]any{"model_type": "Deposited", "purpose": "Reference",
		"external_refs": map[string]any{"pdb": "1ABC"}, "authors": []string{"Nelson, R."}},
		coordinates, []uuid.UUID{structureFactors}, nil, nil)
	qfit := seed.file(entry1, "1abc_qFit.pdb", "model", "L2", "upload", "", "ATOM      1  N   MET A   1\n")
	multiconformer := seed.model(entry1, 2, map[string]any{"model_type": "Multiconformer", "purpose": "Refinement"},
		qfit, nil, []uuid.UUID{structureFactors}, []uuid.UUID{qfit})
	rerefinedFile := seed.file(entry1, "2def.cif", "model", "L2", "copied", "https://files.rcsb.org/download/2DEF.cif", "data_2DEF\n")
	rerefined := seed.model(entry1, 3, map[string]any{"model_type": "Ensemble", "origin": "archive", "purpose": "Reference",
		"external_refs":   map[string]any{"pdb": "2DEF"},
		"related_entries": []map[string]any{{"db_name": "PDB", "db_id": "1ABC", "content_type": "re-refinement"}}},
		rerefinedFile, nil, nil, nil)

	// and entry 2: a Deposited model changed at RCSB with structure factors over 1 GiB, a Single
	// Conformer model with a log, and an Ensemble
	fasta2 := seed.file("e2", "3ghi.fasta", "fasta", "L1", "pending", "https://www.rcsb.org/fasta/entry/3GHI", ">3GHI\nGGG\n")
	entry2 := seed.entry(map[string]any{"external_refs": map[string]any{"pdb": "3GHI"}}, fasta2)
	changed := seed.file(entry2, "3ghi.cif", "model", "L2", "changed", "https://files.rcsb.org/download/3GHI.cif", "data_3GHI old\n")
	overLimit := seed.file(entry2, "3ghi-sf.cif", "structure_factors", "L1", "over_limit",
		"https://files.rcsb.org/download/3GHI-sf.cif", "huge")
	deposited2 := seed.model(entry2, 1, map[string]any{"model_type": "Deposited", "purpose": "Reference",
		"external_refs": map[string]any{"pdb": "3GHI"}}, changed, []uuid.UUID{overLimit}, nil, nil)
	single := seed.file(entry2, "model.cif", "model", "L2", "upload", "", "data_single\n")
	log := seed.file(entry2, "refine.log", "other", "L3", "upload", "", "refinement done\n")
	singleConformer := seed.model(entry2, 2, map[string]any{"model_type": "Single Conformer", "purpose": "Refinement"},
		single, nil, nil, []uuid.UUID{single, log})
	ensembleFile := seed.file(entry2, "ens.pdb", "model", "L2", "upload", "", "MODEL 1\nENDMDL\nMODEL 2\nENDMDL\n")
	ensemble := seed.model(entry2, 3, map[string]any{"model_type": "Ensemble", "purpose": "Refinement", "member_count": 2},
		ensembleFile, nil, nil, nil)

	// and a saved selection of all six models
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), schemaEnv)
	modelIDs := []string{deposited.id, multiconformer.id, rerefined.id, deposited2.id, singleConformer.id, ensemble.id}
	//nolint:bodyclose // createdID closes the response body.
	selectionID := createdID(t, postJSON(t, backend.URL+"/v1/selections", auth.AccessToken,
		map[string]any{"model_ids": modelIDs}), http.StatusCreated)

	// and, after the save, the PDB revises entry 1's coordinate file: a new published revision
	// links a new file at the same RCSB URL
	revisedCoordinates := seed.file(entry1, "1abc.cif", "model", "L2", "copied", "https://files.rcsb.org/download/1ABC.cif",
		"data_1ABC v2\n")
	seed.modelRevision(archiveModel{id: deposited.id, revisionID: uuid.New()}, 2,
		map[string]any{"model_type": "Deposited", "purpose": "Reference", "external_refs": map[string]any{"pdb": "1ABC"}},
		revisedCoordinates, []uuid.UUID{structureFactors}, nil, nil)

	// when every file choice is asked for, plus an archive whose limit is too small
	ids := map[string]string{}
	for _, choice := range []string{"all_files", "original_mmcif", "modified_coordinates"} {
		//nolint:bodyclose // createdID closes the response body.
		ids[choice] = createdID(t, postJSON(t, backend.URL+"/v1/downloads", auth.AccessToken,
			map[string]any{"selection_id": selectionID, "file_choice": choice}), http.StatusAccepted)
	}
	blockedID := uuid.New()
	seed.exec(`insert into download_jobs (id, selection_id, file_choice, max_archive_bytes) values ($1, $2, 'all_files', 10)`,
		blockedID, selectionID)
	output := runArchiveWorker(t, root, workerBinaryPath, s3.URL(), schemaEnv)

	// then all files: every held file once, each with its recorded bytes and the pinned version
	all := readDownload(t, backend.URL, ids["all_files"])
	entries, names, rows := openArchive(t, s3, all)
	rootFolder := "dynamic-pdb-" + selectionID
	assert.Equal(t, rootFolder+"/README.txt", names[0], output)
	assert.Equal(t, rootFolder+"/manifest.tsv", names[len(names)-1])
	included := map[uuid.UUID]string{
		fasta1:           rootFolder + "/" + entry1 + "/1abc.fasta",
		coordinates:      rootFolder + "/" + entry1 + "/" + deposited.id + "/1abc.cif",
		structureFactors: rootFolder + "/" + entry1 + "/" + deposited.id + "/1abc-sf.cif",
		qfit:             rootFolder + "/" + entry1 + "/" + multiconformer.id + "/1abc_qFit.pdb",
		rerefinedFile:    rootFolder + "/" + entry1 + "/" + rerefined.id + "/2def.cif",
		single:           rootFolder + "/" + entry2 + "/" + singleConformer.id + "/model.cif",
		log:              rootFolder + "/" + entry2 + "/" + singleConformer.id + "/refine.log",
		ensembleFile:     rootFolder + "/" + entry2 + "/" + ensemble.id + "/ens.pdb",
	}
	for fileID, path := range included {
		assert.Equal(t, seed.content[fileID], entries[path], path)
		status, manifestPath := manifestStatus(rows, fileID)
		assert.Equal(t, "included", status, path)
		assert.Equal(t, path, manifestPath)
	}
	assert.Len(t, names, len(included)+2)
	assert.Equal(t, "data_1ABC v1\n", entries[included[coordinates]], "the archive holds the pinned version, not the revised one")
	status, _ := manifestStatus(rows, revisedCoordinates)
	assert.Empty(t, status, "the revised file is not part of the selection")
	status, _ = manifestStatus(rows, changed)
	assert.Equal(t, "changed-at-source", status)
	status, _ = manifestStatus(rows, overLimit)
	assert.Equal(t, "linked", status)
	status, _ = manifestStatus(rows, fasta2)
	assert.Equal(t, "linked", status)
	require.NotNil(t, all.Counts)
	assert.Equal(t, 8, all.Counts.Included)
	assert.Equal(t, 2, all.Counts.Linked)
	assert.Equal(t, 1, all.Counts.ChangedAtSource)

	// and original mmCIF: the PDB coordinate files the selected models' records hold
	_, names, rows = openArchive(t, s3, readDownload(t, backend.URL, ids["original_mmcif"]))
	assert.Equal(t, []string{
		rootFolder + "/README.txt",
		included[coordinates],
		included[rerefinedFile],
		rootFolder + "/manifest.tsv",
	}, names)
	status, _ = manifestStatus(rows, changed)
	assert.Equal(t, "changed-at-source", status)

	// and modified coordinates: the Dynamic PDB model files
	_, names, _ = openArchive(t, s3, readDownload(t, backend.URL, ids["modified_coordinates"]))
	assert.Equal(t, []string{
		rootFolder + "/README.txt",
		included[qfit],
		included[single],
		included[ensembleFile],
		rootFolder + "/manifest.tsv",
	}, names)

	// and the archive over its limit is blocked, with nothing written for it
	blocked := readDownload(t, backend.URL, blockedID.String())
	assert.Equal(t, "blocked", blocked.Status)
	require.NotNil(t, blocked.Message)
	assert.Contains(t, *blocked.Message, "more than the 10 bytes one archive may hold")
	s3.mu.Lock()
	for path := range s3.bodies {
		assert.NotContains(t, path, blockedID.String())
	}
	s3.mu.Unlock()

	// and asking again for a ready archive returns it
	//nolint:bodyclose // createdID closes the response body.
	again := createdID(t, postJSON(t, backend.URL+"/v1/downloads", auth.AccessToken,
		map[string]any{"selection_id": selectionID, "file_choice": "all_files"}), http.StatusOK)
	assert.Equal(t, ids["all_files"], again)

	var finished int
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select count(*) from download_jobs where status in ('ready', 'blocked') and lease_until is null`).Scan(&finished))
	assert.Equal(t, 4, finished)
}
