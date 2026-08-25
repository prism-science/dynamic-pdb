package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_reject_request_when_declared_body_exceeds_limit(t *testing.T) {
	// given
	nextCalled := false
	handler := RequestBodyLimitMiddleware(4)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.False(t, nextCalled)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.JSONEq(
		t,
		`{"code":"REQUEST_BODY_TOO_LARGE","message":"request body exceeds 1 MiB limit"}`,
		recorder.Body.String(),
	)
}

func Test_should_reject_chunked_request_when_body_exceeds_limit(t *testing.T) {
	// given
	nextCalled := false
	handler := RequestBodyLimitMiddleware(4)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	request.ContentLength = -1
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.False(t, nextCalled)
	assert.Contains(t, recorder.Body.String(), `"code":"REQUEST_BODY_TOO_LARGE"`)
}

func Test_should_forward_request_when_body_is_within_limit(t *testing.T) {
	// given
	var receivedBody string
	handler := RequestBodyLimitMiddleware(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("1234"))
	request.ContentLength = -1
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusNoContent, recorder.Code)
	assert.Equal(t, "1234", receivedBody)
}
