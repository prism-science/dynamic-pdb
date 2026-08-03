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
	description := "Entry description " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	createdBy := createDBTestUser(t)
	method := models.StructureMethodXRayCrystallography
	organism := "Homo sapiens"
	revision := models.EntryRevision{
		ID:                uuid.New(),
		EntryID:           uuid.New(),
		State:             models.RevisionStatePending,
		Name:              "entry-" + uuid.NewString(),
		Description:       &description,
		ThumbnailImageURL: &thumbnailImageURL,
		Metadata: models.EntryMetadata{
			ExternalRefs: map[models.EntrySource]string{
				models.EntrySourcePDB: "1ABC",
			},
			Organism: &organism,
			Method:   &method,
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
	assert.Equal(t, createdBy, got.CreatedBy)
	assert.Equal(t, revision.Name, got.Name)
	require.NotNil(t, got.Description)
	assert.Equal(t, description, *got.Description)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, "1ABC", got.Metadata.ExternalRefs[models.EntrySourcePDB])
	require.NotNil(t, got.Metadata.Organism)
	assert.Equal(t, organism, *got.Metadata.Organism)
	require.NotNil(t, got.Metadata.Method)
	assert.Equal(t, method, *got.Metadata.Method)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
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
	assert.Equal(t, second.ID, got[0].ID)
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

func Test_should_mark_entry_revision_deleted_when_entries_delete_called_by_owner(t *testing.T) {
	// given
	ctx := context.Background()
	revision := createDBTestEntryRevision(t, "deleted entry revision", time.Now().UTC())

	// when
	err := testDB.Entries.Delete(ctx, revision.EntryID, revision.ID, revision.CreatedBy)
	require.NoError(t, err)
	got, err := testDB.Entries.Get(ctx, db.EntryRevisionFilters{ID: &revision.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, models.RevisionStateDeleted, got.State)
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

func proteinSequenceTokenForTest(id uuid.UUID) string {
	const alphabet = "ACDEFGHIKLMNPQRSTVWY"

	var token strings.Builder
	token.Grow(len(id))
	for _, value := range id {
		token.WriteByte(alphabet[int(value)%len(alphabet)])
	}
	return token.String()
}

func entryRevisionListContainsEntryID(revisions []models.EntryRevision, entryID uuid.UUID) bool {
	for _, revision := range revisions {
		if revision.EntryID == entryID {
			return true
		}
	}
	return false
}
