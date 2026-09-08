package sifts

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL     = "https://ftp.ebi.ac.uk/pub/databases/msd/sifts/split_xml"
	baseURLEnv         = "DYNAMIC_PDB_SIFTS_URL"
	defaultHTTPTimeout = 5 * time.Minute
	getAttempts        = 8
	retryBaseDelay     = 500 * time.Millisecond
	retryMaxDelay      = 10 * time.Second
)

var errNotFound = errors.New("SIFTS resource not found")

type statusError struct {
	status int
}

func (e statusError) Error() string {
	return fmt.Sprintf("unexpected HTTP status %d", e.status)
}

type RemoteClient struct {
	httpClient *http.Client
	baseURL    string
}

type Option func(*RemoteClient)

func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *RemoteClient) {
		client.httpClient = httpClient
	}
}

func WithBaseURL(baseURL string) Option {
	return func(client *RemoteClient) {
		client.baseURL = cleanBaseURL(baseURL)
	}
}

func NewClient(options ...Option) *RemoteClient {
	client := &RemoteClient{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
		baseURL:    cleanBaseURL(os.Getenv(baseURLEnv)),
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *RemoteClient) GetUniProtRelease(ctx context.Context, pdbID string) (*string, error) {
	pdbID = strings.ToLower(strings.TrimSpace(pdbID))
	if len(pdbID) != 4 {
		return nil, fmt.Errorf("PDB ID must contain four characters: %q", pdbID)
	}

	url := c.baseURL + "/" + pdbID[1:3] + "/" + pdbID + ".xml.gz"
	contents, err := c.get(ctx, url)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get SIFTS XML for %s: %w", pdbID, err)
	}

	gzipReader, err := gzip.NewReader(bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("open SIFTS XML gzip for %s: %w", pdbID, err)
	}
	var decodedDocument document
	if err := xml.NewDecoder(gzipReader).Decode(&decodedDocument); err != nil {
		closeErr := gzipReader.Close()
		return nil, fmt.Errorf("decode SIFTS XML for %s: %w", pdbID, errors.Join(err, closeErr))
	}
	if err := gzipReader.Close(); err != nil {
		return nil, fmt.Errorf("close SIFTS XML gzip for %s: %w", pdbID, err)
	}

	for _, database := range decodedDocument.Databases {
		if !strings.EqualFold(strings.TrimSpace(database.Source), "UniProt") {
			continue
		}
		return optionalString(database.Version), nil
	}
	return nil, nil
}

type document struct {
	Databases []database `xml:"listDB>db"`
}

type database struct {
	Source  string `xml:"dbSource,attr"`
	Version string `xml:"dbVersion,attr"`
}

func (c *RemoteClient) get(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= getAttempts; attempt++ {
		contents, err := c.getOnce(ctx, url)
		if err == nil {
			return contents, nil
		}
		lastErr = err
		if !shouldRetryGet(ctx, err, attempt) {
			return nil, err
		}
		if err := waitBeforeRetry(ctx, attempt); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *RemoteClient) getOnce(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		closeErr := response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			if closeErr != nil {
				return nil, fmt.Errorf("%w: close response: %w", errNotFound, closeErr)
			}
			return nil, errNotFound
		}
		if closeErr != nil {
			return nil, fmt.Errorf("unexpected HTTP status %d and close response: %w", response.StatusCode, closeErr)
		}
		return nil, statusError{status: response.StatusCode}
	}
	contents, err := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return contents, nil
}

func shouldRetryGet(ctx context.Context, err error, attempt int) bool {
	if err == nil || attempt >= getAttempts || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, errNotFound) {
		return false
	}
	var status statusError
	if errors.As(err, &status) {
		return status.status == http.StatusTooManyRequests || status.status >= http.StatusInternalServerError
	}
	return true
}

func waitBeforeRetry(ctx context.Context, attempt int) error {
	delay := retryBaseDelay << (attempt - 1)
	if delay > retryMaxDelay {
		delay = retryMaxDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cleanBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultBaseURL
	}
	return strings.TrimRight(value, "/")
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
