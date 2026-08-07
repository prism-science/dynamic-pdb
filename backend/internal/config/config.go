package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/services/cdn"
)

type Config struct {
	Server ServerConfig `mapstructure:"server"`
	Auth   auth.Config  `mapstructure:"auth"`
	DB     db.Config    `mapstructure:"db"`
	CDN    cdn.Config   `mapstructure:"cdn"`
}

type ServerConfig struct {
	Addr string `mapstructure:"addr"`
}

func ReadFromFile(filename string) (Config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetEnvPrefix("DYNAMIC_PDB")
	v.SetEnvKeyReplacer(strings.NewReplacer("::", "_"))
	v.AutomaticEnv()
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
	return nil
}
