package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_return_internal_error_and_log_when_handler_panics(t *testing.T) {
	// given
	var logs bytes.Buffer
	handler := middleware.RequestID(RecoverMiddleware(newJSONTestLogger(&logs))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("nil metadata")
	})))
	request := httptest.NewRequest(http.MethodGet, "/v1/entries/dpdb_1", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.JSONEq(
		t,
		`{"errors":[{"status":"500","code":"INTERNAL_ERROR","detail":"internal server error"}]}`,
		recorder.Body.String(),
	)
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "ERROR", entry["level"])
	assert.Equal(t, "nil metadata", entry["panic"])
	assert.Equal(t, "/v1/entries/dpdb_1", entry["path"])
	assert.NotEmpty(t, entry["request_id"])
	assert.Contains(t, entry["stack"], "recover_middleware_test.go")
}

func Test_should_repanic_when_handler_aborts_with_err_abort_handler(t *testing.T) {
	// given
	var logs bytes.Buffer
	handler := RecoverMiddleware(newJSONTestLogger(&logs))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	request := httptest.NewRequest(http.MethodGet, "/v1/entries", nil)

	// when
	serve := func() { handler.ServeHTTP(httptest.NewRecorder(), request) }

	// then
	assert.PanicsWithValue(t, http.ErrAbortHandler, serve)
	assert.Empty(t, logs.String())
}
