package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

func fixturePDBRederivation(outcome models.ArchiveRederivationOutcome, dryRun bool) models.ArchiveRederivation {
	row := models.ArchiveRederivation{
		ArchiveRederivationCandidate: models.ArchiveRederivationCandidate{
			EntryRevisionID: uuid.MustParse(fixturePDBRevision),
			EntryID:         "e_pdb",
			PDBID:           "1ABC",
		},
		Outcome:          outcome,
		DryRun:           dryRun,
		ReferenceVersion: ptr(fixtureReferenceVersion),
	}
	switch outcome {
	case models.ArchiveRederivationDerived:
		row.Before = &models.ArchiveRederivationValues{Compounds: &models.EntryComposition{}}
		row.After = &models.ArchiveRederivationValues{Compounds: &models.EntryComposition{Ligands: &[]string{"HEM"}}}
	case models.ArchiveRederivationFailed:
		row.Error = ptr("download RCSB mmCIF of 1ABC: not found")
	case models.ArchiveRederivationUnchanged, models.ArchiveRederivationSkippedInReview:
	default:
		panic("fixturePDBRederivation: unsupported outcome " + string(outcome))
	}
	return row
}

func (fixture *migratedFixture) rederivationRow(t *testing.T) (outcome string, dryRun bool, attempts int) {
	t.Helper()
	require.NoError(t, fixture.database.QueryRow(
		`select outcome, dry_run, attempts from archive_rederivations where entry_revision_id = $1`, fixturePDBRevision,
	).Scan(&outcome, &dryRun, &attempts))
	return outcome, dryRun, attempts
}

// activateEntries gives the fixture's entries and revisions the active entry state that archive
// entries have; the old-shape fixture leaves both at the column default, new.
func (fixture *migratedFixture) activateEntries(t *testing.T) {
	t.Helper()
	_, err := fixture.database.Exec(`update entries set state = 'active'`)
	require.NoError(t, err)
	_, err = fixture.database.Exec(`update entry_revisions set entry_state = 'active'`)
	require.NoError(t, err)
}

func Test_should_count_attempts_when_real_runs_log_the_same_revision(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	repository := fixture.appDatabase(t).ArchiveRederivations
	ctx := context.Background()

	// when
	require.NoError(t, repository.Record(ctx, fixturePDBRederivation(models.ArchiveRederivationFailed, false)))
	require.NoError(t, repository.Record(ctx, fixturePDBRederivation(models.ArchiveRederivationFailed, false)))

	// then
	outcome, dryRun, attempts := fixture.rederivationRow(t)
	assert.Equal(t, "failed", outcome)
	assert.False(t, dryRun)
	assert.Equal(t, 2, attempts)
}

func Test_should_keep_real_row_when_dry_run_logs_the_same_revision(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	repository := fixture.appDatabase(t).ArchiveRederivations
	ctx := context.Background()
	require.NoError(t, repository.Record(ctx, fixturePDBRederivation(models.ArchiveRederivationFailed, false)))

	// when
	err := repository.Record(ctx, fixturePDBRederivation(models.ArchiveRederivationDerived, true))

	// then
	require.NoError(t, err)
	outcome, dryRun, attempts := fixture.rederivationRow(t)
	assert.Equal(t, "failed", outcome)
	assert.False(t, dryRun)
	assert.Equal(t, 1, attempts)
}

func Test_should_replace_dry_run_row_and_start_counting_when_real_run_logs_the_revision(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	repository := fixture.appDatabase(t).ArchiveRederivations
	ctx := context.Background()
	require.NoError(t, repository.Record(ctx, fixturePDBRederivation(models.ArchiveRederivationDerived, true)))

	// when
	err := repository.Record(ctx, fixturePDBRederivation(models.ArchiveRederivationDerived, false))

	// then
	require.NoError(t, err)
	outcome, dryRun, attempts := fixture.rederivationRow(t)
	assert.Equal(t, "derived", outcome)
	assert.False(t, dryRun)
	assert.Equal(t, 1, attempts)
	after := fixture.jsonbAt(t, `select after from archive_rederivations where entry_revision_id = $1`, fixturePDBRevision)
	assert.Equal(t, []any{"HEM"}, after["compounds"].(map[string]any)["ligands"])
}

func Test_should_list_only_unfinished_active_archive_revisions_when_candidates_are_listed(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	fixture.activateEntries(t)
	repository := fixture.appDatabase(t).ArchiveRederivations
	ctx := context.Background()
	cases := map[string]struct {
		row      *models.ArchiveRederivation
		attempts int
		listed   bool
	}{
		"no row":             {listed: true},
		"dry run row":        {row: new(fixturePDBRederivation(models.ArchiveRederivationDerived, true)), listed: true},
		"in review":          {row: new(fixturePDBRederivation(models.ArchiveRederivationSkippedInReview, false)), listed: true},
		"failed twice":       {row: new(fixturePDBRederivation(models.ArchiveRederivationFailed, false)), attempts: 2, listed: true},
		"failed three times": {row: new(fixturePDBRederivation(models.ArchiveRederivationFailed, false)), attempts: 3},
		"derived":            {row: new(fixturePDBRederivation(models.ArchiveRederivationDerived, false))},
		"unchanged":          {row: new(fixturePDBRederivation(models.ArchiveRederivationUnchanged, false))},
	}

	for name, testCase := range cases {
		_, err := fixture.database.Exec(`delete from archive_rederivations`)
		require.NoError(t, err, name)
		if testCase.row != nil {
			require.NoError(t, repository.Record(ctx, *testCase.row), name)
		}
		if testCase.attempts > 0 {
			_, err := fixture.database.Exec(`update archive_rederivations set attempts = $1`, testCase.attempts)
			require.NoError(t, err, name)
		}

		// when
		candidates, err := repository.ListCandidates(ctx, uuid.Nil, 10, 3)

		// then
		require.NoError(t, err, name)
		if testCase.listed {
			assert.Equal(t, []models.ArchiveRederivationCandidate{{
				EntryRevisionID: uuid.MustParse(fixturePDBRevision), EntryID: "e_pdb", PDBID: "1ABC",
			}}, candidates, name)
		} else {
			assert.Empty(t, candidates, name)
		}
	}
}

func Test_should_list_only_later_revisions_when_candidates_are_paged(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	fixture.activateEntries(t)
	repository := fixture.appDatabase(t).ArchiveRederivations

	// when
	candidates, err := repository.ListCandidates(context.Background(), uuid.MustParse(fixturePDBRevision), 10, 3)

	// then
	require.NoError(t, err)
	assert.Empty(t, candidates)
}

func Test_should_resolve_only_open_rows_of_given_objects_when_review_rows_are_resolved(t *testing.T) {
	// given
	fixture := newMigratedFixture(t)
	repository := fixture.appDatabase(t).ArchiveRederivations
	ctx := context.Background()
	resolution := models.MigrationReviewResolution{
		ObjectType: "entry",
		Issue:      "ligands_cofactors_rederive_from_archive",
		ObjectIDs:  []string{"e_pdb", "e_missing"},
		ResolvedBy: "archive-rederive-job",
	}
	resolvedAt := func() *time.Time {
		var at *time.Time
		require.NoError(t, fixture.database.Get(&at,
			`select resolved_at from schema_migration_review
			 where object_type = 'entry' and object_id = 'e_pdb' and issue = 'ligands_cofactors_rederive_from_archive'`))
		return at
	}

	// when
	require.NoError(t, repository.ResolveMigrationReviews(ctx, resolution))
	first := resolvedAt()
	require.NoError(t, repository.ResolveMigrationReviews(ctx, resolution))

	// then
	require.NotNil(t, first)
	assert.Equal(t, first, resolvedAt(), "a resolved row keeps when it was resolved")
	var resolvedBy string
	require.NoError(t, fixture.database.Get(&resolvedBy,
		`select resolved_by from schema_migration_review
		 where object_type = 'entry' and object_id = 'e_pdb' and issue = 'ligands_cofactors_rederive_from_archive'`))
	assert.Equal(t, "archive-rederive-job", resolvedBy)
	var otherResolved int
	require.NoError(t, fixture.database.Get(&otherResolved,
		`select count(*) from schema_migration_review where resolved_at is not null and object_id <> 'e_pdb'`))
	assert.Zero(t, otherResolved)
}

func Test_should_set_composition_in_place_and_keep_other_metadata_when_composition_is_updated(t *testing.T) {
	// given
	ctx := context.Background()
	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	revision := dbTestEntryRevisionWithPDBID(t, "composition", "1ABC", models.RevisionStateInReview, createdAt)
	revision.EntryState = models.EntryStateActive
	revision.Metadata.Details = ptr("kept details")
	created, err := testDB.Entries.Create(ctx, revision)
	require.NoError(t, err)
	active, err := testDB.Entries.ActivateRevision(ctx, created.EntryID, created.ID)
	require.NoError(t, err)
	record := &models.CompoundSources{
		Ligands:   map[string]models.CompoundSource{"NAG": {Rule: models.CompoundRuleBranched, ListVersion: "2026.10.0"}},
		Cofactors: map[string]models.CompoundSource{},
		Excluded:  map[string]models.CompoundSource{},
	}
	glycans := &[]models.Glycan{{EntityID: "2", CompoundIDs: []string{"NAG"}, Descriptors: []models.GlycanDescriptor{}}}

	// when
	err = testDB.Entries.UpdateComposition(ctx, active.ID, models.EntryComposition{
		Ligands: &[]string{"NAG"}, Cofactors: &[]string{}, CompoundSources: record, GlycanDescriptors: glycans,
	})
	require.NoError(t, err)
	got, err := testDB.Entries.Get(ctx, db.EntryRevisionFilters{ID: &active.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, &[]string{"NAG"}, got.Metadata.Ligands)
	assert.Equal(t, &[]string{}, got.Metadata.Cofactors)
	assert.Equal(t, ptr("kept details"), got.Metadata.Details)
	assert.Equal(t, "1ABC", got.Metadata.ExternalRefs[models.EntrySourcePDB])
	assert.Equal(t, record, got.CompoundSources)
	assert.Equal(t, glycans, got.GlycanDescriptors)
	assert.True(t, active.UpdatedAt.Equal(got.UpdatedAt), "updated_at stays as it was")
}

func Test_should_refuse_composition_update_when_revision_is_not_active(t *testing.T) {
	// given
	revision := createDBTestEntryRevisionWithPDBID(t, "pending composition", "1ABC", models.RevisionStatePending, time.Now().UTC())

	// when
	err := testDB.Entries.UpdateComposition(context.Background(), revision.ID, models.EntryComposition{
		Ligands: &[]string{}, Cofactors: &[]string{},
	})

	// then
	require.ErrorIs(t, err, db.ErrEntryRevisionNotFound)
}

func Test_should_set_only_mutation_fields_when_polymer_entity_mutations_are_updated(t *testing.T) {
	// given
	ctx := context.Background()
	now := time.Now().UTC()
	entryRevision := createDBTestEntryRevision(t, "mutations-entry", now)
	artifact := createDBTestArtifact(t, entryRevision.CreatedBy, "mutations sequence", now)
	require.NoError(t, testDB.ProteinSequences.Create(ctx, entryRevision.ID, artifact.ID, []models.FASTARecord{
		{Header: "polymer", Sequence: "ACDEFGHIK"},
	}))
	sequences, err := testDB.ProteinSequences.List(ctx, db.ProteinSequenceFilters{EntryRevisionID: &entryRevision.ID})
	require.NoError(t, err)
	entity, err := testDB.PolymerEntities.Create(ctx, models.PolymerEntity{
		ID:                uuid.New(),
		ProteinSequenceID: sequences[0].ID,
		Metadata: models.PolymerEntityMetadata{
			LabelEntityID:   ptr("1"),
			Description:     ptr("kept description"),
			Mutations:       []string{"C4T"},
			MutationsRaw:    ptr("C4T, X5"),
			MutationsStatus: ptr(models.MutationsStatusNeedsReview),
		},
		CreatedAt: now,
	})
	require.NoError(t, err)

	// when
	err = testDB.PolymerEntities.UpdateMutations(ctx, models.ArchivePolymerEntityMutations{
		ID:              entity.ID,
		Mutations:       []string{"C54T"},
		MutationsStatus: ptr(models.MutationsStatusParsed),
	})
	require.NoError(t, err)
	got, err := testDB.PolymerEntities.Get(ctx, db.PolymerEntityFilters{ID: &entity.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, models.PolymerEntityMetadata{
		LabelEntityID:   ptr("1"),
		Description:     ptr("kept description"),
		Mutations:       []string{"C54T"},
		MutationsStatus: ptr(models.MutationsStatusParsed),
	}, got.Metadata)
}
