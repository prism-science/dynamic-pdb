package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://cloud.umami.is/api/send"
	sendTimeout     = 5 * time.Second
	// Umami drops events whose User-Agent looks like a bot, and every server
	// request does. The downloader's own agent travels in the event data instead.
	senderUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
	// Sent by the website's viewer proxy. Those requests are viewer loads, which
	// the website already counts.
	websiteProxyUserAgent = "dynamic-pdb-website-proxy"
)

type Config struct {
	Endpoint  string `mapstructure:"endpoint"`
	WebsiteID string `mapstructure:"website_id"`
	Hostname  string `mapstructure:"hostname"`
}

type Download struct {
	Path      string
	EntryID   string
	ModelID   string
	Filename  string
	UserAgent string
}

type Tracker interface {
	TrackDownload(download Download)
}

// New returns a tracker that reports to Umami, or one that does nothing when no
// website id is configured.
func New(cfg Config) Tracker {
	if strings.TrimSpace(cfg.WebsiteID) == "" {
		return disabled{}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = defaultEndpoint
	}
	return &umami{cfg: cfg, httpClient: &http.Client{Timeout: sendTimeout}}
}

type disabled struct{}

func (disabled) TrackDownload(Download) {}

type umami struct {
	cfg        Config
	httpClient *http.Client
}

// TrackDownload reports in the background so a download never waits on, or
// fails because of, the analytics service.
func (u *umami) TrackDownload(download Download) {
	if download.UserAgent == websiteProxyUserAgent {
		return
	}
	go func() {
		if err := u.send(download); err != nil {
			slog.Warn("analytics download event failed", "path", download.Path, "err", err)
		}
	}()
}

type sendRequest struct {
	Type    string      `json:"type"`
	Payload sendPayload `json:"payload"`
}

type sendPayload struct {
	Website  string            `json:"website"`
	Hostname string            `json:"hostname"`
	URL      string            `json:"url"`
	Name     string            `json:"name"`
	Data     map[string]string `json:"data"`
}

func (u *umami) send(download Download) error {
	data := map[string]string{
		"entry_id": download.EntryID,
		"filename": download.Filename,
		"client":   clientKind(download.UserAgent),
		"agent":    agentProduct(download.UserAgent),
	}
	if download.ModelID != "" {
		data["model_id"] = download.ModelID
	}
	body, err := json.Marshal(sendRequest{
		Type: "event",
		Payload: sendPayload{
			Website:  u.cfg.WebsiteID,
			Hostname: u.cfg.Hostname,
			URL:      download.Path,
			Name:     "api-download",
			Data:     data,
		},
	})
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", senderUserAgent)

	response, err := u.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send event: %w", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			slog.Warn("analytics response body close failed", "err", err)
		}
	}()
	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("send event: unexpected status %d", response.StatusCode)
	}
	return nil
}

func clientKind(userAgent string) string {
	if strings.HasPrefix(userAgent, "Mozilla/") {
		return "browser"
	}
	return "script"
}

// agentProduct keeps the first token of the User-Agent ("curl", "python-requests")
// so the dashboard can show which tools fetch files.
func agentProduct(userAgent string) string {
	product, _, _ := strings.Cut(strings.TrimSpace(userAgent), "/")
	product, _, _ = strings.Cut(product, " ")
	if product == "" {
		return "unknown"
	}
	if len(product) > 40 {
		return product[:40]
	}
	return product
}
