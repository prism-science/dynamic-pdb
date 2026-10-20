package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_create_and_list_artifacts_for_entry_revision_when_artifacts_repository_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "artifact entry revision", now)
	format := "fasta"
	artifact, err := testDB.Artifacts.Create(ctx, models.Artifact{
		ID:     uuid.New(),
		Name:   "sequence artifact " + uuid.NewString(),
		Level:  models.ArtifactLevelL0,
		Type:   models.ArtifactTypeFASTA,
		Format: &format,
		Metadata: models.FASTAMetadata{
			Records: []models.FASTARecord{{Header: "first", Sequence: "ACDEFGHIK"}},
		},
		CreatedBy: entryRevision.CreatedBy,
		CreatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, testDB.Artifacts.AttachToEntryRevision(ctx, entryRevision.ID, artifact.ID))

	// when
	got, err := testDB.Artifacts.List(ctx, db.ArtifactFilters{
		EntryRevisionID: &entryRevision.ID,
		Types:           []models.ArtifactType{models.ArtifactTypeFASTA},
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, artifact.ID, got[0].ID)
	assert.Equal(t, models.ArtifactLevelL0, got[0].Level)
	assert.Equal(t, models.ArtifactTypeFASTA, got[0].Type)
	fasta, err := got[0].FASTA()
	require.NoError(t, err)
	require.Len(t, fasta.Records, 1)
	assert.Equal(t, "first", fasta.Records[0].Header)
}

func Test_should_create_and_list_metrics_for_model_revision_when_metrics_repository_called(t *testing.T) {
	// given
	ctx := context.Background()
	entryRevision := createDBTestEntryRevision(t, "metric entry revision", time.Now().UTC())
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "metric model revision", time.Now().UTC())
	metric, err := testDB.Metrics.Create(ctx, models.Metric{
		ID:        uuid.New(),
		Key:       models.MetricKeyRFree,
		Value:     0.234,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.NoError(t, testDB.Metrics.AttachToModelRevision(ctx, modelRevision.ID, metric.ID))

	// when
	got, err := testDB.Metrics.List(ctx, db.MetricFilters{
		ModelRevisionID: &modelRevision.ID,
		Keys:            []models.MetricKey{models.MetricKeyRFree},
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, metric.ID, got[0].ID)
	assert.Equal(t, models.MetricKeyRFree, got[0].Key)
	assert.InDelta(t, 0.234, got[0].Value, 0.0001)
}

func Test_should_group_metrics_by_model_revision_when_listing_by_model_revision_ids(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	entryRevision := createDBTestEntryRevision(t, "batch metric entry revision", now)
	firstRevision := createDBTestModelRevision(t, entryRevision.EntryID, "first batch metric model revision", now)
	secondRevision := createDBTestModelRevision(t, entryRevision.EntryID, "second batch metric model revision", now)
	revisionWithoutMetrics := createDBTestModelRevision(t, entryRevision.EntryID, "metricless model revision", now)
	laterFirstMetric := createDBTestMetric(t, firstRevision.ID, models.MetricKeyRWork, 0.2, now.Add(time.Minute))
	earlierFirstMetric := createDBTestMetric(t, firstRevision.ID, models.MetricKeyRFree, 0.25, now)
	secondMetric := createDBTestMetric(t, secondRevision.ID, models.MetricKeyClashscore, 3.5, now)

	// when
	got, err := testDB.Metrics.ListByModelRevisionIDs(ctx, []uuid.UUID{
		firstRevision.ID,
		secondRevision.ID,
		revisionWithoutMetrics.ID,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Len(t, got[firstRevision.ID], 2)
	assert.Equal(t, earlierFirstMetric.ID, got[firstRevision.ID][0].ID)
	assert.Equal(t, laterFirstMetric.ID, got[firstRevision.ID][1].ID)
	require.Len(t, got[secondRevision.ID], 1)
	assert.Equal(t, secondMetric.ID, got[secondRevision.ID][0].ID)
	assert.Equal(t, models.MetricKeyClashscore, got[secondRevision.ID][0].Key)
	assert.InDelta(t, 3.5, got[secondRevision.ID][0].Value, 0.0001)
	assert.NotContains(t, got, revisionWithoutMetrics.ID)
}

func Test_should_return_empty_map_when_listing_metrics_for_no_model_revisions(t *testing.T) {
	// given
	ctx := context.Background()

	// when
	got, err := testDB.Metrics.ListByModelRevisionIDs(ctx, nil)

	// then
	require.NoError(t, err)
	assert.Empty(t, got)
}

func createDBTestMetric(
	t *testing.T,
	modelRevisionID uuid.UUID,
	key models.MetricKey,
	value float64,
	createdAt time.Time,
) *models.Metric {
	t.Helper()

	metric, err := testDB.Metrics.Create(context.Background(), models.Metric{
		ID:        uuid.New(),
		Key:       key,
		Value:     value,
		CreatedAt: createdAt,
	})
	require.NoError(t, err)
	require.NoError(t, testDB.Metrics.AttachToModelRevision(context.Background(), modelRevisionID, metric.ID))
	return metric
}

func Test_should_create_and_list_runs_for_model_revision_when_runs_repository_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "run entry revision", now)
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "run model revision", now)
	softwareName := "qFit"
	run, err := testDB.Runs.Create(ctx, models.Run{
		ID:           uuid.New(),
		Name:         "qFit run " + uuid.NewString(),
		SoftwareName: &softwareName,
		Parameters: map[string]any{
			"occupancy": 0.42,
		},
		Metadata: map[string]any{
			"note": "test run",
		},
		CreatedBy: modelRevision.CreatedBy,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	artifact := createDBTestArtifact(t, modelRevision.CreatedBy, "run output artifact", now)
	require.NoError(t, testDB.Runs.AttachToModelRevision(ctx, modelRevision.ID, run.ID))
	require.NoError(t, testDB.Runs.AttachArtifact(ctx, run.ID, artifact.ID, models.RunArtifactDirectionOutput))

	// when
	got, err := testDB.Runs.List(ctx, db.RunFilters{ModelRevisionID: &modelRevision.ID})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, run.ID, got[0].ID)
	require.NotNil(t, got[0].SoftwareName)
	assert.Equal(t, softwareName, *got[0].SoftwareName)
	assert.Equal(t, "test run", got[0].Metadata["note"])
}
