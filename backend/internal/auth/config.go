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

	// ReviewerUserID is a temporary hardcoded reviewer (users.id) allowed to
	// approve/reject submissions. Empty disables review decisions for everyone.
	// TODO: replace with a proper reviewer/curator role.
	ReviewerUserID string `mapstructure:"reviewer_user_id"`
}
