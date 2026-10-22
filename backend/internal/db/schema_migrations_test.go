package db_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firstSchemaCleanupMigration is the first C1 migration; everything before it is the
// "old shape" the fixture is seeded in.
const firstSchemaCleanupMigration = "V20261023090000__"

const (
	fixtureUser           = "00000000-0000-0000-0000-0000000000a1"
	fixtureArtifact       = "00000000-0000-0000-0000-0000000000b1"
	fixtureSequenceBase   = "00000000-0000-0000-0000-00000000c0"
	fixtureEntityBase     = "00000000-0000-0000-0000-00000000d0"
	fixtureRevisionBase   = "00000000-0000-0000-0000-00000000e0"
	fixtureModelRevBase   = "00000000-0000-0000-0000-00000000f0"
	fixtureMetricBase     = "00000000-0000-0000-0000-00000000a0"
	fixtureRunWithVersion = "00000000-0000-0000-0000-0000000000c1"
	fixtureRunNoVersion   = "00000000-0000-0000-0000-0000000000c2"
)

// oldShapeFixture is data as the pre-C1 code wrote it. Entry ids: e_pdb has a PDB ID,
// e_free has none, e_known already holds entry-level lists, e_plain has no model lists.
const oldShapeFixture = `
insert into users(id, source, external_ref, created_at, updated_at)
values ('` + fixtureUser + `', 'test', 'fixture', now(), now());
insert into artifacts(id, name, level, sha256, size_bytes, created_by)
values ('` + fixtureArtifact + `', 'seq', 'L0', 'ABC', -1, '` + fixtureUser + `');
insert into artifacts(id, name, level, sha256, created_by)
values ('00000000-0000-0000-0000-0000000000b2', 'good', 'L2', repeat('a', 64), '` + fixtureUser + `');

insert into entries(id, created_by) values ('e_pdb', '` + fixtureUser + `'), ('e_free', '` + fixtureUser + `'),
    ('e_known', '` + fixtureUser + `'), ('e_plain', '` + fixtureUser + `');
insert into entry_revisions(id, entry_id, state, title, metadata, created_by) values
    ('` + fixtureRevisionBase + `01', 'e_pdb', 'active', 'pdb', '{"external_refs":{"pdb":"1ABC"}}', '` + fixtureUser + `'),
    ('` + fixtureRevisionBase + `02', 'e_free', 'active', 'free', '{"method":"X-ray"}', '` + fixtureUser + `'),
    ('` + fixtureRevisionBase + `03', 'e_known', 'active', 'known', '{"ligands":["GOL"]}', '` + fixtureUser + `'),
    ('` + fixtureRevisionBase + `04', 'e_plain', 'active', 'plain', '{}', '` + fixtureUser + `'),
    ('` + fixtureRevisionBase + `05', 'e_free', 'archived', 'free old', '{}', '` + fixtureUser + `');

insert into models(id, entry_id, created_by, created_at) values
    ('m_pdb', 'e_pdb', '` + fixtureUser + `', '2026-01-01'),
    ('m_free_1', 'e_free', '` + fixtureUser + `', '2026-01-01'),
    ('m_free_2', 'e_free', '` + fixtureUser + `', '2026-01-02'),
    ('m_known', 'e_known', '` + fixtureUser + `', '2026-01-01'),
    ('m_plain', 'e_plain', '` + fixtureUser + `', '2026-01-01');
insert into model_revisions(id, model_id, state, title, metadata, created_by) values
    ('` + fixtureModelRevBase + `01', 'm_pdb', 'active', 'm', '{"model_type":"Deposited","purpose":"Reference","ligands":["HEM"],"cofactors":["ZN"]}', '` + fixtureUser + `'),
    ('` + fixtureModelRevBase + `02', 'm_free_1', 'active', 'm', '{"model_type":"Refinement","purpose":"Model Building","ligands":["ZN","ATP"],"cofactors":[]}', '` + fixtureUser + `'),
    ('` + fixtureModelRevBase + `03', 'm_free_2', 'active', 'm', '{"model_type":"Ensemble","purpose":"Bogus","ligands":["ATP","MG","ZN"]}', '` + fixtureUser + `'),
    ('` + fixtureModelRevBase + `04', 'm_known', 'active', 'm', '{"model_type":"Multiconformer","ligands":["NAG"]}', '` + fixtureUser + `'),
    ('` + fixtureModelRevBase + `05', 'm_plain', 'active', 'm', '{}', '` + fixtureUser + `');

insert into runs(id, name, software_name, software_version, created_by) values
    ('` + fixtureRunWithVersion + `', 'refine', 'phenix', '1.21', '` + fixtureUser + `'),
    ('` + fixtureRunNoVersion + `', 'build', 'coot', null, '` + fixtureUser + `');
insert into model_revision_runs(model_revision_id, run_id) values ('` + fixtureModelRevBase + `04', '` + fixtureRunWithVersion + `');
insert into run_artifacts(run_id, artifact_id, direction) values
    ('` + fixtureRunWithVersion + `', '00000000-0000-0000-0000-0000000000b2', 'input');

insert into metrics(id, key, value) values
    ('` + fixtureMetricBase + `01', 'r_free', 0.2),
    ('` + fixtureMetricBase + `02', 'r_free', 0.3),
    ('` + fixtureMetricBase + `03', 'r_work', 0.18),
    ('` + fixtureMetricBase + `04', 'made_up', 1);
insert into model_revision_metrics(model_revision_id, metric_id) values
    ('` + fixtureModelRevBase + `02', '` + fixtureMetricBase + `01'),
    ('` + fixtureModelRevBase + `02', '` + fixtureMetricBase + `02'),
    ('` + fixtureModelRevBase + `02', '` + fixtureMetricBase + `03'),
    ('` + fixtureModelRevBase + `03', '` + fixtureMetricBase + `04');

insert into protein_sequences(id, entry_revision_id, source_artifact_id, record_index, header, sequence)
select ('` + fixtureSequenceBase + `' || lpad(n::text, 2, '0'))::uuid, '` + fixtureRevisionBase + `01',
       '` + fixtureArtifact + `', n, 'h', 'A' from generate_series(1, 8) n;
insert into polymer_entities(id, protein_sequence_id, metadata)
select ('` + fixtureEntityBase + `' || lpad(n::text, 2, '0'))::uuid,
       ('` + fixtureSequenceBase + `' || lpad(n::text, 2, '0'))::uuid, metadata
from (values
    (1, '{"mutations":"C54T, C97A"}'::jsonb),
    (2, '{"mutations":"ENGINEERED MUTATION"}'),
    (3, '{"mutations":"C54T;DELETION,A(-3)G, K7R"}'),
    (4, '{"mutations":""}'),
    (5, '{"description":"no mutations key"}'),
    (6, '{"mutations":["C54T","X"]}'),
    (7, '{"mutations":["C54T"],"uniprot_mappings":[{"accession":"P1","source":"sifts"},{"accession":"P1","source":"alignment","unp_release":"2026_01"}]}'),
    (8, '{"mutations":"R1Q","uniprot_mappings":[{"accession":"P2","source":"struct_ref"}]}')
) as seeded(n, metadata);
`

type migratedFixture struct {
	database *sqlx.DB
}

func newMigratedFixture(t *testing.T) *migratedFixture {
	t.Helper()

	schemaName := "migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	adminDB, err := sqlx.Connect("postgres", dbTestDSN("sslmode=disable"))
	require.NoError(t, err)
	_, err = adminDB.Exec(fmt.Sprintf(`create schema "%s"`, schemaName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := adminDB.Exec(fmt.Sprintf(`drop schema "%s" cascade`, schemaName))
		assert.NoError(t, dropErr)
		assert.NoError(t, adminDB.Close())
	})

	database, err := sqlx.Connect("postgres", dbTestDSN("sslmode=disable search_path="+schemaName))
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })

	paths, err := schemaMigrationPaths()
	require.NoError(t, err)
	seeded := false
	for _, path := range paths {
		if !seeded && strings.HasPrefix(filepath.Base(path), firstSchemaCleanupMigration) {
			_, err := database.Exec(oldShapeFixture)
			require.NoError(t, err, "seed old-shape fixture")
			seeded = true
		}
		migrationSQL, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = database.Exec(string(migrationSQL))
		require.NoError(t, err, "apply %s", filepath.Base(path))
	}
	require.True(t, seeded, "first C1 migration not found")
	return &migratedFixture{database: database}
}

func (fixture *migratedFixture) jsonbAt(t *testing.T, query string, args ...any) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, fixture.database.Get(&raw, query, args...))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	return decoded
}

func (fixture *migratedFixture) reviewDetails(t *testing.T, objectType, objectID, issue string) map[string]any {
	t.Helper()
	return fixture.jsonbAt(t,
		`select details from schema_migration_review where object_type = $1 and object_id = $2 and issue = $3`,
		objectType, objectID, issue)
}

func (fixture *migratedFixture) reviewCount(t *testing.T, objectType, objectID string) int {
	t.Helper()
	var count int
	require.NoError(t, fixture.database.Get(&count,
		`select count(*) from schema_migration_review where object_type = $1 and object_id = $2`, objectType, objectID))
	return count
}

func Test_should_flag_pdb_entry_and_keep_lists_unset_when_ligands_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	entryMetadata := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, fixtureRevisionBase+"01")

	// then
	assert.NotContains(t, entryMetadata, "ligands")
	assert.NotContains(t, entryMetadata, "cofactors")
	details := fixture.reviewDetails(t, "entry", "e_pdb", "ligands_cofactors_rederive_from_archive")
	modelLists := details["model_lists"].([]any)
	require.Len(t, modelLists, 1)
	assert.Equal(t, []any{"HEM"}, modelLists[0].(map[string]any)["ligands"])
	assert.Equal(t, []any{"ZN"}, modelLists[0].(map[string]any)["cofactors"])
}

func Test_should_merge_model_lists_onto_active_revision_when_entry_has_no_pdb_id(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	active := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, fixtureRevisionBase+"02")
	archived := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, fixtureRevisionBase+"05")

	// then
	assert.Equal(t, []any{"ZN", "ATP", "MG"}, active["ligands"])
	assert.Equal(t, []any{}, active["cofactors"], "one model knew it was empty, the other did not say")
	assert.Equal(t, "X-ray", active["method"])
	assert.NotContains(t, archived, "ligands", "immutable history is untouched")
	review := fixture.reviewDetails(t, "entry", "e_free", "ligands_cofactors_merged_from_models")
	assert.Equal(t, []any{"ZN", "ATP", "MG"}, review["merged_ligands"])

	var activeRevisions int
	require.NoError(t, fixture.database.Get(&activeRevisions,
		`select count(*) from entry_revisions where entry_id = 'e_free' and state = 'active'`))
	assert.Equal(t, 1, activeRevisions)
}

func Test_should_not_overwrite_entry_lists_when_entry_revision_already_has_them(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	metadata := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, fixtureRevisionBase+"03")

	// then
	assert.Equal(t, []any{"GOL"}, metadata["ligands"])
	assert.NotContains(t, metadata, "cofactors")
	review := fixture.reviewDetails(t, "entry", "e_known", "ligands_cofactors_merged_from_models")
	assert.Nil(t, review["merged_ligands"])
}

func Test_should_leave_entry_untouched_when_no_model_has_lists(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	metadata := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, fixtureRevisionBase+"04")

	// then
	assert.Empty(t, metadata)
	assert.Zero(t, fixture.reviewCount(t, "entry", "e_plain"))
}

func Test_should_keep_model_revision_lists_when_ligands_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	first := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, fixtureModelRevBase+"02")
	second := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, fixtureModelRevBase+"03")
	pdb := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, fixtureModelRevBase+"01")

	// then
	assert.Equal(t, []any{"ZN", "ATP"}, first["ligands"])
	assert.Equal(t, []any{}, first["cofactors"])
	assert.Equal(t, []any{"ATP", "MG", "ZN"}, second["ligands"])
	assert.Equal(t, []any{"HEM"}, pdb["ligands"])
	assert.Equal(t, []any{"ZN"}, pdb["cofactors"])
}

func Test_should_structure_mutations_with_raw_and_status_when_polymer_entities_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	type expectation struct {
		number    string
		mutations []any
		raw       any
		status    any
	}
	expectations := []expectation{
		{"01", []any{"C54T", "C97A"}, "C54T, C97A", "parsed"},
		{"02", []any{}, "ENGINEERED MUTATION", "needs_review"},
		{"03", []any{"C54T", "K7R"}, "C54T;DELETION,A(-3)G, K7R", "needs_review"},
		{"04", []any{}, "", "none"},
		{"06", []any{"C54T", "X"}, nil, "needs_review"},
		{"08", []any{"R1Q"}, "R1Q", "parsed"},
	}

	for _, want := range expectations {
		// when
		metadata := fixture.jsonbAt(t, `select metadata from polymer_entities where id = $1`, fixtureEntityBase+want.number)

		// then
		assert.Equal(t, want.mutations, metadata["mutations"], "entity %s", want.number)
		assert.Equal(t, want.raw, metadata["mutations_raw"], "entity %s", want.number)
		assert.Equal(t, want.status, metadata["mutations_status"], "entity %s", want.number)
	}
	unknown := fixture.jsonbAt(t, `select metadata from polymer_entities where id = $1`, fixtureEntityBase+"05")
	assert.Equal(t, map[string]any{"description": "no mutations key"}, unknown)
	assert.Equal(t, 1, fixture.reviewCount(t, "polymer_entity", fixtureEntityBase+"03"))
	assert.Equal(t, 0, fixture.reviewCount(t, "polymer_entity", fixtureEntityBase+"01"))
	details := fixture.reviewDetails(t, "polymer_entity", fixtureEntityBase+"02", "mutations_need_review")
	assert.Equal(t, "ENGINEERED MUTATION", details["mutations_raw"])
}

func Test_should_quarantine_duplicate_metric_keys_and_enforce_uniqueness_when_metrics_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	var keyed []string
	require.NoError(t, fixture.database.Select(&keyed,
		`select coalesce(metric_key, '-') from model_revision_metrics order by metric_id`))
	_, duplicateInsertErr := fixture.database.Exec(`insert into metrics(id, key, value) values ($1, 'r_work', 1)`,
		"00000000-0000-0000-0000-0000000000a9")
	require.NoError(t, duplicateInsertErr)
	_, linkErr := fixture.database.Exec(`insert into model_revision_metrics(model_revision_id, metric_id) values ($1, $2)`,
		fixtureModelRevBase+"02", "00000000-0000-0000-0000-0000000000a9")

	// then
	assert.Equal(t, []string{"-", "-", "r_work", "made_up"}, keyed, "both r_free rows stay in place, unindexed")
	require.Error(t, linkErr)
	assert.Contains(t, linkErr.Error(), "model_revision_metrics_revision_key_idx")
	details := fixture.reviewDetails(t, "model_revision", fixtureModelRevBase+"02", "duplicate_metric_key:r_free")
	assert.Len(t, details["metrics"], 2)
}

func Test_should_add_constraints_not_valid_when_stable_invariants_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	wantNotValid := []string{
		"metrics_key_known_chk", "model_revisions_model_type_chk", "model_revisions_purpose_chk",
		"entry_revisions_composition_lists_chk", "artifacts_sha256_format_chk", "artifacts_size_bytes_chk",
		"polymer_entities_uniprot_mappings_chk", "run_artifacts_direction_chk", "run_artifacts_position_chk",
	}

	// when
	var unvalidated []string
	require.NoError(t, fixture.database.Select(&unvalidated, `
		select conname from pg_constraint
		where connamespace = current_schema()::regnamespace and contype = 'c' and not convalidated
		order by conname`))

	// then
	assert.ElementsMatch(t, wantNotValid, unvalidated)
}

func Test_should_reject_new_violations_when_not_valid_constraints_exist(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	statements := map[string]string{
		"metric key":     `insert into metrics(id, key, value) values (gen_random_uuid(), 'nope', 1)`,
		"sha256":         `insert into artifacts(id, name, level, sha256, created_by) values (gen_random_uuid(), 'a', 'L0', repeat('A', 64), '` + fixtureUser + `')`,
		"size":           `insert into artifacts(id, name, level, size_bytes, created_by) values (gen_random_uuid(), 'a', 'L0', -5, '` + fixtureUser + `')`,
		"model type":     `update model_revisions set metadata = '{"model_type":"Refinement"}' where id = '` + fixtureModelRevBase + `05'`,
		"sifts release":  `insert into polymer_entities(id, protein_sequence_id, metadata) values (gen_random_uuid(), '` + fixtureSequenceBase + `01', '{"uniprot_mappings":[{"accession":"P1","source":"sifts"}]}')`,
		"alignment":      `insert into polymer_entities(id, protein_sequence_id, metadata) values (gen_random_uuid(), '` + fixtureSequenceBase + `01', '{"uniprot_mappings":[{"accession":"P1","source":"alignment"}]}')`,
		"unknown source": `insert into polymer_entities(id, protein_sequence_id, metadata) values (gen_random_uuid(), '` + fixtureSequenceBase + `01', '{"uniprot_mappings":[{"accession":"P1","source":"blast","unp_release":"1"}]}')`,
	}

	for name, statement := range statements {
		// when
		_, err := fixture.database.Exec(statement)

		// then
		assert.Error(t, err, name)
	}
}

func Test_should_accept_alignment_source_with_release_when_uniprot_constraint_applies(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	_, err := fixture.database.Exec(`insert into polymer_entities(id, protein_sequence_id, metadata)
		values (gen_random_uuid(), $1, '{"uniprot_mappings":[
			{"accession":"P1","source":"alignment","unp_release":"2026_01"},
			{"accession":"P1","source":"struct_ref"}]}')`, fixtureSequenceBase+"01")

	// then
	require.NoError(t, err)
}

func Test_should_allow_artifact_as_input_and_output_when_run_artifacts_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	_, outputErr := fixture.database.Exec(`insert into run_artifacts(run_id, artifact_id, direction, position)
		values ($1, '00000000-0000-0000-0000-0000000000b2', 'output', 0)`, fixtureRunWithVersion)
	_, duplicateErr := fixture.database.Exec(`insert into run_artifacts(run_id, artifact_id, direction)
		values ($1, '00000000-0000-0000-0000-0000000000b2', 'output')`, fixtureRunWithVersion)

	// then
	require.NoError(t, outputErr)
	require.Error(t, duplicateErr)
}

func Test_should_keep_one_active_revision_index_when_migrations_applied(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	var indexes []string
	require.NoError(t, fixture.database.Select(&indexes, `
		select indexname from pg_indexes
		where schemaname = current_schema() and indexdef like '%UNIQUE%' and indexdef like '%state = ''active''%'
		  and tablename in ('entry_revisions', 'model_revisions') and indexname like '%\_active\_idx'
		order by indexname`))

	// then
	assert.Equal(t, []string{"entry_revisions_active_idx", "model_revisions_active_idx"}, indexes)
}

type inventoryRow struct {
	CheckName  string         `db:"check_name"`
	ObjectType string         `db:"object_type"`
	ObjectID   sql.NullString `db:"object_id"`
	Details    []byte         `db:"details"`
	CheckCount sql.NullInt64  `db:"check_count"`
}

func Test_should_list_every_violation_in_read_only_transaction_when_inventory_runs(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	inventorySQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "inventory", "schema_inventory.sql"))
	require.NoError(t, err)

	// when
	transaction, err := fixture.database.BeginTxx(context.Background(), &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	var rows []inventoryRow
	selectErr := transaction.Select(&rows, string(inventorySQL))
	require.NoError(t, transaction.Rollback())
	require.NoError(t, selectErr)

	// then
	counts := map[string]int64{}
	idsByCheck := map[string][]string{}
	for _, row := range rows {
		counts[row.CheckName] = row.CheckCount.Int64
		idsByCheck[row.CheckName] = append(idsByCheck[row.CheckName], row.ObjectID.String)
	}
	assert.Equal(t, map[string]int64{
		"metric_unknown_key":                     1,
		"metric_missing_software":                0,
		"metric_duplicate_key":                   1,
		"model_invalid_type":                     1,
		"model_invalid_purpose":                  1,
		"run_missing_software_version":           1,
		"computed_model_without_run":             2,
		"artifact_malformed_sha256":              1,
		"artifact_negative_size":                 1,
		"model_revision_has_composition_lists":   4,
		"entry_composition_lists_unresolved":     1,
		"polymer_entity_mutations_unstructured":  3,
		"polymer_entity_uniprot_mapping_invalid": 1,
	}, counts)
	assert.Equal(t, []string{fixtureMetricBase + "04"}, idsByCheck["metric_unknown_key"])
	assert.Equal(t, []string{fixtureModelRevBase + "02"}, idsByCheck["metric_duplicate_key"])
	assert.Equal(t, []string{fixtureModelRevBase + "03"}, idsByCheck["model_invalid_purpose"])
	assert.Equal(t, []string{fixtureModelRevBase + "02"}, idsByCheck["model_invalid_type"], "Refinement is a purpose, not a model type")
	assert.Equal(t, []string{fixtureRunNoVersion}, idsByCheck["run_missing_software_version"])
	assert.Equal(t, []string{"e_pdb"}, idsByCheck["entry_composition_lists_unresolved"])
	assert.Equal(t, "n/a until Phase D", metricSoftwareStatus(t, rows))
}

func metricSoftwareStatus(t *testing.T, rows []inventoryRow) string {
	t.Helper()
	for _, row := range rows {
		if row.CheckName != "metric_missing_software" {
			continue
		}
		var details map[string]string
		require.NoError(t, json.Unmarshal(row.Details, &details))
		return details["status"]
	}
	require.Fail(t, "metric_missing_software row missing")
	return ""
}
