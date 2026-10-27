package integration

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_record_baseline_version_without_new_revision_when_job_first_runs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an archive entry whose job has not recorded an archive version yet
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
	_, err := database.Conn.ExecContext(t.Context(),
		`update data_sync_jobs set archive_revision_ordinal = null, archive_version = null where entry_id = $1`,
		fixture.EntryID)
	require.NoError(t, err)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newRCSBStub(t, pdbID)
	defer rcsb.Close()
	setRCSBURLs(t, rcsb.URL())

	// when
	startBackend(t, root, backendBinaryPath, s3.URL())
	var ordinal sql.NullInt64
	var version sql.NullString
	require.Eventually(t, func() bool {
		err := database.Conn.QueryRowContext(t.Context(),
			`select archive_revision_ordinal, archive_version from data_sync_jobs where entry_id = $1 and model_id is null`,
			fixture.EntryID,
		).Scan(&ordinal, &version)
		return err == nil && ordinal.Valid
	}, 20*time.Second, 100*time.Millisecond)

	// then
	assert.Equal(t, int64(2), ordinal.Int64)
	assert.Equal(t, "1.1", version.String)
	var activeRevisionID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select id from entry_revisions where entry_id = $1 and state = 'active'`, fixture.EntryID,
	).Scan(&activeRevisionID))
	assert.Equal(t, fixture.RevisionID, activeRevisionID)
	assert.NotContains(t, rcsb.paths(), "/rest/v1/core/polymer_entity/"+pdbID+"/1")
}

func Test_should_mark_entry_obsolete_and_stop_its_job_when_rcsb_obsoleted_it(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an archive entry RCSB obsoleted, and the registry entry of its successor
	root := repoRoot(t)
	backendBinaryPath := buildBackend(t, root)
	database := setupDB(t)
	auth := seedUserAndIssueToken(t, database)
	pdbID := integrationPDBID()
	successorPDBID := integrationPDBID()
	fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
	successor := seedScheduledDataSyncEntry(t, database, auth.UserID, successorPDBID)
	_, err := database.Conn.ExecContext(t.Context(),
		`update data_sync_jobs set scheduled_at = now() + interval '1 day' where entry_id = $1`, successor.EntryID)
	require.NoError(t, err)
	s3 := newS3Stub(t)
	defer s3.Close()
	rcsb := newObsoleteRCSBStub(t, pdbID, successorPDBID)
	defer rcsb.Close()
	setRCSBURLs(t, rcsb.URL())

	// when
	backend := startBackend(t, root, backendBinaryPath, s3.URL())
	var stopReason sql.NullString
	require.Eventually(t, func() bool {
		err := database.Conn.QueryRowContext(t.Context(),
			`select stop_reason from data_sync_jobs where entry_id = $1 and model_id is null`, fixture.EntryID,
		).Scan(&stopReason)
		return err == nil && stopReason.Valid
	}, 20*time.Second, 100*time.Millisecond)
	entry := getJSON[obsoleteEntryDocument](t, backend.URL+"/v1/entries/"+fixture.EntryID, auth.AccessToken)

	// then
	assert.Equal(t, "RCSB obsoleted "+pdbID, stopReason.String)
	status := entry.Data.Attributes.ArchiveStatus
	require.NotNil(t, status)
	assert.Equal(t, "obsolete", status.State)
	require.NotNil(t, status.ObsoletedAt)
	assert.Equal(t, time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC), status.ObsoletedAt.UTC())
	require.Len(t, status.SupersededBy, 1)
	assert.Equal(t, successorPDBID, status.SupersededBy[0].PDBID)
	require.NotNil(t, status.SupersededBy[0].EntryID)
	assert.Equal(t, successor.EntryID, *status.SupersededBy[0].EntryID)
	require.NotNil(t, entry.Data.Attributes.Title)
	assert.Equal(t, "outdated title", *entry.Data.Attributes.Title)
	var changeSummary string
	var parentRevisionID uuid.UUID
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select change_summary, parent_revision_id from entry_revisions where entry_id = $1 and state = 'active'`,
		fixture.EntryID,
	).Scan(&changeSummary, &parentRevisionID))
	assert.Equal(t, "RCSB obsoleted "+pdbID+" on 2024-01-17; superseded by "+successorPDBID, changeSummary)
	assert.Equal(t, fixture.RevisionID, parentRevisionID)
}

type obsoleteEntryDocument struct {
	Data struct {
		Attributes struct {
			Title         *string `json:"title"`
			ArchiveStatus *struct {
				State        string     `json:"state"`
				ObsoletedAt  *time.Time `json:"obsoleted_at"`
				SupersededBy []struct {
					PDBID   string  `json:"pdb_id"`
					EntryID *string `json:"entry_id"`
				} `json:"superseded_by"`
			} `json:"archive_status"`
		} `json:"attributes"`
	} `json:"data"`
}

func setRCSBURLs(t *testing.T, url string) {
	t.Helper()
	t.Setenv("DYNAMIC_PDB_RCSB_DATA_URL", url)
	t.Setenv("DYNAMIC_PDB_RCSB_FILES_URL", url)
	t.Setenv("DYNAMIC_PDB_RCSB_WWW_URL", url)
	t.Setenv("DYNAMIC_PDB_RCSB_CDN_URL", url)
}
