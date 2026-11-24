package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conformerModel is one model of the conformer test: a real file cut to a few residues
// (lib/cif/testdata/conformers) and the model type its revision gets.
type conformerModel struct {
	file      string
	format    string
	modelType string
	// scope and rows are what the job stores for the file: rows as "type count xresidues".
	scope string
	key   string
	rows  []string
}

var conformerModels = []conformerModel{
	{"deposited_2CO3.cif", "mmcif", "Deposited", "counted", "label",
		[]string{"ASP 1 x1", "ILE 2 x1", "SER 1 x1", "VAL 1 x1", "VAL 2 x1"}},
	// A re-refinement: a second Deposited model of the same entry, from another file.
	{"deposited_7TDY.cif", "mmcif", "Deposited", "counted", "label",
		[]string{"GLU 1 x2", "ILE 1 x1", "MET 2 x1", "VAL 1 x1"}},
	{"single_conformer_5RGD.cif", "mmcif", "Single Conformer", "counted", "label",
		[]string{"ASP 1 x1", "GLN 2 x1", "ILE 1 x2", "LEU 1 x1", "SER 1 x2"}},
	{"qfit_4hxp.cif", "mmcif", "Multiconformer", "counted", "label",
		[]string{"ARG 2 x1", "ASN 1 x1", "ASN 2 x2", "GLN 1 x1", "LYS 1 x1", "LYS 2 x1", "PRO 2 x2", "SER 1 x1", "THR 2 x1"}},
	{"qfit_6llx.pdb", "pdb", "Multiconformer", "counted", "author",
		[]string{"ALA 1 x1", "LEU 1 x2", "SER 1 x1", "TYR 1 x1"}},
	// Ensembles get no counts: model frames, or one frame with whole-chain letters typed Ensemble.
	{"ensemble_5ai3.pdb", "pdb", "Ensemble", "ensemble_model_frames", "-", nil},
	{"altloc_ensemble_44PS.cif", "mmcif", "Ensemble", "ensemble_model_type", "-", nil},
}

func Test_should_store_conformer_counts_for_every_model_type_when_the_coordinate_index_job_reads_files(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: one entry with a model of every type, each file in our bucket. The job runs first with
	// conformer counts off, as before their switch-on, then with them and the conformer fill on.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	jobBinaryPath := buildCoordinateIndexJob(t, root)
	database, schemaEnv := setupMigratedSchema(t, root)
	auth := seedUserAndIssueToken(t, database)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), schemaEnv)
	entryID, _ := seedEntryRevision(t, database, auth.UserID, "active")
	files := make([]uuid.UUID, 0, len(conformerModels))
	for _, model := range conformerModels {
		content, err := os.ReadFile(filepath.Join(root, "lib", "cif", "testdata", "conformers", model.file))
		require.NoError(t, err)
		artifactID := createModelWithStoredFile(t, backend.URL, auth.AccessToken, s3, entryID, model.file, model.format, string(content))
		setModelType(t, database, artifactID, model.modelType)
		files = append(files, artifactID)
	}
	activateModelsOfEntry(t, database, entryID)
	countsOff := append(append([]string{}, schemaEnv...), "DYNAMIC_PDB_COORDINATE_INDEX_RESIDUE_COUNTS=false")
	firstOutput := runCoordinateIndexJob(t, root, jobBinaryPath, s3.URL(), countsOff)
	for _, artifactID := range files {
		require.Equal(t, "done", coordinateIndexStatus(t, database, artifactID), firstOutput)
		require.Equal(t, "-", conformerScope(t, database, artifactID), "no counts while they are off")
	}

	// when
	fillOn := append(append([]string{}, schemaEnv...), "DYNAMIC_PDB_COORDINATE_INDEX_RESIDUE_FILL=true")
	output := runCoordinateIndexJob(t, root, jobBinaryPath, s3.URL(), fillOn)

	// then: every file read again, with counts for the non-ensembles only, and its chains kept.
	assert.Contains(t, output, "residue_fill_sets_requeued=7")
	for index, model := range conformerModels {
		artifactID := files[index]
		assert.Equal(t, "done", coordinateIndexStatus(t, database, artifactID), model.file)
		assert.Equal(t, model.scope, conformerScope(t, database, artifactID), model.file)
		assert.Equal(t, model.key, residueKey(t, database, artifactID), model.file)
		assert.Equal(t, model.rows, residueCounts(t, database, artifactID), model.file)
		assert.NotEmpty(t, coordinateIndexChains(t, database, artifactID), model.file)
	}

	// and example search 1's residue part, as S4 will ask it: models with a residue of a type with
	// more than k conformations, never an Ensemble.
	assert.ElementsMatch(t, []uuid.UUID{files[0]}, filesWithConformers(t, database, "VAL", 1))
	assert.ElementsMatch(t, []uuid.UUID{files[0], files[1], files[2]}, filesWithConformers(t, database, "ILE", 0))
	assert.ElementsMatch(t, []uuid.UUID{files[3]}, filesWithConformers(t, database, "ASN", 1))
	assert.Empty(t, filesWithConformers(t, database, "SER", 1), "44PS's serine has six letters, but it is an Ensemble")

	// and the same through GET /v1/models, where the residue filters are on (local config, S4):
	// deposited and multiconformer models match, an Ensemble never does.
	search := func(query string) []uuid.UUID {
		return searchModelFiles(t, database, backend.URL, auth.AccessToken, "/v1/models?"+query)
	}
	assert.ElementsMatch(t, []uuid.UUID{files[0]}, search("residue_type=VAL&residue_conformers_min=2"))
	assert.ElementsMatch(t, []uuid.UUID{files[0], files[1], files[2]}, search("residue_type=ile"))
	assert.ElementsMatch(t, []uuid.UUID{files[1]}, search("residue_type=MET&residue_conformers_min=2"), "the re-refinement")
	assert.ElementsMatch(t, []uuid.UUID{files[0], files[2]}, search("residue_type=GLN&residue_type=ILE&residue_conformers_min=2"))
	assert.ElementsMatch(t, []uuid.UUID{files[3]}, search("residue_type=ASN&residue_conformers_min=2&residues_min=2"))
	assert.Empty(t, search("residue_type=LEU&residue_conformers_min=2"), "6LLX's leucines have one conformer each")
	assert.Empty(t, search("residue_type=SER&residue_conformers_min=2"), "no ensemble ever matches")
	assert.Empty(t, search("residue_type=PHE"), "44PS's phenylalanine: its model is an Ensemble")

	// and a saved selection of that search holds the same models.
	selectionResponse := postJSON(t, backend.URL+"/v1/selections", "",
		map[string]any{"searches": []string{"residue_type=GLN&residue_type=ILE&residue_conformers_min=2"}})
	defer func() {
		assert.NoError(t, selectionResponse.Body.Close())
	}()
	require.Equal(t, http.StatusCreated, selectionResponse.StatusCode)
	var selection struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(selectionResponse.Body).Decode(&selection))
	type selectionModels struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	held := getJSON[selectionModels](t, backend.URL+"/v1/selections/"+selection.Data.ID+"/models", "")
	heldFiles := make([]uuid.UUID, 0, len(held.Data))
	for _, model := range held.Data {
		var artifactID uuid.UUID
		require.NoError(t, database.Conn.QueryRowContext(t.Context(),
			`select primary_artifact_id from model_revisions where model_id = $1 and state = 'active'`, model.ID).Scan(&artifactID))
		heldFiles = append(heldFiles, artifactID)
	}
	assert.ElementsMatch(t, []uuid.UUID{files[0], files[2]}, heldFiles)
}

// setModelType gives the pending revision of the model using a file a model type, as an upload of
// that type would.
func setModelType(t *testing.T, database integrationDB, artifactID uuid.UUID, modelType string) {
	t.Helper()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		_, err := transaction.ExecContext(t.Context(),
			`update model_revisions set metadata = metadata || jsonb_build_object('model_type', cast($2 as text))
			 where primary_artifact_id = $1`, artifactID, modelType)
		require.NoError(t, err)
	})
}

func conformerScope(t *testing.T, database integrationDB, artifactID uuid.UUID) string {
	t.Helper()
	var scope string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select coalesce(conformers, '-') from coordinate_index_sets where artifact_id = $1`, artifactID).Scan(&scope))
	return scope
}

func residueKey(t *testing.T, database integrationDB, artifactID uuid.UUID) string {
	t.Helper()
	var key string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select coalesce(residue_key, '-') from coordinate_index_sets where artifact_id = $1`, artifactID).Scan(&key))
	return key
}

// residueCounts writes each count row of a file as "type count xresidues", nil when it has none.
func residueCounts(t *testing.T, database integrationDB, artifactID uuid.UUID) []string {
	t.Helper()
	rows := textRows(t, database,
		`select residue_type || ' ' || conformer_count || ' x' || residues
		 from coordinate_index_residue_counts where artifact_id = $1 order by residue_type, conformer_count`, artifactID)
	if len(rows) == 0 {
		return nil
	}
	return rows
}

// filesWithConformers returns the files of active non-Ensemble models with a residue of a type
// with more than moreThan conformations.
func filesWithConformers(t *testing.T, database integrationDB, residueType string, moreThan int) []uuid.UUID {
	t.Helper()
	rows, err := database.Conn.QueryContext(t.Context(),
		`select distinct model_revisions.primary_artifact_id
		 from model_revisions
		 join coordinate_index_residue_counts counts on counts.artifact_id = model_revisions.primary_artifact_id
		 where model_revisions.state = 'active'
		   and coalesce(model_revisions.metadata ->> 'model_type', '') <> 'Ensemble'
		   and counts.residue_type = $1 and counts.conformer_count > $2`, residueType, moreThan)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, rows.Close())
	}()
	files := []uuid.UUID{}
	for rows.Next() {
		var artifactID uuid.UUID
		require.NoError(t, rows.Scan(&artifactID))
		files = append(files, artifactID)
	}
	require.NoError(t, rows.Err())
	return files
}
