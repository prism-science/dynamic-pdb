package integration

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_return_residue_data_only_from_model_detail_when_models_are_listed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	entryID, modelID := seedActiveModelWithResidueData(t, database, auth.UserID)
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())

	// when
	entryModels := getJSON[modelListResponse](t, backend.URL+"/v1/entries/"+entryID+"/models", auth.AccessToken)
	modelDetail := getJSON[modelDocumentResponse](
		t,
		backend.URL+"/v1/entries/"+entryID+"/models/"+modelID,
		auth.AccessToken,
	).Data.Attributes

	// then
	require.Len(t, entryModels.Items, 1)
	assert.Equal(t, modelID, entryModels.Items[0].ID)
	assert.Nil(t, entryModels.Items[0].ResidueData, "model lists must not carry residue_data")
	assert.NotContains(t, entryModels.Items[0].Metadata, "residue_data")
	assert.Equal(t, "Multiconformer", entryModels.Items[0].Metadata["model_type"])
	require.NotNil(t, modelDetail.ResidueData)
	require.Len(t, *modelDetail.ResidueData, 2)
	assert.Equal(t, "A", (*modelDetail.ResidueData)[0]["label_asym_id"])
	assert.InDelta(t, 0.97, (*modelDetail.ResidueData)[0]["rscc"], 0.000001)
}

func Test_should_follow_links_next_through_entry_models_when_listing_by_cursor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	entryID := seedActiveEntry(t, database, auth.UserID)
	base := time.Now().UTC().Truncate(time.Microsecond)
	createdOldestFirst := make([]string, 0, 3)
	for index := range 3 {
		modelID := fmt.Sprintf("%s_m_%03d", entryID, index+1)
		seedActiveModel(t, database, entryID, modelID, auth.UserID, base.Add(time.Duration(index)*time.Second), `{}`)
		createdOldestFirst = append(createdOldestFirst, modelID)
	}
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackend(t, root, backendBinaryPath, s3.URL())

	// when
	firstPage := getJSON[cursorPageResponse](t, backend.URL+"/v1/entries/"+entryID+"/models?limit=2", auth.AccessToken)
	require.NotNil(t, firstPage.Links)
	secondPage := getJSON[cursorPageResponse](t, firstPage.Links.Next, auth.AccessToken)

	// then
	assert.True(t, strings.HasPrefix(firstPage.Links.Next, backend.URL+"/v1/entries/"+entryID+"/models?"))
	assert.Equal(t, createdOldestFirst, append(firstPage.ids(), secondPage.ids()...))
	assert.Nil(t, secondPage.Links)
}

type cursorPageResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	Links *struct {
		Next string `json:"next"`
	} `json:"links"`
}

func (r cursorPageResponse) ids() []string {
	ids := make([]string, 0, len(r.Data))
	for _, item := range r.Data {
		ids = append(ids, item.ID)
	}
	return ids
}

func seedActiveModelWithResidueData(t *testing.T, database integrationDB, createdBy string) (string, string) {
	t.Helper()
	entryID := seedActiveEntry(t, database, createdBy)
	modelID := entryID + "_m_001"
	seedActiveModel(t, database, entryID, modelID, createdBy, time.Now().UTC(), `{
		"model_type": "Multiconformer",
		"residue_data": [
			{"label_asym_id": "A", "label_seq_id": 1, "rscc": 0.97},
			{"label_asym_id": "A", "label_seq_id": 2, "rscc": 0.88}
		]
	}`)
	return entryID, modelID
}

func seedActiveEntry(t *testing.T, database integrationDB, createdBy string) string {
	t.Helper()
	entryID := "entry-" + uuid.NewString()
	now := time.Now().UTC()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		_, err := transaction.ExecContext(
			t.Context(),
			`insert into entries(id, state, created_by, created_at) values ($1, 'active', $2, $3)`,
			entryID,
			createdBy,
			now,
		)
		require.NoError(t, err)
		_, err = transaction.ExecContext(
			t.Context(),
			`insert into entry_revisions(
			   id, entry_id, revision_number, state, entry_state, published_at, title,
			   metadata, created_by, created_at, updated_at
			 ) values ($1, $2, 1, 'active', 'active', $3, 'list test entry', '{}'::jsonb, $4, $3, $3)`,
			uuid.New(),
			entryID,
			now,
			createdBy,
		)
		require.NoError(t, err)
	})
	return entryID
}

func seedActiveModel(
	t *testing.T,
	database integrationDB,
	entryID string,
	modelID string,
	createdBy string,
	createdAt time.Time,
	metadata string,
) {
	t.Helper()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		_, err := transaction.ExecContext(
			t.Context(),
			`insert into models(id, entry_id, state, created_by, created_at) values ($1, $2, 'active', $3, $4)`,
			modelID,
			entryID,
			createdBy,
			createdAt,
		)
		require.NoError(t, err)
		_, err = transaction.ExecContext(
			t.Context(),
			`insert into model_revisions(
			   id, model_id, revision_number, state, model_state, published_at,
			   title, metadata, created_by, created_at, updated_at
			 ) values ($1, $2, 1, 'active', 'active', $3, 'list test model', $4::jsonb, $5, $3, $3)`,
			uuid.New(),
			modelID,
			createdAt,
			metadata,
			createdBy,
		)
		require.NoError(t, err)
	})
}

func runInTransactionForTest(t *testing.T, database integrationDB, run func(transaction *sql.Tx)) {
	t.Helper()
	transaction, err := database.Conn.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer func() {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("roll back list fixture: %v", rollbackErr)
		}
	}()
	run(transaction)
	require.NoError(t, transaction.Commit())
}
