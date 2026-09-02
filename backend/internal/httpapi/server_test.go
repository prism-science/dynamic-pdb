package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func Test_should_return_json_api_media_type_when_liveness_probe_is_called(t *testing.T) {
	// given
	handler := Handler((*Server)(nil))
	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"meta":{"status":"ok"}}`, recorder.Body.String())
}

func Test_should_write_json_api_media_type_when_error_response_is_written(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()

	// when
	writeError(recorder, http.StatusNotFound, "NOT_FOUND", "entry not found")

	// then
	require.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	assert.JSONEq(
		t,
		`{"errors":[{"status":"404","code":"NOT_FOUND","detail":"entry not found"}]}`,
		recorder.Body.String(),
	)
}

func Test_should_return_json_error_when_generated_path_parameter_binding_fails(t *testing.T) {
	// given
	handler := HandlerWithOptions((*Server)(nil), ChiServerOptions{
		ErrorHandlerFunc: RouteErrorHandler,
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/entries/not-a-uuid", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	var body Error
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&body))
	require.Len(t, body.Errors, 1)
	assert.Equal(t, "400", body.Errors[0].Status)
	require.NotNil(t, body.Errors[0].Code)
	require.NotNil(t, body.Errors[0].Detail)
	assert.Equal(t, "BAD_REQUEST", *body.Errors[0].Code)
	assert.Contains(t, *body.Errors[0].Detail, "entry_id")
}

func Test_should_return_json_error_when_generated_query_parameter_binding_fails(t *testing.T) {
	// given
	handler := HandlerWithOptions((*Server)(nil), ChiServerOptions{
		ErrorHandlerFunc: RouteErrorHandler,
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/entries?limit=bad", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	var body Error
	require.NoError(t, json.NewDecoder(recorder.Body).Decode(&body))
	require.Len(t, body.Errors, 1)
	assert.Equal(t, "400", body.Errors[0].Status)
	require.NotNil(t, body.Errors[0].Code)
	require.NotNil(t, body.Errors[0].Detail)
	assert.Equal(t, "BAD_REQUEST", *body.Errors[0].Code)
	assert.Contains(t, *body.Errors[0].Detail, "limit")
}

func Test_should_reject_request_body_when_content_type_is_not_json_api(t *testing.T) {
	// given
	handler := MediaTypeMiddleware()(Handler((*Server)(nil)))
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/github/code/exchange", strings.NewReader(`{}`))
	request.Header.Set("Accept", JSONAPIMediaType)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	assert.JSONEq(
		t,
		`{"errors":[{"status":"415","code":"UNSUPPORTED_MEDIA_TYPE","detail":"request Content-Type must be application/vnd.api+json"}]}`,
		recorder.Body.String(),
	)
}

func Test_should_reject_request_body_when_content_type_is_missing(t *testing.T) {
	// given
	handler := MediaTypeMiddleware()(Handler((*Server)(nil)))
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/github/code/exchange", strings.NewReader(`{}`))
	request.Header.Set("Accept", JSONAPIMediaType)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
}

func Test_should_reject_request_body_when_json_api_content_type_has_unsupported_parameter(t *testing.T) {
	// given
	handler := MediaTypeMiddleware()(Handler((*Server)(nil)))
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/github/code/exchange", strings.NewReader(`{}`))
	request.Header.Set("Accept", JSONAPIMediaType)
	request.Header.Set("Content-Type", JSONAPIMediaType+"; charset=utf-8")
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
}

func Test_should_reject_request_when_accept_header_does_not_allow_json_api(t *testing.T) {
	// given
	handler := MediaTypeMiddleware()(Handler((*Server)(nil)))
	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	request.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusNotAcceptable, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	assert.JSONEq(
		t,
		`{"errors":[{"status":"406","code":"NOT_ACCEPTABLE","detail":"request Accept header must allow application/vnd.api+json"}]}`,
		recorder.Body.String(),
	)
}

func Test_should_allow_request_when_accept_header_allows_any_media_type(t *testing.T) {
	// given
	handler := MediaTypeMiddleware()(Handler((*Server)(nil)))
	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	request.Header.Set("Accept", "*/*")
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
}

func Test_should_allow_request_when_accept_header_is_missing(t *testing.T) {
	// given
	handler := MediaTypeMiddleware()(Handler((*Server)(nil)))
	request := httptest.NewRequest(http.MethodGet, "/livez", nil)
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
}

func Test_should_check_reviewer_role_when_review_access_required(t *testing.T) {
	// given
	userID := uuid.New()
	authorizer := &authorizerStub{hasRoleResult: true}
	server := &Server{authorizer: authorizer}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(WithUser(request.Context(), &models.User{ID: userID}))
	recorder := httptest.NewRecorder()

	// when
	allowed := server.requireReviewer(recorder, request)

	// then
	assert.True(t, allowed)
	assert.Equal(t, userID, authorizer.userID)
	assert.Equal(t, models.RoleKeyReviewer, authorizer.role)
}

func Test_should_check_matching_permission_when_entry_revision_state_updated(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		permission models.PermissionKey
	}{
		{name: "approve", state: "active", permission: models.PermissionKeyRevisionsApprove},
		{name: "reject", state: "rejected", permission: models.PermissionKeyRevisionsReject},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			userID := uuid.New()
			authorizer := &authorizerStub{}
			server := &Server{authorizer: authorizer}
			request := httptest.NewRequest(
				http.MethodPatch,
				"/",
				strings.NewReader(`{"state":"`+test.state+`"}`),
			)
			request = request.WithContext(WithUser(request.Context(), &models.User{ID: userID}))
			recorder := httptest.NewRecorder()

			// when
			server.UpdateEntryRevisionState(recorder, request, uuid.New(), uuid.New())

			// then
			assert.Equal(t, http.StatusForbidden, recorder.Code)
			assert.Equal(t, userID, authorizer.userID)
			assert.Equal(t, test.permission, authorizer.permission)
		})
	}
}

type authorizerStub struct {
	hasRoleResult bool
	canResult     bool
	userID        uuid.UUID
	role          models.RoleKey
	permission    models.PermissionKey
}

func (s *authorizerStub) HasRole(
	_ context.Context,
	userID uuid.UUID,
	role models.RoleKey,
) (bool, error) {
	s.userID = userID
	s.role = role
	return s.hasRoleResult, nil
}

func (s *authorizerStub) Can(
	_ context.Context,
	userID uuid.UUID,
	permission models.PermissionKey,
) (bool, error) {
	s.userID = userID
	s.permission = permission
	return s.canResult, nil
}
