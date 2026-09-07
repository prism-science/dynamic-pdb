package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_return_entry_revision_with_metadata_when_entries_create_and_get_called(t *testing.T) {
	// given
	now := time.Now().UTC()
	title := "Entry title " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	createdBy := createDBTestUser(t)
	method := models.StructureMethodXRayCrystallography
	details := "Additional structure details"
	revision := models.EntryRevision{
		ID:                uuid.New(),
		EntryID:           "entry-1abc",
		State:             models.RevisionStatePending,
		Title:             &title,
		ThumbnailImageURL: &thumbnailImageURL,
		Metadata: models.EntryMetadata{
			ExternalRefs: map[models.EntrySource]string{
				models.EntrySourcePDB: "1ABC",
			},
			Details: &details,
			Method:  &method,
			Crystallography: &models.EntryCrystallography{Crystals: []models.EntryCrystal{{
				ID: "1",
				Growth: &models.EntryCrystalGrowth{
					PH:                ptr(7.5),
					TemperatureKelvin: ptr(293.0),
				},
				Diffractions: []models.EntryDiffraction{{
					ID:                "1",
					TemperatureKelvin: ptr(100.0),
				}},
			}}},
		},
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	created, err := testDB.Entries.Create(context.Background(), revision)
	require.NoError(t, err)
	got, err := testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{ID: &created.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, revision.ID, got.ID)
	assert.Equal(t, revision.EntryID, got.EntryID)
	assert.Equal(t, models.EntryStateActive, got.EntryState)
	assert.Equal(t, createdBy, got.CreatedBy)
	assert.Equal(t, revision.Title, got.Title)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, "1ABC", got.Metadata.ExternalRefs[models.EntrySourcePDB])
	require.NotNil(t, got.Metadata.Method)
	assert.Equal(t, method, *got.Metadata.Method)
	assert.Equal(t, revision.Metadata.Details, got.Metadata.Details)
	assert.Equal(t, revision.Metadata.Crystallography, got.Metadata.Crystallography)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
	newEntryState := models.EntryStateNew
	_, err = testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{
		ID:         &created.ID,
		EntryState: &newEntryState,
	})
	require.NoError(t, err)
	activeEntryState := models.EntryStateActive
	_, err = testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{
		ID:         &created.ID,
		EntryState: &activeEntryState,
	})
	require.ErrorIs(t, err, db.ErrEntryRevisionNotFound)
}

func Test_should_return_not_found_when_entries_get_misses(t *testing.T) {
	// given / when
	_, err := testDB.Entries.Get(context.Background(), db.EntryRevisionFilters{ID: ptr(uuid.New())})

	// then
	require.ErrorIs(t, err, db.ErrEntryRevisionNotFound)
}

func Test_should_list_entry_revisions_matching_search_with_pagination_when_entries_list_called(t *testing.T) {
	// given
	ctx := context.Background()
	token := "entrytoken" + strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()
	first := createDBTestEntryRevision(t, "first "+token, now)
	second := createDBTestEntryRevision(t, "second "+token, now.Add(time.Second))
	unmatched := createDBTestEntryRevision(t, "unmatched "+uuid.NewString(), now.Add(2*time.Second))
	require.NoError(t, testDB.EntrySearch.IndexEntryRevision(ctx, *first))
	require.NoError(t, testDB.EntrySearch.IndexEntryRevision(ctx, *second))
	require.NoError(t, testDB.EntrySearch.IndexEntryRevision(ctx, *unmatched))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Entries.List(ctx, db.EntryRevisionFilters{
		Query:  token,
		Limit:  &limit,
		Offset: &offset,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, first.ID, got[0].ID)
}

func Test_should_filter_entry_revisions_by_protein_sequence_when_entries_list_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	sequenceToken := proteinSequenceTokenForTest(uuid.New())
	entryRevision := createDBTestEntryRevision(t, "sequence entry", now)
	artifact := createDBTestArtifact(t, entryRevision.CreatedBy, "protein sequence artifact", now)
	require.NoError(t, testDB.ProteinSequences.Create(
		ctx,
		entryRevision.ID,
		artifact.ID,
		[]models.FASTARecord{{Header: "test protein", Sequence: "M" + sequenceToken + "K"}},
	))
	queryWithWhitespace := strings.ToLower(sequenceToken[:8] + "\n" + sequenceToken[8:])

	// when
	sequenceMatches, err := testDB.Entries.List(ctx, db.EntryRevisionFilters{ProteinSequence: queryWithWhitespace})

	// then
	require.NoError(t, err)
	assert.True(t, entryRevisionListContainsEntryID(sequenceMatches, entryRevision.EntryID))
}

func Test_should_filter_entry_revisions_by_pdb_id_when_entries_list_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	pdbID := pdbIDForTest()
	unmatchedPDBID := pdbIDForTest()
	matched := createDBTestEntryRevisionWithPDBID(t, "matched pdb ref", pdbID, models.RevisionStateActive, now)
	unmatched := createDBTestEntryRevisionWithPDBID(t, "unmatched pdb ref", unmatchedPDBID, models.RevisionStateActive, now.Add(time.Second))

	// when
	got, err := testDB.Entries.List(ctx, db.EntryRevisionFilters{PDBIDs: []string{strings.ToLower(pdbID)}})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, matched.EntryID, got[0].EntryID)
	assert.NotEqual(t, unmatched.EntryID, got[0].EntryID)
}

func Test_should_reject_duplicate_active_entry_revision_when_pdb_id_ref_matches(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	pdbID := pdbIDForTest()
	createDBTestEntryRevisionWithPDBID(t, "first pdb ref", pdbID, models.RevisionStateActive, now)
	duplicate := dbTestEntryRevisionWithPDBID(t, "duplicate pdb ref", strings.ToLower(pdbID), models.RevisionStateActive, now.Add(time.Second))

	// when
	_, err := testDB.Entries.Create(ctx, duplicate)

	// then
	require.Error(t, err)
}

func Test_should_return_error_when_entries_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Entries.List(context.Background(), db.EntryRevisionFilters{Limit: &limit})

	// then
	require.Error(t, err)
}

func Test_should_return_error_when_entries_list_called_with_text_and_protein_sequence(t *testing.T) {
	// given
	filters := db.EntryRevisionFilters{
		Query:           "text",
		ProteinSequence: "ACDEFGHIK",
	}

	// when
	_, err := testDB.Entries.List(context.Background(), filters)

	// then
	require.Error(t, err)
}

func Test_should_archive_previous_active_revision_when_entry_revision_activated(t *testing.T) {
	// given
	ctx := context.Background()
	createdBy := createDBTestUser(t)
	entryID := "entry-" + uuid.NewString()
	now := time.Now().UTC()
	revisionNumber := 1
	active, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID:             uuid.New(),
		EntryID:        entryID,
		RevisionNumber: &revisionNumber,
		State:          models.RevisionStateActive,
		EntryState:     models.EntryStateActive,
		Title:          ptr("active entry revision"),
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	require.NoError(t, err)
	pending, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID:               uuid.New(),
		EntryID:          entryID,
		ParentRevisionID: &active.ID,
		State:            models.RevisionStateInReview,
		EntryState:       models.EntryStateActive,
		Title:            ptr("replacement entry revision"),
		CreatedBy:        createdBy,
		CreatedAt:        now.Add(time.Second),
		UpdatedAt:        now.Add(time.Second),
	})
	require.NoError(t, err)

	// when
	err = testDB.Do(ctx, func(ctx context.Context) error {
		_, err := testDB.Entries.ActivateRevision(ctx, entryID, pending.ID)
		return err
	})
	require.NoError(t, err)
	archived, err := testDB.Entries.Get(ctx, db.EntryRevisionFilters{ID: &active.ID})
	require.NoError(t, err)
	activated, err := testDB.Entries.Get(ctx, db.EntryRevisionFilters{ID: &pending.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, models.RevisionStateArchived, archived.State)
	assert.Equal(t, models.RevisionStateActive, activated.State)
	assert.Equal(t, models.EntryStateActive, activated.EntryState)
	activeEntryState := models.EntryStateActive
	_, err = testDB.Entries.Get(ctx, db.EntryRevisionFilters{
		ID:         &activated.ID,
		EntryState: &activeEntryState,
	})
	require.NoError(t, err)
	require.NotNil(t, activated.RevisionNumber)
	assert.Equal(t, 2, *activated.RevisionNumber)
}

func proteinSequenceTokenForTest(id uuid.UUID) string {
	const alphabet = "ACDEFGHIKLMNPQRSTVWY"

	var token strings.Builder
	token.Grow(len(id))
	for _, value := range id {
		token.WriteByte(alphabet[int(value)%len(alphabet)])
	}
	return token.String()
}

func entryRevisionListContainsEntryID(revisions []models.EntryRevision, entryID string) bool {
	for _, revision := range revisions {
		if revision.EntryID == entryID {
			return true
		}
	}
	return false
}

func pdbIDForTest() string {
	token := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	return "1" + token[:3]
}

func createDBTestEntryRevisionWithPDBID(
	t *testing.T,
	name string,
	pdbID string,
	state models.RevisionState,
	createdAt time.Time,
) *models.EntryRevision {
	t.Helper()

	revision := dbTestEntryRevisionWithPDBID(t, name, pdbID, state, createdAt)
	created, err := testDB.Entries.Create(context.Background(), revision)
	require.NoError(t, err)
	return created
}

func dbTestEntryRevisionWithPDBID(
	t *testing.T,
	name string,
	pdbID string,
	state models.RevisionState,
	createdAt time.Time,
) models.EntryRevision {
	t.Helper()

	return models.EntryRevision{
		ID:      uuid.New(),
		EntryID: "entry-" + uuid.NewString(),
		State:   state,
		Title:   ptr(name + "-" + uuid.NewString()),
		Metadata: models.EntryMetadata{
			ExternalRefs: map[models.EntrySource]string{
				models.EntrySourcePDB: pdbID,
			},
		},
		CreatedBy: createDBTestUser(t),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
}
