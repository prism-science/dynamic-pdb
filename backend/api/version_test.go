package api

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_the_api_and_schema_versions_from_the_embedded_spec(t *testing.T) {
	// when
	apiVersion, schemaVersion, err := Versions()

	// then
	require.NoError(t, err)
	assert.Regexp(t, `^[0-9]+\.[0-9]+\.[0-9]+$`, apiVersion)
	assert.Regexp(t, `^[0-9]+\.[0-9]+\.[0-9]+$`, schemaVersion)
}

func Test_should_refuse_a_spec_without_both_version_lines(t *testing.T) {
	// given
	spec := []byte("openapi: 3.0.3\ninfo:\n  title: Dynamic PDB API\n  version: 1.1.0\n")

	// when
	_, _, err := versionsOf(spec)

	// then
	require.Error(t, err)
}

// The spec's schema version, the schema doc's file name, its title and its newest change log entry
// must agree, so a release cannot bump one and forget the others.
func Test_should_name_the_same_schema_version_in_the_spec_and_the_schema_doc(t *testing.T) {
	// given
	_, schemaVersion, err := Versions()
	require.NoError(t, err)
	docPath := filepath.Join("..", "..", "docs", "schema", "dynamic-pdb-schema-v"+schemaVersion+".md")

	// when
	doc, err := os.ReadFile(docPath)

	// then
	require.NoError(t, err, "the schema doc for the spec's version")
	title := regexp.MustCompile(`(?m)\A# Dynamic PDB Schema v([0-9]+\.[0-9]+\.[0-9]+)\s*$`).FindSubmatch(doc)
	require.NotNil(t, title, "the doc's first line is its title with its version")
	assert.Equal(t, schemaVersion, string(title[1]))
	newest := regexp.MustCompile(`(?m)^## Change log\s+- \*\*v([0-9]+\.[0-9]+\.[0-9]+) `).FindSubmatch(doc)
	require.NotNil(t, newest, "the change log's first entry")
	assert.Equal(t, schemaVersion, string(newest[1]))
}
