package e2etest

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
)

// DownloadsSuite covers archive requests on a server that has selections and downloads on. The
// archive worker runs in the integration suite; here the jobs stay queued.
type DownloadsSuite struct {
	baseSuite
}

func TestDownloads(t *testing.T) {
	suite.Run(t, new(DownloadsSuite))
}

// savedSelectionForDownloadsTest saves a selection of one published Deposited model.
func (s *DownloadsSuite) savedSelectionForDownloadsTest(login string, githubID int64) string {
	token := issueEntryTokenForGitHubIDForTest(s.T(), login, githubID)
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{"title": "archive entry"}})
	model := createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "archive model", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	return saveSelectionForTest(s.T(), map[string]any{"model_ids": []string{model.ModelId}}).Data.Id
}

func (s *DownloadsSuite) requestArchive(body any, status int) httpapi.DownloadDocument {
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodPost, "/v1/downloads", body, "")
	document := decodeJSONResponse[httpapi.DownloadDocument](s.T(), response, status)
	if status == http.StatusAccepted {
		s.Equal(testPublicBaseURL+"/v1/downloads/"+document.Data.Id, response.Header.Get("Location"))
	}
	return document
}

func (s *DownloadsSuite) Test_should_return_the_queued_archive_when_it_is_asked_for_again() {
	// given a saved selection
	selectionID := s.savedSelectionForDownloadsTest("downloads-owner", 8921)

	// when an archive of all its files is asked for twice
	first := s.requestArchive(map[string]any{"selection_id": selectionID, "file_choice": "all_files"}, http.StatusAccepted)
	again := s.requestArchive(map[string]any{"selection_id": selectionID, "file_choice": "all_files"}, http.StatusOK)

	// then both answers name one queued archive
	s.Equal(first.Data.Id, again.Data.Id)
	s.Equal(httpapi.DownloadStatusQueued, first.Data.Attributes.Status)
	s.Equal(httpapi.DownloadFileChoiceAllFiles, first.Data.Attributes.FileChoice)
	s.Equal(selectionID, first.Data.Attributes.SelectionId.String())
	s.Equal(int64(10_000_000_000), first.Data.Attributes.MaxArchiveBytes)
	s.Nil(first.Data.Attributes.ArchiveUrl)
	s.Equal(testPublicBaseURL+"/v1/selections/"+selectionID, first.Data.Links.Selection)

	// and it can be read by its link, signed out
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodGet,
		"/v1/downloads/"+first.Data.Id, nil, "")
	read := decodeJSONResponse[httpapi.DownloadDocument](s.T(), response, http.StatusOK)
	s.Equal(first.Data, read.Data)

	// and another file choice is another archive
	original := s.requestArchive(map[string]any{"selection_id": selectionID, "file_choice": "original_mmcif"},
		http.StatusAccepted)
	s.NotEqual(first.Data.Id, original.Data.Id)
}

func (s *DownloadsSuite) Test_should_answer_422_pointing_at_the_selection_when_it_is_unknown() {
	// given an archive request of a selection nobody saved
	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodPost, "/v1/downloads",
		map[string]any{"selection_id": uuid.NewString(), "file_choice": "all_files"}, "")

	// then
	document := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(document.Errors, 1)
	s.Require().NotNil(document.Errors[0].Source)
	s.Equal("/selection_id", document.Errors[0].Source.Pointer)
	s.True(strings.Contains(*document.Errors[0].Detail, "no saved selection"))
}

func (s *DownloadsSuite) Test_should_answer_404_when_an_archive_is_unknown_or_downloads_are_off() {
	// given an unknown archive ID, and a server with downloads off
	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	unknown := jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodGet,
		"/v1/downloads/"+uuid.NewString(), nil, "")

	// then
	decodeJSONResponse[httpapi.Error](s.T(), unknown, http.StatusNotFound)

	// and
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	off := jsonRequestToServerWithToken(s.T(), testServer.URL, http.MethodPost, "/v1/downloads",
		map[string]any{"selection_id": uuid.NewString(), "file_choice": "all_files"}, "")
	document := decodeJSONResponse[httpapi.Error](s.T(), off, http.StatusNotFound)
	s.Equal("DOWNLOADS_OFF", *document.Errors[0].Code)
}

func (s *DownloadsSuite) prepareArchive(id string) *http.Response {
	//nolint:bodyclose // the callers decode and close the response body.
	return jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodPost,
		"/v1/downloads/"+id+"/prepare", nil, "")
}

func (s *DownloadsSuite) Test_should_build_the_archive_only_once_prepared_when_its_preview_is_asked_for() {
	// given a saved selection, and a request for the preview only
	selectionID := s.savedSelectionForDownloadsTest("downloads-previewer", 8922)
	preview := s.requestArchive(map[string]any{"selection_id": selectionID, "file_choice": "original_mmcif",
		"prepare": false}, http.StatusAccepted)
	s.False(preview.Data.Attributes.PrepareRequested)
	s.Equal(httpapi.DownloadStatusQueued, preview.Data.Attributes.Status)
	s.Nil(preview.Data.Attributes.Preview, "the worker has not planned it yet")

	// when it is prepared
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	prepared := decodeJSONResponse[httpapi.DownloadDocument](s.T(), s.prepareArchive(preview.Data.Id),
		http.StatusAccepted)

	// then it is asked for once
	s.Equal(preview.Data.Id, prepared.Data.Id)
	s.True(prepared.Data.Attributes.PrepareRequested)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	again := decodeJSONResponse[httpapi.DownloadDocument](s.T(), s.prepareArchive(preview.Data.Id),
		http.StatusOK)
	s.True(again.Data.Attributes.PrepareRequested)

	// and an unknown archive is not found
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	decodeJSONResponse[httpapi.Error](s.T(), s.prepareArchive(uuid.NewString()), http.StatusNotFound)
}

func (s *DownloadsSuite) Test_should_answer_409_when_a_blocked_archive_is_prepared() {
	// given an archive whose planning blocked it
	selectionID := s.savedSelectionForDownloadsTest("downloads-blocked", 8923)
	request := s.requestArchive(map[string]any{"selection_id": selectionID, "file_choice": "all_files",
		"prepare": false}, http.StatusAccepted)
	ctx := s.T().Context()
	now := time.Now().UTC()
	var claimed *models.DownloadJob
	for {
		job, err := testDatabase.Downloads.Claim(ctx, db.DownloadLanePlanning, now, now.Add(time.Minute))
		s.Require().NoError(err)
		s.Require().NotNil(job, "the request is waiting to be planned")
		if job.ID.String() == request.Data.Id {
			claimed = job
			break
		}
		s.Require().NoError(testDatabase.Downloads.Release(ctx, job.ID, job.Attempts))
	}
	s.Require().NoError(testDatabase.Downloads.Block(ctx, claimed.ID, claimed.Attempts, claimed.MaxArchiveBytes+1,
		models.DownloadPreview{IncludedBytes: claimed.MaxArchiveBytes + 1, FileTypes: []models.DownloadPreviewFileType{},
			NotIncluded: []models.DownloadPreviewReason{}},
		"the files to include add up to 10.0 GB, more than the 10.0 GB one archive may hold", now))

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	document := decodeJSONResponse[httpapi.Error](s.T(), s.prepareArchive(request.Data.Id),
		http.StatusConflict)

	// then
	s.Equal("DOWNLOAD_NOT_PREPARABLE", *document.Errors[0].Code)

	// and reading it shows the blocked preview and why
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	read := decodeJSONResponse[httpapi.DownloadDocument](s.T(), jsonRequestToServerWithToken(s.T(), selectionsServer.URL,
		http.MethodGet, "/v1/downloads/"+request.Data.Id, nil, ""), http.StatusOK)
	s.Equal(httpapi.DownloadStatusBlocked, read.Data.Attributes.Status)
	s.Require().NotNil(read.Data.Attributes.Preview)
	s.True(read.Data.Attributes.Preview.OverLimit)
}
