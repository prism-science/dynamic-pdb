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

const baseURL = "https://api.github.com"

//nolint:gosec // G101: this is GitHub's public OAuth token endpoint URL, not a credential.
const oauthAccessTokenURL = "https://github.com/login/oauth/access_token"

const defaultHTTPTimeout = 10 * time.Second

var (
	ErrInvalidToken               = errors.New("github: invalid access token")
	ErrInvalidCode                = errors.New("github: invalid authorization code")
	ErrAccessDenied               = errors.New("github: access denied")
	ErrIncorrectClientCredentials = errors.New("github: incorrect client credentials")
	ErrRedirectURIMismatch        = errors.New("github: redirect uri mismatch")
	ErrOAuthNotConfigured         = errors.New("github: oauth client credentials are not configured")
)

type Client interface {
	GetUser(ctx context.Context, accessToken string) (User, error)
	ListOrgs(ctx context.Context, accessToken string) ([]Organization, error)
	ExchangeCode(ctx context.Context, code, redirectURI string) (string, error)
}

type RemoteClient struct {
	httpClient        *http.Client
	oauthClientID     string
	oauthClientSecret string
}

var _ Client = (*RemoteClient)(nil)

type User struct {
	ID        int64
	Login     string
	Email     string
	Name      string
	AvatarURL string
}

type Organization struct {
	ID    int64
	Login string
}

type Option func(*RemoteClient)

func WithHTTPClient(h *http.Client) Option {
	return func(c *RemoteClient) { c.httpClient = h }
}

func WithOAuthClientID(id string) Option {
	return func(c *RemoteClient) { c.oauthClientID = id }
}

func WithOAuthClientSecret(secret string) Option {
	return func(c *RemoteClient) { c.oauthClientSecret = secret }
}

func NewClient(opts ...Option) *RemoteClient {
	c := &RemoteClient{
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *RemoteClient) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	if c.oauthClientID == "" || c.oauthClientSecret == "" {
		return "", ErrOAuthNotConfigured
	}

	form := url.Values{}
	form.Set("client_id", c.oauthClientID)
	form.Set("client_secret", c.oauthClientSecret)
	form.Set("code", code)
	if redirectURI != "" {
		form.Set("redirect_uri", redirectURI)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		oauthAccessTokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: POST /login/oauth/access_token: %w", err)
	}
	defer resp.Body.Close()

	var raw struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf(
			"github: decode oauth access token (status %s, body %q): %w",
			resp.Status,
			body,
			err,
		)
	}
	if raw.AccessToken != "" {
		return raw.AccessToken, nil
	}

	switch raw.Error {
	case "bad_verification_code":
		return "", ErrInvalidCode
	case "access_denied":
		return "", ErrAccessDenied
	case "incorrect_client_credentials":
		return "", ErrIncorrectClientCredentials
	case "redirect_uri_mismatch":
		return "", ErrRedirectURIMismatch
	case "":
		return "", fmt.Errorf("github: token: empty response (status %s)", resp.Status)
	default:
		desc := raw.ErrorDesc
		if desc == "" {
			desc = raw.Error
		}
		return "", fmt.Errorf("github: token: %s: %s", raw.Error, desc)
	}
}

func (c *RemoteClient) GetUser(ctx context.Context, accessToken string) (User, error) {
	var raw struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if _, err := c.get(ctx, accessToken, "/user", &raw); err != nil {
		return User{}, err
	}
	return User{
		ID:        raw.ID,
		Login:     raw.Login,
		Email:     raw.Email,
		Name:      raw.Name,
		AvatarURL: raw.AvatarURL,
	}, nil
}

func (c *RemoteClient) ListOrgs(ctx context.Context, accessToken string) ([]Organization, error) {
	orgs := []Organization{}
	nextPath := "/user/orgs?per_page=100"
	for nextPath != "" {
		var raw []struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		}
		header, err := c.get(ctx, accessToken, nextPath, &raw)
		if err != nil {
			return nil, err
		}
		for _, o := range raw {
			orgs = append(orgs, Organization{ID: o.ID, Login: o.Login})
		}
		nextPath = nextLinkPath(header.Get("Link"))
	}
	return orgs, nil
}

func (c *RemoteClient) get(ctx context.Context, accessToken, path string, out any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: %s %s: %w", req.Method, path, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrInvalidToken
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("github: %s %s: %s: %s", req.Method, path, resp.Status, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("github: decode %s: %w", path, err)
	}
	return resp.Header.Clone(), nil
}

func nextLinkPath(linkHeader string) string {
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.IndexByte(part, '<')
		end := strings.IndexByte(part, '>')
		if start < 0 || end <= start+1 {
			return ""
		}
		target := part[start+1 : end]
		u, err := url.Parse(target)
		if err != nil {
			return ""
		}
		if u.RequestURI() != "" {
			return u.RequestURI()
		}
		return target
	}
	return ""
}
