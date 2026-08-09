// Package manifest builds upload manifest drafts from local data directories.
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Options struct {
	DataRoot   string
	OutputPath string
}

func Init(options Options) (string, Manifest, Stats, error) {
	if strings.TrimSpace(options.DataRoot) == "" {
		return "", Manifest{}, Stats{}, errors.New("data folder is required")
	}

	outputPath := options.OutputPath
	if outputPath == "" {
		outputPath = DefaultFilename
	}
	outputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return "", Manifest{}, Stats{}, fmt.Errorf("resolve output path: %w", err)
	}

	manifest, stats, err := Build(options.DataRoot)
	if err != nil {
		return "", Manifest{}, Stats{}, err
	}
	encoded, err := yaml.Marshal(manifest)
	if err != nil {
		return "", Manifest{}, Stats{}, fmt.Errorf("encode manifest YAML: %w", err)
	}
	if err := os.WriteFile(outputPath, encoded, 0o644); err != nil {
		return "", Manifest{}, Stats{}, fmt.Errorf("write manifest: %w", err)
	}
	return outputPath, manifest, stats, nil
}

func Read(path string) (Manifest, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(contents, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest YAML: %w", err)
	}
	return manifest, nil
}
