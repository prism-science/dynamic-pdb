package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/types"
	"dynamic-pdb/lib/cif"
)

// modeledCompoundsTestDB is a schema of its own, so claims see only the sets a test made.
type modeledCompoundsTestDB struct {
	database *db.DB
	sql      *sqlx.DB
	userID   uuid.UUID
}

func newModeledCompoundsTestDB(t *testing.T) *modeledCompoundsTestDB {
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

	now := time.Now().UTC()
	user, err := database.Users.Create(context.Background(), models.User{
		ID:          uuid.New(),
		ExternalRef: types.ExternalRef{Source: "test", Value: uuid.NewString()},
		Email:       "user-" + uuid.NewString() + "@example.com",
		DisplayName: "Modeled Compounds Test User",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	return &modeledCompoundsTestDB{database: database, sql: rawSQL, userID: user.ID}
}

// createArtifact makes an artifact; a stored one gets a URI in its own folder in our bucket,
// the others an RCSB URL.
func (f *modeledCompoundsTestDB) createArtifact(t *testing.T, artifactType models.ArtifactType, stored bool) *models.Artifact {
	t.Helper()

	id := uuid.New()
	uri := "https://files.rcsb.org/download/1ABC.cif"
	if stored {
		uri = fmt.Sprintf("https://bucket.example.org/entry-1/models/model-1/artifacts/%s/model.cif", id)
	}
	format := "mmcif"
	artifact, err := f.database.Artifacts.Create(context.Background(), models.Artifact{
		ID:        id,
		Name:      "model.cif",
		Level:     models.ArtifactLevelL1,
		Type:      artifactType,
		URI:       &uri,
		Format:    &format,
		Metadata:  map[string]any{},
		CreatedBy: f.userID,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	return artifact
}

func (f *modeledCompoundsTestDB) setStatus(t *testing.T, artifactID uuid.UUID) string {
	t.Helper()

	var status string
	err := f.sql.Get(&status, `select status from modeled_compound_sets where artifact_id = $1`, artifactID)
	require.NoError(t, err)
	return status
}

func (f *modeledCompoundsTestDB) save(t *testing.T, artifactID uuid.UUID, result cif.ModeledCompounds) {
	t.Helper()

	_, err := f.database.SaveModeledCompounds(context.Background(), db.SaveModeledCompoundsParams{
		ArtifactID: artifactID,
		CreatedBy:  f.userID,
		Result:     result,
		ReadAt:     time.Now().UTC(),
	})
	require.NoError(t, err)
}

func Test_should_give_only_a_model_file_in_our_bucket_a_pending_set_when_artifacts_are_created(t *testing.T) {
	// given
	fixture := newModeledCompoundsTestDB(t)

	// when
	stored := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	archive := fixture.createArtifact(t, models.ArtifactTypeModel, false)
	storedOther := fixture.createArtifact(t, models.ArtifactTypeStructureFactors, true)

	// then
	assert.Equal(t, "pending", fixture.setStatus(t, stored.ID))
	for _, artifact := range []*models.Artifact{archive, storedOther} {
		_, err := fixture.database.ModeledCompounds.Get(context.Background(), artifact.ID)
		require.ErrorIs(t, err, db.ErrModeledCompoundSetNotFound)
	}
}

func Test_should_claim_a_due_set_once_when_a_second_claim_comes_within_the_lease(t *testing.T) {
	// given
	fixture := newModeledCompoundsTestDB(t)
	artifact := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	now := time.Now().UTC().Add(time.Second)

	// when
	first, err := fixture.database.ModeledCompounds.ClaimDue(context.Background(), now, 10, time.Hour, 3)
	require.NoError(t, err)
	second, err := fixture.database.ModeledCompounds.ClaimDue(context.Background(), now, 10, time.Hour, 3)
	require.NoError(t, err)

	// then
	require.Len(t, first, 1)
	assert.Equal(t, artifact.ID, first[0].ArtifactID)
	assert.Equal(t, 1, first[0].Attempts)
	assert.Equal(t, artifact.URI, first[0].URI)
	assert.Equal(t, artifact.Format, first[0].Format)
	assert.Empty(t, second)
}

func Test_should_store_the_copies_and_finish_the_set_when_a_read_is_saved(t *testing.T) {
	// given
	fixture := newModeledCompoundsTestDB(t)
	artifact := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	result := cif.ModeledCompounds{
		Copies: []cif.CompoundCopy{
			{CCDID: "GOL", LabelAsymID: "C", AuthAsymID: "A", AuthSeqID: "301", EntityID: "3", Source: cif.SourceNonpolyScheme},
			{CCDID: "MSE", AuthAsymID: "A", AuthSeqID: "2", Source: cif.SourcePDBHetatm, NeedsReview: true, ReviewReason: "could be a chain residue"},
		},
		Complete: false,
		Warnings: []string{"more than 100000 compound copies: the rest were not listed"},
	}

	// when
	fixture.save(t, artifact.ID, result)

	// then
	set, err := fixture.database.ModeledCompounds.Get(context.Background(), artifact.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ModeledCompoundStatusDone, set.Status)
	require.NotNil(t, set.ParserVersion)
	assert.Equal(t, cif.ParserVersion, *set.ParserVersion)
	require.NotNil(t, set.Complete)
	assert.False(t, *set.Complete)
	assert.Equal(t, result.Warnings, set.Warnings)
	assert.NotNil(t, set.ReadAt)
	assert.Equal(t, []models.ModeledCompoundCopy{
		{CCDID: "GOL", LabelAsymID: "C", AuthAsymID: "A", AuthSeqID: "301", EntityID: "3", Source: models.ModeledCompoundSourceNonpolyScheme},
		{CCDID: "MSE", AuthAsymID: "A", AuthSeqID: "2", Source: models.ModeledCompoundSourcePDBHetatm, NeedsReview: true, ReviewReason: "could be a chain residue"},
	}, set.Copies)
}

func Test_should_replace_the_old_copies_when_a_file_is_read_again(t *testing.T) {
	// given: a done set that a later parser version sends back to pending.
	fixture := newModeledCompoundsTestDB(t)
	artifact := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	fixture.save(t, artifact.ID, cif.ModeledCompounds{
		Copies:   []cif.CompoundCopy{{CCDID: "SO4", AuthAsymID: "A", AuthSeqID: "1", Source: cif.SourcePDBHetatm}},
		Complete: true,
	})
	_, err := fixture.sql.Exec(`update modeled_compound_sets set status = 'pending' where artifact_id = $1`, artifact.ID)
	require.NoError(t, err)

	// when
	fixture.save(t, artifact.ID, cif.ModeledCompounds{
		Copies:   []cif.CompoundCopy{{CCDID: "PO4", AuthAsymID: "A", AuthSeqID: "1", Source: cif.SourcePDBHetatm}},
		Complete: true,
	})

	// then
	set, err := fixture.database.ModeledCompounds.Get(context.Background(), artifact.ID)
	require.NoError(t, err)
	require.Len(t, set.Copies, 1)
	assert.Equal(t, "PO4", set.Copies[0].CCDID)
}

func Test_should_refuse_to_save_when_the_set_is_no_longer_pending(t *testing.T) {
	// given: a set another run already finished.
	fixture := newModeledCompoundsTestDB(t)
	artifact := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	fixture.save(t, artifact.ID, cif.ModeledCompounds{Complete: true})

	// when
	_, err := fixture.database.SaveModeledCompounds(context.Background(), db.SaveModeledCompoundsParams{
		ArtifactID: artifact.ID,
		CreatedBy:  fixture.userID,
		Result:     cif.ModeledCompounds{Copies: []cif.CompoundCopy{{CCDID: "ZN", Source: cif.SourceAtomSite}}, Complete: true},
		ReadAt:     time.Now().UTC(),
	})

	// then: nothing of the second save is kept.
	require.ErrorIs(t, err, db.ErrModeledCompoundSetNotFound)
	set, err := fixture.database.ModeledCompounds.Get(context.Background(), artifact.ID)
	require.NoError(t, err)
	assert.Empty(t, set.Copies)
}

func Test_should_retry_and_then_fail_when_reads_fail(t *testing.T) {
	// given
	fixture := newModeledCompoundsTestDB(t)
	artifact := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	retryAt := time.Now().UTC().Add(time.Hour)

	// when
	err := fixture.database.ModeledCompounds.RecordFailure(context.Background(), artifact.ID, "read timed out", &retryAt)
	require.NoError(t, err)
	retried, err := fixture.database.ModeledCompounds.Get(context.Background(), artifact.ID)
	require.NoError(t, err)
	claims, err := fixture.database.ModeledCompounds.ClaimDue(context.Background(), time.Now().UTC(), 10, time.Hour, 3)
	require.NoError(t, err)
	err = fixture.database.ModeledCompounds.RecordFailure(context.Background(), artifact.ID, "not a structure file", nil)
	require.NoError(t, err)

	// then
	assert.Equal(t, models.ModeledCompoundStatusPending, retried.Status)
	assert.Equal(t, ptr("read timed out"), retried.LastError)
	assert.Empty(t, claims, "a set is not due before its retry time")
	failed, err := fixture.database.ModeledCompounds.Get(context.Background(), artifact.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ModeledCompoundStatusFailed, failed.Status)
	assert.Equal(t, ptr("not a structure file"), failed.LastError)
}

func Test_should_find_the_entry_by_a_file_compound_when_its_active_model_uses_the_file(t *testing.T) {
	// given: an active entry with an active model whose file is ours.
	fixture := newModeledCompoundsTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	artifact := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	entry, err := fixture.database.Entries.Create(ctx, models.EntryRevision{
		ID: uuid.New(), EntryID: "entry-" + uuid.NewString(), State: models.RevisionStateInReview,
		EntryState: models.EntryStateActive, Title: ptr("modeled compounds"), CreatedBy: fixture.userID,
		CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	_, err = fixture.database.Entries.ActivateRevision(ctx, entry.EntryID, entry.ID)
	require.NoError(t, err)
	model, err := fixture.database.Models.Create(ctx, entry.EntryID, models.ModelRevision{
		ID: uuid.New(), ModelID: "model-" + uuid.NewString(), State: models.RevisionStateInReview,
		ModelState: models.ModelStateActive, PrimaryArtifactID: &artifact.ID, Title: ptr("model"),
		CreatedBy: fixture.userID, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	_, err = fixture.database.Models.ActivateRevision(ctx, model.ID)
	require.NoError(t, err)

	// when
	reindexed, err := fixture.database.SaveModeledCompounds(ctx, db.SaveModeledCompoundsParams{
		ArtifactID: artifact.ID,
		CreatedBy:  fixture.userID,
		Result:     cif.ModeledCompounds{Copies: []cif.CompoundCopy{{CCDID: "ZZ7", Source: cif.SourceAtomSite}}, Complete: true},
		ReadAt:     now,
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, 1, reindexed)
	revisions, err := fixture.database.Entries.List(ctx, db.EntryRevisionFilters{Query: "ZZ7"})
	require.NoError(t, err)
	assert.True(t, entryRevisionListContainsEntryID(revisions, entry.EntryID))
}

// insertArtifactLikeOlderCode adds an artifact the way code from before the sets did: no set.
func (f *modeledCompoundsTestDB) insertArtifactLikeOlderCode(t *testing.T, artifactType string, stored bool, createdAt time.Time) uuid.UUID {
	t.Helper()

	id := uuid.New()
	uri := "https://files.rcsb.org/download/1ABC.cif"
	if stored {
		uri = fmt.Sprintf("https://bucket.example.org/entry-1/models/model-1/artifacts/%s/model.cif", id)
	}
	_, err := f.sql.Exec(`insert into artifacts(id, name, level, type, uri, format, metadata, created_by, created_at)
		values ($1, 'model.cif', 'L1', $2, $3, 'mmcif', '{}', $4, $5)`, id, artifactType, uri, f.userID, createdAt)
	require.NoError(t, err)
	return id
}

func (f *modeledCompoundsTestDB) hasSet(t *testing.T, artifactID uuid.UUID) bool {
	t.Helper()

	var count int
	require.NoError(t, f.sql.Get(&count, `select count(*) from modeled_compound_sets where artifact_id = $1`, artifactID))
	return count == 1
}

func Test_should_fail_a_set_instead_of_claiming_it_when_its_reads_never_came_back(t *testing.T) {
	// given: a set claimed three times, each claim lapsing because the job's run ended.
	fixture := newModeledCompoundsTestDB(t)
	artifact := fixture.createArtifact(t, models.ArtifactTypeModel, true)
	start := time.Now().UTC().Add(time.Second)
	for attempt := range 3 {
		claims, err := fixture.database.ModeledCompounds.ClaimDue(context.Background(), start.Add(time.Duration(attempt)*2*time.Hour), 10, time.Hour, 3)
		require.NoError(t, err)
		require.Len(t, claims, 1)
	}

	// when: the third lease has lapsed too.
	claims, err := fixture.database.ModeledCompounds.ClaimDue(context.Background(), start.Add(6*time.Hour), 10, time.Hour, 3)

	// then
	require.NoError(t, err)
	assert.Empty(t, claims)
	set, err := fixture.database.ModeledCompounds.Get(context.Background(), artifact.ID)
	require.NoError(t, err)
	assert.Equal(t, models.ModeledCompoundStatusFailed, set.Status)
	assert.Equal(t, ptr("the read stopped without a result 3 times: the job ran out of memory or time"), set.LastError)
}

func Test_should_give_only_recent_model_files_in_our_bucket_a_set_when_older_code_stored_them(t *testing.T) {
	// given
	fixture := newModeledCompoundsTestDB(t)
	now := time.Now().UTC()
	recent := fixture.insertArtifactLikeOlderCode(t, "model", true, now.Add(-time.Hour))
	old := fixture.insertArtifactLikeOlderCode(t, "model", true, now.Add(-72*time.Hour))
	archive := fixture.insertArtifactLikeOlderCode(t, "model", false, now.Add(-time.Hour))
	maps := fixture.insertArtifactLikeOlderCode(t, "structure_factors", true, now.Add(-time.Hour))

	// when
	added, err := fixture.database.ModeledCompounds.AddMissingSince(context.Background(), now.Add(-48*time.Hour))

	// then
	require.NoError(t, err)
	assert.Equal(t, int64(1), added)
	assert.True(t, fixture.hasSet(t, recent))
	assert.False(t, fixture.hasSet(t, old))
	assert.False(t, fixture.hasSet(t, archive))
	assert.False(t, fixture.hasSet(t, maps))
}

func Test_should_give_every_stored_model_file_a_set_once_when_the_historical_fill_pages_through_artifacts(t *testing.T) {
	// given
	fixture := newModeledCompoundsTestDB(t)
	long := time.Now().UTC().Add(-365 * 24 * time.Hour)
	stored := fixture.insertArtifactLikeOlderCode(t, "model", true, long)
	archive := fixture.insertArtifactLikeOlderCode(t, "model", false, long)
	maps := fixture.insertArtifactLikeOlderCode(t, "structure_factors", true, long)

	// when: pages of two artifacts, then the whole fill again.
	added := int64(0)
	for _, pass := range []string{"first", "again"} {
		after := uuid.Nil
		for {
			page, err := fixture.database.ModeledCompounds.AddMissingAfter(context.Background(), after, 2)
			require.NoError(t, err, pass)
			added += page.Added
			if page.Scanned < 2 {
				break
			}
			after = page.LastArtifactID
		}
	}

	// then
	assert.Equal(t, int64(1), added, "the second pass adds nothing")
	assert.True(t, fixture.hasSet(t, stored))
	assert.False(t, fixture.hasSet(t, archive))
	assert.False(t, fixture.hasSet(t, maps))
}
