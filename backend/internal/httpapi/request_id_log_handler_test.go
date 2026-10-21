package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_add_request_id_when_context_carries_one(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := newRequestIDJSONTestLogger(&logs)
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "host/abc-000001")

	// when
	logger.ErrorContext(ctx, "list entries failed", "err", "boom")

	// then
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "host/abc-000001", entry["request_id"])
	assert.Equal(t, "boom", entry["err"])
}

func Test_should_omit_request_id_when_context_has_none(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := newRequestIDJSONTestLogger(&logs)

	// when
	logger.ErrorContext(context.Background(), "data sync failed")

	// then
	entry := decodeSingleLogEntry(t, &logs)
	assert.NotContains(t, entry, "request_id")
}

func Test_should_not_repeat_request_id_when_record_already_carries_one(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := newRequestIDJSONTestLogger(&logs)
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "host/abc-000001")

	// when
	logger.InfoContext(ctx, "http request", "request_id", "host/abc-000001")

	// then
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "host/abc-000001", entry["request_id"])
	assert.Equal(t, 1, bytes.Count(logs.Bytes(), []byte(`"request_id"`)))
}

func Test_should_keep_adding_request_id_when_logger_has_extra_attrs(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := newRequestIDJSONTestLogger(&logs).With("component", "httpapi")
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "host/abc-000001")

	// when
	logger.ErrorContext(ctx, "list entries failed")

	// then
	entry := decodeSingleLogEntry(t, &logs)
	assert.Equal(t, "host/abc-000001", entry["request_id"])
	assert.Equal(t, "httpapi", entry["component"])
}

func Test_should_add_request_id_inside_group_when_logger_has_group(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := newRequestIDJSONTestLogger(&logs).WithGroup("upload")
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "host/abc-000001")

	// when
	logger.ErrorContext(ctx, "complete file upload failed")

	// then
	entry := decodeSingleLogEntry(t, &logs)
	group, ok := entry["upload"].(map[string]any)
	require.True(t, ok, "expected an upload group, got %v", entry)
	assert.Equal(t, "host/abc-000001", group["request_id"])
}

func Test_should_share_request_id_between_handler_error_and_access_log_when_request_fails(t *testing.T) {
	// given
	var logs bytes.Buffer
	logger := newRequestIDJSONTestLogger(&logs)
	useDefaultLoggerForTest(t, logger)
	server := &Server{}
	failingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.writeMutationError(r.Context(), w, errors.New("connection reset"), "create entry")
	})
	handler := middleware.RequestID(AccessLogMiddleware(logger)(failingHandler))
	request := httptest.NewRequest(http.MethodPost, "/v1/entries", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	entries := decodeLogEntries(t, &logs)
	require.Len(t, entries, 2)
	handlerError, accessLog := entries[0], entries[1]
	assert.Equal(t, "create entry failed", handlerError["msg"])
	assert.Equal(t, "http request", accessLog["msg"])
	assert.InDelta(t, http.StatusInternalServerError, accessLog["status"], 0)
	require.NotEmpty(t, accessLog["request_id"])
	assert.Equal(t, accessLog["request_id"], handlerError["request_id"])
	assert.Equal(t, accessLog["request_id"], recorder.Header().Get(middleware.RequestIDHeader))
}

func newRequestIDJSONTestLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(NewRequestIDLogHandler(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug})))
}

// useDefaultLoggerForTest also restores the log package, which slog.SetDefault redirects.
func useDefaultLoggerForTest(t *testing.T, logger *slog.Logger) {
	t.Helper()

	previousLogger := slog.Default()
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})
}

func decodeLogEntries(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()

	var entries []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(line, &entry))
		entries = append(entries, entry)
	}
	return entries
}
