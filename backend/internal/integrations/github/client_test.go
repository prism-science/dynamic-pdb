package github_test

// `_, _ = w.Write(...)` in the httptest fakes below is intentional: a Write
// failure can only happen if the client disconnected, which surfaces as the
// client-side test assertion failing.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/integrations/github"
)

type rewriteTransport struct {
	target *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.target.Scheme
	req.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newClientForServer(t *testing.T, server *httptest.Server) *github.RemoteClient {
	t.Helper()
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	return github.NewClient(
		github.WithHTTPClient(&http.Client{
			Transport: &rewriteTransport{target: target},
		}),
		github.WithOAuthClientID("client-id"),
		github.WithOAuthClientSecret("client-secret"),
	)
}

func Test_should_return_user_when_github_responds_with_200(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 42,
			"login": "octocat",
			"email": "octo@example.com",
			"name": "Octo Cat",
			"avatar_url": "https://example.com/octo.png"
		}`))
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	user, err := client.GetUser(context.Background(), "test-token")

	// then
	require.NoError(t, err)
	assert.Equal(t, int64(42), user.ID)
	assert.Equal(t, "octocat", user.Login)
	assert.Equal(t, "octo@example.com", user.Email)
	assert.Equal(t, "Octo Cat", user.Name)
	assert.Equal(t, "https://example.com/octo.png", user.AvatarURL)
}

func Test_should_return_err_invalid_token_when_get_user_responds_with_401(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	_, err := client.GetUser(context.Background(), "bad-token")

	// then
	assert.ErrorIs(t, err, github.ErrInvalidToken)
}

func Test_should_return_generic_error_when_get_user_responds_with_500(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	_, err := client.GetUser(context.Background(), "any")

	// then
	require.Error(t, err)
	assert.NotErrorIs(t, err, github.ErrInvalidToken)
}

func Test_should_return_orgs_when_list_orgs_responds_with_200(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/orgs", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id": 1, "login": "Astera-org"},
			{"id": 2, "login": "diff-use"}
		]`))
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	orgs, err := client.ListOrgs(context.Background(), "test-token")

	// then
	require.NoError(t, err)
	assert.Equal(t, []github.Organization{
		{ID: 1, Login: "Astera-org"},
		{ID: 2, Login: "diff-use"},
	}, orgs)
}

func Test_should_follow_next_link_when_list_orgs_is_paginated(t *testing.T) {
	// given
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/user/orgs", r.URL.Path)
		assert.Equal(t, "100", r.URL.Query().Get("per_page"))
		w.Header().Set("Content-Type", "application/json")

		requests++
		switch r.URL.Query().Get("page") {
		case "":
			w.Header().Set("Link", `<https://api.github.com/user/orgs?per_page=100&page=2>; rel="next"`)
			_, _ = w.Write([]byte(`[{"id": 1, "login": "org-one"}]`))
		case "2":
			_, _ = w.Write([]byte(`[{"id": 2, "login": "org-two"}]`))
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	orgs, err := client.ListOrgs(context.Background(), "test-token")

	// then
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	assert.Equal(t, []github.Organization{
		{ID: 1, Login: "org-one"},
		{ID: 2, Login: "org-two"},
	}, orgs)
}

func Test_should_return_empty_list_when_user_has_no_orgs(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	orgs, err := client.ListOrgs(context.Background(), "test-token")

	// then
	require.NoError(t, err)
	assert.Empty(t, orgs)
}

func Test_should_return_err_invalid_token_when_list_orgs_responds_with_401(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	_, err := client.ListOrgs(context.Background(), "bad-token")

	// then
	assert.ErrorIs(t, err, github.ErrInvalidToken)
}

func Test_should_return_access_token_when_exchange_code_responds_with_200(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/login/oauth/access_token", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client-id", r.PostForm.Get("client_id"))
		assert.Equal(t, "client-secret", r.PostForm.Get("client_secret"))
		assert.Equal(t, "code-123", r.PostForm.Get("code"))
		assert.Equal(t, "https://example.com/auth/github/callback", r.PostForm.Get("redirect_uri"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"gh-access-token"}`))
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	token, err := client.ExchangeCode(
		context.Background(),
		"code-123",
		"https://example.com/auth/github/callback",
	)

	// then
	require.NoError(t, err)
	assert.Equal(t, "gh-access-token", token)
}

func Test_should_return_err_invalid_code_when_exchange_code_responds_with_bad_verification_code(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`))
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	_, err := client.ExchangeCode(context.Background(), "bad-code", "https://example.com/callback")

	// then
	assert.ErrorIs(t, err, github.ErrInvalidCode)
}

func Test_should_return_err_incorrect_client_credentials_when_exchange_code_responds_with_incorrect_client_credentials(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"incorrect_client_credentials","error_description":"The client_id and/or client_secret passed are incorrect."}`))
	}))
	defer server.Close()
	client := newClientForServer(t, server)

	// when
	_, err := client.ExchangeCode(context.Background(), "code-123", "https://example.com/callback")

	// then
	assert.ErrorIs(t, err, github.ErrIncorrectClientCredentials)
}
