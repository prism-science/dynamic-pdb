package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_should_return_local_env_when_dynamic_pdb_env_is_empty(t *testing.T) {
	// given
	t.Setenv("DYNAMIC_PDB_ENV", "")

	// when
	env := envFromEnvironment()

	// then
	assert.Equal(t, "local", env)
}

func Test_should_return_configured_env_when_dynamic_pdb_env_is_set(t *testing.T) {
	// given
	t.Setenv("DYNAMIC_PDB_ENV", "production")

	// when
	env := envFromEnvironment()

	// then
	assert.Equal(t, "production", env)
}

func Test_should_return_default_mmseqs_cache_dir_when_env_is_empty(t *testing.T) {
	// given
	t.Setenv("DYNAMIC_PDB_MMSEQS_CACHE_DIR", "")

	// when
	cacheDir := mmseqsCacheDirFromEnvironment()

	// then
	assert.Equal(t, ".tmp/mmseqs", cacheDir)
}

func Test_should_return_configured_mmseqs_cache_dir_when_env_is_set(t *testing.T) {
	// given
	t.Setenv("DYNAMIC_PDB_MMSEQS_CACHE_DIR", "/cache/mmseqs")

	// when
	cacheDir := mmseqsCacheDirFromEnvironment()

	// then
	assert.Equal(t, "/cache/mmseqs", cacheDir)
}
