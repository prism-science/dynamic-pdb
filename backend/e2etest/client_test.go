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
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	resp, err := http.Post(testServer.URL+path, "application/json", &buf)
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
