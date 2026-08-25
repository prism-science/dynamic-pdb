package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/models"
)

func Test_should_ignore_forwarded_header_when_environment_is_local(t *testing.T) {
	// given
	var clientIP string
	handler := GlobalRateLimitMiddleware("local")(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		clientIP = middleware.GetClientIP(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.1")

	// when
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// then
	assert.Equal(t, "192.0.2.1", clientIP)
}

func Test_should_use_client_added_by_outer_proxy_when_environment_is_production(t *testing.T) {
	// given
	var clientIP string
	handler := GlobalRateLimitMiddleware("production")(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		clientIP = middleware.GetClientIP(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.2:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.1, 10.0.0.1")

	// when
	handler.ServeHTTP(httptest.NewRecorder(), request)

	// then
	assert.Equal(t, "198.51.100.1", clientIP)
}

func Test_should_reject_request_when_global_ip_limit_was_exceeded(t *testing.T) {
	// given
	handler := GlobalRateLimitMiddleware("local")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for range 100 {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNoContent, recorder.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
}

func Test_should_identify_admin_when_user_id_is_in_admin_user_ids(t *testing.T) {
	// given
	adminUserID := uuid.MustParse("8ca59596-c4b4-4f3f-94be-6dd73f76f050")
	otherAdminUserID := uuid.MustParse("5a8ed753-5eb0-483f-b9f8-a8c58b191017")
	server := NewServer(nil, nil, auth.Config{
		AdminUserIDs: []string{
			adminUserID.String(),
			otherAdminUserID.String(),
		},
	}, nil, nil)

	// when
	got := server.isAdmin(&models.User{ID: otherAdminUserID})

	// then
	assert.True(t, got)
}

func Test_should_not_identify_admin_when_user_id_is_not_configured(t *testing.T) {
	// given
	server := NewServer(nil, nil, auth.Config{
		AdminUserIDs: []string{
			"8ca59596-c4b4-4f3f-94be-6dd73f76f050",
		},
	}, nil, nil)

	// when
	got := server.isAdmin(&models.User{ID: uuid.MustParse("5a8ed753-5eb0-483f-b9f8-a8c58b191017")})

	// then
	assert.False(t, got)
}
