package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_cancel_get_request_when_read_timeout_elapses(t *testing.T) {
	// given
	var contextErr error
	handler := ReadRequestTimeoutMiddleware(20 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			contextErr = r.Context().Err()
		case <-time.After(5 * time.Second):
		}
	}))
	request := httptest.NewRequest(http.MethodGet, "/v1/entries", nil)
	started := time.Now()

	// when
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// then
	require.ErrorIs(t, contextErr, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), time.Second)
}

func Test_should_not_set_deadline_when_request_is_a_write(t *testing.T) {
	// given
	hasDeadline := true
	handler := ReadRequestTimeoutMiddleware(20 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, hasDeadline = r.Context().Deadline()
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/files/complete", nil)

	// when
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// then
	assert.False(t, hasDeadline)
}

func Test_should_panic_when_read_request_timeout_is_not_positive(t *testing.T) {
	// when
	create := func() { ReadRequestTimeoutMiddleware(0) }

	// then
	assert.PanicsWithValue(t, "read request timeout must be positive", create)
}
