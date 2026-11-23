package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

var downloadsTestNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

// downloadsTestServer has downloads on, enforces request keys and has no database: every case it
// serves is answered before a request reaches the database.
func downloadsTestServer(requests int) *Server {
	server := (&Server{
		publicBaseURL:   "https://dynamicpdb.test/api",
		requestKeysMode: validation.ModeEnforce,
		validationMode:  validation.ModeEnforce,
	}).WithDownloads(DownloadsPolicy{
		Enabled: true, MaxArchiveBytes: 10_000_000_000, MaxUnfinished: 20, MaxUnfinishedPerSelection: 3,
		Requests: requests, InternalRequests: requests, Window: time.Minute, TrustedProxies: clusterProxies,
	})
	server.bulkDownloads.now = func() time.Time { return downloadsTestNow }
	return server
}

func newDownloadRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/downloads", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONAPIMediaType)
	request.RemoteAddr = "192.0.2.1:1234"
	return request
}

func Test_should_answer_404_on_every_downloads_route_when_downloads_are_off(t *testing.T) {
	// given
	server := &Server{publicBaseURL: "https://dynamicpdb.test/api"}

	// when
	create := httptest.NewRecorder()
	server.CreateDownload(create, newDownloadRequest(`{}`))
	get := httptest.NewRecorder()
	server.GetDownload(get, httptest.NewRequest(http.MethodGet, "/v1/downloads/x", nil), uuid.NewString())

	// then
	for _, recorder := range []*httptest.ResponseRecorder{create, get} {
		assert.Equal(t, http.StatusNotFound, recorder.Code)
		errs := decodeErrorObjects(t, recorder)
		require.Len(t, errs, 1)
		assert.Equal(t, "DOWNLOADS_OFF", *errs[0].Code)
	}
}

func Test_should_answer_422_naming_each_field_when_an_archive_request_is_invalid(t *testing.T) {
	// given
	cases := map[string]struct {
		body     string
		pointers []string
	}{
		"empty":                {`{}`, []string{"/selection_id", "/file_choice"}},
		"selection not a uuid": {`{"selection_id":"abc","file_choice":"all_files"}`, []string{"/selection_id"}},
		"unknown file choice": {`{"selection_id":"` + uuid.NewString() + `","file_choice":"everything"}`,
			[]string{"/file_choice"}},
		"unknown key": {`{"selection_id":"` + uuid.NewString() + `","file_choice":"all_files","zip":true}`,
			[]string{"/zip"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// when
			recorder := httptest.NewRecorder()
			downloadsTestServer(1000).CreateDownload(recorder, newDownloadRequest(tc.body))

			// then
			assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			var pointers []string
			for _, errorObject := range decodeErrorObjects(t, recorder) {
				require.NotNil(t, errorObject.Source)
				pointers = append(pointers, errorObject.Source.Pointer)
			}
			assert.Equal(t, tc.pointers, pointers)
		})
	}
}

func Test_should_answer_400_when_an_archive_request_body_is_not_json(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()

	// when
	downloadsTestServer(1000).CreateDownload(recorder, newDownloadRequest(`{"selection_id":`))

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func Test_should_answer_429_with_retry_after_when_a_client_asks_for_too_many_archives(t *testing.T) {
	// given
	server := downloadsTestServer(1)
	first := httptest.NewRecorder()
	server.CreateDownload(first, newDownloadRequest(`{}`))
	require.Equal(t, http.StatusUnprocessableEntity, first.Code)

	// when
	second := httptest.NewRecorder()
	server.CreateDownload(second, newDownloadRequest(`{}`))

	// then
	assert.Equal(t, http.StatusTooManyRequests, second.Code)
	assert.NotEmpty(t, second.Header().Get("Retry-After"))
	assert.Contains(t, *decodeErrorObjects(t, second)[0].Detail, "1 archives per 1m0s")
}

func Test_should_answer_each_status_and_code_when_an_archive_request_fails(t *testing.T) {
	// given
	cases := []struct {
		err        error
		status     int
		code       string
		retryAfter bool
	}{
		{fmt.Errorf("create download: %w", db.ErrDownloadSelectionNotFound), http.StatusUnprocessableEntity,
			string(validation.CodeInvalidValue), false},
		{fmt.Errorf("create download: %w", db.ErrDownloadSelectionBusy), http.StatusTooManyRequests, "SELECTION_BUSY", true},
		{fmt.Errorf("create download: %w", db.ErrDownloadsBusy), http.StatusServiceUnavailable, "DOWNLOADS_BUSY", true},
		{errors.New("connection refused"), http.StatusInternalServerError, "INTERNAL_ERROR", false},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			// when
			recorder := httptest.NewRecorder()
			downloadsTestServer(1000).writeDownloadCreateError(recorder, newDownloadRequest(`{}`), tc.err)

			// then
			assert.Equal(t, tc.status, recorder.Code)
			assert.Equal(t, tc.code, *decodeErrorObjects(t, recorder)[0].Code)
			assert.Equal(t, tc.retryAfter, recorder.Header().Get("Retry-After") != "")
		})
	}
}

func Test_should_answer_404_when_a_download_id_is_not_a_uuid(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()

	// when
	downloadsTestServer(1000).GetDownload(recorder, httptest.NewRequest(http.MethodGet, "/v1/downloads/x", nil), "not-a-uuid")

	// then
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func readyDownloadJob(expiresAt time.Time) domainmodels.DownloadJob {
	finished := downloadsTestNow.Add(-time.Hour)
	return domainmodels.DownloadJob{
		ID: uuid.MustParse("6f1c4a2e-0000-4000-8000-0000000000aa"), SelectionID: uuid.MustParse("6f1c4a2e-0000-4000-8000-0000000000bb"),
		FileChoice: domainmodels.DownloadFileChoiceAllFiles, Status: domainmodels.DownloadStatusReady,
		MaxArchiveBytes: 10_000_000_000, CreatedAt: finished.Add(-time.Minute), FinishedAt: &finished, ExpiresAt: &expiresAt,
		PlannedBytes: ptr(int64(100)), ArchiveBytes: ptr(int64(80)), ArchiveSHA256: ptr(strings.Repeat("c", 64)),
		ArchiveURL: ptr("https://files.dynamicpdb.test/downloads/a/archive.zip"), ManifestURL: ptr("https://files.dynamicpdb.test/downloads/a/manifest.tsv"),
		Counts: &domainmodels.DownloadCounts{Included: 3, Linked: 1, ChangedAtSource: 1},
	}
}

func Test_should_offer_the_archive_until_it_expires_when_a_ready_download_is_answered(t *testing.T) {
	// given
	server := downloadsTestServer(1000)

	// when
	ready, err := server.downloadDocument(readyDownloadJob(downloadsTestNow.Add(time.Hour)))

	// then
	require.NoError(t, err)
	assert.Equal(t, DownloadStatusReady, ready.Data.Attributes.Status)
	assert.Equal(t, "https://files.dynamicpdb.test/downloads/a/archive.zip", *ready.Data.Attributes.ArchiveUrl)
	assert.NotNil(t, ready.Data.Attributes.ManifestUrl)
	assert.Nil(t, ready.Data.Attributes.Message)
	assert.Equal(t, &DownloadCounts{Included: 3, Linked: 1, ChangedAtSource: 1}, ready.Data.Attributes.Counts)
	assert.Equal(t, "https://dynamicpdb.test/api/v1/downloads/6f1c4a2e-0000-4000-8000-0000000000aa", ready.Data.Links.Self)
	assert.Equal(t, "https://dynamicpdb.test/api/v1/selections/6f1c4a2e-0000-4000-8000-0000000000bb", ready.Data.Links.Selection)
	assert.Equal(t, "downloads", ready.Data.Type)

	// and once it expires
	expired, err := server.downloadDocument(readyDownloadJob(downloadsTestNow))
	require.NoError(t, err)
	assert.Equal(t, DownloadStatusExpired, expired.Data.Attributes.Status)
	assert.Nil(t, expired.Data.Attributes.ArchiveUrl, "an expired archive is no longer offered")
	assert.Nil(t, expired.Data.Attributes.ManifestUrl)
	assert.Equal(t, strings.Repeat("c", 64), *expired.Data.Attributes.ArchiveSha256)
	assert.Contains(t, *expired.Data.Attributes.Message, "Ask again with POST /v1/downloads")
}

func Test_should_say_why_and_what_to_do_when_an_archive_is_blocked_or_failed(t *testing.T) {
	// given
	server := downloadsTestServer(1000)
	blocked := readyDownloadJob(downloadsTestNow)
	blocked.Status = domainmodels.DownloadStatusBlocked
	blocked.BlockedReason = ptr("the files to include add up to at least 12.0 GB")

	// when
	document, err := server.downloadDocument(blocked)

	// then
	require.NoError(t, err)
	assert.Equal(t, "the files to include add up to at least 12.0 GB. Choose fewer models or a smaller file choice, "+
		"or download the files with the scripted download, which has no size limit.", *document.Data.Attributes.Message)
	assert.Nil(t, document.Data.Attributes.ArchiveUrl)

	// and a failed archive says why
	failed := readyDownloadJob(downloadsTestNow)
	failed.Status = domainmodels.DownloadStatusFailed
	failed.Error = ptr("the archive could not be written to storage")
	document, err = server.downloadDocument(failed)
	require.NoError(t, err)
	assert.Equal(t, DownloadStatusFailed, document.Data.Attributes.Status)
	assert.True(t, strings.HasPrefix(*document.Data.Attributes.Message, "the archive could not be written to storage."))

	// and a queued archive has no message
	queued := readyDownloadJob(downloadsTestNow)
	queued.Status = domainmodels.DownloadStatusQueued
	document, err = server.downloadDocument(queued)
	require.NoError(t, err)
	assert.Nil(t, document.Data.Attributes.Message)
}

func Test_should_fail_on_an_unknown_status_or_file_choice_when_a_download_is_answered(t *testing.T) {
	// given
	server := downloadsTestServer(1000)
	unknownStatus := readyDownloadJob(downloadsTestNow)
	unknownStatus.Status = "lost"

	// when
	_, err := server.downloadDocument(unknownStatus)

	// then
	require.ErrorContains(t, err, "unknown download status")

	// and
	unknownChoice := readyDownloadJob(downloadsTestNow.Add(time.Hour))
	unknownChoice.FileChoice = "everything"
	_, err = server.downloadDocument(unknownChoice)
	require.ErrorContains(t, err, "unknown download file choice")

	_, err = downloadMessage(unknownStatus, "lost")
	require.ErrorContains(t, err, "unknown download status")
}

func Test_should_use_the_documented_keys_when_a_download_document_is_serialized(t *testing.T) {
	// given
	document, err := downloadsTestServer(1000).downloadDocument(readyDownloadJob(downloadsTestNow.Add(time.Hour)))
	require.NoError(t, err)

	// when
	data, err := json.Marshal(document)

	// then
	require.NoError(t, err)
	for _, key := range []string{`"selection_id"`, `"file_choice":"all_files"`, `"status":"ready"`, `"max_archive_bytes":10000000000`,
		`"archive_sha256"`, `"changed_at_source":1`} {
		assert.Contains(t, string(data), key)
	}
}
