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
	affiliation := "Department of Chemistry, Boston University"
	row := entityRow{
		ID:           entityID,
		EntryID:      entryID,
		ExperimentID: uuid.NullUUID{},
		Type:         string(models.EntityTypeModel),
		Level: sql.NullString{
			String: string(level),
			Valid:  true,
		},
		Name: "qFit model",
		Payload: []byte(
			`{"file_url":"s3://dynamic-pdb/models/qfit.cif","authors":["Hendrickson, W.A.","Teeter, M.M."],"affiliation":"Department of Chemistry, Boston University"}`,
		),
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
	assert.Equal(t, []string{"Hendrickson, W.A.", "Teeter, M.M."}, payload.Authors)
	require.NotNil(t, payload.Affiliation)
	assert.Equal(t, affiliation, *payload.Affiliation)
}

func Test_should_decode_data_payload_when_entity_from_row_called(t *testing.T) {
	// given
	entityID := uuid.New()
	entryID := uuid.New()
	now := time.Now().UTC()
	row := entityRow{
		ID:        entityID,
		EntryID:   entryID,
		Type:      string(models.EntityTypeData),
		Name:      "reflections",
		Payload:   []byte(`{"file_url":"s3://dynamic-pdb/data/reflections.mtz"}`),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	entity, err := entityFromRow(&row)

	// then
	require.NoError(t, err)

	payload, err := entity.Data()
	require.NoError(t, err)
	assert.Equal(t, "s3://dynamic-pdb/data/reflections.mtz", payload.FileURL)
}

func Test_should_decode_data_payload_metadata_when_entity_from_row_called(t *testing.T) {
	// given
	entityID := uuid.New()
	entryID := uuid.New()
	now := time.Now().UTC()
	row := entityRow{
		ID:      entityID,
		EntryID: entryID,
		Type:    string(models.EntityTypeData),
		Name:    "fasta",
		Payload: []byte(
			`{"file_url":"https://www.rcsb.org/fasta/entry/5GY3/download","type":"fasta","size":385,"metadata":{"length":310,"chains":1}}`,
		),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	entity, err := entityFromRow(&row)

	// then
	require.NoError(t, err)

	payload, err := entity.Data()
	require.NoError(t, err)
	assert.Equal(t, "https://www.rcsb.org/fasta/entry/5GY3/download", payload.FileURL)
	assert.Equal(t, "fasta", payload.Type)
	require.NotNil(t, payload.Size)
	assert.Equal(t, int64(385), *payload.Size)
	assert.Equal(t, float64(310), payload.Metadata["length"])
	assert.Equal(t, float64(1), payload.Metadata["chains"])
}

func Test_should_decode_program_payload_when_entity_from_row_called(t *testing.T) {
	// given
	entityID := uuid.New()
	entryID := uuid.New()
	now := time.Now().UTC()
	row := entityRow{
		ID:        entityID,
		EntryID:   entryID,
		Type:      string(models.EntityTypeProgram),
		Name:      "qFit",
		Payload:   []byte(`{"name":"qFit","version":"4.0.0","description":"Multiconformer model builder"}`),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// when
	entity, err := entityFromRow(&row)

	// then
	require.NoError(t, err)

	payload, err := entity.Program()
	require.NoError(t, err)
	assert.Equal(t, "qFit", payload.Name)
	assert.Equal(t, "4.0.0", payload.Version)
	assert.Equal(t, "Multiconformer model builder", payload.Description)
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
