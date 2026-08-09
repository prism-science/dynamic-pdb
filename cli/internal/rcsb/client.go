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
	defaultCDNBaseURL   = "https://cdn.rcsb.org"

	dataBaseURLEnv  = "DYNAMIC_PDB_RCSB_DATA_URL"
	filesBaseURLEnv = "DYNAMIC_PDB_RCSB_FILES_URL"
	wwwBaseURLEnv   = "DYNAMIC_PDB_RCSB_WWW_URL"
	cdnBaseURLEnv   = "DYNAMIC_PDB_RCSB_CDN_URL"
)

type Client interface {
	GetEntry(ctx context.Context, pdbID string) (Entry, error)
	GetPolymerEntity(ctx context.Context, pdbID string, entityID string) (PolymerEntity, error)
	GetCoordinates(ctx context.Context, pdbID string) (Artifact, error)
	GetStructureFactors(ctx context.Context, pdbID string) (Artifact, error)
	GetFASTA(ctx context.Context, pdbID string) (Artifact, error)
	GetPreviewImage(ctx context.Context, pdbID string) (PreviewImage, error)
}

type RemoteClient struct {
	httpClient   *http.Client
	dataBaseURL  string
	filesBaseURL string
	wwwBaseURL   string
	cdnBaseURL   string
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

func WithCDNBaseURL(cdnBaseURL string) Option {
	return func(client *RemoteClient) {
		client.cdnBaseURL = cleanBaseURL(cdnBaseURL, defaultCDNBaseURL)
	}
}

func NewClient(options ...Option) *RemoteClient {
	client := &RemoteClient{
		httpClient:   &http.Client{Timeout: defaultHTTPTimeout},
		dataBaseURL:  cleanBaseURL(os.Getenv(dataBaseURLEnv), defaultDataBaseURL),
		filesBaseURL: cleanBaseURL(os.Getenv(filesBaseURLEnv), defaultFilesBaseURL),
		wwwBaseURL:   cleanBaseURL(os.Getenv(wwwBaseURLEnv), defaultWWWBaseURL),
		cdnBaseURL:   cleanBaseURL(os.Getenv(cdnBaseURLEnv), defaultCDNBaseURL),
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *RemoteClient) GetEntry(ctx context.Context, pdbID string) (Entry, error) {
	url := c.dataBaseURL + "/rest/v1/core/entry/" + strings.ToUpper(pdbID)
	contents, err := c.get(ctx, url)
	if err != nil {
		return Entry{}, fmt.Errorf("get RCSB entry: %w", err)
	}

	var payload Entry
	if err := json.Unmarshal(contents, &payload); err != nil {
		return Entry{}, fmt.Errorf("decode RCSB entry: %w", err)
	}
	return payload, nil
}

func (c *RemoteClient) GetPolymerEntity(ctx context.Context, pdbID string, entityID string) (PolymerEntity, error) {
	url := c.dataBaseURL + "/rest/v1/core/polymer_entity/" + strings.ToUpper(pdbID) + "/" + strings.TrimSpace(entityID)
	contents, err := c.get(ctx, url)
	if err != nil {
		return PolymerEntity{}, fmt.Errorf("get RCSB polymer entity %s_%s: %w", pdbID, entityID, err)
	}
	var payload PolymerEntity
	if err := json.Unmarshal(contents, &payload); err != nil {
		return PolymerEntity{}, fmt.Errorf("decode RCSB polymer entity %s_%s: %w", pdbID, entityID, err)
	}
	return payload, nil
}

func (c *RemoteClient) GetCoordinates(ctx context.Context, pdbID string) (Artifact, error) {
	url := c.filesBaseURL + "/download/" + strings.ToUpper(pdbID) + ".cif"
	contents, err := c.get(ctx, url)
	if err != nil {
		return Artifact{}, fmt.Errorf("get RCSB deposited coordinates: %w", err)
	}
	return Artifact{
		Filename: strings.ToLower(pdbID) + ".cif",
		Format:   "cif",
		URI:      url,
		Contents: contents,
	}, nil
}

func (c *RemoteClient) GetPreviewImage(_ context.Context, pdbID string) (PreviewImage, error) {
	pdbID = strings.ToLower(strings.TrimSpace(pdbID))
	if len(pdbID) != 4 {
		return PreviewImage{}, fmt.Errorf("PDB ID must have 4 characters: %s", pdbID)
	}
	url := c.cdnBaseURL + "/images/structures/" + pdbID[1:3] + "/" + pdbID + "/" + pdbID + "_assembly-1.jpeg"
	return PreviewImage{
		Filename: pdbID + "_assembly-1.jpeg",
		Format:   "image",
		URI:      url,
	}, nil
}

func (c *RemoteClient) GetStructureFactors(_ context.Context, pdbID string) (Artifact, error) {
	url := c.filesBaseURL + "/download/" + strings.ToUpper(pdbID) + "-sf.cif"
	return Artifact{
		Filename: strings.ToLower(pdbID) + "-sf.cif",
		Format:   "structure_factors_cif",
		URI:      url,
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
