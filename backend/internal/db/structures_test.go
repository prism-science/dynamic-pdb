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

func Test_should_return_structure_with_optional_fields_when_structures_create_and_get_called(t *testing.T) {
	// given
	now := time.Now().UTC()
	description := "Structure description " + uuid.NewString()
	thumbnailImageURL := "s3://dynamic-pdb/thumbnails/" + uuid.NewString() + ".png"
	createdBy := createDBTestUser(t)
	structure := models.Structure{
		ID:                uuid.New(),
		CreatedBy:         createdBy,
		Name:              "structure-" + uuid.NewString(),
		Description:       &description,
		ThumbnailImageURL: &thumbnailImageURL,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// when
	created, err := testDB.Structures.Create(context.Background(), structure)
	require.NoError(t, err)
	got, err := testDB.Structures.Get(context.Background(), created.ID)

	// then
	require.NoError(t, err)
	assert.Equal(t, structure.ID, got.ID)
	assert.Equal(t, createdBy, got.CreatedBy)
	assert.Equal(t, structure.Name, got.Name)
	require.NotNil(t, got.Description)
	assert.Equal(t, description, *got.Description)
	require.NotNil(t, got.ThumbnailImageURL)
	assert.Equal(t, thumbnailImageURL, *got.ThumbnailImageURL)
	assert.Equal(t, now.Unix(), got.CreatedAt.Unix())
	assert.Equal(t, now.Unix(), got.UpdatedAt.Unix())
}

func Test_should_return_not_found_when_structures_get_misses(t *testing.T) {
	// given / when
	_, err := testDB.Structures.Get(context.Background(), uuid.New())

	// then
	require.ErrorIs(t, err, db.ErrStructureNotFound)
}

func Test_should_list_structures_matching_search_with_pagination_when_structures_list_called(t *testing.T) {
	// given
	ctx := context.Background()
	token := "structuretoken" + strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Now().UTC()
	first := createDBTestStructure(t, "first "+token, now)
	second := createDBTestStructure(t, "second "+token, now.Add(time.Second))
	unmatched := createDBTestStructure(t, "unmatched "+uuid.NewString(), now.Add(2*time.Second))
	require.NoError(t, testDB.StructureSearch.IndexStructure(ctx, *first))
	require.NoError(t, testDB.StructureSearch.IndexStructure(ctx, *second))
	require.NoError(t, testDB.StructureSearch.IndexStructure(ctx, *unmatched))
	limit := 1
	offset := 1

	// when
	got, err := testDB.Structures.List(ctx, db.StructureFilters{
		Query:  token,
		Limit:  &limit,
		Offset: &offset,
	})

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, second.ID, got[0].ID)
}

func Test_should_filter_structures_by_protein_sequence_when_structures_list_called(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	sequenceToken := proteinSequenceTokenForTest(uuid.New())
	sequenceStructure := createDBTestStructure(t, "sequence structure", now)
	sequenceEntity, err := testDB.Entities.Create(ctx, models.Entity{
		ID:          uuid.New(),
		StructureID: sequenceStructure.ID,
		Type:        models.EntityTypeData,
		Name:        "protein sequence",
		Payload: models.DataPayload{
			FileURL: "https://files.example/protein.fasta",
			Type:    "fasta",
			Metadata: map[string]any{
				"sequence": "M" + sequenceToken + "K",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	require.NotNil(t, sequenceEntity)
	queryWithWhitespace := strings.ToLower(sequenceToken[:8] + "\n" + sequenceToken[8:])

	// when
	sequenceMatches, err := testDB.Structures.List(ctx, db.StructureFilters{ProteinSequence: queryWithWhitespace})

	// then
	require.NoError(t, err)
	assert.True(t, structureListContainsID(sequenceMatches, sequenceStructure.ID))
}

func Test_should_return_error_when_structures_list_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1

	// when
	_, err := testDB.Structures.List(context.Background(), db.StructureFilters{Limit: &limit})

	// then
	require.Error(t, err)
}

func Test_should_return_error_when_structures_list_called_with_text_and_protein_sequence(t *testing.T) {
	// given
	filters := db.StructureFilters{
		Query:           "text",
		ProteinSequence: "ACDEFGHIK",
	}

	// when
	_, err := testDB.Structures.List(context.Background(), filters)

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
