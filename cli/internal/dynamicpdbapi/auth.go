package dynamicpdbapi

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultHTTPTimeout = 30 * time.Second

var ErrUnauthorized = errors.New("dynamicpdbapi: unauthorized")

type AuthClient interface {
	ExchangeGitHubToken(ctx context.Context, githubToken string) (TokenResponse, error)
}

type RemoteAuthClient struct {
	serverURL  string
	httpClient *http.Client
}

var _ AuthClient = (*RemoteAuthClient)(nil)

type AuthOption func(*RemoteAuthClient)

func WithHTTPClient(httpClient *http.Client) AuthOption {
	return func(client *RemoteAuthClient) {
		client.httpClient = httpClient
	}
}

func NewAuthClient(serverURL string, options ...AuthOption) *RemoteAuthClient {
	client := &RemoteAuthClient{
		serverURL:  strings.TrimRight(serverURL, "/"),
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *RemoteAuthClient) ExchangeGitHubToken(ctx context.Context, githubToken string) (TokenResponse, error) {
	//nolint:gosec // The GitHub OAuth token is the required request payload, not a hard-coded credential.
	payload, err := json.Marshal(struct {
		AccessToken string `json:"access_token"`
	}{AccessToken: githubToken})
	if err != nil {
		return TokenResponse{}, fmt.Errorf("dynamicpdbapi: encode GitHub token exchange request: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.serverURL+"/v1/auth/github/exchange",
		bytes.NewReader(payload),
	)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("dynamicpdbapi: create GitHub token exchange request: %w", err)
	}
	request.Header.Set("Content-Type", jsonAPIMediaType)
	request.Header.Set("Accept", jsonAPIMediaType)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("dynamicpdbapi: exchange GitHub token: %w", err)
	}
	body, err := readResponseBody(response)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("dynamicpdbapi: read GitHub token exchange response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return TokenResponse{}, decodeError(response.StatusCode, body)
	}

	token, err := decodeAttributes[TokenResponse](body)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("dynamicpdbapi: decode GitHub token exchange response: %w", err)
	}
	if token.TokenType == "" || token.AccessToken == "" || token.ExpiresAt.IsZero() || token.Login == "" {
		return TokenResponse{}, errors.New("dynamicpdbapi: backend returned an incomplete token response")
	}
	return token, nil
}

func decodeError(status int, body []byte) error {
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var jsonAPIPayload struct {
		Errors []struct {
			Code   string `json:"code"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &jsonAPIPayload); err == nil && len(jsonAPIPayload.Errors) > 0 {
		payload.Code = jsonAPIPayload.Errors[0].Code
		payload.Message = jsonAPIPayload.Errors[0].Detail
		if payload.Message == "" {
			payload.Message = jsonAPIPayload.Errors[0].Title
		}
	} else if err := json.Unmarshal(body, &payload); err != nil {
		var xmlPayload struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		}
		if xmlErr := xml.Unmarshal(body, &xmlPayload); xmlErr == nil && (xmlPayload.Code != "" || xmlPayload.Message != "") {
			payload.Code = xmlPayload.Code
			payload.Message = xmlPayload.Message
		} else {
			payload.Message = strings.TrimSpace(string(body))
		}
	}
	if payload.Message == "" {
		payload.Message = http.StatusText(status)
	}
	backendError := &Error{Status: status, Code: payload.Code, Message: payload.Message}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("dynamicpdbapi: authorization failed: %w", errors.Join(ErrUnauthorized, backendError))
	}
	return backendError
}

func readResponseBody(response *http.Response) ([]byte, error) {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read and close response: %w", err)
	}
	return body, nil
}
