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

func Test_should_create_and_list_polymer_entity_when_repository_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "polymer-entity-entry", now)
	artifact := createDBTestArtifact(t, entryRevision.CreatedBy, "polymer sequence", now)
	require.NoError(t, testDB.ProteinSequences.Create(ctx, entryRevision.ID, artifact.ID, []models.FASTARecord{
		{Header: "polymer", Sequence: "ACDEFGHIK"},
	}))
	sequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &entryRevision.ID,
	})
	require.NoError(t, err)
	require.Len(t, sequences, 1)
	entity := models.PolymerEntity{
		ID:                uuid.New(),
		ProteinSequenceID: sequences[0].ID,
		Metadata: models.PolymerEntityMetadata{
			LabelEntityID: ptr("1"),
			LabelAsymID:   ptr("A"),
			AuthAsymID:    ptr("X"),
			Description:   ptr("test polymer"),
			SourceOrganisms: []models.PolymerEntityOrganism{
				{
					ScientificName: "Escherichia coli",
					NCBITaxonomyID: ptr(562),
				},
			},
			Construct: ptr("C-terminal catalytic domain"),
			Mutations: ptr("A123G"),
			UniProtMappings: []models.PolymerEntityUniProtReference{
				{
					Accession:      "P69441",
					Source:         models.UniProtReferenceSourceSIFTS,
					UniProtRelease: ptr("2026_03"),
				},
			},
			ResidueData: []models.ResidueData{{
				LabelAsymID: "A", LabelSeqID: 52, LabelCompID: "ILE", AuthAsymID: ptr("X"),
				AuthSeqID: ptr(52), UniProtPosition: ptr("P69441:52"), RSCC: ptr(0.97),
			}},
		},
		CreatedAt: now,
	}

	// when
	created, err := testDB.PolymerEntities.Create(ctx, entity)
	require.NoError(t, err)
	require.NoError(t, testDB.PolymerEntities.AttachToEntryRevision(ctx, entryRevision.ID, created.ID))
	got, err := testDB.PolymerEntities.Get(ctx, db.PolymerEntityFilters{
		EntryRevisionID: &entryRevision.ID,
		ID:              &created.ID,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, entity.ID, got.ID)
	assert.Equal(t, entity.ProteinSequenceID, got.ProteinSequenceID)
	assert.Equal(t, entity.Metadata, got.Metadata)
	assert.Equal(t, entity.CreatedAt.Unix(), got.CreatedAt.Unix())
}

func Test_should_copy_polymer_entity_links_when_repository_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryID := "entry-" + uuid.NewString()
	createdBy := createDBTestUser(t)
	firstRevision, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID: uuid.New(), EntryID: entryID, State: models.RevisionStateActive,
		EntryState: models.EntryStateActive, Title: ptr("first revision"), CreatedBy: createdBy,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	secondRevision, err := testDB.Entries.Create(ctx, models.EntryRevision{
		ID: uuid.New(), EntryID: entryID, ParentRevisionID: &firstRevision.ID,
		State: models.RevisionStatePending, EntryState: models.EntryStateActive,
		Title: ptr("second revision"), CreatedBy: createdBy,
		CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second),
	})
	require.NoError(t, err)
	artifact := createDBTestArtifact(t, createdBy, "shared polymer sequence", now)
	require.NoError(t, testDB.ProteinSequences.Create(ctx, firstRevision.ID, artifact.ID, []models.FASTARecord{
		{Header: "shared polymer", Sequence: "LMNPQRSTV"},
	}))
	sequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &firstRevision.ID,
	})
	require.NoError(t, err)
	require.Len(t, sequences, 1)
	entity, err := testDB.PolymerEntities.Create(ctx, models.PolymerEntity{
		ID: uuid.New(), ProteinSequenceID: sequences[0].ID,
		Metadata: models.PolymerEntityMetadata{}, CreatedAt: now,
	})
	require.NoError(t, err)
	require.NoError(t, testDB.PolymerEntities.AttachToEntryRevision(ctx, firstRevision.ID, entity.ID))

	// when
	err = testDB.PolymerEntities.CopyEntryRevisionLinks(ctx, firstRevision.ID, secondRevision.ID)

	// then
	require.NoError(t, err)
	entities, err := testDB.PolymerEntities.List(ctx, db.PolymerEntityFilters{
		EntryRevisionID: &secondRevision.ID,
	})
	require.NoError(t, err)
	require.Len(t, entities, 1)
	assert.Equal(t, entity.ID, entities[0].ID)
}
