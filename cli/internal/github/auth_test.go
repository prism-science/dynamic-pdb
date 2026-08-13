package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_request_device_code_with_client_id_and_scope(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		if err := request.ParseForm(); err != nil {
			assert.NoError(t, err)
			return
		}
		assert.Equal(t, "client-id", request.Form.Get("client_id"))
		assert.Equal(t, DefaultScope, request.Form.Get("scope"))
		if err := json.NewEncoder(response).Encode(map[string]any{
			"device_code":      "device-code",
			"user_code":        "ABCD-EFGH",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         2,
		}); err != nil {
			assert.NoError(t, err)
		}
	}))
	defer server.Close()
	client := NewAuthClient("client-id", WithAuthorizeURL(server.URL))

	// when
	deviceCode, err := client.RequestDeviceCode(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, "device-code", deviceCode.DeviceCode)
	assert.Equal(t, "ABCD-EFGH", deviceCode.UserCode)
	assert.Equal(t, 900*time.Second, deviceCode.ExpiresIn)
	assert.Equal(t, 2*time.Second, deviceCode.Interval)
}

func Test_should_wait_until_github_returns_access_token(t *testing.T) {
	// given
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			assert.NoError(t, err)
			return
		}
		assert.Equal(t, "device-code", request.Form.Get("device_code"))
		if requests.Add(1) == 1 {
			if err := json.NewEncoder(response).Encode(map[string]string{"error": "authorization_pending"}); err != nil {
				assert.NoError(t, err)
			}
			return
		}
		if err := json.NewEncoder(response).Encode(map[string]string{"access_token": "github-token"}); err != nil {
			assert.NoError(t, err)
		}
	}))
	defer server.Close()
	client := NewAuthClient("client-id", WithTokenURL(server.URL))
	deviceCode := DeviceCode{DeviceCode: "device-code", Interval: time.Millisecond}

	// when
	token, err := client.WaitForToken(context.Background(), deviceCode)

	// then
	require.NoError(t, err)
	assert.Equal(t, "github-token", token)
	assert.Equal(t, int32(2), requests.Load())
}
