package mmseqs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Commands struct {
	binaryPath string
}

func NewCommands(binaryPath string) (*Commands, error) {
	if strings.TrimSpace(binaryPath) == "" {
		return nil, errors.New("mmseqs binary path is empty")
	}
	return &Commands{binaryPath: binaryPath}, nil
}

func (c *Commands) Version(ctx context.Context) (string, error) {
	output, err := c.run(ctx, "version")
	if err != nil {
		return "", fmt.Errorf("run mmseqs version: %w", err)
	}
	return strings.TrimSpace(output), nil
}

func (c *Commands) CreateDB(ctx context.Context, fastaPath, databasePath string) error {
	if _, err := c.run(ctx, "createdb", fastaPath, databasePath); err != nil {
		return fmt.Errorf("run mmseqs createdb: %w", err)
	}
	return nil
}

func (c *Commands) CreateIndex(ctx context.Context, databasePath, tmpDir string) error {
	if _, err := c.run(ctx, "createindex", databasePath, tmpDir); err != nil {
		return fmt.Errorf("run mmseqs createindex: %w", err)
	}
	return nil
}

func (c *Commands) ConcatDBs(ctx context.Context, firstDatabasePath, secondDatabasePath, outputDatabasePath string) error {
	if _, err := c.run(ctx, "concatdbs", firstDatabasePath, secondDatabasePath, outputDatabasePath); err != nil {
		return fmt.Errorf("run mmseqs concatdbs: %w", err)
	}
	return nil
}

func (c *Commands) EasySearch(
	ctx context.Context,
	queryFastaPath string,
	targetDatabasePath string,
	resultPath string,
	tmpDir string,
) error {
	args := []string{
		"easy-search",
		queryFastaPath,
		targetDatabasePath,
		resultPath,
		tmpDir,
		"--format-output",
		FormatOutput,
		"--max-seqs",
		"200",
		"--min-seq-id",
		"0.3",
		"-e",
		"0.1",
		"-c",
		"0.5",
		"--cov-mode",
		"0",
	}
	if _, err := c.run(ctx, args...); err != nil {
		return fmt.Errorf("run mmseqs easy-search: %w", err)
	}
	return nil
}

func (c *Commands) run(ctx context.Context, args ...string) (string, error) {
	//nolint:gosec // mmseqs binary path is controlled by deployment config and tests.
	command := exec.CommandContext(ctx, c.binaryPath, args...)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return output.String(), fmt.Errorf("%s %s failed: %w: %s", c.binaryPath, strings.Join(args, " "), err, output.String())
	}
	return output.String(), nil
}
