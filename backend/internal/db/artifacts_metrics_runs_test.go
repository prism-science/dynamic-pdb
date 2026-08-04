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
	got, err := testDB.Artifacts.List(ctx, db.ArtifactFilters{EntryRevisionID: &entryRevision.ID})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, artifact.ID, got[0].ID)
	assert.Equal(t, models.ArtifactLevelL0, got[0].Level)
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
