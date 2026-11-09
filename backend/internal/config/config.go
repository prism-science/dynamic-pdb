package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/jobs"
	"dynamic-pdb/backend/internal/services/analytics"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/backend/internal/validation"
)

type Config struct {
	Server     ServerConfig        `mapstructure:"server"`
	Auth       auth.Config         `mapstructure:"auth"`
	DB         db.Config           `mapstructure:"db"`
	CDN        cdn.Config          `mapstructure:"cdn"`
	Analytics  analytics.Config    `mapstructure:"analytics"`
	Validation validation.Config   `mapstructure:"validation"`
	DataSync   jobs.DataSyncConfig `mapstructure:"data_sync"`
	Entries    EntriesConfig       `mapstructure:"entries"`
	Revisions  RevisionsConfig     `mapstructure:"revisions"`
	// ArtifactExtraction holds the switch for extracting uploaded files before they are saved.
	ArtifactExtraction ArtifactExtractionConfig `mapstructure:"artifact_extraction"`
	// ModelSearch holds the switch and limits of the search filters on GET /v1/models.
	ModelSearch ModelSearchConfig `mapstructure:"model_search"`
}

type ServerConfig struct {
	Addr string `mapstructure:"addr"`
	// PublicBaseURL is the API's external base URL, including any path prefix the
	// ingress strips (for example https://dynamicpdb.com/api). Pagination links start with it.
	PublicBaseURL string          `mapstructure:"public_base_url"`
	RateLimit     RateLimitConfig `mapstructure:"rate_limit"`
}

// EntriesConfig holds the switches for changing entries.
type EntriesConfig struct {
	// DeleteEnabled lets reviewers soft-delete entries with DELETE /v1/entries/{entry_id}. It is
	// on only on dev, so researchers can delete their test entries and retest the deposition forms.
	DeleteEnabled bool `mapstructure:"delete_enabled"`
}

// ArtifactExtractionConfig holds the switches for reading values from structure files.
type ArtifactExtractionConfig struct {
	// UploadsEnabled turns on POST /v1/files/extract, which the deposition forms call to show the
	// values of an uploaded mmCIF before the entry or model is saved. Off, the forms keep their
	// own browser preview of the file.
	UploadsEnabled bool `mapstructure:"uploads_enabled"`
}

// RevisionsConfig holds the switches for reviewing revisions.
type RevisionsConfig struct {
	// SelfApprovalEnabled lets a reviewer approve entry and model revisions they created. It is on
	// only on dev, so a researcher can publish their own test entries; elsewhere a second
	// reviewer must approve them.
	SelfApprovalEnabled bool `mapstructure:"self_approval_enabled"`
}

// ModelSearchConfig holds the search filters of GET /v1/models.
type ModelSearchConfig struct {
	// FiltersEnabled turns on the filters, the capped total, and the entry and review warnings in
	// each result. Off, a filter parameter gets 400 and the list answers as before.
	FiltersEnabled bool `mapstructure:"filters_enabled"`
	// CountCap is where the total stops counting: above it the total is "CountCap+".
	CountCap int `mapstructure:"count_cap"`
	// RateLimit caps searches that count their results, per client and server instance.
	RateLimit ModelSearchRateLimitConfig `mapstructure:"rate_limit"`
}

// ModelSearchRateLimitConfig caps counted searches (requests without a cursor) per client, on
// top of the server's global limit. Clients are told apart as for server.rate_limit.
type ModelSearchRateLimitConfig struct {
	// Requests caps each client that reaches the server through the load balancer and ingress.
	Requests int `mapstructure:"requests"`
	// InternalRequests caps each in-cluster caller, such as a website pod.
	InternalRequests int           `mapstructure:"internal_requests"`
	Window           time.Duration `mapstructure:"window"`
}

// RateLimitConfig caps how many requests one client may send per window on each server instance.
type RateLimitConfig struct {
	// Requests caps each client that reaches the server through the load balancer and ingress.
	Requests int           `mapstructure:"requests"`
	Window   time.Duration `mapstructure:"window"`
	// InternalRequests caps each in-cluster caller, such as a website pod, that calls the
	// server directly instead of through the ingress.
	InternalRequests int `mapstructure:"internal_requests"`
	// TrustedProxies are the CIDRs of the load balancer, ingress and node addresses in front of
	// the server. Only X-Forwarded-For entries added by them are believed. Empty trusts none.
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

// TrustedProxyPrefixes parses TrustedProxies.
func (c RateLimitConfig) TrustedProxyPrefixes() ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(c.TrustedProxies))
	for _, value := range c.TrustedProxies {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy CIDR: %w", err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

const (
	defaultRateLimitRequests         = 100
	defaultRateLimitWindow           = 10 * time.Second
	defaultRateLimitInternalRequests = 1000

	defaultModelSearchCountCap                  = 10000
	defaultModelSearchRateLimitRequests         = 30
	defaultModelSearchRateLimitInternalRequests = 300
	defaultModelSearchRateLimitWindow           = 10 * time.Second
)

func ReadFromFile(filename string) (Config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	v.SetDefault("server::rate_limit::requests", defaultRateLimitRequests)
	v.SetDefault("server::rate_limit::window", defaultRateLimitWindow)
	v.SetDefault("server::rate_limit::internal_requests", defaultRateLimitInternalRequests)
	v.SetDefault("validation::mode", string(validation.ModeLogOnly))
	v.SetDefault("validation::request_keys", string(validation.ModeLogOnly))
	v.SetDefault("data_sync::coordinate_revisions", false)
	v.SetDefault("entries::delete_enabled", false)
	v.SetDefault("revisions::self_approval_enabled", false)
	v.SetDefault("artifact_extraction::uploads_enabled", false)
	v.SetDefault("model_search::filters_enabled", false)
	v.SetDefault("model_search::count_cap", defaultModelSearchCountCap)
	v.SetDefault("model_search::rate_limit::requests", defaultModelSearchRateLimitRequests)
	v.SetDefault("model_search::rate_limit::internal_requests", defaultModelSearchRateLimitInternalRequests)
	v.SetDefault("model_search::rate_limit::window", defaultModelSearchRateLimitWindow)
	v.SetConfigType("yaml")
	v.AddConfigPath("config")
	v.SetConfigName(filename)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("config: read: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: unmarshal: %w", err)
	}
	cfg.Server.PublicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.Server.PublicBaseURL), "/")
	if err := validate(cfg); err != nil {
		return Config{}, fmt.Errorf("config: validate: %w", err)
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if len(cfg.Auth.AllowedOrgs) == 0 {
		return fmt.Errorf("auth.allowed_orgs is required")
	}
	if strings.TrimSpace(cfg.Auth.GitHub.ClientID) == "" {
		return fmt.Errorf("auth.github.client_id is required")
	}
	if strings.TrimSpace(cfg.Auth.GitHub.ClientSecret) == "" {
		return fmt.Errorf("auth.github.client_secret is required")
	}
	if strings.TrimSpace(cfg.Auth.JWT.Secret) == "" {
		return fmt.Errorf("auth.jwt.secret is required")
	}
	if strings.TrimSpace(cfg.Auth.JWT.Issuer) == "" {
		return fmt.Errorf("auth.jwt.issuer is required")
	}
	if cfg.Auth.JWT.TTL <= 0 {
		return fmt.Errorf("auth.jwt.ttl must be positive")
	}
	if cfg.Server.RateLimit.Requests <= 0 {
		return fmt.Errorf("server.rate_limit.requests must be positive")
	}
	if cfg.Server.RateLimit.Window <= 0 {
		return fmt.Errorf("server.rate_limit.window must be positive")
	}
	if cfg.Server.RateLimit.InternalRequests <= 0 {
		return fmt.Errorf("server.rate_limit.internal_requests must be positive")
	}
	if _, err := cfg.Server.RateLimit.TrustedProxyPrefixes(); err != nil {
		return fmt.Errorf("server.rate_limit.trusted_proxies: %w", err)
	}
	if cfg.ModelSearch.CountCap <= 0 {
		return fmt.Errorf("model_search.count_cap must be positive")
	}
	if cfg.ModelSearch.RateLimit.Requests <= 0 {
		return fmt.Errorf("model_search.rate_limit.requests must be positive")
	}
	if cfg.ModelSearch.RateLimit.InternalRequests <= 0 {
		return fmt.Errorf("model_search.rate_limit.internal_requests must be positive")
	}
	if cfg.ModelSearch.RateLimit.Window <= 0 {
		return fmt.Errorf("model_search.rate_limit.window must be positive")
	}
	if err := validatePublicBaseURL(cfg.Server.PublicBaseURL); err != nil {
		return fmt.Errorf("server.public_base_url: %w", err)
	}
	if err := cfg.Validation.Mode.Validate(); err != nil {
		return fmt.Errorf("validation.mode: %w", err)
	}
	if err := cfg.Validation.RequestKeys.Validate(); err != nil {
		return fmt.Errorf("validation.request_keys: %w", err)
	}
	return nil
}

func validatePublicBaseURL(value string) error {
	if value == "" {
		return fmt.Errorf("is required")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("host is required")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("must not have a query or fragment")
	}
	return nil
}
