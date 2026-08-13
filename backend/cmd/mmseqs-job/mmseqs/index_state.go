package mmseqs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

//nolint:revive // MMseqsIndexState is the domain name used by the job state file.
type MMseqsIndexState struct {
	LatestCompletedRunID *uuid.UUID `json:"latest_completed_run_id,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func ReadIndexState(cacheDir string) (*MMseqsIndexState, error) {
	data, err := os.ReadFile(IndexStatePath(cacheDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read mmseqs index state: %w", err)
	}

	var value MMseqsIndexState
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode mmseqs index state: %w", err)
	}
	return &value, nil
}

func WriteIndexState(cacheDir string, value MMseqsIndexState) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode mmseqs index state: %w", err)
	}
	data = append(data, '\n')

	path := IndexStatePath(cacheDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create mmseqs index state dir: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("write mmseqs index state temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish mmseqs index state: %w", err)
	}
	return nil
}

func IndexStatePath(cacheDir string) string {
	return filepath.Join(cacheDir, "current", "mmseqs_index_state.json")
}
