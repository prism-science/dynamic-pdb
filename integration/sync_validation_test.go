package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enforceValidation runs the backend with schema validation in enforce mode.
var enforceValidation = []string{"DYNAMIC_PDB_VALIDATION_MODE=enforce"}

func Test_should_keep_entry_and_report_version_when_rcsb_values_break_schema_in_enforce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an archive entry whose RCSB version 1.1 has resolution 0
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	rcsb.setEntryInfoOverrides(map[string]any{"resolution_combined": []float64{0}})
	setRCSBURLs(t, rcsb.URL())

	// when
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), enforceValidation)
	review := waitForSyncReview(t, database, "entry", fixture.EntryID)

	// then
	assert.False(t, review.ResolvedAt.Valid)
	details := review.details(t)
	assert.Equal(t, pdbID, details.PDBID)
	assert.Equal(t, "1.1", details.ArchiveVersion)
	assert.Contains(t, details.pointers(), "/resolution")
	job := syncJobOf(t, database, fixture.EntryID, nil)
	assert.Equal(t, "1.0", job.ArchiveVersion.String, "the rejected version is not recorded")
	assert.True(t, job.ScheduledAt.After(time.Now().Add(6*24*time.Hour)), "next try at the normal cadence, not within the hour")
	assert.Equal(t, fixture.RevisionID, activeEntryRevisionID(t, database, fixture.EntryID), "the entry keeps its revision")
	assert.Contains(t, backend.Logs(), "data sync skipped archive version")
}

func Test_should_keep_model_and_report_version_when_rcsb_values_break_schema_in_enforce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: a Deposited archive model whose RCSB version 1.1 has more unmodeled residues than residues
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncModel(t, database, auth.UserID, pdbID, "Deposited")
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	rcsb.setEntryInfoOverrides(map[string]any{"deposited_unmodeled_polymer_monomer_count": 300})
	setRCSBURLs(t, rcsb.URL())

	// when
	startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), enforceValidation)
	review := waitForSyncReview(t, database, "model", fixture.ModelID)

	// then
	assert.False(t, review.ResolvedAt.Valid)
	details := review.details(t)
	assert.Equal(t, "1.1", details.ArchiveVersion)
	assert.Contains(t, details.pointers(), "/metadata/unmodeled_fraction")
	job := syncJobOf(t, database, fixture.EntryID, &fixture.ModelID)
	assert.Equal(t, "1.0", job.ArchiveVersion.String, "the rejected version is not recorded")
	assert.True(t, job.ScheduledAt.After(time.Now().Add(6*24*time.Hour)), "next try at the normal cadence, not within the hour")
	var activeRevisionID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select id from model_revisions where model_id = $1 and state = 'active'`, fixture.ModelID,
	).Scan(&activeRevisionID))
	assert.Equal(t, fixture.RevisionID, activeRevisionID, "the model keeps its revision")
}

func Test_should_resolve_review_when_rejected_entry_version_applies_later_in_enforce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an entry with an open review row from an earlier rejected try, and valid RCSB values now
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
	_, err := database.Conn.ExecContext(t.Context(),
		`insert into schema_migration_review (object_type, object_id, issue, details)
		 values ('entry', $1, 'sync_validation_failed', '{"archive_version": "1.1"}')`,
		fixture.EntryID)
	require.NoError(t, err)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	setRCSBURLs(t, rcsb.URL())

	// when
	startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), enforceValidation)
	require.Eventually(t, func() bool {
		return syncJobOf(t, database, fixture.EntryID, nil).ArchiveVersion.String == "1.1"
	}, 20*time.Second, 100*time.Millisecond)

	// then
	review := waitForResolvedSyncReview(t, database, "entry", fixture.EntryID)
	assert.Equal(t, "data-sync", review.ResolvedBy.String)
	assert.NotEqual(t, fixture.RevisionID, activeEntryRevisionID(t, database, fixture.EntryID), "version 1.1 is applied")
}

func Test_should_retry_within_the_hour_and_open_no_review_when_rcsb_fails_in_enforce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an archive entry whose RCSB entry request fails
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	rcsb.setEntryStatus(http.StatusForbidden)
	setRCSBURLs(t, rcsb.URL())

	// when
	backend := startBackendWithEnv(t, root, backendBinaryPath, s3.URL(), enforceValidation)
	require.Eventually(t, func() bool {
		return strings.Contains(backend.Logs(), "data sync job failed; retry scheduled")
	}, 20*time.Second, 100*time.Millisecond)

	// then
	job := syncJobOf(t, database, fixture.EntryID, nil)
	assert.WithinDuration(t, time.Now().Add(time.Hour), job.ScheduledAt, 10*time.Minute, "a failed run is retried within the hour")
	var reviews int
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select count(*) from schema_migration_review where object_id = $1`, fixture.EntryID,
	).Scan(&reviews))
	assert.Zero(t, reviews, "an RCSB failure is not a schema violation")
}

type syncReviewRow struct {
	Details    []byte
	ResolvedAt sql.NullTime
	ResolvedBy sql.NullString
}

type syncReviewDetails struct {
	PDBID          string `json:"pdb_id"`
	ArchiveVersion string `json:"archive_version"`
	Errors         []struct {
		Pointer string `json:"pointer"`
	} `json:"errors"`
}

func (r syncReviewRow) details(t *testing.T) syncReviewDetails {
	t.Helper()
	var details syncReviewDetails
	require.NoError(t, json.Unmarshal(r.Details, &details))
	return details
}

func (d syncReviewDetails) pointers() []string {
	pointers := make([]string, 0, len(d.Errors))
	for _, fieldError := range d.Errors {
		pointers = append(pointers, fieldError.Pointer)
	}
	return pointers
}

// waitForSyncReview waits for the object's sync_validation_failed row and returns it.
func waitForSyncReview(t *testing.T, database integrationDB, objectType string, objectID string) syncReviewRow {
	t.Helper()
	return waitForSyncReviewWhere(t, database, objectType, objectID, func(syncReviewRow) bool { return true })
}

// waitForResolvedSyncReview waits until the object's sync_validation_failed row is resolved and
// returns it. The job records an applied archive version and resolves the row in two
// statements, so a test that has seen the version can still read the row open.
func waitForResolvedSyncReview(t *testing.T, database integrationDB, objectType string, objectID string) syncReviewRow {
	t.Helper()
	return waitForSyncReviewWhere(t, database, objectType, objectID, func(row syncReviewRow) bool {
		return row.ResolvedAt.Valid
	})
}

// waitForSyncReviewWhere waits until the object's sync_validation_failed row exists and done
// accepts it, and returns it.
func waitForSyncReviewWhere(
	t *testing.T,
	database integrationDB,
	objectType string,
	objectID string,
	done func(syncReviewRow) bool,
) syncReviewRow {
	t.Helper()
	var row syncReviewRow
	require.Eventually(t, func() bool {
		err := database.Conn.QueryRowContext(t.Context(),
			`select details, resolved_at, resolved_by from schema_migration_review
			 where object_type = $1 and object_id = $2 and issue = 'sync_validation_failed'`,
			objectType, objectID,
		).Scan(&row.Details, &row.ResolvedAt, &row.ResolvedBy)
		return err == nil && done(row)
	}, 20*time.Second, 100*time.Millisecond)
	return row
}

type syncJobRow struct {
	ArchiveVersion sql.NullString
	ScheduledAt    time.Time
}

// syncJobOf returns the entry's job, or the model's job when modelID is not nil.
func syncJobOf(t *testing.T, database integrationDB, entryID string, modelID *string) syncJobRow {
	t.Helper()
	var job syncJobRow
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select archive_version, scheduled_at from data_sync_jobs
		 where entry_id = $1 and model_id is not distinct from $2`,
		entryID, modelID,
	).Scan(&job.ArchiveVersion, &job.ScheduledAt))
	return job
}

func activeEntryRevisionID(t *testing.T, database integrationDB, entryID string) uuid.UUID {
	t.Helper()
	var revisionID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select id from entry_revisions where entry_id = $1 and state = 'active'`, entryID,
	).Scan(&revisionID))
	return revisionID
}
