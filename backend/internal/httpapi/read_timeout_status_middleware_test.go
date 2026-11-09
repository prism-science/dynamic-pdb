package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_answer_504_when_read_fails_after_its_deadline(t *testing.T) {
	// given
	handler := ReadTimeoutStatusMiddleware()(writeStatusHandler(http.StatusInternalServerError))
	request := requestWithPassedDeadline(t, http.MethodGet)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	errorObject := decodeSingleErrorObject(t, recorder)
	assert.Equal(t, "504", errorObject.Status)
	require.NotNil(t, errorObject.Code)
	assert.Equal(t, readTimeoutErrorCode, *errorObject.Code)
	assert.NotContains(t, recorder.Body.String(), "INTERNAL_ERROR")
	assert.Equal(t, "10", recorder.Header().Get("Retry-After"))
}

func Test_should_keep_500_when_read_fails_before_its_deadline(t *testing.T) {
	// given
	handler := ReadTimeoutStatusMiddleware()(writeStatusHandler(http.StatusInternalServerError))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/entries", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "INTERNAL_ERROR")
}

func Test_should_keep_non_500_status_when_read_ends_after_its_deadline(t *testing.T) {
	// given
	handler := ReadTimeoutStatusMiddleware()(writeStatusHandler(http.StatusNotFound))
	request := requestWithPassedDeadline(t, http.MethodGet)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func Test_should_keep_500_when_write_fails_after_its_deadline(t *testing.T) {
	// given
	handler := ReadTimeoutStatusMiddleware()(writeStatusHandler(http.StatusInternalServerError))
	request := requestWithPassedDeadline(t, http.MethodPost)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func Test_should_keep_500_when_client_cancels_read(t *testing.T) {
	// given
	handler := ReadTimeoutStatusMiddleware()(writeStatusHandler(http.StatusInternalServerError))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/entries", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func Test_should_pass_successful_read_body_through_after_its_deadline(t *testing.T) {
	// given
	handler := ReadTimeoutStatusMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("stored entry"))
	}))
	request := requestWithPassedDeadline(t, http.MethodGet)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "stored entry", recorder.Body.String())
}

func Test_should_log_504_with_route_when_read_times_out_in_request_lifecycle(t *testing.T) {
	// given
	var logs bytes.Buffer
	router := chi.NewRouter()
	router.Use(RequestLifecycleMiddlewares(newJSONTestLogger(&logs), 20*time.Millisecond)...)
	router.Get("/v1/entries/{entry_id}", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/entries/dpdb_abc", nil)
	recorder := httptest.NewRecorder()

	// when
	router.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "ERROR", entry["level"])
	assert.InDelta(t, http.StatusGatewayTimeout, entry["status"], 0)
	assert.Equal(t, "/v1/entries/{entry_id}", entry["route"])
	assert.Equal(t, "/v1/entries/dpdb_abc", entry["path"])
	assert.Equal(t, "context deadline exceeded", entry["context_error"])
	assert.Contains(t, entry, "duration_ms")
}

func Test_should_keep_panic_as_500_when_read_panics_after_its_deadline(t *testing.T) {
	// given
	var logs bytes.Buffer
	router := chi.NewRouter()
	router.Use(RequestLifecycleMiddlewares(newJSONTestLogger(&logs), 20*time.Millisecond)...)
	router.Get("/v1/entries", func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		panic("handler bug")
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/entries", nil)
	recorder := httptest.NewRecorder()

	// when
	router.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "INTERNAL_ERROR")
}

func writeStatusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, status, "INTERNAL_ERROR", "failed to list entries")
	})
}

// requestWithPassedDeadline builds a request whose context deadline has already passed, as
// ReadRequestTimeoutMiddleware leaves it once a slow read runs out of time.
func requestWithPassedDeadline(t *testing.T, method string) *http.Request {
	t.Helper()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	t.Cleanup(cancel)
	return httptest.NewRequestWithContext(ctx, method, "/v1/entries", nil)
}

func decodeSingleErrorObject(t *testing.T, recorder *httptest.ResponseRecorder) ErrorObject {
	t.Helper()

	var document Error
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &document))
	require.Len(t, document.Errors, 1)
	return document.Errors[0]
}
