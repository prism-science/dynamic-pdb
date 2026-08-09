// Package config reads and writes the Dynamic PDB client configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dynamic-pdb/cli/internal/paths"

	"github.com/BurntSushi/toml"
)

const (
	DefaultServerURL      = "https://dynamicpdb.com/api"
	DefaultGitHubClientID = "Ov23liRjWaMI9Ur5X57e"
)

type Config struct {
	Server Server `toml:"server,omitempty"`
	GitHub GitHub `toml:"github,omitempty"`
	Auth   Auth   `toml:"auth,omitempty"`
}

type Server struct {
	URL string `toml:"url,omitempty"`
}

type GitHub struct {
	ClientID string `toml:"client_id,omitempty"`
}

type Auth struct {
	TokenType   string    `toml:"token_type,omitempty"`
	AccessToken string    `toml:"access_token,omitempty"`
	ExpiresAt   time.Time `toml:"expires_at,omitempty"`
	Name        string    `toml:"name,omitempty"`
	Email       string    `toml:"email,omitempty"`
	Login       string    `toml:"login,omitempty"`
}

func (c Config) ServerURL() string {
	if c.Server.URL != "" {
		return c.Server.URL
	}
	return DefaultServerURL
}

func (c Config) GitHubClientID() string {
	if c.GitHub.ClientID != "" {
		return c.GitHub.ClientID
	}
	return DefaultGitHubClientID
}

func Load(dataHome string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(paths.ConfigPath(dataHome), &cfg); err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("config: decode: %w", err)
	}
	cfg.Server.URL = strings.TrimRight(cfg.Server.URL, "/")
	return cfg, nil
}

func Save(dataHome string, cfg Config) error {
	cfg.Server.URL = strings.TrimRight(cfg.Server.URL, "/")
	if cfg.Auth.AccessToken != "" && cfg.Auth.TokenType == "" {
		cfg.Auth.TokenType = "Bearer"
	}

	configPath := paths.ConfigPath(dataHome)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("config: create data directory: %w", err)
	}

	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(cfg); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := writeAtomic(configPath, encoded.Bytes(), 0o600); err != nil {
		return fmt.Errorf("config: save: %w", err)
	}
	return nil
}

func Update(dataHome string, modify func(*Config) error) error {
	cfg, err := Load(dataHome)
	if err != nil {
		return fmt.Errorf("config: load before update: %w", err)
	}
	if err := modify(&cfg); err != nil {
		return fmt.Errorf("config: modify: %w", err)
	}
	if err := Save(dataHome, cfg); err != nil {
		return fmt.Errorf("config: persist update: %w", err)
	}
	return nil
}

func ClearAuth(dataHome string) error {
	if err := Update(dataHome, func(cfg *Config) error {
		cfg.Auth = Auth{}
		return nil
	}); err != nil {
		return fmt.Errorf("config: clear auth: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()

	if _, err := temporary.Write(data); err != nil {
		cleanupErr := closeAndRemove(temporary, temporaryPath)
		return fmt.Errorf("write temporary file: %w", errors.Join(err, cleanupErr))
	}
	if err := temporary.Chmod(mode); err != nil {
		cleanupErr := closeAndRemove(temporary, temporaryPath)
		return fmt.Errorf("set temporary file permissions: %w", errors.Join(err, cleanupErr))
	}
	if err := temporary.Close(); err != nil {
		removeErr := removeTemporary(temporaryPath)
		return fmt.Errorf("close temporary file: %w", errors.Join(err, removeErr))
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		removeErr := removeTemporary(temporaryPath)
		return fmt.Errorf("replace config file: %w", errors.Join(err, removeErr))
	}
	return nil
}

func closeAndRemove(file *os.File, path string) error {
	closeErr := file.Close()
	removeErr := removeTemporary(path)
	return errors.Join(closeErr, removeErr)
}

func removeTemporary(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove temporary file: %w", err)
	}
	return nil
}
