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

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
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
	fixtureNewModelRev    = "00000000-0000-0000-0000-0000000000f9"
)

type migratedFixture struct {
	database   *sqlx.DB
	schemaName string
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
	oldShapeFixture, err := os.ReadFile(filepath.Join("..", "..", "migrations", "inventory", "old_shape_fixture.sql"))
	require.NoError(t, err)
	seeded := false
	for _, path := range paths {
		if !seeded && strings.HasPrefix(filepath.Base(path), firstSchemaCleanupMigration) {
			_, err := database.Exec(string(oldShapeFixture))
			require.NoError(t, err, "seed old-shape fixture")
			seeded = true
		}
		migrationSQL, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = database.Exec(string(migrationSQL))
		require.NoError(t, err, "apply %s", filepath.Base(path))
	}
	require.True(t, seeded, "first C1 migration not found")
	return &migratedFixture{database: database, schemaName: schemaName}
}

// appDatabase opens the application repositories on the migrated fixture schema.
func (fixture *migratedFixture) appDatabase(t *testing.T) *db.DB {
	t.Helper()
	appDB, err := db.NewDB(db.Config{
		Host:               "localhost",
		Port:               35432,
		Name:               "dynamic_pdb_local",
		Username:           "postgres",
		Password:           "password",
		ConnectionParams:   "sslmode=disable search_path=" + fixture.schemaName,
		MaxOpenConnections: 1,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, appDB.Close()) })
	return appDB
}

func (fixture *migratedFixture) jsonbAt(t *testing.T, query string, args ...any) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, fixture.database.Get(&raw, query, args...))
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	return decoded
}

func (fixture *migratedFixture) rawBytes(t *testing.T, query string, args ...any) []byte {
	t.Helper()
	var raw []byte
	require.NoError(t, fixture.database.Get(&raw, query, args...))
	return raw
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

func Test_should_not_merge_lists_with_non_string_elements_when_ligands_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	mixed := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, fixtureRevisionBase+"06")
	review := fixture.reviewDetails(t, "entry", "e_mixed", "ligands_cofactors_not_merged")

	// then
	assert.NotContains(t, mixed, "ligands", "[1,null,{...},...] is never turned into strings")
	assert.Equal(t, []any{"ZN"}, mixed["cofactors"], "the other kind is still merged")
	assert.Equal(t, true, review["ligands_not_merged"])
	assert.Equal(t, false, review["cofactors_not_merged"])
	modelLists := review["model_lists"].([]any)
	require.Len(t, modelLists, 1)
	assert.Equal(t,
		[]any{float64(1), nil, map[string]any{"a": float64(1)}, "ATP", []any{"x"}, true},
		modelLists[0].(map[string]any)["ligands"], "original value kept in the review row")
}

func Test_should_flag_string_valued_list_when_ligands_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)

	// when
	entryMetadata := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, fixtureRevisionBase+"07")
	review := fixture.reviewDetails(t, "entry", "e_string", "ligands_cofactors_not_merged")
	modelMetadata := fixture.jsonbAt(t, `select metadata from model_revisions where id = $1`, fixtureModelRevBase+"07")

	// then
	assert.NotContains(t, entryMetadata, "ligands")
	assert.Equal(t, []any{"MG"}, entryMetadata["cofactors"])
	assert.Equal(t, true, review["ligands_not_merged"])
	assert.Equal(t, "ATP", review["model_lists"].([]any)[0].(map[string]any)["ligands"])
	assert.Equal(t, "ATP", modelMetadata["ligands"], "model revision keeps the original value")
}

func Test_should_flag_mutations_that_are_neither_string_nor_array_when_polymer_entities_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	type expectation struct {
		number   string
		original any
	}
	expectations := []expectation{{"09", float64(14)}, {"10", map[string]any{"a": float64(1)}}, {"11", true}}

	for _, want := range expectations {
		// when
		metadata := fixture.jsonbAt(t, `select metadata from polymer_entities where id = $1`, fixtureEntityBase+want.number)
		details := fixture.reviewDetails(t, "polymer_entity", fixtureEntityBase+want.number, "mutations_unsupported_type")

		// then
		assert.Equal(t, want.original, metadata["mutations"], "entity %s is not converted", want.number)
		assert.NotContains(t, metadata, "mutations_raw")
		assert.NotContains(t, metadata, "mutations_status")
		assert.Equal(t, want.original, details["mutations"], "entity %s", want.number)
	}
	nullEntity := fixture.jsonbAt(t, `select metadata from polymer_entities where id = $1`, fixtureEntityBase+"12")
	assert.Nil(t, nullEntity["mutations"])
	assert.Zero(t, fixture.reviewCount(t, "polymer_entity", fixtureEntityBase+"12"), "explicit null means unknown")
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
	assert.Equal(t, []string{
		"r_free#duplicate#" + fixtureMetricBase + "01",
		"r_free#duplicate#" + fixtureMetricBase + "02",
		"r_work",
		"made_up",
	}, keyed, "both r_free rows stay in place under unique quarantine keys")
	require.Error(t, linkErr)
	assert.Contains(t, linkErr.Error(), "model_revision_metrics_revision_key_idx")
	details := fixture.reviewDetails(t, "model_revision", fixtureModelRevBase+"02", "duplicate_metric_key:r_free")
	assert.Len(t, details["metrics"], 2)
}

func Test_should_copy_quarantined_duplicate_metrics_when_revision_is_created(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	appDB := fixture.appDatabase(t)
	_, err := fixture.database.Exec(`insert into model_revisions(id, model_id, state, title, metadata, created_by)
		values ($1, 'm_free_1', 'pending', 'next', '{}', $2)`, fixtureNewModelRev, fixtureUser)
	require.NoError(t, err)

	// when
	copyErr := appDB.Metrics.CopyModelRevisionLinks(context.Background(),
		uuid.MustParse(fixtureModelRevBase+"02"), uuid.MustParse(fixtureNewModelRev))

	// then
	require.NoError(t, copyErr)
	var copied []string
	require.NoError(t, fixture.database.Select(&copied, `
		select metrics.key || ' ' || coalesce(link.metric_key, '-')
		from model_revision_metrics link join metrics on metrics.id = link.metric_id
		where link.model_revision_id = $1 order by metrics.created_at, metrics.id, metrics.value`, fixtureNewModelRev))
	assert.Len(t, copied, 3, "no metric is dropped from the new revision")
	assert.ElementsMatch(t, []string{
		"r_free r_free#duplicate#" + fixtureMetricBase + "01",
		"r_free r_free#duplicate#" + fixtureMetricBase + "02",
		"r_work r_work",
	}, copied)
	var sourceLinks int
	require.NoError(t, fixture.database.Get(&sourceLinks,
		`select count(*) from model_revision_metrics where model_revision_id = $1`, fixtureModelRevBase+"02"))
	assert.Equal(t, 3, sourceLinks, "the source revision is untouched")
}

func Test_should_copy_metrics_except_listed_keys_when_sync_replaces_rcsb_metrics(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	appDB := fixture.appDatabase(t)
	_, err := fixture.database.Exec(`insert into model_revisions(id, model_id, state, title, metadata, created_by)
		values ($1, 'm_free_1', 'pending', 'next', '{}', $2)`, fixtureNewModelRev, fixtureUser)
	require.NoError(t, err)

	// when
	copyErr := appDB.Metrics.CopyModelRevisionLinksExcludingKeys(context.Background(),
		uuid.MustParse(fixtureModelRevBase+"02"), uuid.MustParse(fixtureNewModelRev),
		[]models.MetricKey{models.MetricKeyRWork})

	// then
	require.NoError(t, copyErr)
	var copiedKeys []string
	require.NoError(t, fixture.database.Select(&copiedKeys,
		`select metric_key from model_revision_metrics where model_revision_id = $1 order by metric_key`, fixtureNewModelRev))
	assert.Equal(t, []string{
		"r_free#duplicate#" + fixtureMetricBase + "01",
		"r_free#duplicate#" + fixtureMetricBase + "02",
	}, copiedKeys)
}

func Test_should_fill_metric_key_and_reject_second_link_when_application_attaches_duplicates(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	appDB := fixture.appDatabase(t)
	ctx := context.Background()
	_, err := fixture.database.Exec(`insert into metrics(id, key, value) values
		('00000000-0000-0000-0000-0000000000a8', 'cc', 1), ('00000000-0000-0000-0000-0000000000a9', 'cc', 2)`)
	require.NoError(t, err)

	// when
	firstErr := appDB.Metrics.AttachToModelRevision(ctx, uuid.MustParse(fixtureModelRevBase+"05"),
		uuid.MustParse("00000000-0000-0000-0000-0000000000a8"))
	secondErr := appDB.Metrics.AttachToModelRevision(ctx, uuid.MustParse(fixtureModelRevBase+"05"),
		uuid.MustParse("00000000-0000-0000-0000-0000000000a9"))

	// then
	require.NoError(t, firstErr)
	require.Error(t, secondErr)
	var storedKey string
	require.NoError(t, fixture.database.Get(&storedKey,
		`select metric_key from model_revision_metrics where model_revision_id = $1`, fixtureModelRevBase+"05"))
	assert.Equal(t, "cc", storedKey)
}

func Test_should_backfill_in_batches_and_stay_idempotent_when_backfill_migration_reruns(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	_, err := fixture.database.Exec(`insert into model_revisions(id, model_id, state, title, metadata, created_by)
		select gen_random_uuid(), 'm_plain', 'archived', 'bulk', '{}', $1 from generate_series(1, 1500)`, fixtureUser)
	require.NoError(t, err)
	_, err = fixture.database.Exec(`insert into metrics(id, key, value)
		select gen_random_uuid(), key, 1 from generate_series(1, 4) n, (values ('cc'), ('rscc'), ('rmsf'), ('wilson_b')) k(key) limit 4`)
	require.NoError(t, err)
	_, err = fixture.database.Exec(`insert into model_revision_metrics(model_revision_id, metric_id)
		select model_revisions.id, metrics.id from model_revisions cross join metrics
		where model_revisions.title = 'bulk' and metrics.key in ('cc', 'rscc', 'rmsf', 'wilson_b')`)
	require.NoError(t, err)
	_, err = fixture.database.Exec(`update model_revision_metrics set metric_key = null`)
	require.NoError(t, err)
	backfill, err := os.ReadFile(migrationPathWithPrefix(t, "V20261023090400__"))
	require.NoError(t, err)

	// when
	_, rerunErr := fixture.database.Exec(string(backfill))

	// then
	require.NoError(t, rerunErr)
	var unkeyed, quarantined, reviews int
	require.NoError(t, fixture.database.Get(&unkeyed, `select count(*) from model_revision_metrics where metric_key is null`))
	require.NoError(t, fixture.database.Get(&quarantined,
		`select count(*) from model_revision_metrics where metric_key like '%#duplicate#%'`))
	require.NoError(t, fixture.database.Get(&reviews,
		`select count(*) from schema_migration_review where issue like 'duplicate_metric_key:%'`))
	assert.Zero(t, unkeyed)
	assert.Equal(t, 2, quarantined)
	assert.Equal(t, 6000+4, linkCount(t, fixture), "6000 bulk links span two batches")
	assert.Equal(t, 1, reviews, "rerun does not duplicate review rows")
}

func linkCount(t *testing.T, fixture *migratedFixture) int {
	t.Helper()
	var count int
	require.NoError(t, fixture.database.Get(&count, `select count(*) from model_revision_metrics`))
	return count
}

func migrationPathWithPrefix(t *testing.T, prefix string) string {
	t.Helper()
	paths, err := schemaMigrationPaths()
	require.NoError(t, err)
	for _, path := range paths {
		if strings.HasPrefix(filepath.Base(path), prefix) {
			return path
		}
	}
	require.Failf(t, "migration not found", "prefix %s", prefix)
	return ""
}

func Test_should_add_only_structural_constraints_when_stable_invariants_migrated(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	wantNotValid := []string{
		"entry_revisions_composition_lists_chk", "run_artifacts_direction_chk", "run_artifacts_position_chk",
	}
	deferredToEnforce := []string{
		"metrics_key_known_chk", "model_revisions_model_type_chk", "model_revisions_purpose_chk",
		"artifacts_sha256_format_chk", "artifacts_size_bytes_chk", "polymer_entities_uniprot_mappings_chk",
	}

	// when
	var unvalidated, allChecks []string
	require.NoError(t, fixture.database.Select(&unvalidated, `
		select conname from pg_constraint
		where connamespace = current_schema()::regnamespace and contype = 'c' and not convalidated
		order by conname`))
	require.NoError(t, fixture.database.Select(&allChecks, `
		select conname from pg_constraint
		where connamespace = current_schema()::regnamespace and contype = 'c'`))

	// then
	assert.ElementsMatch(t, wantNotValid, unvalidated)
	for _, deferred := range deferredToEnforce {
		assert.NotContains(t, allChecks, deferred)
	}
}

func Test_should_still_update_legacy_rows_when_vocabulary_checks_are_deferred(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	statements := map[string]string{
		"archive revision with legacy purpose": `update model_revisions set state = 'archived' where id = '` + fixtureModelRevBase + `03'`,
		"archive revision with legacy type":    `update model_revisions set title = title where id = '` + fixtureModelRevBase + `02'`,
		"update artifact with legacy sha256":   `update artifacts set name = 'renamed' where id = '` + fixtureArtifact + `'`,
		"update unknown metric":                `update metrics set value = 2 where id = '` + fixtureMetricBase + `04'`,
	}

	for name, statement := range statements {
		// when
		_, err := fixture.database.Exec(statement)

		// then
		assert.NoError(t, err, name)
	}
}

func Test_should_reject_new_violations_when_kept_constraints_exist(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	statements := map[string]string{
		"direction": `insert into run_artifacts(run_id, artifact_id, direction)
			values ('` + fixtureRunWithVersion + `', '` + fixtureArtifact + `', 'sideways')`,
		"position":        `update run_artifacts set position = -1 where direction = 'input'`,
		"entry list":      `update entry_revisions set metadata = '{"ligands":[1]}' where id = '` + fixtureRevisionBase + `04'`,
		"entry cofactors": `update entry_revisions set metadata = '{"cofactors":"ZN"}' where id = '` + fixtureRevisionBase + `04'`,
	}

	for name, statement := range statements {
		// when
		_, err := fixture.database.Exec(statement)

		// then
		assert.Error(t, err, name)
	}
}

func Test_should_accept_every_write_the_application_makes_when_kept_constraints_exist(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	appDB := fixture.appDatabase(t)
	ctx := context.Background()
	runID := uuid.MustParse(fixtureRunWithVersion)
	artifactID := uuid.MustParse("00000000-0000-0000-0000-0000000000b2")
	emptyLigands := []string{}
	cofactors := []string{"ZN"}
	entryRevision := models.EntryRevision{
		ID: uuid.New(), EntryID: "e_plain", State: models.RevisionStateInReview,
		Title: new("composition"), CreatedBy: uuid.MustParse(fixtureUser),
		Metadata: models.EntryMetadata{Ligands: &emptyLigands, Cofactors: &cofactors},
	}
	unknownLists := models.EntryRevision{
		ID: uuid.New(), EntryID: "e_known", State: models.RevisionStateInReview,
		Title: new("unknown composition"), CreatedBy: uuid.MustParse(fixtureUser),
	}

	// when
	inputErr := appDB.Runs.AttachArtifact(ctx, runID, artifactID, models.RunArtifactDirectionInput)
	outputErr := appDB.Runs.AttachArtifact(ctx, runID, artifactID, models.RunArtifactDirectionOutput)
	repeatErr := appDB.Runs.AttachArtifact(ctx, runID, artifactID, models.RunArtifactDirectionOutput)
	withLists, withListsErr := appDB.Entries.Create(ctx, entryRevision)
	withoutLists, withoutListsErr := appDB.Entries.Create(ctx, unknownLists)

	// then
	require.NoError(t, inputErr)
	require.NoError(t, outputErr)
	require.NoError(t, repeatErr)
	require.NoError(t, withListsErr)
	require.NoError(t, withoutListsErr)
	require.NotNil(t, withLists.Metadata.Ligands)
	assert.Empty(t, *withLists.Metadata.Ligands, "[] is kept as known-empty")
	assert.Equal(t, cofactors, *withLists.Metadata.Cofactors)
	assert.Nil(t, withoutLists.Metadata.Ligands, "omitted stays unknown")
	var rawMetadata map[string]any
	require.NoError(t, json.Unmarshal(fixture.rawBytes(t,
		`select metadata from entry_revisions where id = $1`, withoutLists.ID), &rawMetadata))
	assert.NotContains(t, rawMetadata, "ligands")
	var links int
	require.NoError(t, fixture.database.Get(&links,
		`select count(*) from run_artifacts where run_id = $1 and artifact_id = $2`, runID, artifactID))
	assert.Equal(t, 2, links)
}

func Test_should_keep_migrated_lists_when_next_entry_revision_is_created(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	appDB := fixture.appDatabase(t)
	ctx := context.Background()
	active, err := appDB.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID: new("e_free"), State: new(models.RevisionStateActive),
	})
	require.NoError(t, err)
	next := *active
	next.ID = uuid.New()
	next.State = models.RevisionStateInReview
	next.Title = new("edited")

	// when
	created, createErr := appDB.Entries.Create(ctx, next)

	// then
	require.NoError(t, createErr)
	assert.Equal(t, []string{"ZN", "ATP", "MG"}, *created.Metadata.Ligands)
	assert.Equal(t, []string{}, *created.Metadata.Cofactors)
	assert.Equal(t, []string{"ZN", "ATP", "MG"}, *active.Metadata.Ligands, "the migrated lists are visible to the app")
	stored := fixture.jsonbAt(t, `select metadata from entry_revisions where id = $1`, created.ID)
	assert.Equal(t, []any{"ZN", "ATP", "MG"}, stored["ligands"])
	assert.Equal(t, []any{}, stored["cofactors"])
}

func Test_should_accept_alignment_source_with_release_when_uniprot_mappings_stored(t *testing.T) {
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
		"metric_unknown_key":                           1,
		"metric_missing_software":                      0,
		"metric_duplicate_key":                         1,
		"model_invalid_type":                           1,
		"model_invalid_purpose":                        1,
		"run_missing_software_version":                 1,
		"computed_model_without_run":                   2,
		"artifact_malformed_sha256":                    1,
		"artifact_negative_size":                       1,
		"model_revision_has_composition_lists":         6,
		"entry_composition_lists_unresolved":           3,
		"polymer_entity_mutations_unstructured":        4,
		"model_revision_composition_lists_unmergeable": 2,
		"run_artifact_invalid_direction":               1,
		"polymer_entity_mutations_unsupported_type":    3,
		"polymer_entity_mutations_array_not_strings":   1,
		"polymer_entity_uniprot_mapping_invalid":       1,
	}, counts)
	assert.Equal(t, []string{fixtureMetricBase + "04"}, idsByCheck["metric_unknown_key"])
	assert.Equal(t, []string{fixtureModelRevBase + "02"}, idsByCheck["metric_duplicate_key"])
	assert.Equal(t, []string{fixtureModelRevBase + "03"}, idsByCheck["model_invalid_purpose"])
	assert.Equal(t, []string{fixtureModelRevBase + "02"}, idsByCheck["model_invalid_type"], "Refinement is a purpose, not a model type")
	assert.Equal(t, []string{fixtureRunNoVersion}, idsByCheck["run_missing_software_version"])
	assert.Equal(t, []string{"e_mixed", "e_pdb", "e_string"}, idsByCheck["entry_composition_lists_unresolved"],
		"the PDB entry waits for the archive, the two others for review of unmergeable lists")
	assert.Equal(t, []string{fixtureModelRevBase + "06", fixtureModelRevBase + "07"},
		idsByCheck["model_revision_composition_lists_unmergeable"])
	assert.Equal(t, []string{fixtureRunNoVersion}, idsByCheck["run_artifact_invalid_direction"])
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
