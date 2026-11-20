package integration

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mmcifWithChains writes an mmCIF file of homomer chains named A, B, C…: entity 1 is
// polypeptide(L), its sequence has sequenceLength positions, and each chain models its first
// three residues.
func mmcifWithChains(chains int, sequenceLength int) string {
	var text strings.Builder
	text.WriteString("data_TEST\n_entity_poly.entity_id 1\n_entity_poly.type 'polypeptide(L)'\n#\n")
	text.WriteString("loop_\n_entity_poly_seq.entity_id\n_entity_poly_seq.num\n_entity_poly_seq.mon_id\n")
	for position := 1; position <= sequenceLength; position++ {
		fmt.Fprintf(&text, "1 %d ALA\n", position)
	}
	text.WriteString("#\nloop_\n_atom_site.group_PDB\n_atom_site.label_comp_id\n_atom_site.label_asym_id\n" +
		"_atom_site.label_entity_id\n_atom_site.label_seq_id\n_atom_site.auth_seq_id\n_atom_site.auth_asym_id\n")
	for chain := range chains {
		name := string(rune('A' + chain))
		for residue := 1; residue <= 3; residue++ {
			fmt.Fprintf(&text, "ATOM ALA %s 1 %d %d %s\n", name, residue, residue, name)
		}
	}
	text.WriteString("HETATM HOH Z 2 . 401 A\n#\n")
	return text.String()
}

// pdbWithChains writes a PDB-format file without SEQRES records: chains A, B, C… of three
// alanines each, every chain ended by TER.
func pdbWithChains(chains int) string {
	var text strings.Builder
	serial := 1
	for chain := range chains {
		name := string(rune('A' + chain))
		for residue := 1; residue <= 3; residue++ {
			fmt.Fprintf(&text, "ATOM  %5d  CA  ALA %s%4d       1.000   2.000   3.000  1.00 20.00           C\n", serial, name, residue)
			serial++
		}
		fmt.Fprintf(&text, "TER   %5d      ALA %s%4d\n", serial, name, 3)
		serial++
	}
	text.WriteString("END\n")
	return text.String()
}

func Test_should_find_models_by_their_protein_chains_when_the_coordinate_index_job_has_read_their_files(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: three models of one entry, each with a file in our bucket. The entry's FASTA names
	// chains A to E with 350 residues. The PDB-format file states no lengths, so its chains take
	// the FASTA's; the mmCIF files state theirs.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	jobBinaryPath := buildCoordinateIndexJob(t, root)
	// The job reads every due file, so it runs in a schema of its own.
	database, schemaEnv := setupMigratedSchema(t, root)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), schemaEnv)
	entryID, entryRevisionID := seedEntryRevision(t, database, auth.UserID)
	seedFASTARecord(t, database, entryRevisionID, auth.UserID, "1ABC_1|Chains A, B, C, D, E|test protein", 350)
	fiveLong := createModelWithStoredFile(t, backend.URL, auth.AccessToken, s3, entryID, "five.cif", "mmcif", mmcifWithChains(5, 400))
	fiveFromFASTA := createModelWithStoredFile(t, backend.URL, auth.AccessToken, s3, entryID, "five.pdb", "pdb", pdbWithChains(5))
	fourLong := createModelWithStoredFile(t, backend.URL, auth.AccessToken, s3, entryID, "four.cif", "mmcif", mmcifWithChains(4, 400))
	assert.Equal(t, "pending", coordinateIndexStatus(t, database, fiveLong), "the API adds the set with the artifact")
	activateModelsOfEntry(t, database, entryID)

	// when
	output := runCoordinateIndexJob(t, root, jobBinaryPath, s3.URL(), schemaEnv)

	// then: the chains are stored, with where each length and protein test came from.
	for _, artifactID := range []uuid.UUID{fiveLong, fiveFromFASTA, fourLong} {
		assert.Equal(t, "done", coordinateIndexStatus(t, database, artifactID), output)
	}
	assert.Equal(t, []string{
		"A 350 fasta_auth_chain true residue_majority", "B 350 fasta_auth_chain true residue_majority",
		"C 350 fasta_auth_chain true residue_majority", "D 350 fasta_auth_chain true residue_majority",
		"E 350 fasta_auth_chain true residue_majority",
	}, coordinateIndexChains(t, database, fiveFromFASTA))
	assert.Equal(t, "A 400 entity_poly_seq true entity_poly_type", coordinateIndexChains(t, database, fiveLong)[0])
	assert.Contains(t, output, "coordinate index job finished")

	// and example 2's chain part finds the two models with five chains longer than 300.
	found := searchModelFiles(t, database, backend.URL, auth.AccessToken, "/v1/models?protein_chains_min=5&protein_chain_length_min=301")
	assert.ElementsMatch(t, []uuid.UUID{fiveLong, fiveFromFASTA}, found)
	found = searchModelFiles(t, database, backend.URL, auth.AccessToken, "/v1/models?protein_chains_min=5&protein_chain_length_min=351")
	assert.ElementsMatch(t, []uuid.UUID{fiveLong}, found)
	// and the every mode: all four chains of the four-chain file are longer than 360.
	found = searchModelFiles(t, database, backend.URL, auth.AccessToken,
		"/v1/models?protein_chain_mode=every&protein_chains_min=4&protein_chain_length_min=361")
	assert.ElementsMatch(t, []uuid.UUID{fiveLong, fourLong}, found)
}

// seedFASTARecord stores one FASTA record of an entry revision, as activation does.
func seedFASTARecord(t *testing.T, database integrationDB, entryRevisionID uuid.UUID, createdBy string, header string, length int) {
	t.Helper()
	fastaID := uuid.New()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		_, err := transaction.ExecContext(t.Context(),
			`insert into artifacts(id, name, level, type, created_by) values ($1, 'sequences.fasta', 'L0', 'fasta', $2)`,
			fastaID, createdBy)
		require.NoError(t, err)
		_, err = transaction.ExecContext(t.Context(),
			`insert into protein_sequences(id, entry_revision_id, source_artifact_id, record_index, header, sequence)
			 values ($1, $2, $3, 0, $4, $5)`,
			uuid.New(), entryRevisionID, fastaID, header, strings.Repeat("A", length))
		require.NoError(t, err)
	})
}

// activateModelsOfEntry publishes the models created through the API, as a reviewer's approval
// would, so the model list and search return them.
func activateModelsOfEntry(t *testing.T, database integrationDB, entryID string) {
	t.Helper()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		_, err := transaction.ExecContext(t.Context(), `update models set state = 'active' where entry_id = $1`, entryID)
		require.NoError(t, err)
		_, err = transaction.ExecContext(t.Context(),
			`update model_revisions
			 set state = 'active', model_state = 'active', revision_number = 1, published_at = now()
			 where model_id in (select id from models where entry_id = $1)`, entryID)
		require.NoError(t, err)
	})
}

func coordinateIndexStatus(t *testing.T, database integrationDB, artifactID uuid.UUID) string {
	t.Helper()
	var status string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select status from coordinate_index_sets where artifact_id = $1`, artifactID).Scan(&status))
	return status
}

// coordinateIndexChains writes each chain of a file as "chain length source protein protein-source".
func coordinateIndexChains(t *testing.T, database integrationDB, artifactID uuid.UUID) []string {
	t.Helper()
	return textRows(t, database,
		`select auth_asym_id || ' ' || coalesce(deposited_length::text, '-') || ' ' || coalesce(length_source, '-') || ' ' ||
		        protein::text || ' ' || protein_source
		 from coordinate_index_chains where artifact_id = $1 order by auth_asym_id`, artifactID)
}

// searchModelFiles runs a model search and returns the model files of the models it found.
func searchModelFiles(t *testing.T, database integrationDB, backendURL string, token string, path string) []uuid.UUID {
	t.Helper()
	type modelList struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	list := getJSON[modelList](t, backendURL+path, token)
	files := make([]uuid.UUID, 0, len(list.Data))
	for _, model := range list.Data {
		var artifactID uuid.UUID
		require.NoError(t, database.Conn.QueryRowContext(t.Context(),
			`select primary_artifact_id from model_revisions where model_id = $1 and state = 'active'`, model.ID).Scan(&artifactID))
		files = append(files, artifactID)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].String() < files[right].String() })
	return files
}

func buildCoordinateIndexJob(t *testing.T, root string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "dynamic-pdb-coordinate-index-job")
	cmd := exec.Command(goCommand(), "build", "-o", binaryPath, "./backend/cmd/coordinate-index-job")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".tmp", "go-build-cache"))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return binaryPath
}

func runCoordinateIndexJob(t *testing.T, root string, binaryPath string, s3URL string, extraEnv []string) string {
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
