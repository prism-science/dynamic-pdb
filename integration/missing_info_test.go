package integration

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type missingInfoDocument struct {
	Data struct {
		Attributes struct {
			MissingInfo *[]missingInfoItem `json:"missing_info"`
		} `json:"attributes"`
	} `json:"data"`
}

type missingInfoItem struct {
	Kind     string  `json:"kind"`
	MetricID *string `json:"metric_id"`
	RunID    *string `json:"run_id"`
}

// seededProvenance is one run and one metric stored on a model's active revision.
type seededProvenance struct {
	RunID    string
	MetricID string
}

func Test_should_label_missing_info_when_old_computed_model_lacks_required_provenance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: models stored before the provenance rules, as old records are, plus a
	// fully described one. Inserted with SQL, because enforce mode rejects the gaps.
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	entryID := seedActiveEntry(t, database, auth.UserID)
	now := time.Now().UTC()
	computedID, depositedID, describedID, runlessID := entryID+"_m_001", entryID+"_m_002", entryID+"_m_003", entryID+"_m_004"
	// Runs are linked before the revisions are published, as in the app.
	seedModel(t, database, entryID, computedID, auth.UserID, now, `{"model_type": "Single Conformer"}`, "pending")
	seedModel(t, database, entryID, depositedID, auth.UserID, now, `{"model_type": "Deposited"}`, "pending")
	seedModel(t, database, entryID, describedID, auth.UserID, now, `{"model_type": "Multiconformer"}`, "pending")
	seedActiveModel(t, database, entryID, runlessID, auth.UserID, now, `{}`)
	computed := seedRunAndMetric(t, database, computedID, auth.UserID, nil, false)
	seedRunAndMetric(t, database, depositedID, auth.UserID, nil, false)
	seedRunAndMetric(t, database, describedID, auth.UserID, new("1.21"), true)
	for _, modelID := range []string{computedID, depositedID, describedID} {
		publishSeededModel(t, database, modelID)
	}
	s3 := newS3Stub(t)
	defer s3.Close()
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(),
		[]string{"DYNAMIC_PDB_VALIDATION_MODE=enforce"})
	modelsURL := backend.URL + "/v1/entries/" + entryID + "/models/"

	// when
	computedInfo := getJSON[missingInfoDocument](t, modelsURL+computedID, auth.AccessToken)
	depositedInfo := getJSON[missingInfoDocument](t, modelsURL+depositedID, auth.AccessToken)
	describedInfo := getJSON[missingInfoDocument](t, modelsURL+describedID, auth.AccessToken)
	runlessInfo := getJSON[missingInfoDocument](t, modelsURL+runlessID, auth.AccessToken)

	// then
	require.NotNil(t, computedInfo.Data.Attributes.MissingInfo)
	assert.ElementsMatch(t, []missingInfoItem{
		{Kind: "metric_software", MetricID: &computed.MetricID},
		{Kind: "run_software_version", RunID: &computed.RunID},
	}, *computedInfo.Data.Attributes.MissingInfo)
	require.NotNil(t, depositedInfo.Data.Attributes.MissingInfo)
	assert.Empty(t, *depositedInfo.Data.Attributes.MissingInfo)
	require.NotNil(t, describedInfo.Data.Attributes.MissingInfo)
	assert.Empty(t, *describedInfo.Data.Attributes.MissingInfo)
	require.NotNil(t, runlessInfo.Data.Attributes.MissingInfo)
	assert.Equal(t, []missingInfoItem{{Kind: "model_run"}}, *runlessInfo.Data.Attributes.MissingInfo)
}

// seedRunAndMetric stores a PHENIX run with the given version and an r_free metric on
// the model's pending revision. The metric names no software; linkMetric links it to the run.
func seedRunAndMetric(
	t *testing.T,
	database integrationDB,
	modelID string,
	createdBy string,
	softwareVersion *string,
	linkMetric bool,
) seededProvenance {
	t.Helper()
	runID, metricID := uuid.New(), uuid.New()
	var metricRunID *uuid.UUID
	if linkMetric {
		metricRunID = &runID
	}
	now := time.Now().UTC()
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		var revisionID uuid.UUID
		require.NoError(t, transaction.QueryRowContext(t.Context(),
			`select id from model_revisions where model_id = $1 and state = 'pending'`, modelID).Scan(&revisionID))
		_, err := transaction.ExecContext(t.Context(),
			`insert into runs(id, name, software_name, software_version, parameters, metadata, created_by, created_at, updated_at)
			 values ($1, 'refinement', 'PHENIX', $2, '{}'::jsonb, '{}'::jsonb, $3, $4, $4)`,
			runID, softwareVersion, createdBy, now)
		require.NoError(t, err)
		_, err = transaction.ExecContext(t.Context(),
			`insert into model_revision_runs(model_revision_id, run_id) values ($1, $2)`, revisionID, runID)
		require.NoError(t, err)
		_, err = transaction.ExecContext(t.Context(),
			`insert into metrics(id, key, value, run_id, created_at) values ($1, 'r_free', 0.21, $2, $3)`,
			metricID, metricRunID, now)
		require.NoError(t, err)
		_, err = transaction.ExecContext(t.Context(),
			`insert into model_revision_metrics(model_revision_id, metric_id) values ($1, $2)`, revisionID, metricID)
		require.NoError(t, err)
	})
	return seededProvenance{RunID: runID.String(), MetricID: metricID.String()}
}
