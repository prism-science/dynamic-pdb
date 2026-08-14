package mmseqs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_run_easy_search_with_similarity_limits_when_command_called(t *testing.T) {
	// given
	argsPath := filepath.Join(t.TempDir(), "args.txt")
	binaryPath := filepath.Join(t.TempDir(), "mmseqs")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$@" > "` + argsPath + `"
result_path="$4"
mkdir -p "$(dirname "$result_path")"
: > "$result_path"
`
	require.NoError(t, os.WriteFile(binaryPath, []byte(script), 0o755))
	commands, err := NewCommands(binaryPath)
	require.NoError(t, err)
	resultPath := filepath.Join(t.TempDir(), "result.tsv")

	// when
	err = commands.EasySearch(
		context.Background(),
		"query.fasta",
		"target-db",
		resultPath,
		"tmp",
	)

	// then
	require.NoError(t, err)
	data, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Equal(t, []string{
		"easy-search",
		"query.fasta",
		"target-db",
		resultPath,
		"tmp",
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
	}, args)
}
