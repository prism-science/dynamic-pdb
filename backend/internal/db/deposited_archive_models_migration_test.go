package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mistypedArchiveRevision      = "00000000-0000-0000-0000-0000000001a1"
	mistypedArchiveOldRevision   = "00000000-0000-0000-0000-0000000001a2"
	depositorSingleConformer     = "00000000-0000-0000-0000-0000000001a3"
	secondArchiveLookalike       = "00000000-0000-0000-0000-0000000001a4"
	realDepositedNextToLookalike = "00000000-0000-0000-0000-0000000001a5"
)

// mistypedArchiveModelsSeed holds an archive model typed Single Conformer (with an archived older
// revision), a depositor's Single Conformer model, and an entry whose "Deposited model" lookalike
// sits next to a real Deposited model.
const mistypedArchiveModelsSeed = `
insert into entries(id, created_by) values
    ('e_mistyped', '00000000-0000-0000-0000-0000000000a1'),
    ('e_depositor', '00000000-0000-0000-0000-0000000000a1'),
    ('e_both', '00000000-0000-0000-0000-0000000000a1');
insert into models(id, entry_id, created_by) values
    ('m_mistyped', 'e_mistyped', '00000000-0000-0000-0000-0000000000a1'),
    ('m_depositor', 'e_depositor', '00000000-0000-0000-0000-0000000000a1'),
    ('m_lookalike', 'e_both', '00000000-0000-0000-0000-0000000000a1'),
    ('m_deposited', 'e_both', '00000000-0000-0000-0000-0000000000a1');
insert into model_revisions(id, model_id, state, title, metadata, created_by) values
    ('` + mistypedArchiveRevision + `', 'm_mistyped', 'active', 'Deposited model',
     '{"external_refs":{"pdb":"5R8T"},"model_type":"Single Conformer","purpose":"Model Building"}',
     '00000000-0000-0000-0000-0000000000a1'),
    ('` + mistypedArchiveOldRevision + `', 'm_mistyped', 'archived', 'Deposited model',
     '{"external_refs":{"pdb":"5R8T"},"model_type":"Single Conformer","purpose":"Model Building"}',
     '00000000-0000-0000-0000-0000000000a1'),
    ('` + depositorSingleConformer + `', 'm_depositor', 'active', 'My refinement',
     '{"external_refs":{"pdb":"5R8U"},"model_type":"Single Conformer","purpose":"Model Building"}',
     '00000000-0000-0000-0000-0000000000a1'),
    ('` + secondArchiveLookalike + `', 'm_lookalike', 'active', 'Deposited model',
     '{"external_refs":{"pdb":"5R8V"},"model_type":"Single Conformer","purpose":"Model Building"}',
     '00000000-0000-0000-0000-0000000000a1'),
    ('` + realDepositedNextToLookalike + `', 'm_deposited', 'active', 'Deposited model',
     '{"external_refs":{"pdb":"5R8V"},"model_type":"Deposited","purpose":"Reference"}',
     '00000000-0000-0000-0000-0000000000a1');
insert into entry_search_index(entry_id, model_type, model_id, updated_at, search_text, search_tsv) values
    ('e_mistyped', 'model_revision', '` + mistypedArchiveRevision + `', now(),
     'Deposited model Doe, J. Model Building Single Conformer',
     to_tsvector('simple', 'Deposited model Doe, J. Model Building Single Conformer'));
`

func Test_should_retype_active_archive_model_as_deposited_reference_when_it_is_typed_single_conformer(t *testing.T) {
	// given
	fixture := newMigratedFixtureWithExtraSeed(t, mistypedArchiveModelsSeed)

	// when
	metadata := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, mistypedArchiveRevision)
	var searchText string
	require.NoError(t, fixture.database.Get(&searchText,
		`select search_text from entry_search_index where model_id = $1`, mistypedArchiveRevision))
	var foundByDeposited bool
	require.NoError(t, fixture.database.Get(&foundByDeposited,
		`select search_tsv @@ plainto_tsquery('simple', 'Deposited Reference') from entry_search_index where model_id = $1`,
		mistypedArchiveRevision))

	// then
	assert.Equal(t, "Deposited", metadata["model_type"])
	assert.Equal(t, "Reference", metadata["purpose"])
	assert.Equal(t, "5R8T", metadata["external_refs"].(map[string]any)["pdb"])
	assert.Equal(t, "Deposited model Doe, J. Reference Deposited", searchText)
	assert.True(t, foundByDeposited)
}

func Test_should_keep_history_and_other_models_when_archive_models_are_retyped(t *testing.T) {
	// given
	fixture := newMigratedFixtureWithExtraSeed(t, mistypedArchiveModelsSeed)

	// when
	oldRevision := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, mistypedArchiveOldRevision)
	depositorModel := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, depositorSingleConformer)
	lookalike := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, secondArchiveLookalike)

	// then
	assert.Equal(t, "Single Conformer", oldRevision["model_type"])
	assert.Equal(t, "Single Conformer", depositorModel["model_type"])
	assert.Equal(t, "Single Conformer", lookalike["model_type"])
}
