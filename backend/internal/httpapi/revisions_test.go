package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_skip_group_without_matching_revisions_when_revision_groups_are_built(t *testing.T) {
	// given
	created := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	withEntryRevision := revisionGroupTestEntryRevision("DPDB-1", domainmodels.RevisionStateInReview, created)
	// DPDB-2's in-review revisions were decided after the groups were listed: only its active
	// revision is left.
	decidedActive := revisionGroupTestEntryRevision("DPDB-2", domainmodels.RevisionStateActive, created)
	withModelRevisionActive := revisionGroupTestEntryRevision("DPDB-3", domainmodels.RevisionStateActive, created)
	withModelRevision := domainmodels.ModelRevision{
		ID:        uuid.New(),
		EntryID:   "DPDB-3",
		ModelID:   "DPDB-3_m_001",
		State:     domainmodels.RevisionStateInReview,
		CreatedAt: created,
		UpdatedAt: created,
	}
	groups := []db.RevisionGroup{
		{EntryID: "DPDB-1", LatestCreatedAt: created.Add(2 * time.Second)},
		{EntryID: "DPDB-2", LatestCreatedAt: created.Add(time.Second)},
		{EntryID: "DPDB-3", LatestCreatedAt: created},
	}

	// when
	items, err := revisionGroupItemsFromRevisions(
		context.Background(),
		groups,
		[]domainmodels.EntryRevision{withEntryRevision},
		[]domainmodels.ModelRevision{withModelRevision},
		[]domainmodels.EntryRevision{decidedActive, withModelRevisionActive},
	)

	// then
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "DPDB-1", items[0].Id)
	assert.Len(t, items[0].Attributes.EntryRevisions, 1)
	assert.Empty(t, items[0].Attributes.ModelRevisions)
	assert.Equal(t, "DPDB-3", items[1].Id)
	assert.Empty(t, items[1].Attributes.EntryRevisions)
	assert.Len(t, items[1].Attributes.ModelRevisions, 1)
}

func revisionGroupTestEntryRevision(
	entryID string,
	state domainmodels.RevisionState,
	created time.Time,
) domainmodels.EntryRevision {
	return domainmodels.EntryRevision{
		ID:        uuid.New(),
		EntryID:   entryID,
		State:     state,
		CreatedBy: uuid.New(),
		CreatedAt: created,
		UpdatedAt: created,
	}
}
