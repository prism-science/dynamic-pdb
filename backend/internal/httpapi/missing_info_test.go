package httpapi

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

func Test_should_return_each_missing_info_kind_when_gaps_are_converted(t *testing.T) {
	// given
	metricID, runID := uuid.New(), uuid.New()
	gaps := []validation.MissingInfo{
		{Kind: validation.MissingInfoMetricSoftware, MetricID: &metricID},
		{Kind: validation.MissingInfoRunSoftwareVersion, RunID: &runID},
		{Kind: validation.MissingInfoModelRun},
	}

	// when
	items, err := missingInfoFromValidation(gaps)

	// then
	require.NoError(t, err)
	assert.Equal(t, []MissingInfo{
		{Kind: MissingInfoKindMetricSoftware, MetricId: &metricID},
		{Kind: MissingInfoKindRunSoftwareVersion, RunId: &runID},
		{Kind: MissingInfoKindModelRun},
	}, items)
}

func Test_should_return_empty_list_when_nothing_is_missing(t *testing.T) {
	// when
	items, err := missingInfoFromValidation(nil)

	// then
	require.NoError(t, err)
	assert.NotNil(t, items)
	assert.Empty(t, items)
}

func Test_should_fail_when_missing_info_kind_is_unknown(t *testing.T) {
	// given
	gaps := []validation.MissingInfo{{Kind: "metric_unit"}}

	// when
	_, err := missingInfoFromValidation(gaps)

	// then
	require.ErrorContains(t, err, "unknown missing info kind: metric_unit")
}

func Test_should_omit_missing_info_when_runs_cannot_be_listed(t *testing.T) {
	// given: a database that refuses every query.
	closed, err := sql.Open("postgres", "host=127.0.0.1 port=1 sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	server := &Server{database: &db.DB{
		Runs: db.NewRunsRepository(sqlx.NewDb(closed, "postgres"), db.DefaultQuerierProvider),
	}}
	revision := domainmodels.ModelRevision{ID: uuid.New()}

	// when
	items := server.missingInfoForRevision(t.Context(), revision, nil)

	// then
	assert.Nil(t, items)
}
