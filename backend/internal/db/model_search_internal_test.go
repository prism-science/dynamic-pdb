package db

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The planner uses an expression index only for a query expression written like the indexed
// one. Each index migration must hold the model search's expression, without its table name.
func TestModelSearchIndexExpressions(t *testing.T) {
	cases := []struct {
		migration  string
		table      string
		expression string
	}{
		{"V20261109120000__model_revisions_active_model_type_idx.sql", "model_revisions", modelTypeExpression},
		{"V20261109120100__model_revisions_active_purpose_idx.sql", "model_revisions", modelPurposeExpression},
		{"V20261109120200__model_revisions_active_archive_idx.sql", "model_revisions", archiveModelExpression},
		{"V20261109120300__entry_revisions_active_resolution_idx.sql", "entry_revisions", entryResolutionExpression},
		{"V20261109120400__entry_revisions_active_ligands_idx.sql", "entry_revisions", entryLigandsExpression},
		{"V20261109120500__entry_revisions_active_cofactors_idx.sql", "entry_revisions", entryCofactorsExpression},
		{"V20261109120600__polymer_entities_source_organisms_idx.sql", "polymer_entities", sourceOrganismsExpression},
		{"V20261109120700__polymer_entities_uniprot_mappings_idx.sql", "polymer_entities", uniProtMappingsExpression},
	}
	for _, tc := range cases {
		t.Run(tc.migration, func(t *testing.T) {
			// given
			contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", "db", "public", "structure", tc.migration))
			require.NoError(t, err)

			// when
			statement := normalizedSQL(withoutSQLComments(string(contents)))
			indexed := normalizedSQL(strings.ReplaceAll(tc.expression, tc.table+".", ""))

			// then
			assert.Contains(t, statement, indexed)
			assert.Contains(t, statement, "on "+tc.table+" ")
		})
	}
}

var sqlWhitespace = regexp.MustCompile(`\s+`)

func normalizedSQL(text string) string {
	text = sqlWhitespace.ReplaceAllString(strings.ToLower(text), " ")
	text = strings.ReplaceAll(text, "( ", "(")
	return strings.ReplaceAll(text, " )", ")")
}

func withoutSQLComments(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
