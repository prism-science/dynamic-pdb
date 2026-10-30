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

func Test_should_keep_compound_record_and_glycans_when_entry_revision_is_created_and_read(t *testing.T) {
	// given
	now := time.Now().UTC()
	record := &models.CompoundSources{
		Ligands: map[string]models.CompoundSource{
			"NAG": {Rule: models.CompoundRuleBranched, ListVersion: "2026.10.0"},
			"SO4": {Rule: models.CompoundRuleList, ListVersion: "2026.10.0", ReviewReason: "often from the buffer"},
		},
		Cofactors: map[string]models.CompoundSource{"ZN": {Rule: models.CompoundRuleMetalc, ListVersion: "2026.10.0"}},
		Excluded:  map[string]models.CompoundSource{"GOL": {Rule: models.CompoundRuleList, ListVersion: "2026.10.0"}},
	}
	glycans := &[]models.Glycan{{
		EntityID:    "2",
		CompoundIDs: []string{"NAG"},
		Descriptors: []models.GlycanDescriptor{{Descriptor: "WURCS=2.0/1,1,0/[a2122h-1b_1-5_2*NCC/3=O]/1/", Type: "WURCS", Program: "PDB2Glycan", ProgramVersion: "1.1.0"}},
	}}
	revision := models.EntryRevision{
		ID:                uuid.New(),
		EntryID:           "entry-" + uuid.NewString(),
		State:             models.RevisionStatePending,
		Title:             ptr("Entry with a compound record"),
		CompoundSources:   record,
		GlycanDescriptors: glycans,
		CreatedBy:         createDBTestUser(t),
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// when
	created, err := testDB.Entries.Create(context.Background(), revision)
	require.NoError(t, err)
	got, err := testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{ID: &created.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, record, created.CompoundSources)
	assert.Equal(t, record, got.CompoundSources)
	assert.Equal(t, glycans, got.GlycanDescriptors)
}

func Test_should_store_null_compound_record_when_entry_revision_has_none(t *testing.T) {
	// given
	now := time.Now().UTC()
	revision := models.EntryRevision{
		ID:        uuid.New(),
		EntryID:   "entry-" + uuid.NewString(),
		State:     models.RevisionStatePending,
		Title:     ptr("Entry without a compound record"),
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
	assert.Nil(t, got.CompoundSources)
	assert.Nil(t, got.GlycanDescriptors)
}

func Test_should_store_empty_glycan_list_when_entry_revision_has_no_glycans(t *testing.T) {
	// given
	now := time.Now().UTC()
	revision := models.EntryRevision{
		ID:                uuid.New(),
		EntryID:           "entry-" + uuid.NewString(),
		State:             models.RevisionStatePending,
		Title:             ptr("Entry known to have no glycans"),
		GlycanDescriptors: &[]models.Glycan{},
		CreatedBy:         createDBTestUser(t),
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// when
	created, err := testDB.Entries.Create(context.Background(), revision)
	require.NoError(t, err)
	got, err := testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{ID: &created.ID})

	// then
	require.NoError(t, err)
	require.NotNil(t, got.GlycanDescriptors)
	assert.Empty(t, *got.GlycanDescriptors)
}
