package auth

import (
	"time"
)

type JWTConfig struct {
	Secret string        `mapstructure:"secret"`
	Issuer string        `mapstructure:"issuer"`
	TTL    time.Duration `mapstructure:"ttl"`
}

type GitHubOAuthConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
}

type Config struct {
	AllowedOrgs []string          `mapstructure:"allowed_orgs"`
	GitHub      GitHubOAuthConfig `mapstructure:"github"`
	JWT         JWTConfig         `mapstructure:"jwt"`

	// AdminUserID is a temporary hardcoded administrator allowed to activate or
	// reject revisions. Empty disables administrative state changes.
	AdminUserID string `mapstructure:"admin_user_id"`
}
