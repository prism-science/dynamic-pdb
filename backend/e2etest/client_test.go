package e2etest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func postJSON(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	return postJSONWithToken(t, path, body, "")
}

func postJSONWithToken(t *testing.T, path string, body any, token string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}

	req, err := http.NewRequest(http.MethodPost, testServer.URL+path, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func getWithToken(t *testing.T, path string, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, testServer.URL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func deleteWithToken(t *testing.T, path string, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, testServer.URL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func ExchangeGithubToken(t *testing.T, accessToken string) *http.Response {
	t.Helper()
	return postJSON(t, "/v1/auth/github/exchange", map[string]string{"access_token": accessToken})
}

func ExchangeGithubCode(t *testing.T, code, redirectURI string) *http.Response {
	t.Helper()
	return postJSON(t, "/v1/auth/github/code/exchange", map[string]string{
		"code":         code,
		"redirect_uri": redirectURI,
	})
}
