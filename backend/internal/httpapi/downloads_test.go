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
	prepare := httptest.NewRecorder()
	server.PrepareDownload(prepare, httptest.NewRequest(http.MethodPost, "/v1/downloads/x/prepare", nil), uuid.NewString())
	failures := httptest.NewRecorder()
	server.ListDownloadFailures(failures, httptest.NewRequest(http.MethodGet, "/v1/downloads/x/failures", nil),
		uuid.NewString(), ListDownloadFailuresParams{})
	retry := httptest.NewRecorder()
	server.RetryDownload(retry, httptest.NewRequest(http.MethodPost, "/v1/downloads/x/retry", nil), uuid.NewString())

	// then
	for _, recorder := range []*httptest.ResponseRecorder{create, get, prepare, failures, retry} {
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
		"prepare not a boolean": {`{"selection_id":"` + uuid.NewString() + `","file_choice":"all_files","prepare":"yes"}`,
			[]string{"/prepare"}},
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
	prepare := httptest.NewRecorder()

	// when
	downloadsTestServer(1000).GetDownload(recorder, httptest.NewRequest(http.MethodGet, "/v1/downloads/x", nil), "not-a-uuid")
	downloadsTestServer(1000).PrepareDownload(prepare, newDownloadRequest(``), "not-a-uuid")
	failures := httptest.NewRecorder()
	downloadsTestServer(1000).ListDownloadFailures(failures, httptest.NewRequest(http.MethodGet, "/v1/downloads/x/failures", nil),
		"not-a-uuid", ListDownloadFailuresParams{})
	retry := httptest.NewRecorder()
	downloadsTestServer(1000).RetryDownload(retry, newDownloadRequest(``), "not-a-uuid")

	// then
	for _, answered := range []*httptest.ResponseRecorder{recorder, prepare, failures, retry} {
		assert.Equal(t, http.StatusNotFound, answered.Code)
	}
}

func readyDownloadJob(expiresAt time.Time) domainmodels.DownloadJob {
	finished := downloadsTestNow.Add(-time.Hour)
	return domainmodels.DownloadJob{
		ID: uuid.MustParse("6f1c4a2e-0000-4000-8000-0000000000aa"), SelectionID: uuid.MustParse("6f1c4a2e-0000-4000-8000-0000000000bb"),
		FileChoice: domainmodels.DownloadFileChoiceAllFiles, Status: domainmodels.DownloadStatusReady,
		MaxArchiveBytes: 10_000_000_000, CreatedAt: finished.Add(-time.Minute), FinishedAt: &finished, ExpiresAt: &expiresAt,
		PlannedBytes: ptr(int64(100)), ArchiveBytes: ptr(int64(80)), ArchiveSHA256: ptr(strings.Repeat("c", 64)),
		ArchiveURL: ptr("https://files.dynamicpdb.test/downloads/a/archive.zip"), ManifestURL: ptr("https://files.dynamicpdb.test/downloads/a/manifest.tsv"),
		Counts: &domainmodels.DownloadCounts{Included: 3, Linked: 1, Unavailable: 1},
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
	assert.Equal(t, &DownloadCounts{Included: 3, Linked: 1, Unavailable: 1}, ready.Data.Attributes.Counts)
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
		`"archive_sha256"`, `"unavailable":1`} {
		assert.Contains(t, string(data), key)
	}
}

func plannedDownloadJob() domainmodels.DownloadJob {
	job := readyDownloadJob(downloadsTestNow.Add(time.Hour))
	job.Status = domainmodels.DownloadStatusPlanned
	job.FinishedAt, job.ExpiresAt, job.ArchiveURL, job.ManifestURL, job.Counts = nil, nil, nil, nil, nil
	job.Preview = &domainmodels.DownloadPreview{
		Entries: 2, Models: 6, ModelsWithoutMatchingFiles: 1, Files: 5,
		FileTypes: []domainmodels.DownloadPreviewFileType{
			{Type: domainmodels.ArtifactTypeModel, Files: 3, BytesKnown: 300},
			{Type: domainmodels.ArtifactTypeStructureFactors, Files: 2, BytesKnown: 2_000_000_000, FilesSizeUnknown: 1},
		},
		Availability: domainmodels.DownloadCounts{Included: 2, Linked: 2, Unavailable: 1},
		NotIncluded: []domainmodels.DownloadPreviewReason{
			{Code: domainmodels.DownloadReasonOverCopyLimit, Files: 2},
			{Code: domainmodels.DownloadReasonOriginalBytesUnavailable, Files: 1},
		},
		Verification:     domainmodels.DownloadPreviewVerification{Verified: 4, Mismatch: 1},
		IncludedBytes:    200,
		LinkedBytesKnown: 2_000_000_000, LinkedFilesSizeUnknown: 1,
		RelatedDatasets:   domainmodels.DownloadPreviewRelatedDatasets{Total: 2, WithDOI: 1, WithAccessionOnly: 1},
		ReuseTerms:        domainmodels.DownloadPreviewReuseTerms{CC0: 3, NotConfirmed: 1, Repository: 1},
		NotVerifiedAtSave: 2,
	}
	return job
}

func Test_should_show_what_the_archive_holds_and_how_sure_each_size_is_when_a_planned_download_is_answered(t *testing.T) {
	// given
	server := downloadsTestServer(1000)

	// when
	document, err := server.downloadDocument(plannedDownloadJob())

	// then
	require.NoError(t, err)
	attributes := document.Data.Attributes
	assert.Equal(t, DownloadStatusPlanned, attributes.Status)
	assert.Nil(t, attributes.Message)
	require.NotNil(t, attributes.Preview)
	preview := attributes.Preview
	assert.Equal(t, DownloadPreviewRecords{Entries: 2, Models: 6, ModelsWithoutMatchingFiles: 1}, preview.Records)
	assert.Equal(t, DownloadCounts{Included: 2, Linked: 2, Unavailable: 1}, preview.Files.Availability)
	assert.Equal(t, 2, preview.Files.NotVerifiedAtSave)
	assert.Equal(t, DownloadPreviewReuseTerms{Cc0: 3, NotConfirmed: 1, Repository: 1}, preview.ReuseTerms)
	assert.Equal(t, DownloadPreviewVerification{Verified: 4, Mismatch: 1}, preview.Files.Verification)
	assert.Equal(t, []DownloadPreviewReason{
		{Code: DownloadReasonCodeOverCopyLimit, Status: DownloadFileAvailabilityLinked, Files: 2,
			Reason: "larger than 1 GiB, so Dynamic PDB keeps only its link"},
		{Code: DownloadReasonCodeOriginalBytesUnavailable, Status: DownloadFileAvailabilityUnavailable, Files: 1,
			Reason: "original bytes unavailable: the source now serves other bytes, and no copy was kept"},
	}, preview.Files.NotIncluded)
	require.Len(t, preview.Files.ByType, 2)
	assert.Equal(t, DownloadSizeKindExact, preview.Files.ByType[0].Size.Kind)
	assert.Equal(t, int64(300), *preview.Files.ByType[0].Size.Bytes)
	assert.Equal(t, DownloadSizeKindPartial, preview.Files.ByType[1].Size.Kind, "1 of 2 sizes is unknown")
	assert.Equal(t, int64(2_000_000_000), *preview.Files.ByType[1].Size.Bytes, "the known size, not an estimate")
	assert.Equal(t, 1, *preview.Files.ByType[1].Size.FilesSizeUnknown)
	assert.Equal(t, DownloadSizeKindExact, preview.Sizes.Included.Kind)
	assert.Equal(t, int64(200), *preview.Sizes.Included.Bytes)
	assert.Equal(t, DownloadSizeKindPartial, preview.Sizes.Linked.Kind)
	assert.Equal(t, 1, *preview.Sizes.Linked.FilesSizeUnknown)
	assert.Equal(t, DownloadPreviewRelatedDatasets{Total: 2, WithDoi: 1, WithAccessionOnly: 1}, preview.RelatedDatasets)
	assert.False(t, preview.OverLimit)
	assert.False(t, attributes.PrepareRequested)

	// and the keys are the documented ones
	data, err := json.Marshal(document)
	require.NoError(t, err)
	for _, key := range []string{`"status":"planned"`, `"prepare_requested":false`, `"preview":{`,
		`"models_without_matching_files":1`, `"not_included":[`, `"code":"over_copy_limit"`,
		`"code":"original_bytes_unavailable"`, `"kind":"partial"`, `"with_accession_only":1`, `"over_limit":false`,
		`"reuse_terms":{"cc0":3,"not_confirmed":1,"repository":1}`, `"not_verified_at_save":2`} {
		assert.Contains(t, string(data), key)
	}
	for _, gone := range []string{"changed-at-source", "changed_at_source", "estimate", "tolerance", "upper bound"} {
		assert.NotContains(t, string(data), gone)
	}

	// and a queued job has no preview yet
	queued := plannedDownloadJob()
	queued.Status, queued.Preview = domainmodels.DownloadStatusQueued, nil
	document, err = server.downloadDocument(queued)
	require.NoError(t, err)
	assert.Nil(t, document.Data.Attributes.Preview)
}

func Test_should_say_exact_partial_or_unknown_never_an_estimate_when_some_sizes_are_not_known(t *testing.T) {
	// given
	cases := []struct {
		name        string
		files       int
		sizeUnknown int
		kind        DownloadSizeKind
		bytes       bool
	}{
		{"every size known", 3, 0, DownloadSizeKindExact, true},
		{"no files", 0, 0, DownloadSizeKindExact, true},
		{"some unknown", 3, 1, DownloadSizeKindPartial, true},
		{"all unknown", 3, 3, DownloadSizeKindUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			size := partialSize(tc.files, tc.sizeUnknown, 10, "files")

			// then
			assert.Equal(t, tc.kind, size.Kind)
			assert.Equal(t, tc.bytes, size.Bytes != nil)
			assert.Equal(t, "files", size.Measures)
		})
	}
}

func Test_should_fail_on_an_unknown_reason_or_file_type_when_a_preview_is_answered(t *testing.T) {
	// given
	server := downloadsTestServer(1000)
	unknownReason := plannedDownloadJob()
	unknownReason.Preview.NotIncluded = []domainmodels.DownloadPreviewReason{{Code: "lost", Files: 1}}
	unknownType := plannedDownloadJob()
	unknownType.Preview.FileTypes = []domainmodels.DownloadPreviewFileType{{Type: "image", Files: 1}}

	// when
	_, reasonErr := server.downloadDocument(unknownReason)
	_, typeErr := server.downloadDocument(unknownType)

	// then
	require.ErrorContains(t, reasonErr, "unknown download reason code")
	require.ErrorContains(t, typeErr, "unknown artifact type")
}

func Test_should_say_the_preview_is_over_the_limit_when_its_included_bytes_pass_it(t *testing.T) {
	// given
	blocked := plannedDownloadJob()
	blocked.Status = domainmodels.DownloadStatusBlocked
	finished := downloadsTestNow
	blocked.FinishedAt = &finished
	blocked.Preview.IncludedBytes = blocked.MaxArchiveBytes + 1

	// when
	document, err := downloadsTestServer(1000).downloadDocument(blocked)

	// then
	require.NoError(t, err)
	assert.True(t, document.Data.Attributes.Preview.OverLimit)
}

func Test_should_answer_each_status_and_code_when_a_retry_fails(t *testing.T) {
	// given
	cases := []struct {
		err        error
		status     int
		code       string
		retryAfter bool
	}{
		{fmt.Errorf("retry download: %w", db.ErrDownloadNotFound), http.StatusNotFound, "NOT_FOUND", false},
		{fmt.Errorf("retry download: %w", db.ErrDownloadNotRetryable), http.StatusConflict, "DOWNLOAD_NOT_RETRYABLE", false},
		{fmt.Errorf("retry download: %w", db.ErrDownloadNothingToRetry), http.StatusConflict, "NOTHING_TO_RETRY", false},
		{fmt.Errorf("retry download: %w", db.ErrDownloadSelectionBusy), http.StatusTooManyRequests, "SELECTION_BUSY", true},
		{fmt.Errorf("retry download: %w", db.ErrDownloadsBusy), http.StatusServiceUnavailable, "DOWNLOADS_BUSY", true},
		{errors.New("connection refused"), http.StatusInternalServerError, "INTERNAL_ERROR", false},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			// when
			recorder := httptest.NewRecorder()
			downloadsTestServer(1000).writeDownloadRetryError(recorder, newDownloadRequest(``), tc.err)

			// then
			assert.Equal(t, tc.status, recorder.Code)
			assert.Equal(t, tc.code, *decodeErrorObjects(t, recorder)[0].Code)
			assert.Equal(t, tc.retryAfter, recorder.Header().Get("Retry-After") != "")
		})
	}
}

func Test_should_show_the_reason_and_whether_a_retry_fetches_it_when_a_failure_is_answered(t *testing.T) {
	// given
	failure := domainmodels.DownloadFailure{
		JobID: uuid.New(), FileID: uuid.MustParse("6f1c4a2e-0000-4000-8000-0000000000cc"), Ordinal: 3,
		EntryID: "dpdb_e1", ModelID: "dpdb_e1_m_003", FileName: "refine.log",
		ReasonCode: domainmodels.DownloadReasonReadFailed, Retryable: true,
	}

	// when
	data, err := downloadFailureData(failure)

	// then
	require.NoError(t, err)
	assert.Equal(t, "download-failures", data.Type)
	assert.Equal(t, "6f1c4a2e-0000-4000-8000-0000000000cc", data.Id)
	assert.Equal(t, DownloadFailureAttributes{
		FileId: failure.FileID, FileName: "refine.log", EntryId: "dpdb_e1", ModelId: "dpdb_e1_m_003",
		Status: DownloadFileAvailabilityUnavailable, ReasonCode: DownloadReasonCodeReadFailed,
		Reason:    "Dynamic PDB's copy could not be read while the archive was prepared; ask for the failed files again",
		Retryable: true,
	}, data.Attributes)

	// and an unknown code fails loudly
	failure.ReasonCode = "lost"
	_, err = downloadFailureData(failure)
	require.ErrorContains(t, err, "unknown download reason code")
}

func Test_should_link_the_failures_and_name_the_parent_when_a_retry_is_answered(t *testing.T) {
	// given a ready retry with no retryable failure left
	job := readyDownloadJob(downloadsTestNow.Add(time.Hour))
	parent := uuid.MustParse("6f1c4a2e-0000-4000-8000-0000000000dd")
	job.RetryOf = &parent
	job.RetryableFiles = ptr(0)

	// when
	document, err := downloadsTestServer(1000).downloadDocument(job)

	// then
	require.NoError(t, err)
	assert.Equal(t, &parent, document.Data.Attributes.RetryOf)
	assert.Equal(t, 0, *document.Data.Attributes.RetryableFiles)
	assert.Equal(t, "https://dynamicpdb.test/api/v1/downloads/6f1c4a2e-0000-4000-8000-0000000000aa/failures",
		*document.Data.Links.Failures)
	data, err := json.Marshal(document)
	require.NoError(t, err)
	for _, key := range []string{`"retry_of":"6f1c4a2e-0000-4000-8000-0000000000dd"`, `"retryable_files":0`, `"failures":"`} {
		assert.Contains(t, string(data), key)
	}
}
