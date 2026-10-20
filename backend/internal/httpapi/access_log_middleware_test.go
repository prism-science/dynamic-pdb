package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_log_server_error_at_error_level_with_request_details(t *testing.T) {
	// given
	var logs bytes.Buffer
	handler := middleware.RequestID(AccessLogMiddleware(newJSONTestLogger(&logs))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "try again later")
	})))
	request := httptest.NewRequest(http.MethodGet, "/v1/models?limit=100&offset=5000", nil)
	request.Header.Set("User-Agent", "llm-tool/1.0")
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "ERROR", entry["level"])
	assert.Equal(t, "http request", entry["msg"])
	assert.Equal(t, http.MethodGet, entry["method"])
	assert.Equal(t, "/v1/models", entry["path"])
	assert.Equal(t, "limit=100&offset=5000", entry["query"])
	assert.InDelta(t, http.StatusServiceUnavailable, entry["status"], 0)
	assert.Equal(t, "llm-tool/1.0", entry["user_agent"])
	assert.Contains(t, entry, "duration_ms")
	require.NotEmpty(t, entry["request_id"])
	assert.Equal(t, entry["request_id"], recorder.Header().Get(middleware.RequestIDHeader))
	assert.NotContains(t, entry, "context_error")
}

func Test_should_log_successful_request_at_info_level(t *testing.T) {
	// given
	var logs bytes.Buffer
	handler := middleware.RequestID(AccessLogMiddleware(newJSONTestLogger(&logs))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})))
	request := httptest.NewRequest(http.MethodGet, "/v1/entries", nil)

	// when
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// then
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "INFO", entry["level"])
	assert.InDelta(t, http.StatusOK, entry["status"], 0)
	assert.InDelta(t, len("ok"), entry["bytes"], 0)
}

func Test_should_log_context_error_when_read_request_times_out(t *testing.T) {
	// given
	var logs bytes.Buffer
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entries")
	})
	handler := middleware.RequestID(
		ReadRequestTimeoutMiddleware(20 * time.Millisecond)(
			AccessLogMiddleware(newJSONTestLogger(&logs))(slowHandler),
		),
	)
	request := httptest.NewRequest(http.MethodGet, "/v1/entries", nil)

	// when
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// then
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "ERROR", entry["level"])
	assert.InDelta(t, http.StatusInternalServerError, entry["status"], 0)
	assert.Equal(t, "context deadline exceeded", entry["context_error"])
}

func newJSONTestLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func decodeSingleLogEntry(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n"))
	require.Len(t, lines, 1)
	var entry map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &entry))
	return entry
}
