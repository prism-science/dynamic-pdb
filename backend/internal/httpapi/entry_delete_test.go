package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_grant_entries_delete_when_deletion_is_on_and_user_can_approve(t *testing.T) {
	// given
	server := (&Server{}).WithEntryDeletion(true)
	stored := []domainmodels.Permission{
		{Key: domainmodels.PermissionKeyRevisionsApprove},
		{Key: domainmodels.PermissionKeyRevisionsReject},
	}

	// when
	keys := server.grantedPermissionKeys(stored)

	// then
	assert.Equal(t, []string{"revisions.approve", "revisions.reject", "entries.delete"}, keys)
}

func Test_should_not_grant_entries_delete_when_deletion_is_off(t *testing.T) {
	// given
	server := (&Server{}).WithEntryDeletion(false)
	stored := []domainmodels.Permission{{Key: domainmodels.PermissionKeyRevisionsApprove}}

	// when
	keys := server.grantedPermissionKeys(stored)

	// then
	assert.Equal(t, []string{"revisions.approve"}, keys)
}

func Test_should_not_grant_entries_delete_when_user_cannot_approve(t *testing.T) {
	// given
	server := (&Server{}).WithEntryDeletion(true)
	stored := []domainmodels.Permission{{Key: domainmodels.PermissionKeyRevisionsReject}}

	// when
	keys := server.grantedPermissionKeys(stored)

	// then
	assert.Equal(t, []string{"revisions.reject"}, keys)
}

func Test_should_drop_only_pdb_id_when_building_deletion_revision(t *testing.T) {
	// given
	title := "entry to delete"
	baseID := uuid.New()
	deletedBy := uuid.New()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	base := domainmodels.EntryRevision{
		ID:         baseID,
		EntryID:    "dpdb_delete",
		State:      domainmodels.RevisionStateActive,
		EntryState: domainmodels.EntryStateActive,
		Title:      &title,
		Metadata: domainmodels.EntryMetadata{
			ExternalRefs: map[domainmodels.EntrySource]string{
				domainmodels.EntrySourcePDB: "9ABC",
				"uniprot":                   "P69905",
			},
		},
		FieldSources: domainmodels.FieldSources{"title": {Source: domainmodels.FieldSourceRCSB}},
	}

	// when
	revision := deletionRevision(base, deletedBy, now)

	// then
	assert.Equal(t, domainmodels.EntryStateDeleted, revision.EntryState)
	assert.Equal(t, domainmodels.RevisionStateInReview, revision.State)
	require.NotNil(t, revision.ParentRevisionID)
	assert.Equal(t, baseID, *revision.ParentRevisionID)
	assert.Equal(t, "dpdb_delete", revision.EntryID)
	assert.Equal(t, &title, revision.Title)
	assert.Equal(t, map[domainmodels.EntrySource]string{"uniprot": "P69905"}, revision.Metadata.ExternalRefs)
	assert.Equal(t, base.FieldSources, revision.FieldSources)
	assert.Equal(t, deletedBy, revision.CreatedBy)
	assert.Equal(t, now, revision.CreatedAt)
	require.NotNil(t, revision.ChangeSummary)
	assert.Equal(t, "Entry deleted", *revision.ChangeSummary)
	assert.Equal(t, "9ABC", base.Metadata.ExternalRefs[domainmodels.EntrySourcePDB],
		"the active revision keeps its PDB ID")
}
