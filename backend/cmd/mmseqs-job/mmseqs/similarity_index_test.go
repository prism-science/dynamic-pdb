package mmseqs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_merge_existing_and_partial_similarity_index_with_headers_when_building_full_index(t *testing.T) {
	// given
	cacheDir := t.TempDir()
	runID := uuid.New()
	mmseqsBinary := createHeaderAwareFakeMMseqsBinary(t)
	commands, err := NewCommands(mmseqsBinary)
	require.NoError(t, err)

	existingIndexPath := filepath.Join(t.TempDir(), "existing", "index")
	require.NoError(t, os.MkdirAll(filepath.Dir(existingIndexPath), 0o755))
	require.NoError(t, os.WriteFile(existingIndexPath, []byte("existing-db\n"), 0o644))
	require.NoError(t, os.WriteFile(existingIndexPath+"_h", []byte("existing-header\n"), 0o644))
	require.NoError(t, os.WriteFile(existingIndexPath+".ids", []byte("existing-sequence\n"), 0o644))
	require.NoError(t, os.WriteFile(existingIndexPath+"_h.ids", []byte("existing-header\n"), 0o644))

	sequenceID := uuid.New()
	sequenceFilePath := filepath.Join(t.TempDir(), "sequences.fasta")
	require.NoError(t, os.WriteFile(sequenceFilePath, []byte(">"+sequenceID.String()+"\nACDEFGHIK\n"), 0o644))

	builder, err := NewSimilarityIndexBuilder(commands, cacheDir, runID)
	require.NoError(t, err)

	// when
	result, err := builder.BuildSimilarityIndex(context.Background(), sequenceFilePath, existingIndexPath)

	// then
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, filepath.Join(cacheDir, "runs", runID.String(), "similarity-index", "index"), result.SimilarityIndexPath)
	assert.Equal(t, filepath.Join(cacheDir, "runs", runID.String(), "similarity-search.tsv"), result.SearchResultPath)
	assert.FileExists(t, result.SimilarityIndexPath)
	assert.FileExists(t, result.SimilarityIndexPath+"_h")

	headerContents, err := os.ReadFile(result.SimilarityIndexPath + "_h")
	require.NoError(t, err)
	assert.Contains(t, string(headerContents), "existing-header")
	assert.Contains(t, string(headerContents), sequenceID.String())

	searchResultContents, err := os.ReadFile(result.SearchResultPath)
	require.NoError(t, err)
	assert.Contains(t, string(searchResultContents), sequenceID.String()+"\t"+sequenceID.String())
	assert.Contains(t, string(searchResultContents), sequenceID.String()+"\texisting-sequence")
}

func Test_should_return_existing_full_similarity_index_when_build_already_completed(t *testing.T) {
	// given
	cacheDir := t.TempDir()
	runID := uuid.New()
	mmseqsBinary := createHeaderAwareFakeMMseqsBinary(t)
	commands, err := NewCommands(mmseqsBinary)
	require.NoError(t, err)

	runDir := filepath.Join(cacheDir, "runs", runID.String())
	fullIndexPath := filepath.Join(runDir, "similarity-index", "index")
	searchResultPath := filepath.Join(runDir, "similarity-search.tsv")
	require.NoError(t, os.MkdirAll(filepath.Dir(fullIndexPath), 0o755))
	require.NoError(t, os.WriteFile(fullIndexPath, []byte("existing-full\n"), 0o644))
	require.NoError(t, os.WriteFile(searchResultPath, []byte("already-complete\n"), 0o644))

	sequenceFilePath := filepath.Join(t.TempDir(), "sequences.fasta")
	require.NoError(t, os.WriteFile(sequenceFilePath, []byte(">"+uuid.NewString()+"\nACDE\n"), 0o644))

	builder, err := NewSimilarityIndexBuilder(commands, cacheDir, runID)
	require.NoError(t, err)

	// when
	result, err := builder.BuildSimilarityIndex(context.Background(), sequenceFilePath, "")

	// then
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, fullIndexPath, result.SimilarityIndexPath)
	assert.Equal(t, searchResultPath, result.SearchResultPath)

	contents, err := os.ReadFile(searchResultPath)
	require.NoError(t, err)
	assert.Equal(t, "already-complete\n", string(contents))
}

func createHeaderAwareFakeMMseqsBinary(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "mmseqs")
	script := `#!/bin/sh
set -eu
command="$1"
shift
case "$command" in
  version)
    echo "fake-mmseqs"
    ;;
  createdb)
    fasta_path="$1"
    database_path="$2"
    mkdir -p "$(dirname "$database_path")"
    awk '/^>/ { sub(/^>/, ""); print $1 }' "$fasta_path" > "$database_path"
    cp "$database_path" "$database_path.ids"
    cp "$database_path" "$database_path""_h"
    cp "$database_path" "$database_path""_h.ids"
    ;;
  createindex)
    database_path="$1"
    tmp_dir="$2"
    mkdir -p "$tmp_dir"
    test -f "$database_path""_h"
    : > "$database_path.lookup"
    ;;
  concatdbs)
    first_database_path="$1"
    second_database_path="$2"
    output_database_path="$3"
    mkdir -p "$(dirname "$output_database_path")"
    cat "$first_database_path" "$second_database_path" > "$output_database_path"
    if [ -f "$first_database_path.ids" ] || [ -f "$second_database_path.ids" ]; then
      cat "$first_database_path.ids" "$second_database_path.ids" > "$output_database_path.ids"
    fi
    ;;
  easy-search)
    query_fasta_path="$1"
    target_database_path="$2"
    result_path="$3"
    tmp_dir="$4"
    mkdir -p "$(dirname "$result_path")" "$tmp_dir"
    awk '/^>/ { sub(/^>/, ""); print $1 }' "$query_fasta_path" > "$tmp_dir/query.ids"
    if [ -f "$target_database_path.ids" ]; then
      target_ids="$target_database_path.ids"
    else
      target_ids="$target_database_path"
    fi
    : > "$result_path"
    while IFS= read -r query_id; do
      while IFS= read -r target_id; do
        printf '%s\t%s\t0.9\t1\t1\t1e-20\t100\t20\t1\t20\t1\t20\tACDEFGHIKLMNPQRSTVWY\tACDEFGHIKLMNPQRSTVWF\n' "$query_id" "$target_id" >> "$result_path"
      done < "$target_ids"
    done < "$tmp_dir/query.ids"
    ;;
  *)
    echo "unexpected fake mmseqs command: $command" >&2
    exit 1
    ;;
esac
`
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(script)+"\n"), 0o755))
	return path
}
