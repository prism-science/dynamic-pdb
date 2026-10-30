package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
	"dynamic-pdb/lib/rcsb"
)

// rederiveFixture is a schema with every migration applied, the application repositories on it,
// and an RCSB that serves the files in files.
type rederiveFixture struct {
	database *db.DB
	raw      *sqlx.DB
	userID   uuid.UUID
	rcsb     *rcsbFileServer
}

type rcsbFileServer struct {
	url      string
	mu       sync.Mutex
	files    map[string]string
	requests map[string]int
	// onServe runs before a file is served, as if something changed the entry meanwhile.
	onServe func(pdbID string)
}

func (s *rcsbFileServer) serve(w http.ResponseWriter, r *http.Request) {
	pdbID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/download/"), ".cif")
	s.mu.Lock()
	s.requests[pdbID]++
	contents, ok := s.files[pdbID]
	onServe := s.onServe
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if onServe != nil {
		onServe(pdbID)
	}
	http.ServeContent(w, r, pdbID+".cif", time.Time{}, strings.NewReader(contents))
}

func (s *rcsbFileServer) requestsFor(pdbID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[pdbID]
}

func newRederiveFixture(t *testing.T) *rederiveFixture {
	t.Helper()
	schemaName := "rederive_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	const dsn = "host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password sslmode=disable"
	admin, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	_, err = admin.Exec(fmt.Sprintf(`create schema "%s"`, schemaName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := admin.Exec(fmt.Sprintf(`drop schema "%s" cascade`, schemaName))
		assert.NoError(t, dropErr)
		assert.NoError(t, admin.Close())
	})
	raw, err := sqlx.Connect("postgres", dsn+" search_path="+schemaName)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, raw.Close()) })
	for _, path := range migrationPaths(t) {
		migrationSQL, err := os.ReadFile(path)
		require.NoError(t, err)
		_, err = raw.Exec(string(migrationSQL))
		require.NoError(t, err, "apply %s", filepath.Base(path))
	}
	database, err := db.NewDB(db.Config{
		Host:               "localhost",
		Port:               35432,
		Name:               "dynamic_pdb_local",
		Username:           "postgres",
		Password:           "password",
		ConnectionParams:   "sslmode=disable search_path=" + schemaName,
		MaxOpenConnections: 4,
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })

	now := time.Now().UTC()
	user, err := database.Users.Create(context.Background(), models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "test", Value: uuid.NewString()},
		Email:       "rederive-" + uuid.NewString() + "@example.com",
		DisplayName: "Rederive Test User",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)

	files := &rcsbFileServer{files: map[string]string{}, requests: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(files.serve))
	t.Cleanup(server.Close)
	files.url = server.URL
	return &rederiveFixture{database: database, raw: raw, userID: user.ID, rcsb: files}
}

func (f *rederiveFixture) job(t *testing.T, cfg ArchiveRederiveConfig) *ArchiveRederiveJob {
	t.Helper()
	if cfg.PageSize == 0 {
		cfg.PageSize = 10
	}
	if cfg.DownloadConcurrency == 0 {
		cfg.DownloadConcurrency = 2
	}
	if cfg.RunFor == 0 {
		cfg.RunFor = time.Minute
	}
	client := rcsb.NewClient(rcsb.WithBaseURLs(f.rcsb.url, f.rcsb.url, f.rcsb.url, f.rcsb.url))
	job, err := NewArchiveRederiveJob(f.database, client, cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	job.sleep = func(context.Context, time.Duration) error { return nil }
	return job
}

type seededArchiveEntry struct {
	entryID    string
	revisionID uuid.UUID
	entityID   uuid.UUID
	updatedAt  time.Time
}

// seedArchiveEntry adds an active archive entry for pdbID with metadata, and one polymer entity,
// label 1, with the C1-parsed mutation C4T.
func (f *rederiveFixture) seedArchiveEntry(t *testing.T, pdbID string, metadata models.EntryMetadata) seededArchiveEntry {
	t.Helper()
	ctx := context.Background()
	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	metadata.ExternalRefs = map[models.EntrySource]string{models.EntrySourcePDB: pdbID}
	revision, err := f.database.Entries.Create(ctx, models.EntryRevision{
		ID:         uuid.New(),
		EntryID:    "entry-" + uuid.NewString(),
		State:      models.RevisionStateInReview,
		EntryState: models.EntryStateActive,
		Title:      new("archive entry " + pdbID),
		Metadata:   metadata,
		CreatedBy:  f.userID,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	})
	require.NoError(t, err)
	active, err := f.database.Entries.ActivateRevision(ctx, revision.EntryID, revision.ID)
	require.NoError(t, err)
	_, err = f.raw.Exec(`update entries set state = 'active' where id = $1`, revision.EntryID)
	require.NoError(t, err)

	format := "cif"
	artifact, err := f.database.Artifacts.Create(ctx, models.Artifact{
		ID:        uuid.New(),
		Name:      "sequence-" + uuid.NewString(),
		Level:     models.ArtifactLevelL2,
		Format:    &format,
		Metadata:  map[string]any{"kind": "test"},
		CreatedBy: f.userID,
		CreatedAt: createdAt,
	})
	require.NoError(t, err)
	require.NoError(t, f.database.ProteinSequences.Create(ctx, active.ID, artifact.ID, []models.FASTARecord{
		{Header: "polymer", Sequence: "ACDEFGHIK"},
	}))
	sequences, err := f.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{EntryRevisionID: &active.ID})
	require.NoError(t, err)
	entity, err := f.database.PolymerEntities.Create(ctx, models.PolymerEntity{
		ID:                uuid.New(),
		ProteinSequenceID: sequences[0].ID,
		Metadata: models.PolymerEntityMetadata{
			LabelEntityID:   new("1"),
			Description:     new("polymer"),
			Mutations:       []string{"C4T"},
			MutationsRaw:    new("C4T"),
			MutationsStatus: new(models.MutationsStatusParsed),
		},
		CreatedAt: createdAt,
	})
	require.NoError(t, err)
	require.NoError(t, f.database.PolymerEntities.AttachToEntryRevision(ctx, active.ID, entity.ID))
	return seededArchiveEntry{entryID: active.EntryID, revisionID: active.ID, entityID: entity.ID, updatedAt: active.UpdatedAt}
}

func (f *rederiveFixture) revision(t *testing.T, id uuid.UUID) models.EntryRevision {
	t.Helper()
	revision, err := f.database.Entries.Get(context.Background(), db.EntryRevisionFilters{ID: &id})
	require.NoError(t, err)
	return *revision
}

func (f *rederiveFixture) entity(t *testing.T, id uuid.UUID) models.PolymerEntity {
	t.Helper()
	entity, err := f.database.PolymerEntities.Get(context.Background(), db.PolymerEntityFilters{ID: &id})
	require.NoError(t, err)
	return *entity
}

func (f *rederiveFixture) logRow(t *testing.T, revisionID uuid.UUID) (outcome string, dryRun bool, attempts int) {
	t.Helper()
	require.NoError(t, f.raw.QueryRow(
		`select outcome, dry_run, attempts from archive_rederivations where entry_revision_id = $1`, revisionID,
	).Scan(&outcome, &dryRun, &attempts))
	return outcome, dryRun, attempts
}

func (f *rederiveFixture) revisionCount(t *testing.T, entryID string) int {
	t.Helper()
	var count int
	require.NoError(t, f.raw.Get(&count, `select count(*) from entry_revisions where entry_id = $1`, entryID))
	return count
}

func Test_should_fill_unknown_lists_and_uniprot_mutations_in_place_when_job_runs(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{Details: new("kept")})
	_, err := fixture.raw.Exec(`insert into schema_migration_review(object_type, object_id, issue) values
		('entry', $1, 'ligands_cofactors_rederive_from_archive'),
		('polymer_entity', $2, 'mutations_need_review')`, entry.entryID, entry.entityID.String())
	require.NoError(t, err)

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	revision := fixture.revision(t, entry.revisionID)
	assert.Equal(t, models.RevisionStateActive, revision.State)
	assert.Equal(t, 1, fixture.revisionCount(t, entry.entryID), "no new revision")
	assert.True(t, entry.updatedAt.Equal(revision.UpdatedAt), "updated_at stays as it was")
	assert.Equal(t, &[]string{"SO4"}, revision.Metadata.Ligands)
	assert.Equal(t, &[]string{}, revision.Metadata.Cofactors)
	assert.Equal(t, new("kept"), revision.Metadata.Details)
	require.NotNil(t, revision.CompoundSources)
	assert.Equal(t, models.CompoundRuleList, revision.CompoundSources.Ligands["SO4"].Rule)
	assert.NotEmpty(t, revision.CompoundSources.Ligands["SO4"].ReviewReason)
	assert.Equal(t, &[]models.Glycan{}, revision.GlycanDescriptors)
	entity := fixture.entity(t, entry.entityID)
	assert.Equal(t, []string{"C54T"}, entity.Metadata.Mutations)
	assert.Equal(t, new("C4T"), entity.Metadata.MutationsRaw)
	assert.Equal(t, new("polymer"), entity.Metadata.Description)
	outcome, dryRun, attempts := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "derived", outcome)
	assert.False(t, dryRun)
	assert.Equal(t, 1, attempts)
	var before, after string
	require.NoError(t, fixture.raw.QueryRow(
		`select before::text, after::text from archive_rederivations where entry_revision_id = $1`, entry.revisionID,
	).Scan(&before, &after))
	assert.Contains(t, before, `"ligands": null`)
	assert.Contains(t, before, `"C4T"`)
	assert.Contains(t, after, `"SO4"`)
	assert.Contains(t, after, `"C54T"`)
	var unresolved int
	require.NoError(t, fixture.raw.Get(&unresolved, `select count(*) from schema_migration_review where resolved_at is null`))
	assert.Zero(t, unresolved)
}

func Test_should_keep_person_set_lists_and_still_derive_mutations_when_job_runs(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{Ligands: &[]string{"LIG"}})

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	revision := fixture.revision(t, entry.revisionID)
	assert.Equal(t, &[]string{"LIG"}, revision.Metadata.Ligands)
	assert.Nil(t, revision.Metadata.Cofactors)
	assert.Nil(t, revision.CompoundSources)
	assert.Equal(t, []string{"C54T"}, fixture.entity(t, entry.entityID).Metadata.Mutations)
	outcome, _, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "skipped_person_set", outcome)
}

func Test_should_keep_mutations_a_person_typed_when_job_runs(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{
		DepositorPolymerEntities: []models.DepositorPolymerEntity{{LabelEntityID: "1", Mutations: []string{"C4T"}}},
	})

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	assert.Equal(t, []string{"C4T"}, fixture.entity(t, entry.entityID).Metadata.Mutations)
	assert.Equal(t, &[]string{"SO4"}, fixture.revision(t, entry.revisionID).Metadata.Ligands)
}

func Test_should_skip_and_retry_later_when_entry_has_revision_in_review(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	_, err := fixture.database.Entries.Create(context.Background(), models.EntryRevision{
		ID:               uuid.New(),
		EntryID:          entry.entryID,
		ParentRevisionID: &entry.revisionID,
		State:            models.RevisionStateInReview,
		EntryState:       models.EntryStateActive,
		Title:            new("draft"),
		CreatedBy:        fixture.userID,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	})
	require.NoError(t, err)

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	assert.Nil(t, fixture.revision(t, entry.revisionID).Metadata.Ligands)
	assert.Equal(t, []string{"C4T"}, fixture.entity(t, entry.entityID).Metadata.Mutations)
	outcome, _, attempts := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "skipped_in_review", outcome)
	assert.Equal(t, 2, attempts)
	assert.Equal(t, 2, fixture.rcsb.requestsFor("1ABC"), "the second run tried again")
}

func Test_should_skip_revision_when_it_stops_being_active_during_the_run(t *testing.T) {
	// given: while the file is downloaded, a new revision of the entry is activated
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	read := fixture.revision(t, entry.revisionID)
	var successorID uuid.UUID
	var activateOnce sync.Once
	fixture.rcsb.onServe = func(string) {
		activateOnce.Do(func() {
			successor := read
			successor.ID = uuid.New()
			successor.ParentRevisionID = &read.ID
			successor.RevisionNumber = nil
			successor.PublishedAt = nil
			successor.State = models.RevisionStateInReview
			created, err := fixture.database.Entries.Create(context.Background(), successor)
			if !assert.NoError(t, err) {
				return
			}
			_, err = fixture.database.Entries.ActivateRevision(context.Background(), entry.entryID, created.ID)
			assert.NoError(t, err)
			successorID = created.ID
		})
	}

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	afterFirstRun := fixture.revision(t, entry.revisionID)
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	assert.Nil(t, afterFirstRun.Metadata.Ligands)
	assert.Equal(t, []string{"C4T"}, fixture.entity(t, entry.entityID).Metadata.Mutations)
	outcome, _, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "skipped_not_active", outcome)
	require.NotEqual(t, uuid.Nil, successorID)
	assert.Equal(t, &[]string{"SO4"}, fixture.revision(t, successorID).Metadata.Ligands, "the next run derives the new active revision")
	successorOutcome, _, _ := fixture.logRow(t, successorID)
	assert.Equal(t, "derived", successorOutcome)
}

func Test_should_log_failure_and_stop_after_three_runs_when_file_cannot_be_downloaded(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	entry := fixture.seedArchiveEntry(t, "9ZZZ", models.EntryMetadata{})

	// when
	for range 4 {
		require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	}

	// then
	outcome, _, attempts := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "failed", outcome)
	assert.Equal(t, 3, attempts)
	assert.Equal(t, 3, fixture.rcsb.requestsFor("9ZZZ"))
	var logged string
	require.NoError(t, fixture.raw.Get(&logged,
		`select error from archive_rederivations where entry_revision_id = $1`, entry.revisionID))
	assert.Contains(t, logged, "download RCSB mmCIF of 9ZZZ")
}

func Test_should_change_nothing_and_not_retry_when_file_ends_before_coordinates(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate + "#" + strings.Repeat(" ", 5*1024*1024)
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	assert.Nil(t, fixture.revision(t, entry.revisionID).Metadata.Ligands)
	assert.Equal(t, []string{"C4T"}, fixture.entity(t, entry.entityID).Metadata.Mutations)
	outcome, _, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "skipped_incomplete_file", outcome)
	assert.Equal(t, 1, fixture.rcsb.requestsFor("1ABC"))
}

func Test_should_only_log_when_job_runs_dry_and_apply_on_the_real_run(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{DryRun: true}).Run(context.Background()))
	afterDryRun := fixture.revision(t, entry.revisionID)
	dryOutcome, dryRun, _ := fixture.logRow(t, entry.revisionID)
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	assert.Nil(t, afterDryRun.Metadata.Ligands)
	assert.Nil(t, afterDryRun.CompoundSources)
	assert.Equal(t, "derived", dryOutcome)
	assert.True(t, dryRun)
	assert.Equal(t, &[]string{"SO4"}, fixture.revision(t, entry.revisionID).Metadata.Ligands)
	outcome, dryRun, attempts := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "derived", outcome)
	assert.False(t, dryRun)
	assert.Equal(t, 1, attempts)
}

func Test_should_not_read_the_file_again_when_revision_is_finished(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	assert.Equal(t, 1, fixture.rcsb.requestsFor("1ABC"))
	_, _, attempts := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, 1, attempts)
}

func Test_should_log_unchanged_when_derived_values_are_already_stored(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	_, err := fixture.raw.Exec(`delete from archive_rederivations`)
	require.NoError(t, err)

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	outcome, _, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "unchanged", outcome)
	var before *string
	require.NoError(t, fixture.raw.Get(&before,
		`select before::text from archive_rederivations where entry_revision_id = $1`, entry.revisionID))
	assert.Nil(t, before)
}

func Test_should_keep_lists_when_person_changed_them_after_they_were_derived(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{Ligands: &[]string{"SO4", "LIG"}})
	_, err := fixture.raw.Exec(
		`update entry_revisions
		 set compound_sources = '{"ligands":{"SO4":{"rule":"list"},"LIG":{"rule":"depositor"}},"cofactors":{},"excluded":{}}',
		     field_sources = '{"/metadata/ligands":{"source":"depositor"}}'
		 where id = $1`, entry.revisionID)
	require.NoError(t, err)

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	revision := fixture.revision(t, entry.revisionID)
	assert.Equal(t, &[]string{"SO4", "LIG"}, revision.Metadata.Ligands)
	assert.Equal(t, models.CompoundRuleDepositor, revision.CompoundSources.Ligands["LIG"].Rule)
	outcome, _, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "skipped_person_set", outcome)
}

func Test_should_stop_at_the_cap_and_continue_when_next_run_starts(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	fixture.rcsb.files["2ABC"] = mmCIFWithMutationAndSulfate
	fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	fixture.seedArchiveEntry(t, "2ABC", models.EntryMetadata{})
	logged := func() int {
		var count int
		require.NoError(t, fixture.raw.Get(&count, `select count(*) from archive_rederivations`))
		return count
	}

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{MaxEntriesPerRun: 1}).Run(context.Background()))
	afterFirstRun := logged()
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{MaxEntriesPerRun: 1}).Run(context.Background()))

	// then
	assert.Equal(t, 1, afterFirstRun)
	assert.Equal(t, 2, logged())
}

func Test_should_reject_config_when_it_cannot_run(t *testing.T) {
	// given
	valid := ArchiveRederiveConfig{PageSize: 200, DownloadConcurrency: 4, RunFor: time.Minute}
	cases := map[string]func(*ArchiveRederiveConfig){
		"negative cap":     func(config *ArchiveRederiveConfig) { config.MaxEntriesPerRun = -1 },
		"no page size":     func(config *ArchiveRederiveConfig) { config.PageSize = 0 },
		"no download slot": func(config *ArchiveRederiveConfig) { config.DownloadConcurrency = 0 },
		"no time to run":   func(config *ArchiveRederiveConfig) { config.RunFor = 0 },
	}

	for name, change := range cases {
		config := valid
		change(&config)

		// when
		err := config.validate()

		// then
		assert.Error(t, err, name)
	}
	assert.NoError(t, valid.validate())
}

func (f *rederiveFixture) runRevertScript(t *testing.T) {
	t.Helper()
	script, err := os.ReadFile(filepath.Join("..", "..", "migrations", "inventory", "revert_archive_rederivation.sql"))
	require.NoError(t, err)
	_, err = f.raw.Exec(string(script))
	require.NoError(t, err)
}

func Test_should_put_back_logged_values_and_reopen_review_rows_when_revert_script_runs(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{Details: new("kept")})
	_, err := fixture.raw.Exec(`insert into schema_migration_review(object_type, object_id, issue) values
		('entry', $1, 'ligands_cofactors_rederive_from_archive'),
		('polymer_entity', $2, 'mutations_need_review')`, entry.entryID, entry.entityID.String())
	require.NoError(t, err)
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// when
	fixture.runRevertScript(t)

	// then
	revision := fixture.revision(t, entry.revisionID)
	assert.Nil(t, revision.Metadata.Ligands)
	assert.Nil(t, revision.Metadata.Cofactors)
	assert.Nil(t, revision.CompoundSources)
	assert.Nil(t, revision.GlycanDescriptors)
	assert.Equal(t, new("kept"), revision.Metadata.Details)
	entity := fixture.entity(t, entry.entityID)
	assert.Equal(t, []string{"C4T"}, entity.Metadata.Mutations)
	assert.Equal(t, new("C4T"), entity.Metadata.MutationsRaw)
	assert.Equal(t, new(models.MutationsStatusParsed), entity.Metadata.MutationsStatus)
	var resolved, logged int
	require.NoError(t, fixture.raw.Get(&resolved, `select count(*) from schema_migration_review where resolved_at is not null`))
	require.NoError(t, fixture.raw.Get(&logged, `select count(*) from archive_rederivations`))
	assert.Zero(t, resolved)
	assert.Zero(t, logged, "a later run derives the revision again")
}

func Test_should_keep_revision_changed_since_when_revert_script_runs(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	_, err := fixture.raw.Exec(
		`update entry_revisions set metadata = metadata || '{"ligands":["SO4","LIG"]}' where id = $1`, entry.revisionID)
	require.NoError(t, err)

	// when
	fixture.runRevertScript(t)

	// then
	assert.Equal(t, &[]string{"SO4", "LIG"}, fixture.revision(t, entry.revisionID).Metadata.Ligands)
	assert.Equal(t, []string{"C54T"}, fixture.entity(t, entry.entityID).Metadata.Mutations,
		"the revision is reverted as a whole or not at all")
	outcome, _, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "derived", outcome)
}

func Test_should_resolve_entry_review_row_when_job_finds_lists_already_derived(t *testing.T) {
	// given: the sync derived the lists after the migration flagged the entry
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	_, err := fixture.raw.Exec(`delete from archive_rederivations`)
	require.NoError(t, err)
	_, err = fixture.raw.Exec(`insert into schema_migration_review(object_type, object_id, issue)
		values ('entry', $1, 'ligands_cofactors_rederive_from_archive')`, entry.entryID)
	require.NoError(t, err)

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	outcome, _, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "unchanged", outcome)
	var resolvedBy *string
	require.NoError(t, fixture.raw.Get(&resolvedBy, `select resolved_by from schema_migration_review where object_id = $1`, entry.entryID))
	assert.Equal(t, new("archive-rederive-job"), resolvedBy)
}

func Test_should_leave_entry_review_row_open_when_person_set_the_lists(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{Ligands: &[]string{"LIG"}})
	_, err := fixture.raw.Exec(`insert into schema_migration_review(object_type, object_id, issue)
		values ('entry', $1, 'ligands_cofactors_rederive_from_archive')`, entry.entryID)
	require.NoError(t, err)

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// then
	var resolvedBy *string
	require.NoError(t, fixture.raw.Get(&resolvedBy, `select resolved_by from schema_migration_review where object_id = $1`, entry.entryID))
	assert.Nil(t, resolvedBy)
}

func Test_should_log_the_skip_a_real_run_would_make_when_job_runs_dry_on_entry_in_review(t *testing.T) {
	// given
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	_, err := fixture.database.Entries.Create(context.Background(), models.EntryRevision{
		ID:               uuid.New(),
		EntryID:          entry.entryID,
		ParentRevisionID: &entry.revisionID,
		State:            models.RevisionStateInReview,
		EntryState:       models.EntryStateActive,
		Title:            new("draft"),
		CreatedBy:        fixture.userID,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	})
	require.NoError(t, err)

	// when
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{DryRun: true}).Run(context.Background()))

	// then
	outcome, dryRun, _ := fixture.logRow(t, entry.revisionID)
	assert.Equal(t, "skipped_in_review", outcome)
	assert.True(t, dryRun)
}

func Test_should_keep_entry_review_row_resolved_when_revert_script_puts_back_only_mutations(t *testing.T) {
	// given: a run that changed only an entity's mutations, the lists being derived already
	fixture := newRederiveFixture(t)
	fixture.rcsb.files["1ABC"] = mmCIFWithMutationAndSulfate
	entry := fixture.seedArchiveEntry(t, "1ABC", models.EntryMetadata{})
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))
	_, err := fixture.raw.Exec(`delete from archive_rederivations`)
	require.NoError(t, err)
	require.NoError(t, fixture.database.PolymerEntities.UpdateMutations(context.Background(), models.ArchivePolymerEntityMutations{
		ID: entry.entityID, Mutations: []string{"C4T"}, MutationsRaw: new("C4T"), MutationsStatus: new(models.MutationsStatusParsed),
	}))
	_, err = fixture.raw.Exec(`insert into schema_migration_review(object_type, object_id, issue)
		values ('entry', $1, 'ligands_cofactors_rederive_from_archive')`, entry.entryID)
	require.NoError(t, err)
	require.NoError(t, fixture.job(t, ArchiveRederiveConfig{}).Run(context.Background()))

	// when
	fixture.runRevertScript(t)

	// then
	assert.Equal(t, []string{"C4T"}, fixture.entity(t, entry.entityID).Metadata.Mutations)
	assert.Equal(t, &[]string{"SO4"}, fixture.revision(t, entry.revisionID).Metadata.Ligands)
	var resolvedBy *string
	require.NoError(t, fixture.raw.Get(&resolvedBy, `select resolved_by from schema_migration_review where object_id = $1`, entry.entryID))
	assert.Equal(t, new("archive-rederive-job"), resolvedBy)
}
