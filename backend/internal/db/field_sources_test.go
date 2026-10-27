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

func Test_should_keep_field_sources_when_entry_revision_is_created_and_read(t *testing.T) {
	// given
	now := time.Now().UTC()
	retrievedAt := now.Truncate(time.Second)
	sources := models.FieldSources{
		models.EntryFieldTitle:      {Source: models.FieldSourceDepositor},
		models.EntryFieldResolution: {Source: models.FieldSourceRCSB, Accession: ptr("1ABC"), RetrievedAt: &retrievedAt},
	}
	revision := models.EntryRevision{
		ID:           uuid.New(),
		EntryID:      "entry-" + uuid.NewString(),
		State:        models.RevisionStatePending,
		Title:        ptr("Entry with sources"),
		FieldSources: sources,
		CreatedBy:    createDBTestUser(t),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// when
	created, err := testDB.Entries.Create(context.Background(), revision)
	require.NoError(t, err)
	got, err := testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{ID: &created.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, sources, created.FieldSources)
	require.Len(t, got.FieldSources, 2)
	assert.Equal(t, sources[models.EntryFieldTitle], got.FieldSources[models.EntryFieldTitle])
	resolution := got.FieldSources[models.EntryFieldResolution]
	assert.Equal(t, models.FieldSourceRCSB, resolution.Source)
	assert.Equal(t, ptr("1ABC"), resolution.Accession)
	require.NotNil(t, resolution.RetrievedAt)
	assert.True(t, retrievedAt.Equal(*resolution.RetrievedAt))
}

func Test_should_store_null_field_sources_when_entry_revision_has_none(t *testing.T) {
	// given
	now := time.Now().UTC()
	revision := models.EntryRevision{
		ID:        uuid.New(),
		EntryID:   "entry-" + uuid.NewString(),
		State:     models.RevisionStatePending,
		Title:     ptr("Entry without sources"),
		CreatedBy: createDBTestUser(t),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	created, err := testDB.Entries.Create(context.Background(), revision)
	require.NoError(t, err)
	got, err := testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{ID: &created.ID})

	// then
	require.NoError(t, err)
	assert.Nil(t, got.FieldSources)
}

func Test_should_keep_field_sources_when_model_revision_is_created_and_read(t *testing.T) {
	// given
	entryRevision := createDBTestEntryRevision(t, "sources-entry", time.Now().UTC())
	now := time.Now().UTC()
	sources := models.FieldSources{
		models.ModelFieldAtomCount:                       {Source: models.FieldSourceDepositor},
		models.ArchiveMetricField(models.MetricKeyRFree): {Source: models.FieldSourceRCSB, Accession: ptr("5AMF")},
	}
	revision := models.ModelRevision{
		ID:           uuid.New(),
		ModelID:      entryRevision.EntryID + "_m_001",
		State:        models.RevisionStatePending,
		Title:        ptr("Model with sources"),
		FieldSources: sources,
		CreatedBy:    createDBTestUser(t),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// when
	created, err := testDB.Models.Create(context.Background(), entryRevision.EntryID, revision)
	require.NoError(t, err)
	got, err := testDB.Models.Get(context.Background(), db.ModelRevisionFilters{ID: &created.ID})
	withoutResidueData, listErr := testDB.Models.List(context.Background(), db.ModelRevisionFilters{
		ID:                 &created.ID,
		ExcludeResidueData: true,
	})

	// then
	require.NoError(t, err)
	require.NoError(t, listErr)
	assert.Equal(t, sources, created.FieldSources)
	assert.Equal(t, sources, got.FieldSources)
	require.Len(t, withoutResidueData, 1)
	assert.Equal(t, sources, withoutResidueData[0].FieldSources)
}
