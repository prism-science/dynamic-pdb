package db_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modeledCompoundSetsBackfillMigration gives existing model files in our bucket a pending set.
const modeledCompoundSetsBackfillMigration = "V20261030120100__"

// modeledCompoundArtifactsSeed adds, the way the backend did before the sets existed (no set
// row): a model file in our bucket, an archive model file, a structure factor file in our
// bucket, and a model file without a URI.
const modeledCompoundArtifactsSeed = `
insert into artifacts(id, name, level, type, uri, format, metadata, created_by) values
    ('00000000-0000-0000-0000-0000000003a1', 'model.cif', 'L1', 'model',
     'https://bucket.example.org/e1/models/m1/artifacts/00000000-0000-0000-0000-0000000003a1/model.cif',
     'mmcif', '{}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-0000000003a2', '1ABC.cif', 'L1', 'model',
     'https://files.rcsb.org/download/1ABC.cif', 'mmcif', '{}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-0000000003a3', 'data.mtz', 'L1', 'structure_factors',
     'https://bucket.example.org/e1/models/m1/artifacts/00000000-0000-0000-0000-0000000003a3/data.mtz',
     'mtz', '{}', '00000000-0000-0000-0000-0000000000a1'),
    ('00000000-0000-0000-0000-0000000003a4', 'linkless.cif', 'L1', 'model', null,
     'mmcif', '{}', '00000000-0000-0000-0000-0000000000a1');
`

func runModeledCompoundSetsBackfill(t *testing.T, fixture *migratedFixture) {
	t.Helper()
	migrationSQL, err := os.ReadFile(migrationPathWithPrefix(t, modeledCompoundSetsBackfillMigration))
	require.NoError(t, err)
	_, err = fixture.database.Exec(string(migrationSQL))
	require.NoError(t, err)
}

func Test_should_give_only_model_files_in_our_bucket_a_pending_set_when_the_backfill_runs_twice(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	_, err := fixture.database.Exec(modeledCompoundArtifactsSeed)
	require.NoError(t, err)

	// when: a rerun adds nothing.
	runModeledCompoundSetsBackfill(t, fixture)
	runModeledCompoundSetsBackfill(t, fixture)

	// then
	type set struct {
		ArtifactID string `db:"artifact_id"`
		Status     string `db:"status"`
	}
	sets := []set{}
	err = fixture.database.Select(&sets, `select artifact_id, status from modeled_compound_sets
		where artifact_id::text like '00000000-0000-0000-0000-0000000003a%' order by artifact_id`)
	require.NoError(t, err)
	assert.Equal(t, []set{{ArtifactID: "00000000-0000-0000-0000-0000000003a1", Status: "pending"}}, sets)
}
