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
