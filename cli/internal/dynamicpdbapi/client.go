package dynamicpdbapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultUploadHTTPTimeout = 10 * time.Minute

type Client interface {
	ListEntries(ctx context.Context, params ListEntriesParams) ([]Entry, error)
	CreateEntry(ctx context.Context, request CreateEntryRequest) error
	CreateModel(ctx context.Context, entryID string, request CreateModelRequest) error
	CreateFileUpload(ctx context.Context, request CreateFileUploadRequest) (FileUploadGrantResponse, error)
	CompleteFileUpload(ctx context.Context, request CompleteFileUploadRequest) error
	PutUploadPart(ctx context.Context, url string, body io.Reader, size int64) (string, error)
}

type RemoteClient struct {
	serverURL  string
	token      string
	httpClient *http.Client
}

var _ Client = (*RemoteClient)(nil)

type ClientOption func(*RemoteClient)

func WithClientHTTPClient(httpClient *http.Client) ClientOption {
	return func(client *RemoteClient) {
		client.httpClient = httpClient
	}
}

func NewClient(serverURL string, token string, options ...ClientOption) *RemoteClient {
	client := &RemoteClient{
		serverURL:  strings.TrimRight(serverURL, "/"),
		token:      token,
		httpClient: &http.Client{Timeout: defaultUploadHTTPTimeout},
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *RemoteClient) CreateEntry(ctx context.Context, request CreateEntryRequest) error {
	if _, err := c.postJSON(ctx, "/v1/entries", request, http.StatusCreated); err != nil {
		return fmt.Errorf("dynamicpdbapi: create entry: %w", err)
	}
	return nil
}

func (c *RemoteClient) ListEntries(ctx context.Context, params ListEntriesParams) ([]Entry, error) {
	query := url.Values{}
	for _, pdbID := range params.PDBIDs {
		pdbID = strings.TrimSpace(pdbID)
		if pdbID != "" {
			query.Add("pdb_id", pdbID)
		}
	}
	path := "/v1/entries"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	body, err := c.getJSON(ctx, path, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("dynamicpdbapi: list entries: %w", err)
	}
	var response entryListResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("dynamicpdbapi: decode entry list: %w", err)
	}
	return response.Items, nil
}

func (c *RemoteClient) CreateModel(ctx context.Context, entryID string, request CreateModelRequest) error {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return fmt.Errorf("dynamicpdbapi: create model: entry id is required")
	}
	if _, err := c.postJSON(ctx, "/v1/entries/"+url.PathEscape(entryID)+"/models", request, http.StatusCreated); err != nil {
		return fmt.Errorf("dynamicpdbapi: create model: %w", err)
	}
	return nil
}

func (c *RemoteClient) CreateFileUpload(ctx context.Context, request CreateFileUploadRequest) (FileUploadGrantResponse, error) {
	body, err := c.postJSON(ctx, "/v1/files", request, http.StatusOK)
	if err != nil {
		return FileUploadGrantResponse{}, fmt.Errorf("dynamicpdbapi: create file upload: %w", err)
	}
	var grant FileUploadGrantResponse
	if err := json.Unmarshal(body, &grant); err != nil {
		return FileUploadGrantResponse{}, fmt.Errorf("dynamicpdbapi: decode file upload grant: %w", err)
	}
	if grant.Key == "" || grant.UploadID == "" || grant.ObjectURL == "" || grant.PartSize <= 0 || len(grant.Parts) == 0 {
		return FileUploadGrantResponse{}, fmt.Errorf("dynamicpdbapi: backend returned incomplete file upload grant")
	}
	return grant, nil
}

func (c *RemoteClient) CompleteFileUpload(ctx context.Context, request CompleteFileUploadRequest) error {
	if _, err := c.postJSON(ctx, "/v1/files/complete", request, http.StatusOK); err != nil {
		return fmt.Errorf("dynamicpdbapi: complete file upload: %w", err)
	}
	return nil
}

func (c *RemoteClient) PutUploadPart(ctx context.Context, url string, body io.Reader, size int64) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return "", fmt.Errorf("dynamicpdbapi: create upload part request: %w", err)
	}
	request.ContentLength = size

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("dynamicpdbapi: upload part: %w", err)
	}
	responseBody, err := readResponseBody(response)
	if err != nil {
		return "", fmt.Errorf("dynamicpdbapi: read upload part response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", decodeError(response.StatusCode, responseBody)
	}
	etag := response.Header.Get("ETag")
	if etag == "" {
		return "", fmt.Errorf("dynamicpdbapi: upload part response missing ETag")
	}
	return etag, nil
}

func (c *RemoteClient) postJSON(ctx context.Context, path string, payload any, expectedStatus int) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", bearerToken(c.token))

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	body, err := readResponseBody(response)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode != expectedStatus {
		return nil, decodeError(response.StatusCode, body)
	}
	return body, nil
}

func (c *RemoteClient) getJSON(ctx context.Context, path string, expectedStatus int) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.serverURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if strings.TrimSpace(c.token) != "" {
		request.Header.Set("Authorization", bearerToken(c.token))
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	body, err := readResponseBody(response)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode != expectedStatus {
		return nil, decodeError(response.StatusCode, body)
	}
	return body, nil
}

func bearerToken(token string) string {
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}
