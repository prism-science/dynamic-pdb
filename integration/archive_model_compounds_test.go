package integration

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	archiveFilesOnEnv = "DYNAMIC_PDB_COMPOUNDS_ARCHIVE_FILES=true"
	archiveFillOnEnv  = "DYNAMIC_PDB_COMPOUNDS_ARCHIVE_FILL=true"
	// The archive fill tops the queue up to the run's file cap, which the chart sets on prod.
	fileCapEnv = "DYNAMIC_PDB_COMPOUNDS_MAX_FILES_PER_RUN=500"
	// dataSyncUserID is the system user the data sync stores archive models as
	// (V20260904120000__system_user.sql).
	dataSyncUserID = "00000000-0000-0000-0000-000000000001"
)

func Test_should_read_archive_model_files_from_their_checksum_copies_only_when_archive_files_is_on(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: an archive model whose file on RCSB the checksum job copied into our bucket.
	root := repoRoot(t)
	jobBinaryPath := buildModelCompoundsJob(t, root)
	database, schemaEnv := setupMigratedSchema(t, root)
	s3 := newS3Stub(t)
	defer s3.Close()
	archive := seedArchiveModelFile(t, database, time.Now().UTC().Add(-time.Hour))
	recordArchiveCopy(t, database, s3, archive, "copied")
	modelRevisionsBefore := modelRevisionRows(t, database, archive.ModelID)
	entryRevisionsBefore := entryRevisionRows(t, database, archive.EntryID)

	// when: a run with both archive switches off.
	switchedOff := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), schemaEnv)

	// then: nothing changes for the archive model.
	assert.False(t, hasModeledCompoundSet(t, database, archive.ArtifactID), switchedOff)
	assert.Contains(t, switchedOff, "archive_sets_added=0")

	// when: a run with archive files on.
	switchedOn := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), slices.Concat(schemaEnv, []string{archiveFilesOnEnv}))

	// then: the copy is read with the same reader as an upload, and search finds the entry by it.
	assert.Equal(t, "done", modeledCompoundStatus(t, database, archive.ArtifactID), switchedOn)
	assert.Equal(t, []string{"GOL A 301 atom_site"}, modeledCompoundCopies(t, database, archive.ArtifactID))
	assert.Contains(t, switchedOn, "read_from=checksum_copy")
	assert.Contains(t, entrySearchText(t, database, archive.EntryID), "GOL")
	readsAfterFirst := modeledCompoundReadRuns(t, database)

	// when: another run.
	again := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), slices.Concat(schemaEnv, []string{archiveFilesOnEnv}))

	// then: nothing is read again, and no run made or changed a model or entry revision.
	assert.Equal(t, readsAfterFirst, modeledCompoundReadRuns(t, database), again)
	assert.Contains(t, again, "files=0")
	assert.Equal(t, modelRevisionsBefore, modelRevisionRows(t, database, archive.ModelID))
	assert.Equal(t, entryRevisionsBefore, entryRevisionRows(t, database, archive.EntryID))
}

func Test_should_queue_older_copies_by_the_fill_and_new_ones_by_the_recent_window_when_archive_files_is_on(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	// given: archive model files created three days ago, one copied and one RCSB replaced before
	// the checksum job could copy it.
	root := repoRoot(t)
	jobBinaryPath := buildModelCompoundsJob(t, root)
	database, schemaEnv := setupMigratedSchema(t, root)
	s3 := newS3Stub(t)
	defer s3.Close()
	threeDaysAgo := time.Now().UTC().Add(-72 * time.Hour)
	older := seedArchiveModelFile(t, database, threeDaysAgo)
	recordArchiveCopy(t, database, s3, older, "copied")
	withoutCopy := seedArchiveModelFile(t, database, threeDaysAgo)
	recordArchiveCopy(t, database, s3, withoutCopy, "mismatch")
	revisionsBefore := modelRevisionRows(t, database, older.ModelID)
	filesOn := slices.Concat(schemaEnv, []string{archiveFilesOnEnv})

	// when: archive files on, the fill off.
	recentOnly := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), filesOn)

	// then: an older copy is outside the recent window.
	assert.False(t, hasModeledCompoundSet(t, database, older.ArtifactID), recentOnly)

	// when: the fill on too.
	filled := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), slices.Concat(filesOn, []string{archiveFillOnEnv, fileCapEnv}))

	// then: the copied file is read; the one without a copy stays as it is (the API: not_stored).
	assert.Contains(t, filled, "archive_sets_added=1")
	assert.Equal(t, "done", modeledCompoundStatus(t, database, older.ArtifactID), filled)
	assert.Equal(t, []string{"GOL A 301 atom_site"}, modeledCompoundCopies(t, database, older.ArtifactID))
	assert.False(t, hasModeledCompoundSet(t, database, withoutCopy.ArtifactID), filled)

	// when: a new archive file arrives (a weekly sync, a new entry, a T3-C coordinate file), and a
	// run comes before the hourly checksum job has copied it.
	newFile := seedArchiveModelFile(t, database, time.Now().UTC())
	beforeCopy := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), filesOn)

	// then
	assert.False(t, hasModeledCompoundSet(t, database, newFile.ArtifactID), beforeCopy)

	// when: the checksum job has copied it.
	recordArchiveCopy(t, database, s3, newFile, "copied")
	afterCopy := runModelCompoundsJob(t, root, jobBinaryPath, s3.URL(), filesOn)

	// then
	assert.Equal(t, "done", modeledCompoundStatus(t, database, newFile.ArtifactID), afterCopy)
	assert.Equal(t, revisionsBefore, modelRevisionRows(t, database, older.ModelID), "no model revision was made or changed")
}

// archiveModelFile is an archive model as the data sync stores it, as the system user: an active
// Deposited model revision whose primary model file is the RCSB download URL, with a pending
// checksum row.
type archiveModelFile struct {
	EntryID    string
	ModelID    string
	PDBID      string
	ArtifactID uuid.UUID
}

func seedArchiveModelFile(t *testing.T, database integrationDB, createdAt time.Time) archiveModelFile {
	t.Helper()
	createdBy := dataSyncUserID
	fixture := archiveModelFile{EntryID: "entry-" + uuid.NewString(), PDBID: integrationPDBID(), ArtifactID: uuid.New()}
	fixture.ModelID = fixture.EntryID + "_m_001"
	// The data sync's model list comes from RCSB's nonpolymer_bound_components.
	modelMetadata := fmt.Sprintf(`{"external_refs":{"pdb":%q},"model_type":"Deposited","ligands":["NA"]}`, fixture.PDBID)
	statements := []struct {
		query string
		args  []any
	}{
		{`insert into entries(id, state, created_by, created_at) values ($1, 'active', $2, $3)`,
			[]any{fixture.EntryID, createdBy, createdAt}},
		{`insert into entry_revisions(
		    id, entry_id, revision_number, state, entry_state, published_at, title, metadata, created_by, created_at, updated_at
		  ) values ($1, $2, 1, 'active', 'active', $3, 'archive entry', $4::jsonb, $5, $3, $3)`,
			[]any{uuid.New(), fixture.EntryID, createdAt, fmt.Sprintf(`{"external_refs":{"pdb":%q}}`, fixture.PDBID), createdBy}},
		{`insert into artifacts(id, name, level, type, uri, sha256, format, size_bytes, metadata, created_by, created_at)
		  values ($1, $2, 'L2', 'model', $3, $4, 'cif', $5, '{}'::jsonb, $6, $7)`,
			[]any{fixture.ArtifactID, fixture.PDBID + ".cif", "https://files.rcsb.org/download/" + fixture.PDBID + ".cif",
				sha256Hex(mmcifWithGlycerol), len(mmcifWithGlycerol), createdBy, createdAt}},
		{`insert into artifact_checksums(artifact_id) values ($1)`, []any{fixture.ArtifactID}},
		{`insert into models(id, entry_id, state, created_by, created_at) values ($1, $2, 'active', $3, $4)`,
			[]any{fixture.ModelID, fixture.EntryID, createdBy, createdAt}},
		{`insert into model_revisions(
		    id, model_id, primary_artifact_id, revision_number, state, model_state, published_at,
		    title, metadata, created_by, created_at, updated_at
		  ) values ($1, $2, $3, 1, 'active', 'active', $4, 'Deposited model', $5::jsonb, $6, $4, $4)`,
			[]any{uuid.New(), fixture.ModelID, fixture.ArtifactID, createdAt, modelMetadata, createdBy}},
		{`insert into model_revision_artifacts(model_revision_id, artifact_id)
		  select id, primary_artifact_id from model_revisions where model_id = $1`,
			[]any{fixture.ModelID}},
	}
	runInTransactionForTest(t, database, func(transaction *sql.Tx) {
		for _, statement := range statements {
			_, err := transaction.ExecContext(t.Context(), statement.query, statement.args...)
			require.NoError(t, err)
		}
	})
	return fixture
}

// recordArchiveCopy records the checksum job's result for an archive model file, as the job
// does: copied, with the copy stored in the S3 stub in the artifact's own folder, or mismatch,
// RCSB's bytes no longer the declared ones and no copy kept. The real job refuses to reach the
// RCSB stub on 127.0.0.1, so the test writes its result.
func recordArchiveCopy(t *testing.T, database integrationDB, s3 *s3Stub, fixture archiveModelFile, copyStatus string) {
	t.Helper()
	var err error
	switch copyStatus {
	case "copied":
		objectPath := fmt.Sprintf("/dynamic-pdb/%s/models/%s/artifacts/%s/%s.cif",
			fixture.EntryID, fixture.ModelID, fixture.ArtifactID, fixture.PDBID)
		s3.recordBody(objectPath, []byte(mmcifWithGlycerol))
		_, err = database.Conn.ExecContext(t.Context(),
			`update artifact_checksums
			 set status = 'verified', sha256 = $2, size_bytes = $3, stored_uri = $4, copy_status = 'copied', checked_at = now()
			 where artifact_id = $1`,
			fixture.ArtifactID, sha256Hex(mmcifWithGlycerol), len(mmcifWithGlycerol), s3.URL()+objectPath)
	case "mismatch":
		_, err = database.Conn.ExecContext(t.Context(),
			`update artifact_checksums
			 set status = 'mismatch', sha256 = $2, size_bytes = 9, copy_status = 'mismatch', checked_at = now()
			 where artifact_id = $1`,
			fixture.ArtifactID, sha256Hex("replaced\n"))
	default:
		require.FailNow(t, "unknown copy status", copyStatus)
	}
	require.NoError(t, err)
}

func hasModeledCompoundSet(t *testing.T, database integrationDB, artifactID uuid.UUID) bool {
	t.Helper()
	var status string
	err := database.Conn.QueryRowContext(t.Context(),
		`select status from modeled_compound_sets where artifact_id = $1`, artifactID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

func modeledCompoundCopies(t *testing.T, database integrationDB, artifactID uuid.UUID) []string {
	t.Helper()
	return textRows(t, database,
		`select ccd_id || ' ' || auth_asym_id || ' ' || auth_seq_id || ' ' || source
		 from modeled_compound_records where artifact_id = $1 order by ordinal`, artifactID)
}

// modeledCompoundReadRuns counts the parser runs the job recorded, one per file read.
func modeledCompoundReadRuns(t *testing.T, database integrationDB) int {
	t.Helper()
	var count int
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select count(*) from runs where name = 'modeled compound read'`).Scan(&count))
	return count
}

// modelRevisionRows is every revision of a model as text: id, state, primary file and metadata.
func modelRevisionRows(t *testing.T, database integrationDB, modelID string) []string {
	t.Helper()
	return textRows(t, database,
		`select id || ' ' || state || ' ' || primary_artifact_id || ' ' || metadata::text
		 from model_revisions where model_id = $1 order by created_at, id`, modelID)
}

// entryRevisionRows is every revision of an entry as text: id, state and metadata.
func entryRevisionRows(t *testing.T, database integrationDB, entryID string) []string {
	t.Helper()
	return textRows(t, database,
		`select id || ' ' || state || ' ' || metadata::text from entry_revisions where entry_id = $1 order by created_at, id`,
		entryID)
}

// entrySearchText is the search text of every search row of an entry.
func entrySearchText(t *testing.T, database integrationDB, entryID string) string {
	t.Helper()
	var text string
	require.NoError(t, database.Conn.QueryRowContext(t.Context(),
		`select coalesce(string_agg(search_text, ' '), '') from entry_search_index where entry_id = $1`, entryID).Scan(&text))
	return text
}

// textRows runs a query that selects one text column and returns its rows in order.
func textRows(t *testing.T, database integrationDB, query string, args ...any) []string {
	t.Helper()
	rows, err := database.Conn.QueryContext(t.Context(), query, args...)
	require.NoError(t, err)
	defer func() { assert.NoError(t, rows.Close()) }()
	values := []string{}
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	return values
}
