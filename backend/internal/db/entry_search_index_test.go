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

func Test_should_index_entry_and_model_revision_text_when_search_index_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	entryToken := "entrytoken" + token
	modelToken := "modeltoken" + token
	externalRefToken := "externalref" + token
	authorToken := "author" + token
	ligandToken := "ligand" + token
	organism := "organism " + entryToken
	spaceGroup := "spacegroup " + entryToken
	method := models.StructureMethodCryoEM
	modelType := models.StructureModelTypeMulticonformer
	atomCount := 123456789
	entryRevision := createDBTestEntryRevision(t, "entry "+entryToken, now)
	entryRevision.Metadata = models.EntryMetadata{
		ExternalRefs: map[models.EntrySource]string{
			models.EntrySourcePDB: externalRefToken,
		},
		Organism:   &organism,
		Method:     &method,
		SpaceGroup: &spaceGroup,
		Resolution: ptr(1.23),
	}
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "model "+modelToken, now)
	modelRevision.Metadata = models.ModelMetadata{
		Authors:   []string{"Researcher " + authorToken},
		ModelType: &modelType,
		AtomCount: &atomCount,
		Ligands:   []string{ligandToken},
	}

	// when
	err := testDB.EntrySearch.IndexEntryRevision(ctx, *entryRevision)
	require.NoError(t, err)
	err = testDB.EntrySearch.IndexModelRevision(ctx, *modelRevision)

	// then
	require.NoError(t, err)
	assertEntryRevisionSearchContains(ctx, t, entryToken, entryRevision.EntryID)
	assertEntryRevisionSearchContains(ctx, t, externalRefToken, entryRevision.EntryID)
	assertEntryRevisionSearchContains(ctx, t, string(method), entryRevision.EntryID)
	assertEntryRevisionSearchContains(ctx, t, modelToken, entryRevision.EntryID)
	assertEntryRevisionSearchContains(ctx, t, authorToken, entryRevision.EntryID)
	assertEntryRevisionSearchContains(ctx, t, ligandToken, entryRevision.EntryID)
	assertEntryRevisionSearchDoesNotContain(ctx, t, string(models.EntrySourcePDB), entryRevision.EntryID)
	assertEntryRevisionSearchDoesNotContain(ctx, t, "123456789", entryRevision.EntryID)
	assertEntryRevisionSearchDoesNotContain(ctx, t, "1.23", entryRevision.EntryID)
}

func Test_should_remove_entry_revision_text_when_search_index_delete_called(t *testing.T) {
	// given
	ctx := context.Background()
	token := "deletetoken" + strings.ReplaceAll(uuid.NewString(), "-", "")
	entryRevision := createDBTestEntryRevision(t, "entry "+token, time.Now().UTC())
	require.NoError(t, testDB.EntrySearch.IndexEntryRevision(ctx, *entryRevision))

	// when
	err := testDB.EntrySearch.DeleteEntryRevision(ctx, entryRevision.EntryID, entryRevision.ID)

	// then
	require.NoError(t, err)
	assertEntryRevisionSearchDoesNotContain(ctx, t, token, entryRevision.EntryID)
}

func Test_should_remove_all_entry_text_when_entry_search_index_delete_called(t *testing.T) {
	// given
	ctx := context.Background()
	token := "deleteentrytoken" + strings.ReplaceAll(uuid.NewString(), "-", "")
	entryRevision := createDBTestEntryRevision(t, "entry "+token, time.Now().UTC())
	modelRevision := createDBTestModelRevision(t, entryRevision.EntryID, "model "+token, time.Now().UTC())
	require.NoError(t, testDB.EntrySearch.IndexEntryRevision(ctx, *entryRevision))
	require.NoError(t, testDB.EntrySearch.IndexModelRevision(ctx, *modelRevision))

	// when
	err := testDB.EntrySearch.DeleteEntry(ctx, entryRevision.EntryID)

	// then
	require.NoError(t, err)
	assertEntryRevisionSearchDoesNotContain(ctx, t, token, entryRevision.EntryID)
}

func assertEntryRevisionSearchContains(ctx context.Context, t *testing.T, query string, entryID uuid.UUID) {
	t.Helper()

	revisions, err := testDB.Entries.List(ctx, db.EntryRevisionFilters{Query: query})
	require.NoError(t, err)
	assert.True(t, entryRevisionListContainsEntryID(revisions, entryID))
}

func assertEntryRevisionSearchDoesNotContain(ctx context.Context, t *testing.T, query string, entryID uuid.UUID) {
	t.Helper()

	revisions, err := testDB.Entries.List(ctx, db.EntryRevisionFilters{Query: query})
	require.NoError(t, err)
	assert.False(t, entryRevisionListContainsEntryID(revisions, entryID))
}
