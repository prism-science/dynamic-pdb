package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

func Test_should_reject_archive_version_when_apply_fails_schema_validation(t *testing.T) {
	// given
	source := archiveRevisionSource{pdbID: "1ABC", version: models.ArchiveVersion{Ordinal: 5, Version: "2.0"}}
	fieldErrors := []validation.FieldError{
		{Pointer: "/resolution", Code: validation.CodeOutOfRange, Detail: "must be greater than 0"},
	}
	err := fmt.Errorf("validate entry update: %w", &validation.Error{Fields: fieldErrors})

	// when
	outcome, outcomeErr := outcomeOfFailedApply(err, source)

	// then
	require.NoError(t, outcomeErr)
	require.NotNil(t, outcome.rejected)
	assert.Equal(t, "1ABC", outcome.rejected.pdbID)
	assert.Equal(t, source.version, outcome.rejected.version)
	assert.Equal(t, fieldErrors, outcome.rejected.fieldErrors)
	assert.Nil(t, outcome.archiveVersion, "a rejected version is not recorded")
}

func Test_should_fail_run_when_apply_fails_for_another_reason(t *testing.T) {
	// given
	source := archiveRevisionSource{pdbID: "1ABC", version: models.ArchiveVersion{Ordinal: 5, Version: "2.0"}}
	err := errors.New("read archive mmCIF: connection reset")

	// when
	outcome, outcomeErr := outcomeOfFailedApply(err, source)

	// then
	require.ErrorIs(t, outcomeErr, err)
	assert.Equal(t, dataSyncOutcome{}, outcome)
}

func Test_should_name_model_or_entry_as_review_object_when_job_is_described(t *testing.T) {
	// given
	modelJob := models.DataSyncJob{EntryID: "dpdb_entry", ModelID: new("dpdb_entry_m_001")}
	entryJob := models.DataSyncJob{EntryID: "dpdb_entry"}

	// when
	modelType, modelID := syncReviewObject(modelJob)
	entryType, entryID := syncReviewObject(entryJob)

	// then
	assert.Equal(t, []string{"model", "dpdb_entry_m_001"}, []string{modelType, modelID})
	assert.Equal(t, []string{"entry", "dpdb_entry"}, []string{entryType, entryID})
}

func Test_should_store_version_and_every_field_error_when_review_details_are_built(t *testing.T) {
	// given
	revisionDate := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	rejected := rejectedArchiveVersion{
		pdbID:   "1ABC",
		version: models.ArchiveVersion{Ordinal: 5, Version: "2.0", RevisionDate: &revisionDate},
		fieldErrors: []validation.FieldError{
			{Pointer: "/resolution", Code: validation.CodeOutOfRange, Detail: "must be greater than 0"},
			{Pointer: "/polymer_entities/0/uniprot_mappings/0/release", Code: validation.CodeRequired, Detail: "is required"},
		},
	}

	// when
	details, err := json.Marshal(syncValidationFailureOf(rejected))

	// then
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"pdb_id": "1ABC",
		"archive_version": "2.0",
		"archive_revision_date": "2026-10-28T00:00:00Z",
		"errors": [
			{"pointer": "/resolution", "code": "OUT_OF_RANGE", "detail": "must be greater than 0"},
			{"pointer": "/polymer_entities/0/uniprot_mappings/0/release", "code": "REQUIRED", "detail": "is required"}
		]
	}`, string(details))
}
