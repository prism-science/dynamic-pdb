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

func Test_should_create_and_list_protein_sequence_similarities_when_repository_called(t *testing.T) {
	// given
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "protein-sequence-similarities-entry", now)
	artifact := createDBTestArtifact(t, entryRevision.CreatedBy, "protein sequences", now)
	require.NoError(t, testDB.ProteinSequences.Create(
		context.Background(),
		entryRevision.ID,
		artifact.ID,
		[]models.FASTARecord{
			{Header: "query", Sequence: "ACDEFGHIKLMNPQRSTVWY"},
			{Header: "target", Sequence: "ACDEFGHIKLMNPQRSTVWF"},
			{Header: "unrelated", Sequence: "YYYYYYYYYYYYYYYYYYYY"},
		},
	))
	sequences, err := testDB.ProteinSequences.List(context.Background(), db.ProteinSequenceFilters{
		EntryRevisionID: &entryRevision.ID,
	})
	require.NoError(t, err)
	require.Len(t, sequences, 3)

	run, err := testDB.ProteinSequenceSimilarities.CreateRun(context.Background(), models.ProteinSequenceSimilarityRun{
		ID:         uuid.New(),
		Tool:       "mmseqs2",
		Parameters: map[string]any{"min_seq_id": 0.5, "coverage": 0.8},
		State:      models.ProteinSequenceSimilarityRunStateRunning,
		StartedAt:  &now,
		CreatedAt:  now,
	})
	require.NoError(t, err)

	// when
	err = testDB.ProteinSequenceSimilarities.Create(context.Background(), []models.ProteinSequenceSimilarity{
		{
			RunID:             run.ID,
			SourceSequenceID:  sequences[0].ID,
			SimilarSequenceID: sequences[1].ID,
			Tool:              "mmseqs2",
			Score:             0.91,
			Metadata: map[string]any{
				"original_score":    100,
				"norm_score":        0.91,
				"score_type":        "bits",
				"normalization":     "min_max_per_run",
				"fident":            0.95,
				"qcov":              1,
				"tcov":              1,
				"evalue":            1e-20,
				"bits":              100,
				"alignment_length":  20,
				"source_start":      1,
				"source_end":        20,
				"similar_start":     1,
				"similar_end":       20,
				"source_alignment":  "ACDEFGHIKLMNPQRSTVWY",
				"similar_alignment": "ACDEFGHIKLMNPQRSTVWF",
			},
		},
		{
			RunID:             run.ID,
			SourceSequenceID:  sequences[0].ID,
			SimilarSequenceID: sequences[2].ID,
			Tool:              "mmseqs2",
			Score:             0.2,
			Metadata: map[string]any{
				"original_score": 25,
				"norm_score":     0.2,
				"score_type":     "bits",
				"normalization":  "min_max_per_run",
				"fident":         0.5,
				"qcov":           0.4,
				"tcov":           0.5,
				"evalue":         1e-5,
			},
		},
		{
			RunID:             run.ID,
			SourceSequenceID:  sequences[1].ID,
			SimilarSequenceID: sequences[0].ID,
			Tool:              "mmseqs2",
			Score:             0.91,
			Metadata:          map[string]any{"original_score": 100, "norm_score": 0.91, "fident": 0.95, "qcov": 1, "tcov": 1, "evalue": 1e-20, "bits": 100},
		},
	})
	require.NoError(t, err)
	finishedAt := now.Add(time.Minute)
	require.NoError(t, testDB.ProteinSequenceSimilarities.UpdateRunState(
		context.Background(),
		run.ID,
		models.ProteinSequenceSimilarityRunStateSucceeded,
		nil,
		&finishedAt,
	))
	similarities, err := testDB.ProteinSequenceSimilarities.List(
		context.Background(),
		db.ProteinSequenceSimilarityFilters{SourceSequenceID: &sequences[0].ID},
	)
	succeededState := models.ProteinSequenceSimilarityRunStateSucceeded
	runs, runErr := testDB.ProteinSequenceSimilarities.ListRuns(
		context.Background(),
		db.ProteinSequenceSimilarityRunFilters{State: &succeededState},
	)

	// then
	require.NoError(t, err)
	require.Len(t, similarities, 2)
	assert.Equal(t, run.ID, similarities[0].RunID)
	assert.Equal(t, sequences[0].ID, similarities[0].SourceSequenceID)
	assert.Equal(t, sequences[1].ID, similarities[0].SimilarSequenceID)
	assert.Equal(t, "mmseqs2", similarities[0].Tool)
	assert.Equal(t, 0.91, similarities[0].Score)
	assert.Equal(t, sequences[2].ID, similarities[1].SimilarSequenceID)
	assert.Equal(t, 0.2, similarities[1].Score)
	assert.Equal(t, float64(100), similarities[0].Metadata["original_score"])
	assert.Equal(t, 0.91, similarities[0].Metadata["norm_score"])
	assert.Equal(t, "bits", similarities[0].Metadata["score_type"])
	assert.Equal(t, "min_max_per_run", similarities[0].Metadata["normalization"])
	assert.Equal(t, 0.95, similarities[0].Metadata["fident"])
	assert.Equal(t, float64(1), similarities[0].Metadata["qcov"])
	assert.Equal(t, float64(1), similarities[0].Metadata["tcov"])
	assert.Equal(t, 1e-20, similarities[0].Metadata["evalue"])
	assert.Equal(t, float64(100), similarities[0].Metadata["bits"])
	assert.Equal(t, float64(20), similarities[0].Metadata["alignment_length"])
	assert.Equal(t, float64(1), similarities[0].Metadata["source_start"])
	assert.Equal(t, float64(20), similarities[0].Metadata["source_end"])
	assert.Equal(t, float64(1), similarities[0].Metadata["similar_start"])
	assert.Equal(t, float64(20), similarities[0].Metadata["similar_end"])
	assert.Equal(t, "ACDEFGHIKLMNPQRSTVWY", similarities[0].Metadata["source_alignment"])
	assert.Equal(t, "ACDEFGHIKLMNPQRSTVWF", similarities[0].Metadata["similar_alignment"])

	require.NoError(t, runErr)
	require.NotEmpty(t, runs)
	listedRun := proteinSequenceSimilarityRunByID(runs, run.ID)
	require.NotNil(t, listedRun)
	assert.Equal(t, models.ProteinSequenceSimilarityRunStateSucceeded, listedRun.State)
	assert.Equal(t, "mmseqs2", listedRun.Tool)
	assert.Equal(t, float64(0.5), listedRun.Parameters["min_seq_id"])
	assert.Equal(t, float64(0.8), listedRun.Parameters["coverage"])
	require.NotNil(t, listedRun.FinishedAt)
	assert.Equal(t, finishedAt, *listedRun.FinishedAt)
}

func proteinSequenceSimilarityRunByID(
	runs []models.ProteinSequenceSimilarityRun,
	id uuid.UUID,
) *models.ProteinSequenceSimilarityRun {
	for index := range runs {
		if runs[index].ID == id {
			return &runs[index]
		}
	}
	return nil
}
