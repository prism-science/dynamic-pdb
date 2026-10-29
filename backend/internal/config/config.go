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

// RevisionsConfig holds the switches for reviewing revisions.
type RevisionsConfig struct {
	// SelfApprovalEnabled lets a reviewer approve entry and model revisions they created. It is on
	// only on dev, so a researcher can publish their own test entries; elsewhere a second
	// reviewer must approve them.
	SelfApprovalEnabled bool `mapstructure:"self_approval_enabled"`
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
	v.SetDefault("data_sync::coordinate_revisions", false)
	v.SetDefault("entries::delete_enabled", false)
	v.SetDefault("revisions::self_approval_enabled", false)
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
	if err := validatePublicBaseURL(cfg.Server.PublicBaseURL); err != nil {
		return fmt.Errorf("server.public_base_url: %w", err)
	}
	if err := cfg.Validation.Mode.Validate(); err != nil {
		return fmt.Errorf("validation.mode: %w", err)
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
