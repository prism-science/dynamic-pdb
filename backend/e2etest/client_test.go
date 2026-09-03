package e2etest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/httpapi"
)

func postJSON(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	return postJSONWithToken(t, path, body, "")
}

func postJSONWithToken(t *testing.T, path string, body any, token string) *http.Response {
	return jsonRequestWithToken(t, http.MethodPost, path, body, token)
}

func patchJSONWithToken(t *testing.T, path string, body any, token string) *http.Response {
	t.Helper()
	return jsonRequestWithToken(t, http.MethodPatch, path, body, token)
}

func jsonRequestWithToken(t *testing.T, method, path string, body any, token string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}

	req, err := http.NewRequest(method, testServer.URL+path, &buf)
	require.NoError(t, err)
	req.Header.Set("Accept", httpapi.JSONAPIMediaType)
	req.Header.Set("Content-Type", httpapi.JSONAPIMediaType)
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
	req.Header.Set("Accept", httpapi.JSONAPIMediaType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func requestWithoutRedirect(t *testing.T, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, testServer.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", httpapi.JSONAPIMediaType)

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func deleteWithToken(t *testing.T, path string, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, testServer.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", httpapi.JSONAPIMediaType)
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
