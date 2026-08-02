package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_return_model_payload_when_model_called_on_model_entity(t *testing.T) {
	// given
	entity := Entity{
		Type: EntityTypeModel,
		Payload: ModelPayload{
			FileURL: "s3://dynamic-pdb/models/qfit.cif",
		},
	}

	// when
	payload, err := entity.Model()

	// then
	require.NoError(t, err)
	assert.Equal(t, "s3://dynamic-pdb/models/qfit.cif", payload.FileURL)
}

func Test_should_return_metrics_payload_when_metrics_called_on_metrics_entity(t *testing.T) {
	// given
	rFree := 0.2342
	entity := Entity{
		Type: EntityTypeMetrics,
		Payload: &MetricsPayload{
			RFree: &rFree,
		},
	}

	// when
	payload, err := entity.Metrics()

	// then
	require.NoError(t, err)
	require.NotNil(t, payload.RFree)
	assert.Equal(t, 0.2342, *payload.RFree)
}

func Test_should_return_data_payload_when_data_called_on_data_entity(t *testing.T) {
	// given
	entity := Entity{
		Type: EntityTypeData,
		Payload: DataPayload{
			FileURL: "s3://dynamic-pdb/data/reflections.mtz",
		},
	}

	// when
	payload, err := entity.Data()

	// then
	require.NoError(t, err)
	assert.Equal(t, "s3://dynamic-pdb/data/reflections.mtz", payload.FileURL)
}

func Test_should_identify_only_fasta_data_entities_when_is_fasta_called(t *testing.T) {
	// given
	fasta := Entity{
		Type:    EntityTypeData,
		Payload: DataPayload{Type: "fasta"},
	}
	otherData := Entity{
		Type:    EntityTypeData,
		Payload: DataPayload{Type: "density-map"},
	}
	model := Entity{
		Type:    EntityTypeModel,
		Payload: ModelPayload{},
	}

	// when
	fastaResult := fasta.IsFASTA()
	otherDataResult := otherData.IsFASTA()
	modelResult := model.IsFASTA()

	// then
	assert.True(t, fastaResult)
	assert.False(t, otherDataResult)
	assert.False(t, modelResult)
}

func Test_should_normalize_fasta_records_when_entity_records_requested(t *testing.T) {
	// given
	entity := Entity{
		Type: EntityTypeData,
		Payload: DataPayload{
			Type: "fasta",
			Metadata: map[string]any{
				"records": []map[string]any{
					{
						"header":   "  4HHB_1|Chains A,C  ",
						"sequence": "vlsp ad\nkt",
					},
				},
			},
		},
	}

	// when
	records, err := entity.FASTARecords()

	// then
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "4HHB_1|Chains A,C", records[0].Header)
	assert.Equal(t, "VLSPADKT", records[0].Sequence)
}

func Test_should_return_error_when_fasta_record_sequence_is_empty(t *testing.T) {
	// given
	entity := Entity{
		Type: EntityTypeData,
		Payload: DataPayload{
			Type: "fasta",
			Metadata: map[string]any{
				"records": []map[string]any{
					{
						"header":   "empty",
						"sequence": " \n ",
					},
				},
			},
		},
	}

	// when
	_, err := entity.FASTARecords()

	// then
	require.ErrorIs(t, err, ErrInvalidFASTARecords)
}

func Test_should_return_unexpected_type_error_when_fasta_records_called_on_model_entity(t *testing.T) {
	// given
	entity := Entity{
		Type:    EntityTypeModel,
		Payload: ModelPayload{},
	}

	// when
	_, err := entity.FASTARecords()

	// then
	require.ErrorIs(t, err, ErrUnexpectedEntityType)
}

func Test_should_return_invalid_payload_error_when_fasta_records_called_on_non_fasta_data_entity(t *testing.T) {
	// given
	entity := Entity{
		Type: EntityTypeData,
		Payload: DataPayload{
			Type: "density-map",
		},
	}

	// when
	_, err := entity.FASTARecords()

	// then
	require.ErrorIs(t, err, ErrInvalidEntityPayload)
}

func Test_should_return_program_payload_when_program_called_on_program_entity(t *testing.T) {
	// given
	entity := Entity{
		Type: EntityTypeProgram,
		Payload: &ProgramPayload{
			Name:        "qFit",
			Version:     "4.0.0",
			Description: "Multiconformer model builder",
		},
	}

	// when
	payload, err := entity.Program()

	// then
	require.NoError(t, err)
	assert.Equal(t, "qFit", payload.Name)
	assert.Equal(t, "4.0.0", payload.Version)
	assert.Equal(t, "Multiconformer model builder", payload.Description)
}

func Test_should_return_unexpected_type_error_when_model_called_on_metrics_entity(t *testing.T) {
	// given
	entity := Entity{
		Type:    EntityTypeMetrics,
		Payload: MetricsPayload{},
	}

	// when
	_, err := entity.Model()

	// then
	require.ErrorIs(t, err, ErrUnexpectedEntityType)
}

func Test_should_return_invalid_payload_error_when_model_payload_has_wrong_type(t *testing.T) {
	// given
	entity := Entity{
		Type:    EntityTypeModel,
		Payload: MetricsPayload{},
	}

	// when
	_, err := entity.Model()

	// then
	require.ErrorIs(t, err, ErrInvalidEntityPayload)
}
