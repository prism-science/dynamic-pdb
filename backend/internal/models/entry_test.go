package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func Test_should_compare_only_entry_revision_data_when_checked_for_equality(t *testing.T) {
	// given
	title := "entry"
	revision := EntryRevision{
		ID:         uuid.New(),
		EntryID:    "dpdb_entry",
		State:      RevisionStateActive,
		EntryState: EntryStateActive,
		Title:      &title,
		Metadata: EntryMetadata{
			ExternalRefs: map[EntrySource]string{EntrySourcePDB: "5AMF"},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	other := revision
	other.ID = uuid.New()
	other.State = RevisionStatePending
	other.CreatedAt = revision.CreatedAt.Add(time.Hour)
	other.UpdatedAt = revision.UpdatedAt.Add(time.Hour)

	// when
	equalBeforeDataChange := revision.HasSameData(other)
	changedTitle := "changed entry"
	other.Title = &changedTitle
	equalAfterDataChange := revision.HasSameData(other)

	// then
	assert.True(t, equalBeforeDataChange)
	assert.False(t, equalAfterDataChange)
}

func Test_should_detect_entry_crystallography_change(t *testing.T) {
	// given
	revision := EntryRevision{Metadata: EntryMetadata{
		Crystallography: &EntryCrystallography{Crystals: []EntryCrystal{{ID: "1"}}},
	}}
	other := revision
	other.Metadata.Crystallography = &EntryCrystallography{Crystals: []EntryCrystal{{
		ID:     "1",
		Growth: &EntryCrystalGrowth{PH: new(7.5)},
	}}}

	// when
	equal := revision.HasSameData(other)

	// then
	assert.False(t, equal)
}
