package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
)

// modelSearchTestDB is a schema of its own, so a search sees only the records a test makes.
type modelSearchTestDB struct {
	database *db.DB
	sql      *sqlx.DB
	userID   uuid.UUID
	runID    uuid.UUID
	// clock orders the records a test makes: each one is a second after the previous.
	clock time.Time
	// models numbers the models a test makes, so their IDs never collide.
	models int
}

func newModelSearchTestDB(t *testing.T) *modelSearchTestDB {
	t.Helper()

	adminDB, schemaName, err := setupDBTestSchema()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, dropDBTestSchema(adminDB, schemaName))
		assert.NoError(t, adminDB.Close())
	})
	database, err := db.NewDB(db.Config{
		Host:               "localhost",
		Port:               35432,
		Name:               "dynamic_pdb_local",
		Username:           "postgres",
		Password:           "password",
		ConnectionParams:   "sslmode=disable search_path=" + schemaName,
		MaxOpenConnections: 1,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	rawSQL, err := sqlx.Connect("postgres", dbTestDSN("sslmode=disable search_path="+schemaName))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, rawSQL.Close()) })

	now := time.Now().UTC().Truncate(time.Second)
	user, err := database.Users.Create(context.Background(), models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "test", Value: uuid.NewString()},
		Email:       "user-" + uuid.NewString() + "@example.com",
		DisplayName: "Model Search Test User",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	runID := uuid.New()
	_, err = rawSQL.Exec(`insert into runs (id, name, created_by) values ($1, 'model compounds reader', $2)`, runID, user.ID)
	require.NoError(t, err)
	return &modelSearchTestDB{database: database, sql: rawSQL, userID: user.ID, runID: runID, clock: now}
}

func (f *modelSearchTestDB) tick() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func (f *modelSearchTestDB) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := f.sql.Exec(query, args...)
	require.NoError(t, err)
}

func jsonText(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

// searchEntry is an active entry with its active revision.
type searchEntry struct {
	id         string
	revisionID uuid.UUID
}

// entry makes an active entry whose active revision has metadata and the given polymer entities.
func (f *modelSearchTestDB) entry(t *testing.T, metadata map[string]any, entities ...map[string]any) searchEntry {
	t.Helper()
	entry := searchEntry{id: "dpdb_" + uuid.NewString()[:8], revisionID: uuid.New()}
	createdAt := f.tick()
	f.exec(t, `insert into entries (id, created_by, created_at, state) values ($1, $2, $3, 'active')`,
		entry.id, f.userID, createdAt)
	f.entryRevision(t, entry, entry.revisionID, 2, "active", metadata)
	for _, entity := range entities {
		f.polymerEntity(t, entry.revisionID, entity)
	}
	return entry
}

func (f *modelSearchTestDB) entryRevision(
	t *testing.T,
	entry searchEntry,
	revisionID uuid.UUID,
	revisionNumber int,
	state string,
	metadata map[string]any,
) {
	t.Helper()
	createdAt := f.tick()
	f.exec(t, `insert into entry_revisions (id, entry_id, revision_number, state, entry_state, title, metadata,
	                                         created_by, created_at, updated_at, published_at)
	           values ($1, $2, $3, $4, 'active', 'search test entry', $5, $6, $7, $7, $7)`,
		revisionID, entry.id, revisionNumber, state, jsonText(t, metadata), f.userID, createdAt)
}

// archivedRevision gives entry an older, archived revision with metadata and polymer entities.
func (f *modelSearchTestDB) archivedRevision(t *testing.T, entry searchEntry, metadata map[string]any, entities ...map[string]any) {
	t.Helper()
	revisionID := uuid.New()
	f.entryRevision(t, entry, revisionID, 1, "archived", metadata)
	for _, entity := range entities {
		f.polymerEntity(t, revisionID, entity)
	}
}

func (f *modelSearchTestDB) polymerEntity(t *testing.T, entryRevisionID uuid.UUID, metadata map[string]any) uuid.UUID {
	t.Helper()
	fasta := f.artifact(t, models.ArtifactLevelL0, models.ArtifactTypeFASTA)
	sequenceID := uuid.New()
	f.exec(t, `insert into protein_sequences (id, entry_revision_id, source_artifact_id, record_index, header, sequence)
	           values ($1, $2, $3, 0, '>test', 'MKV')`, sequenceID, entryRevisionID, fasta)
	entityID := uuid.New()
	f.exec(t, `insert into polymer_entities (id, protein_sequence_id, metadata) values ($1, $2, $3)`,
		entityID, sequenceID, jsonText(t, metadata))
	f.exec(t, `insert into entry_revision_polymer_entities (entry_revision_id, polymer_entity_id) values ($1, $2)`,
		entryRevisionID, entityID)
	return entityID
}

func (f *modelSearchTestDB) artifact(t *testing.T, level models.ArtifactLevel, artifactType models.ArtifactType) uuid.UUID {
	t.Helper()
	artifactID := uuid.New()
	f.exec(t, `insert into artifacts (id, name, level, type, created_by, created_at) values ($1, $2, $3, $4, $5, $6)`,
		artifactID, "file-"+artifactID.String(), string(level), string(artifactType), f.userID, f.tick())
	return artifactID
}

// searchModel is an active model with its active revision and coordinate file.
type searchModel struct {
	id             string
	revisionID     uuid.UUID
	coordinateFile uuid.UUID
}

// model makes an active model of entry whose active revision has metadata and an L2 coordinate
// file as its primary file. The primary file is not linked to the revision; each level in files
// links one more file of that level. As in the app, files are linked before the revision is
// published: a published revision's files cannot change (V20261110170100).
func (f *modelSearchTestDB) model(
	t *testing.T,
	entry searchEntry,
	metadata map[string]any,
	files ...models.ArtifactLevel,
) searchModel {
	t.Helper()
	f.models++
	model := searchModel{
		id:             fmt.Sprintf("%s_m_%03d", entry.id, f.models),
		revisionID:     uuid.New(),
		coordinateFile: f.artifact(t, models.ArtifactLevelL2, models.ArtifactTypeModel),
	}
	createdAt := f.tick()
	f.exec(t, `insert into models (id, entry_id, created_by, created_at, state) values ($1, $2, $3, $4, 'active')`,
		model.id, entry.id, f.userID, createdAt)
	f.exec(t, `insert into model_revisions (id, model_id, primary_artifact_id, revision_number, state, model_state,
	                                         title, metadata, created_by, created_at, updated_at, published_at)
	           values ($1, $2, $3, 1, 'pending', 'active', 'search test model', $4, $5, $6, $6, $6)`,
		model.revisionID, model.id, model.coordinateFile, jsonText(t, metadata), f.userID, createdAt)
	for _, level := range files {
		f.exec(t, `insert into model_revision_artifacts (model_revision_id, artifact_id) values ($1, $2)`,
			model.revisionID, f.artifact(t, level, models.ArtifactTypeOther))
	}
	f.exec(t, `update model_revisions set state = 'active' where id = $1`, model.revisionID)
	return model
}

func (f *modelSearchTestDB) metric(t *testing.T, model searchModel, key models.MetricKey, value float64) {
	t.Helper()
	metricID := uuid.New()
	f.exec(t, `insert into metrics (id, key, value) values ($1, $2, $3)`, metricID, string(key), value)
	f.exec(t, `insert into model_revision_metrics (model_revision_id, metric_id) values ($1, $2)`,
		model.revisionID, metricID)
}

// compoundRecord is one compound copy in a model file; a non-empty reviewReason marks it for review.
type compoundRecord struct {
	ccdID        string
	reviewReason string
}

// compounds gives model's coordinate file a compound set with status and records.
func (f *modelSearchTestDB) compounds(t *testing.T, model searchModel, status string, records ...compoundRecord) {
	t.Helper()
	if status == "done" {
		f.exec(t, `insert into modeled_compound_sets (artifact_id, status, run_id, complete, read_at)
		           values ($1, 'done', $2, true, now())`, model.coordinateFile, f.runID)
	} else {
		f.exec(t, `insert into modeled_compound_sets (artifact_id, status) values ($1, $2)`, model.coordinateFile, status)
	}
	for ordinal, record := range records {
		var reason *string
		if record.reviewReason != "" {
			reason = &record.reviewReason
		}
		f.exec(t, `insert into modeled_compound_records (artifact_id, ordinal, ccd_id, source, needs_review, review_reason)
		           values ($1, $2, $3, 'atom_site', $4, $5)`,
			model.coordinateFile, ordinal, record.ccdID, reason != nil, reason)
	}
}

func (f *modelSearchTestDB) review(t *testing.T, objectType, objectID, issue string, resolved bool) {
	t.Helper()
	if resolved {
		f.exec(t, `insert into schema_migration_review (object_type, object_id, issue, resolved_at, resolved_by)
		           values ($1, $2, $3, now(), 'test')`, objectType, objectID, issue)
		return
	}
	f.exec(t, `insert into schema_migration_review (object_type, object_id, issue) values ($1, $2, $3)`,
		objectType, objectID, issue)
}

func activeSearchFilters(search db.ModelSearch) db.ModelRevisionFilters {
	return db.ModelRevisionFilters{
		State:              ptr(models.RevisionStateActive),
		EntryState:         ptr(models.EntryStateActive),
		ModelState:         ptr(models.ModelStateActive),
		ExcludeResidueData: true,
		Search:             &search,
	}
}

// search returns the IDs of the models matching search, in list order.
func (f *modelSearchTestDB) search(t *testing.T, search db.ModelSearch) []string {
	t.Helper()
	revisions, err := f.database.Models.List(context.Background(), activeSearchFilters(search))
	require.NoError(t, err)
	return modelIDsOf(revisions)
}

func modelIDsOf(revisions []models.ModelRevision) []string {
	ids := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		ids = append(ids, revision.ModelID)
	}
	return ids
}

type modelSearchCase struct {
	name   string
	search db.ModelSearch
	want   []string
}

func runModelSearchCases(t *testing.T, fixture *modelSearchTestDB, cases []modelSearchCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			got := fixture.search(t, tc.search)

			// then
			assert.Equal(t, nonNilStrings(tc.want), got)
		})
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func Test_should_match_entry_filters_on_the_active_entry_revision_when_models_are_searched(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	xray := fixture.entry(t, map[string]any{"method": "X-ray crystallography", "resolution": 1.5, "ligands": []string{"ADP"}, "cofactors": []string{}})
	neutron := fixture.entry(t, map[string]any{"method": "NEUTRON DIFFRACTION", "resolution": 2.5, "ligands": []string{}, "cofactors": []string{"adp", "Mg"}})
	unknown := fixture.entry(t, map[string]any{"method": "X-ray crystallography"})
	fixture.archivedRevision(t, unknown, map[string]any{"method": "CryoEM", "resolution": 1.0, "ligands": []string{"ADP"}})
	xrayModel := fixture.model(t, xray, map[string]any{})
	neutronModel := fixture.model(t, neutron, map[string]any{})
	unknownModel := fixture.model(t, unknown, map[string]any{})

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "no filter", search: db.ModelSearch{}, want: []string{xrayModel.id, neutronModel.id, unknownModel.id}},
		{name: "method ignoring case", search: db.ModelSearch{Methods: []string{"neutron diffraction"}}, want: []string{neutronModel.id}},
		{name: "several methods mean any", search: db.ModelSearch{Methods: []string{"x-ray crystallography", "NEUTRON DIFFRACTION"}},
			want: []string{xrayModel.id, neutronModel.id, unknownModel.id}},
		{name: "archived revision values do not match", search: db.ModelSearch{Methods: []string{"CryoEM"}}, want: nil},
		{name: "resolution bounds are inclusive", search: db.ModelSearch{ResolutionMin: ptr(1.5), ResolutionMax: ptr(2.5)},
			want: []string{xrayModel.id, neutronModel.id}},
		{name: "missing resolution never matches", search: db.ModelSearch{ResolutionMax: ptr(100.0)}, want: []string{xrayModel.id, neutronModel.id}},
		{name: "ligand_or_cofactor matches either list ignoring case", search: db.ModelSearch{LigandsOrCofactors: []string{"adp"}},
			want: []string{xrayModel.id, neutronModel.id}},
		{name: "ligand_or_cofactor values mean any", search: db.ModelSearch{LigandsOrCofactors: []string{"MG", "ATP"}},
			want: []string{neutronModel.id}},
		{name: "different filters must all match", search: db.ModelSearch{LigandsOrCofactors: []string{"ADP"}, ResolutionMax: ptr(2.0)},
			want: []string{xrayModel.id}},
	})
}

func Test_should_match_polymer_entities_of_the_active_entry_revision_when_models_are_searched_by_organism_or_uniprot(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	human := fixture.entry(t, map[string]any{}, map[string]any{
		"source_organisms": []map[string]any{{"scientific_name": "Homo sapiens", "ncbi_taxonomy_id": 9606}},
		"uniprot_mappings": []map[string]any{{"accession": "p69905", "source": "sifts", "unp_release": "2026_03"}},
	})
	mixed := fixture.entry(t, map[string]any{},
		map[string]any{"source_organisms": []map[string]any{{"scientific_name": "Escherichia coli", "ncbi_taxonomy_id": 562}}},
		map[string]any{"source_organisms": []map[string]any{{"scientific_name": "Mus musculus", "ncbi_taxonomy_id": 10090}}},
	)
	fixture.archivedRevision(t, mixed, map[string]any{}, map[string]any{
		"source_organisms": []map[string]any{{"scientific_name": "Homo sapiens", "ncbi_taxonomy_id": 9606}},
		"uniprot_mappings": []map[string]any{{"accession": "P69905", "source": "sifts", "unp_release": "2026_03"}},
	})
	humanModel := fixture.model(t, human, map[string]any{})
	mixedModel := fixture.model(t, mixed, map[string]any{})

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "scientific name ignoring case", search: db.ModelSearch{Organisms: []string{"homo SAPIENS"}}, want: []string{humanModel.id}},
		{name: "NCBI taxonomy ID", search: db.ModelSearch{Organisms: []string{"10090"}}, want: []string{mixedModel.id}},
		{name: "several organisms mean any", search: db.ModelSearch{Organisms: []string{"9606", "Escherichia coli"}},
			want: []string{humanModel.id, mixedModel.id}},
		{name: "UniProt accession ignoring case on both sides", search: db.ModelSearch{UniProtAccessions: []string{"P69905"}}, want: []string{humanModel.id}},
		{name: "unknown accession", search: db.ModelSearch{UniProtAccessions: []string{"Q00000"}}, want: nil},
	})
}

func Test_should_match_model_metadata_filters_when_models_are_searched(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	deposited := fixture.model(t, entry, map[string]any{"model_type": "Deposited", "purpose": "Reference"})
	rerefined := fixture.model(t, entry, map[string]any{"model_type": "Ensemble", "purpose": "Refinement", "origin": "archive"})
	multiconformer := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer", "purpose": "Model Building"})
	untyped := fixture.model(t, entry, map[string]any{})

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "model type", search: db.ModelSearch{ModelTypes: []models.StructureModelType{models.StructureModelTypeEnsemble}},
			want: []string{rerefined.id}},
		{name: "several model types mean any", search: db.ModelSearch{ModelTypes: []models.StructureModelType{
			models.StructureModelTypeDeposited, models.StructureModelTypeMulticonformer}},
			want: []string{deposited.id, multiconformer.id}},
		{name: "purpose", search: db.ModelSearch{Purposes: []models.ModelPurpose{models.ModelPurposeModelBuilding}},
			want: []string{multiconformer.id}},
		{name: "pdb archive includes re-refinements with origin archive",
			search: db.ModelSearch{Sources: []models.ModelSearchSource{models.ModelSearchSourcePDBArchive}},
			want:   []string{deposited.id, rerefined.id}},
		{name: "dynamic pdb deposit is every other model",
			search: db.ModelSearch{Sources: []models.ModelSearchSource{models.ModelSearchSourceDynamicPDBDeposit}},
			want:   []string{multiconformer.id, untyped.id}},
		{name: "both sources are every model", search: db.ModelSearch{Sources: models.ModelSearchSources},
			want: []string{deposited.id, rerefined.id, multiconformer.id, untyped.id}},
	})
}

func Test_should_agree_with_search_source_of_the_metadata_when_models_are_searched_by_source(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	metadataByModel := map[string]models.ModelMetadata{}
	for _, metadata := range []map[string]any{
		{},
		{"model_type": "Deposited"},
		{"model_type": "Deposited", "origin": "archive"},
		{"model_type": "Ensemble", "origin": "archive"},
		{"model_type": "Ensemble"},
		{"model_type": "Single Conformer"},
		{"model_type": "Multiconformer"},
		{"origin": "archive"},
	} {
		model := fixture.model(t, entry, metadata)
		var decoded models.ModelMetadata
		require.NoError(t, json.Unmarshal([]byte(jsonText(t, metadata)), &decoded))
		metadataByModel[model.id] = decoded
	}

	for _, source := range models.ModelSearchSources {
		t.Run(string(source), func(t *testing.T) {
			// when
			got := fixture.search(t, db.ModelSearch{Sources: []models.ModelSearchSource{source}})

			// then
			want := make([]string, 0)
			for modelID, metadata := range metadataByModel {
				if metadata.SearchSource() == source {
					want = append(want, modelID)
				}
			}
			assert.ElementsMatch(t, want, got)
		})
	}
}

func Test_should_match_files_and_compounds_of_the_same_model_when_models_are_searched(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	withADP := fixture.model(t, entry, map[string]any{}, models.ArtifactLevelL1)
	fixture.compounds(t, withADP, "done", compoundRecord{ccdID: "ADP"}, compoundRecord{ccdID: "MG"})
	needsReview := fixture.model(t, entry, map[string]any{}, models.ArtifactLevelL3)
	fixture.compounds(t, needsReview, "done", compoundRecord{ccdID: "ADP", reviewReason: "possible chain residue"})
	notRead := fixture.model(t, entry, map[string]any{})
	fixture.compounds(t, notRead, "pending")

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "primary file level", search: db.ModelSearch{DataLevels: []models.ArtifactLevel{models.ArtifactLevelL2}},
			want: []string{withADP.id, needsReview.id, notRead.id}},
		{name: "attached file level", search: db.ModelSearch{DataLevels: []models.ArtifactLevel{models.ArtifactLevelL1}},
			want: []string{withADP.id}},
		{name: "several levels mean any", search: db.ModelSearch{DataLevels: []models.ArtifactLevel{models.ArtifactLevelL1, models.ArtifactLevelL3}},
			want: []string{withADP.id, needsReview.id}},
		{name: "entry files do not count", search: db.ModelSearch{DataLevels: []models.ArtifactLevel{models.ArtifactLevelL0}}, want: nil},
		{name: "compound ignoring case, needs review included", search: db.ModelSearch{Compounds: []string{"adp"}},
			want: []string{withADP.id, needsReview.id}},
		{name: "compound only in a sibling model's file", search: db.ModelSearch{Compounds: []string{"MG"}}, want: []string{withADP.id}},
		{name: "file not read yet never matches", search: db.ModelSearch{Compounds: []string{"ATP"}}, want: nil},
	})
}

func Test_should_compare_metric_values_and_find_missing_ones_when_models_are_searched_by_metrics(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	low := fixture.model(t, entry, map[string]any{})
	fixture.metric(t, low, models.MetricKeyRFree, 0.15)
	fixture.metric(t, low, models.MetricKeyClashscore, 2)
	high := fixture.model(t, entry, map[string]any{})
	fixture.metric(t, high, models.MetricKeyRFree, 0.30)
	missing := fixture.model(t, entry, map[string]any{})
	fixture.metric(t, missing, models.MetricKeyClashscore, 9)

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "maximum is inclusive", search: db.ModelSearch{MetricBounds: []db.MetricBound{{Key: models.MetricKeyRFree, Max: ptr(0.15)}}},
			want: []string{low.id}},
		{name: "minimum is inclusive", search: db.ModelSearch{MetricBounds: []db.MetricBound{{Key: models.MetricKeyRFree, Min: ptr(0.30)}}},
			want: []string{high.id}},
		{name: "missing value never matches a range", search: db.ModelSearch{MetricBounds: []db.MetricBound{
			{Key: models.MetricKeyRFree, Min: ptr(0.0), Max: ptr(1.0)}}},
			want: []string{low.id, high.id}},
		{name: "bounds on two metrics must both hold", search: db.ModelSearch{MetricBounds: []db.MetricBound{
			{Key: models.MetricKeyRFree, Max: ptr(1.0)}, {Key: models.MetricKeyClashscore, Max: ptr(5.0)}}},
			want: []string{low.id}},
		{name: "metric missing", search: db.ModelSearch{MissingMetrics: []models.MetricKey{models.MetricKeyRFree}},
			want: []string{missing.id}},
		{name: "several missing metrics mean any", search: db.ModelSearch{MissingMetrics: []models.MetricKey{
			models.MetricKeyRFree, models.MetricKeyClashscore}},
			want: []string{high.id, missing.id}},
	})
}

func Test_should_apply_every_model_filter_to_the_same_model_when_an_entry_has_several_models(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{"ligands": []string{"ADP"}})
	ensemble := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	fixture.metric(t, ensemble, models.MetricKeyRFree, 0.30)
	fixture.compounds(t, ensemble, "done", compoundRecord{ccdID: "ATP"})
	multiconformer := fixture.model(t, entry, map[string]any{"model_type": "Multiconformer"})
	fixture.metric(t, multiconformer, models.MetricKeyRFree, 0.15)
	fixture.compounds(t, multiconformer, "done", compoundRecord{ccdID: "GOL"})

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "type of one model, metric of the other", search: db.ModelSearch{
			ModelTypes:   []models.StructureModelType{models.StructureModelTypeEnsemble},
			MetricBounds: []db.MetricBound{{Key: models.MetricKeyRFree, Max: ptr(0.2)}}},
			want: nil},
		{name: "type and metric of one model", search: db.ModelSearch{
			ModelTypes:   []models.StructureModelType{models.StructureModelTypeMulticonformer},
			MetricBounds: []db.MetricBound{{Key: models.MetricKeyRFree, Max: ptr(0.2)}}},
			want: []string{multiconformer.id}},
		{name: "compound of one model, type of the other", search: db.ModelSearch{
			ModelTypes: []models.StructureModelType{models.StructureModelTypeMulticonformer},
			Compounds:  []string{"ATP"}},
			want: nil},
		{name: "entry filter applies to every model of the entry", search: db.ModelSearch{
			LigandsOrCofactors: []string{"ADP"}, Compounds: []string{"GOL"}},
			want: []string{multiconformer.id}},
	})
}

func Test_should_find_models_by_open_review_flags_on_model_revision_entry_and_polymer_entity_when_searched_by_flagged(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	flaggedEntry := fixture.entry(t, map[string]any{})
	fixture.review(t, "entry", flaggedEntry.id, "sync_validation_failed", false)
	flaggedEntryModel := fixture.model(t, flaggedEntry, map[string]any{})
	entityEntry := fixture.entry(t, map[string]any{})
	entityID := fixture.polymerEntity(t, entityEntry.revisionID, map[string]any{})
	fixture.review(t, "polymer_entity", entityID.String(), "mutations_need_review", false)
	flaggedEntityModel := fixture.model(t, entityEntry, map[string]any{})
	plainEntry := fixture.entry(t, map[string]any{})
	flaggedModel := fixture.model(t, plainEntry, map[string]any{})
	fixture.review(t, "model", flaggedModel.id, "sync_validation_failed", false)
	flaggedRevision := fixture.model(t, plainEntry, map[string]any{})
	fixture.review(t, "model_revision", flaggedRevision.revisionID.String(), "duplicate_metric_key:r_free", false)
	resolvedModel := fixture.model(t, plainEntry, map[string]any{})
	fixture.review(t, "model", resolvedModel.id, "sync_validation_failed", true)
	plainModel := fixture.model(t, plainEntry, map[string]any{})

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "only flagged", search: db.ModelSearch{Flagged: ptr(true)},
			want: []string{flaggedEntryModel.id, flaggedEntityModel.id, flaggedModel.id, flaggedRevision.id}},
		{name: "only not flagged", search: db.ModelSearch{Flagged: ptr(false)}, want: []string{resolvedModel.id, plainModel.id}},
	})

	t.Run("page flags name the same objects", func(t *testing.T) {
		// when
		flags, err := fixture.database.MigrationReviews.ListOpenForModelRevisions(context.Background(), []uuid.UUID{
			flaggedEntryModel.revisionID, flaggedEntityModel.revisionID, flaggedModel.revisionID,
			flaggedRevision.revisionID, resolvedModel.revisionID, plainModel.revisionID,
		})

		// then
		require.NoError(t, err)
		issuesOf := func(revisionID uuid.UUID) []string {
			issues := make([]string, 0)
			for _, flag := range flags[revisionID] {
				issues = append(issues, flag.ObjectType+":"+flag.Issue)
			}
			return issues
		}
		assert.Equal(t, []string{"entry:sync_validation_failed"}, issuesOf(flaggedEntryModel.revisionID))
		assert.Equal(t, []string{"polymer_entity:mutations_need_review"}, issuesOf(flaggedEntityModel.revisionID))
		assert.Equal(t, []string{"model:sync_validation_failed"}, issuesOf(flaggedModel.revisionID))
		assert.Equal(t, []string{"model_revision:duplicate_metric_key:r_free"}, issuesOf(flaggedRevision.revisionID))
		assert.Empty(t, issuesOf(resolvedModel.revisionID))
		assert.Empty(t, issuesOf(plainModel.revisionID))
	})
}

func Test_should_cap_the_count_and_ignore_paging_when_matching_models_are_counted(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	for range 3 {
		fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
	}
	fixture.model(t, entry, map[string]any{"model_type": "Deposited"})
	ensembles := db.ModelSearch{ModelTypes: []models.StructureModelType{models.StructureModelTypeEnsemble}}
	paged := activeSearchFilters(ensembles)
	paged.Limit = ptr(1)

	cases := []struct {
		name      string
		filters   db.ModelRevisionFilters
		limit     int
		wantCount int
		wantExact bool
	}{
		{name: "below the cap", filters: activeSearchFilters(ensembles), limit: 4, wantCount: 3, wantExact: true},
		{name: "at the cap", filters: activeSearchFilters(ensembles), limit: 3, wantCount: 3, wantExact: true},
		{name: "above the cap", filters: activeSearchFilters(ensembles), limit: 2, wantCount: 2, wantExact: false},
		{name: "paging is ignored", filters: paged, limit: 10, wantCount: 3, wantExact: true},
		{name: "no filter", filters: activeSearchFilters(db.ModelSearch{}), limit: 10, wantCount: 4, wantExact: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			count, exact, err := fixture.database.Models.CountCapped(context.Background(), tc.filters, tc.limit)

			// then
			require.NoError(t, err)
			assert.Equal(t, tc.wantCount, count)
			assert.Equal(t, tc.wantExact, exact)
		})
	}
}

func Test_should_return_each_match_once_in_order_when_a_search_is_paged_with_after(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	want := make([]string, 0)
	for index := range 5 {
		ensemble := fixture.model(t, entry, map[string]any{"model_type": "Ensemble"})
		want = append(want, ensemble.id)
		if index%2 == 0 {
			fixture.model(t, entry, map[string]any{"model_type": "Deposited"})
		}
	}
	filters := activeSearchFilters(db.ModelSearch{ModelTypes: []models.StructureModelType{models.StructureModelTypeEnsemble}})
	filters.Limit = ptr(2)

	// when
	got := make([]string, 0)
	for {
		page, err := fixture.database.Models.List(context.Background(), filters)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		got = append(got, modelIDsOf(page)...)
		last := page[len(page)-1]
		filters.After = &db.ListPosition{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	// then
	assert.Equal(t, want, got)
}

func Test_should_list_only_named_compounds_marked_for_review_when_compounds_needing_review_are_listed(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	model := fixture.model(t, entry, map[string]any{})
	fixture.compounds(t, model, "done",
		compoundRecord{ccdID: "ADP", reviewReason: "possible chain residue"},
		compoundRecord{ccdID: "ADP", reviewReason: "possible chain residue"},
		compoundRecord{ccdID: "MG"},
		compoundRecord{ccdID: "NAG", reviewReason: "possible chain residue"},
	)

	// when
	got, err := fixture.database.ModeledCompounds.ListNeedingReview(context.Background(),
		[]uuid.UUID{model.coordinateFile}, []string{"adp", "MG"})

	// then
	require.NoError(t, err)
	assert.Equal(t, []models.CompoundNeedingReview{{CCDID: "ADP", ReviewReason: "possible chain residue"}},
		got[model.coordinateFile])
}

func Test_should_refuse_a_search_the_database_cannot_run_when_models_are_searched(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	cases := map[string]db.ModelSearch{
		"unknown model type":       {ModelTypes: []models.StructureModelType{"Hologram"}},
		"unknown source":           {Sources: []models.ModelSearchSource{"elsewhere"}},
		"metric bounded twice":     {MetricBounds: []db.MetricBound{{Key: models.MetricKeyRFree, Max: ptr(0.2)}, {Key: models.MetricKeyRFree, Min: ptr(0.1)}}},
		"unknown metric":           {MetricBounds: []db.MetricBound{{Key: "r_factor", Max: ptr(0.2)}}},
		"taxonomy ID of zero":      {Organisms: []string{"000"}},
		"resolution not a number":  {ResolutionMin: ptr(nan())},
		"unknown missing metric":   {MissingMetrics: []models.MetricKey{"r_factor"}},
		"unknown data level":       {DataLevels: []models.ArtifactLevel{"L9"}},
		"unknown purpose":          {Purposes: []models.ModelPurpose{"Decoration"}},
		"metric bound not a value": {MetricBounds: []db.MetricBound{{Key: models.MetricKeyRFree, Max: ptr(inf())}}},
	}
	for name, search := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			_, err := fixture.database.Models.List(context.Background(), activeSearchFilters(search))

			// then
			require.ErrorIs(t, err, db.ErrInvalidModelSearch)
		})
	}
}

func nan() float64 {
	return math.NaN()
}

func inf() float64 {
	return math.Inf(1)
}

func Test_should_count_quarantined_duplicate_metric_values_when_models_are_searched_by_metrics(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	entry := fixture.entry(t, map[string]any{})
	duplicated := fixture.model(t, entry, map[string]any{})
	fixture.metric(t, duplicated, models.MetricKeyRFree, 0.18)
	fixture.exec(t, `update model_revision_metrics set metric_key = 'r_free#duplicate#' || metric_id
	                 where model_revision_id = $1`, duplicated.revisionID)
	fixture.metric(t, duplicated, models.MetricKeyRFree, 0.31)

	runModelSearchCases(t, fixture, []modelSearchCase{
		{name: "a quarantined value satisfies a bound", search: db.ModelSearch{MetricBounds: []db.MetricBound{
			{Key: models.MetricKeyRFree, Max: ptr(0.2)}}},
			want: []string{duplicated.id}},
		{name: "a metric with quarantined values is not missing", search: db.ModelSearch{
			MissingMetrics: []models.MetricKey{models.MetricKeyRFree}},
			want: nil},
	})
}

func Test_should_return_the_same_pages_when_a_search_with_few_matches_collects_them_first(t *testing.T) {
	// given
	fixture := newModelSearchTestDB(t)
	adp := fixture.entry(t, map[string]any{"ligands": []string{"ADP"}})
	other := fixture.entry(t, map[string]any{"ligands": []string{"ATP"}})
	for index := range 7 {
		fixture.model(t, adp, map[string]any{"model_type": "Ensemble"})
		if index%3 == 0 {
			fixture.model(t, other, map[string]any{"model_type": "Ensemble"})
		}
	}
	search := db.ModelSearch{LigandsOrCofactors: []string{"ADP"}, ModelTypes: []models.StructureModelType{models.StructureModelTypeEnsemble}}
	walkPages := func(matchesFew bool) []string {
		filters := activeSearchFilters(search)
		filters.SearchMatchesFew = matchesFew
		filters.Limit = ptr(3)
		ids := make([]string, 0)
		for range 10 {
			page, err := fixture.database.Models.List(context.Background(), filters)
			require.NoError(t, err)
			if len(page) == 0 {
				return ids
			}
			ids = append(ids, modelIDsOf(page)...)
			last := page[len(page)-1]
			filters.After = &db.ListPosition{CreatedAt: last.CreatedAt, ID: last.ID}
		}
		require.FailNow(t, "paging did not end")
		return nil
	}
	offsetPage := func(matchesFew bool) []string {
		filters := activeSearchFilters(search)
		filters.SearchMatchesFew = matchesFew
		filters.Limit = ptr(2)
		filters.Offset = ptr(3)
		page, err := fixture.database.Models.List(context.Background(), filters)
		require.NoError(t, err)
		return modelIDsOf(page)
	}

	// when
	walked := walkPages(false)
	collected := walkPages(true)

	// then
	assert.Len(t, walked, 7)
	assert.Equal(t, walked, collected)
	assert.Equal(t, offsetPage(false), offsetPage(true))
	assert.Equal(t, walked[3:5], offsetPage(true))
}
