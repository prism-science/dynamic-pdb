package rcsb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultHTTPTimeout = 30 * time.Second

const (
	defaultDataBaseURL  = "https://data.rcsb.org"
	defaultFilesBaseURL = "https://files.rcsb.org"
	defaultWWWBaseURL   = "https://www.rcsb.org"

	dataBaseURLEnv  = "DYNAMIC_PDB_RCSB_DATA_URL"
	filesBaseURLEnv = "DYNAMIC_PDB_RCSB_FILES_URL"
	wwwBaseURLEnv   = "DYNAMIC_PDB_RCSB_WWW_URL"
)

type Client interface {
	GetEntry(ctx context.Context, pdbID string) (map[string]any, error)
	GetPolymerEntity(ctx context.Context, pdbID string, entityID string) (map[string]any, error)
	GetFile(ctx context.Context, pdbID string, file string) (Artifact, error)
	GetFASTA(ctx context.Context, pdbID string) (Artifact, error)
}

type RemoteClient struct {
	httpClient   *http.Client
	dataBaseURL  string
	filesBaseURL string
	wwwBaseURL   string
}

type Option func(*RemoteClient)

func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *RemoteClient) {
		client.httpClient = httpClient
	}
}

func WithBaseURLs(dataBaseURL string, filesBaseURL string, wwwBaseURL string) Option {
	return func(client *RemoteClient) {
		client.dataBaseURL = cleanBaseURL(dataBaseURL, defaultDataBaseURL)
		client.filesBaseURL = cleanBaseURL(filesBaseURL, defaultFilesBaseURL)
		client.wwwBaseURL = cleanBaseURL(wwwBaseURL, defaultWWWBaseURL)
	}
}

func NewClient(options ...Option) *RemoteClient {
	client := &RemoteClient{
		httpClient:   &http.Client{Timeout: defaultHTTPTimeout},
		dataBaseURL:  cleanBaseURL(os.Getenv(dataBaseURLEnv), defaultDataBaseURL),
		filesBaseURL: cleanBaseURL(os.Getenv(filesBaseURLEnv), defaultFilesBaseURL),
		wwwBaseURL:   cleanBaseURL(os.Getenv(wwwBaseURLEnv), defaultWWWBaseURL),
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *RemoteClient) GetEntry(ctx context.Context, pdbID string) (map[string]any, error) {
	url := c.dataBaseURL + "/rest/v1/core/entry/" + strings.ToUpper(pdbID)
	contents, err := c.get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get RCSB entry: %w", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(contents, &payload); err != nil {
		return nil, fmt.Errorf("decode RCSB entry: %w", err)
	}
	return payload, nil
}

func (c *RemoteClient) GetPolymerEntity(ctx context.Context, pdbID string, entityID string) (map[string]any, error) {
	url := c.dataBaseURL + "/rest/v1/core/polymer_entity/" + strings.ToUpper(pdbID) + "/" + strings.TrimSpace(entityID)
	contents, err := c.get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get RCSB polymer entity %s_%s: %w", pdbID, entityID, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(contents, &payload); err != nil {
		return nil, fmt.Errorf("decode RCSB polymer entity %s_%s: %w", pdbID, entityID, err)
	}
	return payload, nil
}

func (c *RemoteClient) GetFile(ctx context.Context, pdbID string, file string) (Artifact, error) {
	filename := strings.TrimSpace(file)
	if filename == "" {
		return Artifact{}, errors.New("RCSB file is required")
	}
	url := c.filesBaseURL + "/download/" + rcsbDownloadFilename(pdbID, filename)
	contents, err := c.get(ctx, url)
	if err != nil {
		return Artifact{}, fmt.Errorf("get RCSB file %s: %w", filename, err)
	}
	return Artifact{
		Filename: filename,
		Format:   formatFromFilename(filename),
		URI:      url,
		Contents: contents,
	}, nil
}

func (c *RemoteClient) GetFASTA(ctx context.Context, pdbID string) (Artifact, error) {
	contents, err := c.get(ctx, c.wwwBaseURL+"/fasta/entry/"+strings.ToUpper(pdbID))
	if err != nil {
		return Artifact{}, fmt.Errorf("get RCSB FASTA: %w", err)
	}
	return Artifact{
		Filename: strings.ToLower(pdbID) + ".fasta",
		Format:   "fasta",
		URI:      c.wwwBaseURL + "/fasta/entry/" + strings.ToUpper(pdbID),
		Contents: contents,
	}, nil
}

func rcsbDownloadFilename(pdbID string, filename string) string {
	pdbID = strings.TrimSpace(pdbID)
	if len(pdbID) != 4 || len(filename) < 4 {
		return filename
	}
	if !strings.EqualFold(filename[:4], pdbID) {
		return filename
	}
	return strings.ToUpper(pdbID) + filename[4:]
}

func formatFromFilename(filename string) string {
	filename = strings.ToLower(strings.TrimSpace(filename))
	switch {
	case strings.HasSuffix(filename, "-sf.cif"):
		return "structure_factors_cif"
	case strings.HasSuffix(filename, ".cif"):
		return "cif"
	case strings.HasSuffix(filename, ".pdb"):
		return "pdb"
	case strings.HasSuffix(filename, ".mtz"):
		return "mtz"
	case strings.HasSuffix(filename, ".jpeg") || strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".png"):
		return "image"
	default:
		extension := strings.TrimPrefix(filename[strings.LastIndex(filename, ".")+1:], ".")
		if extension == filename {
			return ""
		}
		return extension
	}
}

func cleanBaseURL(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	return strings.TrimRight(value, "/")
}

func (c *RemoteClient) get(ctx context.Context, url string) ([]byte, error) {
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
		if closeErr != nil {
			return nil, fmt.Errorf("unexpected HTTP status %d and close response: %w", response.StatusCode, closeErr)
		}
		return nil, fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	contents, err := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return contents, nil
}
