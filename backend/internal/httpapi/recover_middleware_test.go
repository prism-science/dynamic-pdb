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
	assert.Equal(t, false, entry["response_started"])
	assert.NotEmpty(t, entry["request_id"])
	assert.Contains(t, entry["stack"], "recover_middleware_test.go")
}

func Test_should_abort_and_log_when_handler_panics_after_response_started(t *testing.T) {
	cases := map[string]struct {
		startResponse func(http.ResponseWriter)
		expectedBody  string
	}{
		"status written": {
			startResponse: func(w http.ResponseWriter) { w.WriteHeader(http.StatusOK) },
			expectedBody:  "",
		},
		"partial body written": {
			startResponse: func(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"data":[`)) },
			expectedBody:  `{"data":[`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			var logs bytes.Buffer
			handler := RecoverMiddleware(newJSONTestLogger(&logs))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tc.startResponse(w)
				panic("nil metadata")
			}))
			request := httptest.NewRequest(http.MethodGet, "/v1/entries", nil)
			recorder := httptest.NewRecorder()

			// when
			serve := func() { handler.ServeHTTP(recorder, request) }

			// then
			assert.PanicsWithValue(t, http.ErrAbortHandler, serve)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, tc.expectedBody, recorder.Body.String(), "the error JSON must not be appended")
			entry := decodeSingleLogEntry(t, &logs)
			assert.Equal(t, "ERROR", entry["level"])
			assert.Equal(t, "nil metadata", entry["panic"])
			assert.Equal(t, true, entry["response_started"])
		})
	}
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
