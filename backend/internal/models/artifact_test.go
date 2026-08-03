package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_identify_fasta_artifact_when_format_is_fasta(t *testing.T) {
	// given
	format := "fasta"
	artifact := Artifact{Format: &format}

	// when
	isFASTA := artifact.IsFASTA()

	// then
	assert.True(t, isFASTA)
}

func Test_should_normalize_fasta_records_when_artifact_records_requested(t *testing.T) {
	// given
	format := "fasta"
	artifact := Artifact{
		Format: &format,
		Metadata: FASTAMetadata{
			Records: []FASTARecord{
				{
					Header:   "  4HHB_1|Chains A,C  ",
					Sequence: "vlsp ad\nkt",
				},
			},
		},
	}

	// when
	records, err := artifact.FASTARecords()

	// then
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "4HHB_1|Chains A,C", records[0].Header)
	assert.Equal(t, "VLSPADKT", records[0].Sequence)
}

func Test_should_return_error_when_fasta_record_sequence_is_empty(t *testing.T) {
	// given
	format := "fasta"
	artifact := Artifact{
		Format: &format,
		Metadata: FASTAMetadata{
			Records: []FASTARecord{
				{
					Header:   "empty",
					Sequence: " \n ",
				},
			},
		},
	}

	// when
	_, err := artifact.FASTARecords()

	// then
	require.ErrorIs(t, err, ErrInvalidFASTARecords)
}

func Test_should_return_unexpected_format_error_when_fasta_called_on_non_fasta_artifact(t *testing.T) {
	// given
	format := "cif"
	artifact := Artifact{Format: &format}

	// when
	_, err := artifact.FASTA()

	// then
	require.ErrorIs(t, err, ErrUnexpectedArtifactFormat)
}

func Test_should_return_invalid_metadata_error_when_fasta_metadata_has_wrong_type(t *testing.T) {
	// given
	format := "fasta"
	artifact := Artifact{
		Format:   &format,
		Metadata: map[string]any{"records": []any{}},
	}

	// when
	_, err := artifact.FASTA()

	// then
	require.ErrorIs(t, err, ErrInvalidArtifactMetadata)
}
