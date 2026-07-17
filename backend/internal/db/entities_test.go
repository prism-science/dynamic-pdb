package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_encode_model_payload_when_marshal_entity_payload_called(t *testing.T) {
	// given
	payload := models.ModelPayload{
		FileURL: "s3://dynamic-pdb/models/qfit.cif",
	}

	// when
	data, err := marshalEntityPayload(payload)

	// then
	require.NoError(t, err)
	assert.JSONEq(t, `{"file_url":"s3://dynamic-pdb/models/qfit.cif"}`, data)
}

func Test_should_decode_model_payload_when_entity_from_row_called(t *testing.T) {
	// given
	entityID := uuid.New()
	entryID := uuid.New()
	level := models.EntityLevelL2
	now := time.Now().UTC()
	row := entityRow{
		ID:           entityID,
		EntryID:      entryID,
		ExperimentID: uuid.NullUUID{},
		Type:         string(models.EntityTypeModel),
		Level: sql.NullString{
			String: string(level),
			Valid:  true,
		},
		Name:      "qFit model",
		Payload:   []byte(`{"file_url":"s3://dynamic-pdb/models/qfit.cif"}`),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	entity, err := entityFromRow(&row)

	// then
	require.NoError(t, err)
	assert.Equal(t, entityID, entity.ID)
	assert.Equal(t, entryID, entity.EntryID)
	require.NotNil(t, entity.Level)
	assert.Equal(t, level, *entity.Level)

	payload, err := entity.Model()
	require.NoError(t, err)
	assert.Equal(t, "s3://dynamic-pdb/models/qfit.cif", payload.FileURL)
}

func Test_should_build_query_with_optional_filters_when_entity_list_query_called(t *testing.T) {
	// given
	entryID := uuid.New()
	experimentID := uuid.New()
	limit := 50
	offset := 100
	filters := EntityFilters{
		EntryID:      &entryID,
		ExperimentID: &experimentID,
		Types:        []models.EntityType{models.EntityTypeModel},
		Levels:       []models.EntityLevel{models.EntityLevelL2},
		Limit:        &limit,
		Offset:       &offset,
	}

	// when
	query, args, err := entityListQuery(filters)

	// then
	require.NoError(t, err)
	assert.Contains(t, query, "entry_id = :entry_id")
	assert.Contains(t, query, "experiment_id = :experiment_id")
	assert.Contains(t, query, "type = any(cast(:types as text[]))")
	assert.Contains(t, query, "level = any(cast(:levels as text[]))")
	assert.Contains(t, query, "limit :limit")
	assert.Contains(t, query, "offset :offset")
	assert.Equal(t, entryID, args["entry_id"])
	assert.Equal(t, experimentID, args["experiment_id"])
	assert.Contains(t, args, "types")
	assert.Contains(t, args, "levels")
	assert.Equal(t, limit, args["limit"])
	assert.Equal(t, offset, args["offset"])
}

func Test_should_return_error_when_entity_list_query_called_with_negative_limit(t *testing.T) {
	// given
	limit := -1
	filters := EntityFilters{
		Limit: &limit,
	}

	// when
	_, _, err := entityListQuery(filters)

	// then
	require.Error(t, err)
}
