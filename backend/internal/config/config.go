package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/services/analytics"
	"dynamic-pdb/backend/internal/services/cdn"
	"dynamic-pdb/backend/internal/validation"
)

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Auth       auth.Config      `mapstructure:"auth"`
	DB         db.Config        `mapstructure:"db"`
	CDN        cdn.Config       `mapstructure:"cdn"`
	Analytics  analytics.Config `mapstructure:"analytics"`
	Validation ValidationConfig `mapstructure:"validation"`
}

// ValidationConfig controls schema validation of writes. Mode log_only logs violations instead of
// rejecting them and exists only for rolling the schema out.
type ValidationConfig struct {
	Mode validation.Mode `mapstructure:"mode"`
}

type ServerConfig struct {
	Addr string `mapstructure:"addr"`
	// PublicBaseURL is the API's external base URL, including any path prefix the
	// ingress strips (for example https://dynamicpdb.com/api). Pagination links start with it.
	PublicBaseURL string          `mapstructure:"public_base_url"`
	RateLimit     RateLimitConfig `mapstructure:"rate_limit"`
}

// RateLimitConfig caps how many requests one client IP may send per window on each server instance.
type RateLimitConfig struct {
	Requests int           `mapstructure:"requests"`
	Window   time.Duration `mapstructure:"window"`
}

const (
	defaultRateLimitRequests = 100
	defaultRateLimitWindow   = 10 * time.Second
)

func ReadFromFile(filename string) (Config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
	v.SetDefault("server::rate_limit::requests", defaultRateLimitRequests)
	v.SetDefault("server::rate_limit::window", defaultRateLimitWindow)
	v.SetDefault("validation::mode", string(validation.ModeEnforce))
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
