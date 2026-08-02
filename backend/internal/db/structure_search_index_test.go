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

func Test_should_index_only_requested_record_when_index_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	structureToken := "structuretoken" + token
	modelToken := "modeltoken" + token
	thumbnailToken := "thumbnailtoken" + token
	thumbnailURL := "https://images.example/" + thumbnailToken + ".jpeg"
	firstEntityToken := "firstentitytoken" + token
	secondEntityToken := "secondentitytoken" + token
	authorToken := "authortoken" + token
	affiliationToken := "affiliationtoken" + token
	fileURLToken := "fileurltoken" + token
	affiliation := "research affiliation " + affiliationToken
	entityLevel := models.EntityLevelL3
	createdBy := createDBTestUser(t)

	structure, err := testDB.Structures.Create(ctx, models.Structure{
		ID:        uuid.New(),
		CreatedBy: createdBy,
		Name:      "structure " + structureToken,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	structure.ThumbnailImageURL = &thumbnailURL

	model, err := testDB.Models.Create(ctx, models.Model{
		ID:          uuid.New(),
		StructureID: structure.ID,
		CreatedBy:   createdBy,
		Name:        "model " + modelToken,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	model.ThumbnailImageURL = &thumbnailURL

	firstEntity, err := testDB.Entities.Create(ctx, models.Entity{
		ID:          uuid.New(),
		StructureID: structure.ID,
		ModelID:     &model.ID,
		Type:        models.EntityTypeData,
		Level:       &entityLevel,
		Name:        "first entity " + firstEntityToken,
		Payload: models.DataPayload{
			FileURL:     "https://files.example/" + fileURLToken + ".bin",
			Type:        "fasta",
			Authors:     []string{"Researcher " + authorToken},
			Affiliation: &affiliation,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	secondEntity, err := testDB.Entities.Create(ctx, models.Entity{
		ID:          uuid.New(),
		StructureID: structure.ID,
		ModelID:     &model.ID,
		Type:        models.EntityTypeData,
		Name:        "second data file " + secondEntityToken,
		Payload: models.DataPayload{
			FileURL: "s3://dynamic-pdb/test/second.fasta",
			Type:    "fasta",
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	// when
	err = testDB.StructureSearch.IndexStructure(ctx, *structure)

	// then
	require.NoError(t, err)
	assertStructureSearchContains(ctx, t, structureToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, thumbnailToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, modelToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, firstEntityToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, secondEntityToken, structure.ID)

	// when
	err = testDB.StructureSearch.IndexModel(ctx, *model)

	// then
	require.NoError(t, err)
	assertStructureSearchContains(ctx, t, modelToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, thumbnailToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, firstEntityToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, secondEntityToken, structure.ID)

	// when
	err = testDB.StructureSearch.IndexEntity(ctx, *firstEntity)

	// then
	require.NoError(t, err)
	assertStructureSearchContains(ctx, t, firstEntityToken, structure.ID)
	assertStructureSearchContains(ctx, t, authorToken, structure.ID)
	assertStructureSearchContains(ctx, t, affiliationToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, fileURLToken, structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, "fasta", structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, string(entityLevel), structure.ID)
	assertStructureSearchDoesNotContain(ctx, t, secondEntityToken, structure.ID)

	// when
	err = testDB.StructureSearch.IndexEntity(ctx, *secondEntity)

	// then
	require.NoError(t, err)
	assertStructureSearchContains(ctx, t, secondEntityToken, structure.ID)
}

func assertStructureSearchContains(ctx context.Context, t *testing.T, query string, structureID uuid.UUID) {
	t.Helper()

	structures, err := testDB.Structures.List(ctx, db.StructureFilters{Query: query})
	require.NoError(t, err)
	assert.True(t, structureListContainsID(structures, structureID))
}

func assertStructureSearchDoesNotContain(ctx context.Context, t *testing.T, query string, structureID uuid.UUID) {
	t.Helper()

	structures, err := testDB.Structures.List(ctx, db.StructureFilters{Query: query})
	require.NoError(t, err)
	assert.False(t, structureListContainsID(structures, structureID))
}

func structureListContainsID(structures []models.Structure, structureID uuid.UUID) bool {
	for _, structure := range structures {
		if structure.ID == structureID {
			return true
		}
	}
	return false
}
