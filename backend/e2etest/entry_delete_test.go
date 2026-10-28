package e2etest

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
)

type EntryDeleteSuite struct {
	baseSuite
}

func TestEntryDelete(t *testing.T) {
	suite.Run(t, new(EntryDeleteSuite))
}

func (s *EntryDeleteSuite) Test_should_hide_entry_everywhere_when_reviewer_deletes_it() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-hidden-owner", 8301)
	pdbID := unusedPDBIDForTest(s.T())
	title := "entry to delete " + uuid.NewString()
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": title, "external_refs": map[string]string{"pdb": pdbID}},
	})
	model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title":    "model of deleted entry",
			"metadata": map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{
				artifactRequest(uuid.New(), "FASTA", "L2", "fasta", "s3://model/delete.fasta", map[string]any{
					"records": []map[string]any{{"header": "deleted", "sequence": "MACDEFGHIK"}},
				}),
			},
		},
	})
	s.Require().Len(getEntryForTest(s.T(), entry.EntryId).ProteinSequences, 1)

	// when
	//nolint:bodyclose // requireNoContent closes the response body.
	response := deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, adminToken)

	// then
	requireNoContent(s.T(), response)
	//nolint:bodyclose // assertErrorResponse closes the response body.
	assertErrorResponse(s.T(), getWithToken(s.T(), "/v1/entries/"+entry.EntryId, ""), "NOT_FOUND", "entry not found")
	//nolint:bodyclose // assertErrorResponse closes the response body.
	assertErrorResponse(s.T(),
		getWithToken(s.T(), fmt.Sprintf("/v1/entries/%s/models/%s", entry.EntryId, model.ModelId), ""),
		"NOT_FOUND", "model not found")
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
	s.Empty(listEntryIDsForTest(s.T(), "pdb_id="+url.QueryEscape(pdbID)))
	s.Empty(listEntryIDsForTest(s.T(), "query="+url.QueryEscape(title)))
	sequences, err := testDatabase.ProteinSequences.List(s.T().Context(), db.ProteinSequenceFilters{
		EntryRevisionID: &entry.RevisionId,
	})
	s.Require().NoError(err)
	s.Empty(sequences)
}

func (s *EntryDeleteSuite) Test_should_record_deletion_as_active_revision_without_pdb_id_when_entry_is_deleted() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-record-owner", 8302)
	pdbID := unusedPDBIDForTest(s.T())
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "recorded deletion", "external_refs": map[string]string{"pdb": pdbID}},
	})

	// when
	//nolint:bodyclose // requireNoContent closes the response body.
	requireNoContent(s.T(), deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, adminToken))

	// then
	active := models.RevisionStateActive
	deletion, err := testDatabase.Entries.Get(s.T().Context(), db.EntryRevisionFilters{
		EntryID: &entry.EntryId,
		State:   &active,
	})
	s.Require().NoError(err)
	s.Equal(models.EntryStateDeleted, deletion.EntryState)
	s.Equal(tokenUserIDForTest(s.T(), adminToken), deletion.CreatedBy)
	s.Equal("Entry deleted", stringValue(deletion.ChangeSummary))
	s.Equal(&entry.RevisionId, deletion.ParentRevisionID)
	s.Equal("recorded deletion", stringValue(deletion.Title))
	s.NotContains(deletion.Metadata.ExternalRefs, models.EntrySourcePDB)
	archived := models.RevisionStateArchived
	previous, err := testDatabase.Entries.Get(s.T().Context(), db.EntryRevisionFilters{
		ID:    &entry.RevisionId,
		State: &archived,
	})
	s.Require().NoError(err)
	s.Equal(pdbID, previous.Metadata.ExternalRefs[models.EntrySourcePDB])
}

func (s *EntryDeleteSuite) Test_should_accept_pdb_id_of_deleted_entry_when_new_entry_uses_it() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-reuse-owner", 8303)
	pdbID := unusedPDBIDForTest(s.T())
	deleted := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "first entry with the PDB ID", "external_refs": map[string]string{"pdb": pdbID}},
	})
	//nolint:bodyclose // requireNoContent closes the response body.
	requireNoContent(s.T(), deleteEntryForTest(s.T(), entryDeletionServer.URL, deleted.EntryId, adminToken))

	// when
	replacement := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "second entry with the PDB ID", "external_refs": map[string]string{"pdb": pdbID}},
	})

	// then
	s.Equal([]string{replacement.EntryId}, listEntryIDsForTest(s.T(), "pdb_id="+url.QueryEscape(pdbID)))
}

func (s *EntryDeleteSuite) Test_should_reject_open_entry_and_model_revisions_when_entry_is_deleted() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-open-owner", 8304)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry with open revisions"},
	})
	model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model with open revision", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	entryRevision := createEntryRevisionForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"title": "entry change waiting for review"},
	})
	modelRevision := createModelRevisionForTest(s.T(), ownerToken, entry.EntryId, model.ModelId, map[string]any{
		"model": map[string]any{"title": "model change waiting for review"},
	})

	// when
	//nolint:bodyclose // requireNoContent closes the response body.
	requireNoContent(s.T(), deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, adminToken))

	// then
	storedEntryRevision, err := testDatabase.Entries.Get(s.T().Context(), db.EntryRevisionFilters{
		ID: &entryRevision.RevisionId,
	})
	s.Require().NoError(err)
	s.Equal(models.RevisionStateRejected, storedEntryRevision.State)
	storedModelRevision, err := testDatabase.Models.Get(s.T().Context(), db.ModelRevisionFilters{
		ID: &modelRevision.RevisionId,
	})
	s.Require().NoError(err)
	s.Equal(models.RevisionStateRejected, storedModelRevision.State)
}

func (s *EntryDeleteSuite) Test_should_stop_entry_and_model_sync_jobs_when_entry_is_deleted() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-sync-owner", 8309)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"title":         "entry with sync jobs",
			"external_refs": map[string]string{"pdb": unusedPDBIDForTest(s.T())},
		},
	})
	model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model with a sync job", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	rawDatabase, err := sqlx.Connect("postgres",
		"host=localhost port=35432 dbname=dynamic_pdb_local user=postgres password=password sslmode=disable")
	s.Require().NoError(err)
	s.T().Cleanup(func() { s.NoError(rawDatabase.Close()) })
	// the job the activation of an archive Deposited model creates
	_, err = rawDatabase.Exec(`insert into data_sync_jobs(entry_id, model_id, scheduled_at)
		values ($1, $2, now() + interval '7 days')`, entry.EntryId, model.ModelId)
	s.Require().NoError(err)

	// when
	//nolint:bodyclose // requireNoContent closes the response body.
	requireNoContent(s.T(), deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, adminToken))

	// then
	var jobs []struct {
		ModelID    *string `db:"model_id"`
		Stopped    bool    `db:"stopped"`
		StopReason *string `db:"stop_reason"`
	}
	s.Require().NoError(rawDatabase.Select(&jobs, `select model_id, stopped_at is not null as stopped, stop_reason
		from data_sync_jobs where entry_id = $1 order by model_id nulls first`, entry.EntryId))
	s.Require().Len(jobs, 2, "the entry's own job and its model's job")
	s.Nil(jobs[0].ModelID)
	s.Equal(model.ModelId, stringValue(jobs[1].ModelID))
	for _, job := range jobs {
		s.True(job.Stopped)
		s.Equal("entry deleted", stringValue(job.StopReason))
	}
}

func (s *EntryDeleteSuite) Test_should_refuse_deletion_when_it_is_turned_off() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-off-owner", 8305)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry on a server without deletion"},
	})

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := deleteEntryForTest(s.T(), testServer.URL, entry.EntryId, adminToken)

	// then
	s.Equal(http.StatusForbidden, response.StatusCode)
	assertErrorResponse(s.T(), response, "FORBIDDEN", "entry deletion is turned off on this server")
	s.Equal("entry on a server without deletion", getEntryForTest(s.T(), entry.EntryId).Title)
}

func (s *EntryDeleteSuite) Test_should_refuse_deletion_when_user_cannot_approve_revisions() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-forbidden-owner", 8306)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry its owner cannot delete"},
	})

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, ownerToken)

	// then
	s.Equal(http.StatusForbidden, response.StatusCode)
	assertErrorResponse(s.T(), response, "FORBIDDEN", "required permission is missing")
	s.Equal("entry its owner cannot delete", getEntryForTest(s.T(), entry.EntryId).Title)
}

func (s *EntryDeleteSuite) Test_should_require_sign_in_when_deleting_entry() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-anonymous-owner", 8307)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry an anonymous caller cannot delete"},
	})

	// when
	//nolint:bodyclose // the status is all this test reads; closed below.
	response := deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, "")

	// then
	s.Require().NoError(response.Body.Close())
	s.Equal(http.StatusUnauthorized, response.StatusCode)
}

func (s *EntryDeleteSuite) Test_should_return_not_found_when_entry_is_unknown_or_already_deleted() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "delete-twice-owner", 8308)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry deleted twice"},
	})
	//nolint:bodyclose // requireNoContent closes the response body.
	requireNoContent(s.T(), deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, adminToken))

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	secondDelete := deleteEntryForTest(s.T(), entryDeletionServer.URL, entry.EntryId, adminToken)
	//nolint:bodyclose // assertErrorResponse closes the response body.
	unknownDelete := deleteEntryForTest(s.T(), entryDeletionServer.URL, "dpdb_unknown_entry", adminToken)

	// then
	s.Equal(http.StatusNotFound, secondDelete.StatusCode)
	assertErrorResponse(s.T(), secondDelete, "NOT_FOUND", "entry not found")
	s.Equal(http.StatusNotFound, unknownDelete.StatusCode)
	assertErrorResponse(s.T(), unknownDelete, "NOT_FOUND", "entry not found")
}

func deleteEntryForTest(t *testing.T, baseURL, entryID, token string) *http.Response {
	t.Helper()
	return jsonRequestToServerWithToken(t, baseURL, http.MethodDelete, "/v1/entries/"+entryID, nil, token)
}

func requireNoContent(t *testing.T, response *http.Response) {
	t.Helper()
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusNoContent, response.StatusCode)
}

// listEntryIDsForTest returns the IDs of the public entry list filtered by query, a URL query string.
func listEntryIDsForTest(t *testing.T, query string) []string {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, "/v1/entries?"+query, "")
	document := decodeJSONResponse[httpapi.EntryCollectionDocument](t, response, http.StatusOK)
	ids := make([]string, 0, len(document.Data))
	for _, resource := range document.Data {
		ids = append(ids, resource.Id)
	}
	return ids
}

// unusedPDBIDForTest returns a PDB ID that no active revision holds. The local database is shared
// with earlier runs, and the unique index on active PDB IDs would otherwise make creates collide.
func unusedPDBIDForTest(t *testing.T) string {
	t.Helper()
	const symbols = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	active := models.RevisionStateActive
	for range 50 {
		random := uuid.New()
		pdbID := []byte{'9'}
		for _, value := range random[:3] {
			pdbID = append(pdbID, symbols[int(value)%len(symbols)])
		}
		revisions, err := testDatabase.Entries.List(t.Context(), db.EntryRevisionFilters{
			State:  &active,
			PDBIDs: []string{string(pdbID)},
		})
		require.NoError(t, err)
		if len(revisions) == 0 {
			return string(pdbID)
		}
	}
	require.FailNow(t, "no unused test PDB ID after 50 tries")
	return ""
}
