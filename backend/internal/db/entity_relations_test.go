package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_create_and_list_entity_relations_for_entry(t *testing.T) {
	// given
	entry := createDBTestEntry(t, "relations-entry", time.Now().UTC())
	otherEntry := createDBTestEntry(t, "relations-other-entry", time.Now().UTC())
	now := time.Now().UTC()
	levelL0 := models.EntityLevelL0
	levelL2 := models.EntityLevelL2
	source := createDBTestEntity(t, entry.ID, nil, models.EntityTypeData, &levelL0, "source data")
	target := createDBTestEntity(t, entry.ID, nil, models.EntityTypeModel, &levelL2, "target model")
	otherSource := createDBTestEntity(t, otherEntry.ID, nil, models.EntityTypeData, &levelL0, "other source")
	otherTarget := createDBTestEntity(t, otherEntry.ID, nil, models.EntityTypeModel, &levelL2, "other target")

	relation, err := testDB.EntityRelations.Create(context.Background(), models.EntityRelation{
		ID:             uuid.New(),
		SourceEntityID: source.ID,
		TargetEntityID: target.ID,
		RelationType:   models.RelationInputTo,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	require.NoError(t, err)
	_, err = testDB.EntityRelations.Create(context.Background(), models.EntityRelation{
		ID:             uuid.New(),
		SourceEntityID: otherSource.ID,
		TargetEntityID: otherTarget.ID,
		RelationType:   models.RelationInputTo,
		CreatedAt:      now.Add(time.Second),
		UpdatedAt:      now.Add(time.Second),
	})
	require.NoError(t, err)

	// when
	got, err := testDB.EntityRelations.List(context.Background(), entry.ID)

	// then
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, relation.ID, got[0].ID)
	assert.Equal(t, source.ID, got[0].SourceEntityID)
	assert.Equal(t, target.ID, got[0].TargetEntityID)
	assert.Equal(t, models.RelationInputTo, got[0].RelationType)
}
