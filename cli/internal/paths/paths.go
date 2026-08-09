package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func DataHome() (string, error) {
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "dynamic-pdb"), nil
	}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory; set XDG_DATA_HOME: %w", err)
	}
	if userHome == "" {
		return "", errors.New("could not determine home directory; set XDG_DATA_HOME")
	}
	return filepath.Join(userHome, ".local", "share", "dynamic-pdb"), nil
}

func ConfigPath(dataHome string) string {
	return filepath.Join(dataHome, "config.toml")
}
