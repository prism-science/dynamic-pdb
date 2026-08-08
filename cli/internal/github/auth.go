package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultAuthorizeURL = "https://github.com/login/device/code"
	defaultTokenURL     = "https://github.com/login/oauth/access_token" //nolint:gosec // Public OAuth endpoint, not a credential.
	defaultHTTPTimeout  = 10 * time.Second
	DefaultScope        = "read:org"
)

var (
	ErrAuthorizationPending = errors.New("github: authorization pending")
	ErrSlowDown             = errors.New("github: slow down")
	ErrExpiredToken         = errors.New("github: device code expired")
	ErrAccessDenied         = errors.New("github: access denied")
)

type AuthClient interface {
	RequestDeviceCode(ctx context.Context) (DeviceCode, error)
	WaitForToken(ctx context.Context, deviceCode DeviceCode) (string, error)
}

type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresIn       time.Duration
	Interval        time.Duration
}

type RemoteAuthClient struct {
	clientID     string
	scope        string
	httpClient   *http.Client
	authorizeURL string
	tokenURL     string
}

var _ AuthClient = (*RemoteAuthClient)(nil)

type AuthOption func(*RemoteAuthClient)

func WithHTTPClient(httpClient *http.Client) AuthOption {
	return func(client *RemoteAuthClient) {
		client.httpClient = httpClient
	}
}

func WithAuthorizeURL(authorizeURL string) AuthOption {
	return func(client *RemoteAuthClient) {
		client.authorizeURL = authorizeURL
	}
}

func WithTokenURL(tokenURL string) AuthOption {
	return func(client *RemoteAuthClient) {
		client.tokenURL = tokenURL
	}
}

func NewAuthClient(clientID string, options ...AuthOption) *RemoteAuthClient {
	client := &RemoteAuthClient{
		clientID:     clientID,
		scope:        DefaultScope,
		httpClient:   &http.Client{Timeout: defaultHTTPTimeout},
		authorizeURL: defaultAuthorizeURL,
		tokenURL:     defaultTokenURL,
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *RemoteAuthClient) RequestDeviceCode(ctx context.Context) (DeviceCode, error) {
	if c.clientID == "" {
		return DeviceCode{}, errors.New("github: client id is not configured")
	}

	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("scope", c.scope)
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.authorizeURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("github: create device code request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("github: request device code: %w", err)
	}
	body, err := readResponseBody(response)
	if err != nil {
		return DeviceCode{}, fmt.Errorf("github: read device code response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return DeviceCode{}, fmt.Errorf(
			"github: request device code: %s: %s",
			response.Status,
			strings.TrimSpace(string(body)),
		)
	}

	var payload struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		ErrorDesc       string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return DeviceCode{}, fmt.Errorf("github: decode device code response: %w", err)
	}
	if payload.Error != "" {
		return DeviceCode{}, fmt.Errorf("github: request device code: %s: %s", payload.Error, payload.ErrorDesc)
	}
	if payload.DeviceCode == "" || payload.UserCode == "" || payload.VerificationURI == "" {
		return DeviceCode{}, errors.New("github: incomplete device code response")
	}
	interval := payload.Interval
	if interval <= 0 {
		interval = 5
	}
	return DeviceCode{
		DeviceCode:      payload.DeviceCode,
		UserCode:        payload.UserCode,
		VerificationURI: payload.VerificationURI,
		ExpiresIn:       time.Duration(payload.ExpiresIn) * time.Second,
		Interval:        time.Duration(interval) * time.Second,
	}, nil
}

func (c *RemoteAuthClient) WaitForToken(ctx context.Context, deviceCode DeviceCode) (string, error) {
	interval := deviceCode.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return "", fmt.Errorf("github: wait for authorization: %w", ctx.Err())
		case <-timer.C:
		}

		token, err := c.requestToken(ctx, deviceCode.DeviceCode)
		if err == nil {
			return token, nil
		}
		switch {
		case errors.Is(err, ErrAuthorizationPending):
		case errors.Is(err, ErrSlowDown):
			interval += 5 * time.Second
		default:
			return "", fmt.Errorf("github: wait for authorization: %w", err)
		}
	}
}

func (c *RemoteAuthClient) requestToken(ctx context.Context, deviceCode string) (string, error) {
	form := url.Values{}
	form.Set("client_id", c.clientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("github: create token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("github: request token: %w", err)
	}
	body, err := readResponseBody(response)
	if err != nil {
		return "", fmt.Errorf("github: read token response: %w", err)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("github: decode token response: %w", err)
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}

	switch payload.Error {
	case "authorization_pending":
		return "", ErrAuthorizationPending
	case "slow_down":
		return "", ErrSlowDown
	case "expired_token":
		return "", ErrExpiredToken
	case "access_denied":
		return "", ErrAccessDenied
	case "":
		return "", fmt.Errorf("github: empty token response: %s", response.Status)
	default:
		return "", fmt.Errorf("github: token response: %s: %s", payload.Error, payload.ErrorDesc)
	}
}

func readResponseBody(response *http.Response) ([]byte, error) {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read and close response: %w", err)
	}
	return body, nil
}
