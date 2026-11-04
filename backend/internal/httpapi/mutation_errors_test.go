package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_answer_422_naming_the_check_when_database_rejects_a_value(t *testing.T) {
	// given
	server := &Server{}
	recorder := httptest.NewRecorder()
	err := fmt.Errorf("create run artifacts: %w", &pq.Error{Code: "23514", Constraint: "run_artifacts_direction_chk"})

	// when
	handled := server.writeMutationError(context.Background(), recorder, err, "create model")

	// then
	require.True(t, handled)
	assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	var body Error
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Len(t, body.Errors, 1)
	errorObject := body.Errors[0]
	assert.Equal(t, "422", errorObject.Status)
	require.NotNil(t, errorObject.Code)
	assert.Equal(t, "CONSTRAINT_VIOLATION", *errorObject.Code)
	require.NotNil(t, errorObject.Detail)
	assert.Equal(t, "the change breaks the database check run_artifacts_direction_chk", *errorObject.Detail)
	assert.Nil(t, errorObject.Source, "the constraint does not know which field held the value")
}

func Test_should_answer_500_when_database_error_is_not_a_check_violation(t *testing.T) {
	// given
	server := &Server{}
	recorder := httptest.NewRecorder()
	err := errors.Join(errors.New("insert run"), &pq.Error{Code: "23502", Column: "direction"})

	// when
	handled := server.writeMutationError(context.Background(), recorder, err, "create model")

	// then
	require.True(t, handled)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}
