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

func Test_should_order_groups_by_newest_matching_revision_when_revision_groups_are_listed(t *testing.T) {
	// given
	ctx := context.Background()
	author := createDBTestUser(t)
	otherAuthor := createDBTestUser(t)
	base := time.Now().UTC().Truncate(time.Microsecond)
	inReview := models.RevisionStateInReview

	oldest := createRevisionGroupTestEntryRevision(t, author, inReview, base)
	withNewerModel := createRevisionGroupTestEntryRevision(t, author, inReview, base.Add(time.Second))
	createRevisionGroupTestModelRevision(t, withNewerModel.EntryID, author, inReview, base.Add(5*time.Second))
	tiedFirst := createRevisionGroupTestEntryRevision(t, author, inReview, base.Add(3*time.Second))
	tiedSecond := createRevisionGroupTestEntryRevision(t, author, inReview, base.Add(3*time.Second))
	modelOnly := createRevisionGroupTestEntryRevision(t, author, models.RevisionStatePending, base.Add(10*time.Second))
	createRevisionGroupTestModelRevision(t, modelOnly.EntryID, author, inReview, base.Add(2*time.Second))
	createRevisionGroupTestModelRevision(t, oldest.EntryID, otherAuthor, inReview, base.Add(20*time.Second))

	// when
	groups, err := testDB.Entries.ListRevisionGroups(ctx, db.RevisionGroupFilters{
		State:     inReview,
		CreatedBy: &author,
	})

	// then
	require.NoError(t, err)
	tied := []string{tiedFirst.EntryID, tiedSecond.EntryID}
	sort.Strings(tied)
	assert.Equal(t, []db.RevisionGroup{
		{EntryID: withNewerModel.EntryID, LatestCreatedAt: base.Add(5 * time.Second)},
		{EntryID: tied[0], LatestCreatedAt: base.Add(3 * time.Second)},
		{EntryID: tied[1], LatestCreatedAt: base.Add(3 * time.Second)},
		{EntryID: modelOnly.EntryID, LatestCreatedAt: base.Add(2 * time.Second)},
		{EntryID: oldest.EntryID, LatestCreatedAt: base},
	}, normalizeRevisionGroupTimes(groups))
}

func Test_should_return_every_group_once_in_order_when_revision_groups_walk_pages_with_after(t *testing.T) {
	// given
	ctx := context.Background()
	author := createDBTestUser(t)
	base := time.Now().UTC().Truncate(time.Microsecond)
	inReview := models.RevisionStateInReview
	for _, offset := range []time.Duration{0, time.Second, time.Second, time.Second, 2 * time.Second} {
		createRevisionGroupTestEntryRevision(t, author, inReview, base.Add(offset))
	}
	all, err := testDB.Entries.ListRevisionGroups(ctx, db.RevisionGroupFilters{State: inReview, CreatedBy: &author})
	require.NoError(t, err)
	require.Len(t, all, 5)

	// when
	walked := make([]db.RevisionGroup, 0, len(all))
	var after *db.RevisionGroupPosition
	for range len(all) {
		page, err := testDB.Entries.ListRevisionGroups(ctx, db.RevisionGroupFilters{
			State:     inReview,
			CreatedBy: &author,
			Limit:     ptr(2),
			After:     after,
		})
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		walked = append(walked, page...)
		last := page[len(page)-1]
		after = &db.RevisionGroupPosition{LatestCreatedAt: last.LatestCreatedAt, EntryID: last.EntryID}
	}

	// then
	assert.Equal(t, normalizeRevisionGroupTimes(all), normalizeRevisionGroupTimes(walked))
}

func Test_should_reject_after_with_offset_when_revision_groups_are_listed(t *testing.T) {
	// given
	filters := db.RevisionGroupFilters{
		State:  models.RevisionStateInReview,
		Offset: ptr(1),
		After:  &db.RevisionGroupPosition{LatestCreatedAt: time.Now().UTC(), EntryID: "entry"},
	}

	// when
	_, err := testDB.Entries.ListRevisionGroups(context.Background(), filters)

	// then
	require.Error(t, err)
}

func Test_should_return_only_listed_entries_when_revisions_are_filtered_by_entry_ids(t *testing.T) {
	// given
	ctx := context.Background()
	author := createDBTestUser(t)
	now := time.Now().UTC()
	first := createRevisionGroupTestEntryRevision(t, author, models.RevisionStatePending, now)
	second := createRevisionGroupTestEntryRevision(t, author, models.RevisionStatePending, now)
	createRevisionGroupTestEntryRevision(t, author, models.RevisionStatePending, now)
	firstModel := createRevisionGroupTestModelRevision(t, first.EntryID, author, models.RevisionStatePending, now)
	createRevisionGroupTestModelRevision(t, second.EntryID, createDBTestUser(t), models.RevisionStatePending, now)

	// when
	entryRevisions, entryErr := testDB.Entries.List(ctx, db.EntryRevisionFilters{
		EntryIDs:  []string{first.EntryID, second.EntryID},
		CreatedBy: &author,
	})
	modelRevisions, modelErr := testDB.Models.List(ctx, db.ModelRevisionFilters{
		EntryIDs:  []string{first.EntryID, second.EntryID},
		CreatedBy: &author,
	})

	// then
	require.NoError(t, entryErr)
	require.NoError(t, modelErr)
	assert.ElementsMatch(t, []uuid.UUID{first.ID, second.ID}, entryRevisionIDs(entryRevisions))
	require.Len(t, modelRevisions, 1)
	assert.Equal(t, firstModel.ID, modelRevisions[0].ID)
}

func createRevisionGroupTestEntryRevision(
	t *testing.T,
	createdBy uuid.UUID,
	state models.RevisionState,
	createdAt time.Time,
) *models.EntryRevision {
	t.Helper()

	revision, err := testDB.Entries.Create(context.Background(), models.EntryRevision{
		ID:        uuid.New(),
		EntryID:   "entry-" + uuid.NewString(),
		State:     state,
		Title:     ptr("revision group entry " + uuid.NewString()),
		CreatedBy: createdBy,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return revision
}

func createRevisionGroupTestModelRevision(
	t *testing.T,
	entryID string,
	createdBy uuid.UUID,
	state models.RevisionState,
	createdAt time.Time,
) *models.ModelRevision {
	t.Helper()

	revision, err := testDB.Models.Create(context.Background(), entryID, models.ModelRevision{
		ID:        uuid.New(),
		ModelID:   "model-" + uuid.NewString(),
		State:     state,
		Title:     ptr("revision group model " + uuid.NewString()),
		CreatedBy: createdBy,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	})
	require.NoError(t, err)
	return revision
}

// normalizeRevisionGroupTimes puts every time in UTC so groups compare with assert.Equal
// regardless of the location the driver returns.
func normalizeRevisionGroupTimes(groups []db.RevisionGroup) []db.RevisionGroup {
	normalized := make([]db.RevisionGroup, 0, len(groups))
	for _, group := range groups {
		normalized = append(normalized, db.RevisionGroup{EntryID: group.EntryID, LatestCreatedAt: group.LatestCreatedAt.UTC()})
	}
	return normalized
}

func entryRevisionIDs(revisions []models.EntryRevision) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(revisions))
	for _, revision := range revisions {
		ids = append(ids, revision.ID)
	}
	return ids
}
