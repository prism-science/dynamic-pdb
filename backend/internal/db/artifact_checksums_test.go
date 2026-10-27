package db_test

import (
	"context"
	"slices"
	"strings"
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

// checksumTestDB is a schema of its own, so claims see only the rows a test made.
type checksumTestDB struct {
	database *db.DB
	sql      *sqlx.DB
	userID   uuid.UUID
}

func newChecksumTestDB(t *testing.T) *checksumTestDB {
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
		DisplayName: "Checksum Test User",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	require.NoError(t, err)
	return &checksumTestDB{database: database, sql: rawSQL, userID: user.ID}
}

func (f *checksumTestDB) createArtifact(t *testing.T, uri string, sha256 *string, sizeBytes *int64) *models.Artifact {
	t.Helper()

	artifact, err := f.database.Artifacts.Create(context.Background(), models.Artifact{
		ID:        uuid.New(),
		Name:      "file-" + uuid.NewString() + ".cif",
		Level:     models.ArtifactLevelL1,
		Type:      models.ArtifactTypeModel,
		URI:       &uri,
		SHA256:    sha256,
		SizeBytes: sizeBytes,
		Metadata:  map[string]any{},
		CreatedBy: f.userID,
		CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	return artifact
}

// insertArtifactWithoutChecksum inserts an artifact the way code without the checksum table does.
func (f *checksumTestDB) insertArtifactWithoutChecksum(t *testing.T, uri string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := f.sql.Exec(`insert into artifacts(id, name, level, type, uri, created_by)
		values ($1, $2, 'L1', 'model', $3, $4)`, id, "old-"+id.String(), uri, f.userID)
	require.NoError(t, err)
	return id
}

func (f *checksumTestDB) setNextAttemptAt(t *testing.T, artifactID uuid.UUID, at time.Time) {
	t.Helper()

	_, err := f.sql.Exec(`update artifact_checksums set next_attempt_at = $2 where artifact_id = $1`, artifactID, at)
	require.NoError(t, err)
}

func (f *checksumTestDB) listArtifact(t *testing.T, artifactID uuid.UUID) models.Artifact {
	t.Helper()

	artifacts, err := f.database.Artifacts.List(context.Background(), db.ArtifactFilters{ID: &artifactID})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	return artifacts[0]
}

type checksumRow struct {
	Status        string     `db:"status"`
	Attempts      int        `db:"attempts"`
	NextAttemptAt time.Time  `db:"next_attempt_at"`
	LastError     *string    `db:"last_error"`
	CheckedAt     *time.Time `db:"checked_at"`
}

func (f *checksumTestDB) checksumRow(t *testing.T, artifactID uuid.UUID) checksumRow {
	t.Helper()

	var row checksumRow
	require.NoError(t, f.sql.Get(&row, `select status, attempts, next_attempt_at, last_error, checked_at
		from artifact_checksums where artifact_id = $1`, artifactID))
	return row
}

// artifactRows is every artifacts row as JSON, to show that checksum work leaves them unchanged.
func (f *checksumTestDB) artifactRows(t *testing.T) string {
	t.Helper()

	var rows string
	require.NoError(t, f.sql.Get(&rows, `select coalesce(json_agg(artifacts order by id), '[]')::text from artifacts`))
	return rows
}

func (f *checksumTestDB) checksumRowCount(t *testing.T, artifactID uuid.UUID) int {
	t.Helper()

	var count int
	require.NoError(t, f.sql.Get(&count, `select count(*) from artifact_checksums where artifact_id = $1`, artifactID))
	return count
}

func claimedIDs(claims []models.ChecksumClaim) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(claims))
	for _, claim := range claims {
		ids = append(ids, claim.ArtifactID)
	}
	return ids
}

func Test_should_add_pending_checksum_row_when_artifact_created(t *testing.T) {
	// given
	fixture := newChecksumTestDB(t)

	// when
	artifact := fixture.createArtifact(t, "https://files.example.com/a.cif", nil, nil)

	// then
	assert.Equal(t, models.ChecksumStatusPending, artifact.Checksum.Status)
	assert.Equal(t, 1, fixture.checksumRowCount(t, artifact.ID))
	listed := fixture.listArtifact(t, artifact.ID)
	assert.Equal(t, models.ArtifactChecksum{Status: models.ChecksumStatusPending}, listed.Checksum)
}

func Test_should_read_artifact_as_pending_when_it_has_no_checksum_row(t *testing.T) {
	// given
	fixture := newChecksumTestDB(t)
	artifactID := fixture.insertArtifactWithoutChecksum(t, "https://files.example.com/old.cif")

	// when
	listed := fixture.listArtifact(t, artifactID)

	// then
	assert.Equal(t, models.ArtifactChecksum{Status: models.ChecksumStatusPending}, listed.Checksum)
	assert.Equal(t, 0, fixture.checksumRowCount(t, artifactID))
}

func Test_should_add_rows_only_for_artifacts_without_one_when_missing_rows_added(t *testing.T) {
	// given
	ctx := context.Background()
	fixture := newChecksumTestDB(t)
	checked := fixture.createArtifact(t, "https://files.example.com/checked.cif", nil, nil)
	now := time.Now().UTC()
	require.NoError(t, fixture.database.ArtifactChecksums.Record(ctx, []models.ChecksumResult{{
		ArtifactID: checked.ID,
		Status:     models.ChecksumStatusVerified,
		SHA256:     ptr("aa" + sha256Tail),
		SizeBytes:  ptr(int64(10)),
		CheckedAt:  now,
	}}))
	missingID := fixture.insertArtifactWithoutChecksum(t, "https://files.example.com/old.cif")
	artifactsBefore := fixture.artifactRows(t)

	// when
	page, err := fixture.database.ArtifactChecksums.AddMissingAfter(ctx, uuid.Nil, 10)

	// then
	require.NoError(t, err)
	assert.Equal(t, 2, page.Scanned)
	assert.Equal(t, int64(1), page.Added)
	assert.Equal(t, "pending", fixture.checksumRow(t, missingID).Status)
	assert.Equal(t, "verified", fixture.checksumRow(t, checked.ID).Status)
	assert.JSONEq(t, artifactsBefore, fixture.artifactRows(t))
}

func Test_should_add_missing_rows_one_page_of_artifacts_after_another_when_pages_are_smaller_than_the_table(t *testing.T) {
	// given
	ctx := context.Background()
	fixture := newChecksumTestDB(t)
	ids := []uuid.UUID{
		fixture.insertArtifactWithoutChecksum(t, "https://files.example.com/a.cif"),
		fixture.insertArtifactWithoutChecksum(t, "https://files.example.com/b.cif"),
		fixture.insertArtifactWithoutChecksum(t, "https://files.example.com/c.cif"),
	}
	// PostgreSQL orders uuids by their bytes, the same order as their lowercase text.
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })

	// when
	first, firstErr := fixture.database.ArtifactChecksums.AddMissingAfter(ctx, uuid.Nil, 2)
	second, secondErr := fixture.database.ArtifactChecksums.AddMissingAfter(ctx, first.LastArtifactID, 2)
	last, lastErr := fixture.database.ArtifactChecksums.AddMissingAfter(ctx, second.LastArtifactID, 2)

	// then
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.NoError(t, lastErr)
	assert.Equal(t, models.MissingChecksumsPage{Scanned: 2, Added: 2, LastArtifactID: ids[1]}, first)
	assert.Equal(t, models.MissingChecksumsPage{Scanned: 1, Added: 1, LastArtifactID: ids[2]}, second)
	assert.Equal(t, models.MissingChecksumsPage{}, last)
	for _, id := range ids {
		assert.Equal(t, "pending", fixture.checksumRow(t, id).Status)
	}
}

func Test_should_claim_rows_with_the_same_uri_together_when_due_rows_claimed(t *testing.T) {
	// given
	ctx := context.Background()
	fixture := newChecksumTestDB(t)
	declaredSHA := "bb" + sha256Tail
	first := fixture.createArtifact(t, "https://files.rcsb.org/download/1abc-sf.cif", &declaredSHA, ptr(int64(42)))
	second := fixture.createArtifact(t, "https://files.rcsb.org/download/1abc-sf.cif", nil, nil)
	other := fixture.createArtifact(t, "https://files.rcsb.org/download/2xyz-sf.cif", nil, nil)
	now := time.Now().UTC()
	fixture.setNextAttemptAt(t, first.ID, now.Add(-3*time.Hour))
	fixture.setNextAttemptAt(t, second.ID, now.Add(-1*time.Hour))
	fixture.setNextAttemptAt(t, other.ID, now.Add(-2*time.Hour))

	// when
	claims, err := fixture.database.ArtifactChecksums.ClaimDue(ctx, now, 1, time.Hour)

	// then
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{first.ID, second.ID}, claimedIDs(claims))
	for _, claim := range claims {
		assert.Equal(t, 1, claim.Attempts)
		row := fixture.checksumRow(t, claim.ArtifactID)
		assert.WithinDuration(t, now.Add(time.Hour), row.NextAttemptAt, time.Second)
	}
	firstClaim := claims[0]
	if firstClaim.ArtifactID != first.ID {
		firstClaim = claims[1]
	}
	assert.Equal(t, first.URI, firstClaim.URI)
	assert.Equal(t, &declaredSHA, firstClaim.SHA256)
	assert.Equal(t, ptr(int64(42)), firstClaim.SizeBytes)
	assert.Equal(t, first.Name, firstClaim.Name)
	assert.Equal(t, 0, fixture.checksumRow(t, other.ID).Attempts)
}

func Test_should_claim_nothing_when_rows_are_leased_or_finished(t *testing.T) {
	// given
	ctx := context.Background()
	fixture := newChecksumTestDB(t)
	leased := fixture.createArtifact(t, "https://files.example.com/leased.cif", nil, nil)
	finished := fixture.createArtifact(t, "https://files.example.com/finished.cif", nil, nil)
	now := time.Now().UTC()
	fixture.setNextAttemptAt(t, leased.ID, now.Add(30*time.Minute))
	require.NoError(t, fixture.database.ArtifactChecksums.Record(ctx, []models.ChecksumResult{{
		ArtifactID: finished.ID,
		Status:     models.ChecksumStatusFailed,
		Error:      ptr("http status 404"),
		CheckedAt:  now,
	}}))

	// when
	claims, err := fixture.database.ArtifactChecksums.ClaimDue(ctx, now, 10, time.Hour)

	// then
	require.NoError(t, err)
	assert.Empty(t, claims)
}

func Test_should_claim_row_again_when_its_lease_expired(t *testing.T) {
	// given
	ctx := context.Background()
	fixture := newChecksumTestDB(t)
	artifact := fixture.createArtifact(t, "https://files.example.com/crashed.cif", nil, nil)
	now := time.Now().UTC()
	firstClaims, err := fixture.database.ArtifactChecksums.ClaimDue(ctx, now, 10, time.Hour)
	require.NoError(t, err)
	require.Len(t, firstClaims, 1)

	// when
	claims, err := fixture.database.ArtifactChecksums.ClaimDue(ctx, now.Add(61*time.Minute), 10, time.Hour)

	// then
	require.NoError(t, err)
	require.Len(t, claims, 1)
	assert.Equal(t, artifact.ID, claims[0].ArtifactID)
	assert.Equal(t, 2, claims[0].Attempts)
}

func Test_should_store_each_outcome_when_results_recorded(t *testing.T) {
	// given
	ctx := context.Background()
	fixture := newChecksumTestDB(t)
	verified := fixture.createArtifact(t, "https://files.rcsb.org/download/1abc.cif", nil, nil)
	failed := fixture.createArtifact(t, "https://files.rcsb.org/download/gone.cif", nil, nil)
	retried := fixture.createArtifact(t, "https://files.rcsb.org/download/busy.cif", nil, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)
	retryAt := now.Add(2 * time.Hour)
	computedSHA := "cc" + sha256Tail
	storedURI := "https://files.example.com/entry/artifacts/1abc.cif"

	// when
	err := fixture.database.ArtifactChecksums.Record(ctx, []models.ChecksumResult{
		{
			ArtifactID: verified.ID,
			Status:     models.ChecksumStatusVerified,
			SHA256:     &computedSHA,
			SizeBytes:  ptr(int64(7)),
			StoredURI:  &storedURI,
			CheckedAt:  now,
		},
		{ArtifactID: failed.ID, Status: models.ChecksumStatusFailed, Error: ptr("http status 404"), CheckedAt: now},
		{
			ArtifactID:    retried.ID,
			Status:        models.ChecksumStatusPending,
			Error:         ptr("http status 503"),
			CheckedAt:     now,
			NextAttemptAt: &retryAt,
		},
	})

	// then
	require.NoError(t, err)
	assert.Equal(t, models.ArtifactChecksum{
		Status:    models.ChecksumStatusVerified,
		SHA256:    &computedSHA,
		SizeBytes: ptr(int64(7)),
		StoredURI: &storedURI,
		CheckedAt: &now,
	}, normalizeChecksumTime(fixture.listArtifact(t, verified.ID).Checksum))
	failedRow := fixture.checksumRow(t, failed.ID)
	assert.Equal(t, "failed", failedRow.Status)
	assert.Equal(t, ptr("http status 404"), failedRow.LastError)
	retriedRow := fixture.checksumRow(t, retried.ID)
	assert.Equal(t, "pending", retriedRow.Status)
	assert.Equal(t, ptr("http status 503"), retriedRow.LastError)
	assert.Nil(t, retriedRow.CheckedAt)
	assert.True(t, retryAt.Equal(retriedRow.NextAttemptAt))
}

func Test_should_reject_result_when_fields_do_not_fit_its_status(t *testing.T) {
	cases := map[string]models.ChecksumResult{
		"verified without sha256":  {Status: models.ChecksumStatusVerified, SizeBytes: ptr(int64(1))},
		"mismatch without size":    {Status: models.ChecksumStatusMismatch, SHA256: ptr("dd" + sha256Tail)},
		"mismatch with a copy":     {Status: models.ChecksumStatusMismatch, SHA256: ptr("dd" + sha256Tail), SizeBytes: ptr(int64(1)), StoredURI: ptr("https://files.example.com/x")},
		"failed without error":     {Status: models.ChecksumStatusFailed},
		"pending without retry at": {Status: models.ChecksumStatusPending, Error: ptr("timeout")},
		"unknown status":           {Status: models.ChecksumStatus("done")},
	}
	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			// given
			fixture := newChecksumTestDB(t)
			artifact := fixture.createArtifact(t, "https://files.example.com/"+uuid.NewString(), nil, nil)
			result.ArtifactID = artifact.ID
			result.CheckedAt = time.Now().UTC()

			// when
			err := fixture.database.ArtifactChecksums.Record(context.Background(), []models.ChecksumResult{result})

			// then
			require.Error(t, err)
			assert.Equal(t, "pending", fixture.checksumRow(t, artifact.ID).Status)
		})
	}
}

func Test_should_return_entry_and_model_of_each_artifact_when_locations_read(t *testing.T) {
	// given
	ctx := context.Background()
	fixture := newChecksumTestDB(t)
	now := time.Now().UTC()
	entryRevision, err := fixture.database.Entries.Create(ctx, models.EntryRevision{
		ID:        uuid.New(),
		EntryID:   "entry-" + uuid.NewString(),
		State:     models.RevisionStatePending,
		Title:     ptr("checksum entry"),
		CreatedBy: fixture.userID,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	modelRevision, err := fixture.database.Models.Create(ctx, entryRevision.EntryID, models.ModelRevision{
		ID:        uuid.New(),
		ModelID:   "model-" + uuid.NewString(),
		State:     models.RevisionStatePending,
		Title:     ptr("checksum model"),
		CreatedBy: fixture.userID,
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)
	entryFile := fixture.createArtifact(t, "https://files.rcsb.org/download/1abc-sf.cif", nil, nil)
	modelFile := fixture.createArtifact(t, "https://files.rcsb.org/download/1abc.cif", nil, nil)
	unlinked := fixture.createArtifact(t, "https://files.rcsb.org/download/none.cif", nil, nil)
	require.NoError(t, fixture.database.Artifacts.AttachToEntryRevision(ctx, entryRevision.ID, entryFile.ID))
	require.NoError(t, fixture.database.Artifacts.AttachToModelRevision(ctx, modelRevision.ID, modelFile.ID))

	// when
	locations, err := fixture.database.ArtifactChecksums.Locations(ctx, []uuid.UUID{entryFile.ID, modelFile.ID, unlinked.ID})

	// then
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]models.ArtifactLocation{
		entryFile.ID: {EntryID: entryRevision.EntryID},
		modelFile.ID: {EntryID: entryRevision.EntryID, ModelID: &modelRevision.ModelID},
	}, locations)
}

// sha256Tail completes a two-character prefix to a well-formed sha256 hex digest.
var sha256Tail = strings.Repeat("0", 62)

func normalizeChecksumTime(checksum models.ArtifactChecksum) models.ArtifactChecksum {
	if checksum.CheckedAt != nil {
		checkedAt := checksum.CheckedAt.UTC()
		checksum.CheckedAt = &checkedAt
	}
	return checksum
}
