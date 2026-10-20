package db_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_return_every_similar_entry_once_in_order_when_similar_entries_walk_pages_with_after(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	source := createDBTestActiveEntryRevision(t, "similar-source", now)
	sourceSequence := createSimilarEntryTestSequence(t, source, "ACDEFGHIKLMNPQRSTVWY")
	run, err := testDB.ProteinSequenceSimilarities.CreateRun(ctx, models.ProteinSequenceSimilarityRun{
		ID:        uuid.New(),
		Tool:      "mmseqs2",
		State:     models.ProteinSequenceSimilarityRunStateRunning,
		StartedAt: &now,
		CreatedAt: now,
	})
	require.NoError(t, err)
	scores := []float64{0.9, 0.5, 0.5, 0.5, 0.1}
	similarities := make([]models.ProteinSequenceSimilarity, 0, len(scores))
	for _, score := range scores {
		target := createDBTestActiveEntryRevision(t, "similar-target", now)
		targetSequence := createSimilarEntryTestSequence(t, target, "ACDEFGHIKLMNPQRSTVWF")
		similarities = append(similarities, models.ProteinSequenceSimilarity{
			RunID:             run.ID,
			SourceSequenceID:  sourceSequence.ID,
			SimilarSequenceID: targetSequence.ID,
			Tool:              "mmseqs2",
			Score:             score,
			Metadata:          map[string]any{},
		})
	}
	require.NoError(t, testDB.ProteinSequenceSimilarities.Create(ctx, similarities))
	all, err := testDB.ProteinSequenceSimilarities.ListSimilarEntries(ctx, db.SimilarEntryFilters{EntryID: source.EntryID})
	require.NoError(t, err)
	require.Len(t, all, len(scores))

	// when
	walked := make([]string, 0, len(all))
	var after *db.SimilarEntryPosition
	for range len(all) {
		page, err := testDB.ProteinSequenceSimilarities.ListSimilarEntries(ctx, db.SimilarEntryFilters{
			EntryID: source.EntryID,
			Limit:   ptr(2),
			After:   after,
		})
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		for _, entry := range page {
			walked = append(walked, entry.Entry.EntryID)
		}
		last := page[len(page)-1]
		after = &db.SimilarEntryPosition{Score: last.Score, EntryID: last.Entry.EntryID}
	}

	// then
	assert.Equal(t, similarEntryIDs(all), walked)
	tiedEntryIDs := similarEntryIDs(all)[1:4]
	assert.True(t, sort.StringsAreSorted(tiedEntryIDs), "entries with equal scores are ordered by entry id")
}

func Test_should_reject_after_with_offset_when_similar_entries_are_listed(t *testing.T) {
	// given
	filters := db.SimilarEntryFilters{
		EntryID: "entry",
		Offset:  ptr(1),
		After:   &db.SimilarEntryPosition{Score: 0.5, EntryID: "entry"},
	}

	// when
	_, err := testDB.ProteinSequenceSimilarities.ListSimilarEntries(context.Background(), filters)

	// then
	require.Error(t, err)
}

func createSimilarEntryTestSequence(
	t *testing.T,
	revision *models.EntryRevision,
	sequence string,
) models.ProteinSequence {
	t.Helper()

	ctx := context.Background()
	artifact := createDBTestArtifact(t, revision.CreatedBy, "similar entry sequences", revision.CreatedAt)
	require.NoError(t, testDB.ProteinSequences.Create(ctx, revision.ID, artifact.ID, []models.FASTARecord{
		{Header: "chain A", Sequence: sequence},
	}))
	sequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{EntryRevisionID: &revision.ID})
	require.NoError(t, err)
	require.Len(t, sequences, 1)
	return sequences[0]
}

func similarEntryIDs(entries []models.SimilarEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.Entry.EntryID)
	}
	return ids
}
