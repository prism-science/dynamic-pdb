package pdbapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_exchange_github_token_for_dynamic_pdb_token(t *testing.T) {
	// given
	expiresAt := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "/v1/auth/github/exchange", request.URL.Path)
		var body struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			assert.NoError(t, err)
			return
		}
		assert.Equal(t, "github-token", body.AccessToken)
		if err := json.NewEncoder(response).Encode(TokenResponse{
			TokenType:   "Bearer",
			AccessToken: "jwt-token",
			ExpiresAt:   expiresAt,
			Name:        "Octo Cat",
			Email:       "octocat@example.test",
			Login:       "octocat",
		}); err != nil {
			assert.NoError(t, err)
		}
	}))
	defer server.Close()

	// when
	token, err := NewAuthClient(server.URL).ExchangeGitHubToken(context.Background(), "github-token")

	// then
	require.NoError(t, err)
	assert.Equal(t, "jwt-token", token.AccessToken)
	assert.Equal(t, "octocat", token.Login)
	assert.Equal(t, expiresAt, token.ExpiresAt)
}

func Test_should_return_unauthorized_when_backend_rejects_github_account(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusForbidden)
		if err := json.NewEncoder(response).Encode(map[string]string{
			"code":    "FORBIDDEN",
			"message": "user is not in an allowed organization",
		}); err != nil {
			assert.NoError(t, err)
		}
	}))
	defer server.Close()

	// when
	_, err := NewAuthClient(server.URL).ExchangeGitHubToken(context.Background(), "github-token")

	// then
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnauthorized)
	var backendError *Error
	require.True(t, errors.As(err, &backendError))
	assert.Equal(t, http.StatusForbidden, backendError.Status)
	assert.Equal(t, "FORBIDDEN", backendError.Code)
}
