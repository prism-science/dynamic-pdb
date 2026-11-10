package httpapi

import (
	"context"
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

// selectionsTestServer has selections on, enforces request keys and has no database: every case
// it serves is answered before a save reaches the database.
func selectionsTestServer(modelSearchEnabled bool) *Server {
	server := (&Server{
		publicBaseURL:   "https://dynamicpdb.test/api",
		requestKeysMode: validation.ModeEnforce,
		validationMode:  validation.ModeEnforce,
	}).WithSelections(SelectionsPolicy{
		Enabled:          true,
		MaxModels:        1000,
		MaxStoredModels:  100000,
		FreezeTimeout:    time.Minute,
		MaxInFlight:      1,
		Requests:         1000,
		InternalRequests: 1000,
		Window:           time.Minute,
		TrustedProxies:   clusterProxies,
	})
	return server.WithModelSearch(ModelSearchPolicy{
		FiltersEnabled: modelSearchEnabled, CountCap: 10000,
		Requests: 1000, InternalRequests: 1000, Window: time.Minute, TrustedProxies: clusterProxies,
	})
}

func newSelectionRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/selections", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONAPIMediaType)
	request.RemoteAddr = "192.0.2.1:1234"
	return request
}

func decodeErrorObjects(t *testing.T, recorder *httptest.ResponseRecorder) []ErrorObject {
	t.Helper()
	var document Error
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &document))
	return document.Errors
}

func Test_should_read_a_search_query_as_the_model_list_reads_its_parameters_when_a_selection_search_is_given(t *testing.T) {
	// given
	query := "?r_free_max=0.25&model_type=Ensemble&model_type=Multiconformer&organism=Homo+sapiens&flagged=true"
	params := ListModelsAcrossEntriesParams{
		RFreeMax:  ptr(0.25),
		ModelType: &[]StructureModelType{StructureModelTypeEnsemble, StructureModelTypeMulticonformer},
		Organism:  &[]string{"Homo sapiens"},
		Flagged:   ptr(true),
	}
	want, err := modelSearchFromParams(params)
	require.NoError(t, err)

	// when
	search, text, err := modelSearchFromQuery(context.Background(), query)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, want.filters, search.filters)
	assert.Equal(t, "flagged=true&model_type=Ensemble&model_type=Multiconformer&organism=Homo+sapiens&r_free_max=0.25", text)
}

func Test_should_return_no_filters_when_a_selection_search_is_empty(t *testing.T) {
	// when
	search, text, err := modelSearchFromQuery(context.Background(), "")

	// then
	require.NoError(t, err)
	assert.Nil(t, search)
	assert.Empty(t, text)
}

func Test_should_name_the_parameter_when_a_selection_search_query_is_invalid(t *testing.T) {
	cases := []struct {
		name          string
		query         string
		wantParameter string
	}{
		{name: "misspelled filter", query: "modeltype=Ensemble", wantParameter: "modeltype"},
		{name: "limit", query: "model_type=Ensemble&limit=10", wantParameter: "limit"},
		{name: "offset", query: "offset=10", wantParameter: "offset"},
		{name: "cursor", query: "cursor=abc", wantParameter: "cursor"},
		{name: "value of the wrong type", query: "resolution_max=high", wantParameter: "resolution_max"},
		{name: "value outside the vocabulary", query: "model_type=Hologram", wantParameter: "model_type"},
		{name: "minimum above maximum", query: "r_free_min=0.3&r_free_max=0.2", wantParameter: "r_free_min"},
		{name: "not a query string", query: "model_type=%zz", wantParameter: "query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			_, _, err := modelSearchFromQuery(context.Background(), tc.query)

			// then
			var paramError *modelSearchParamError
			require.ErrorAs(t, err, &paramError)
			assert.Equal(t, tc.wantParameter, paramError.parameter)
		})
	}
}

// Every filter of GET /v1/models must be accepted in a selection search, including filters added
// to the spec later.
func Test_should_accept_every_model_list_filter_and_no_paging_parameter_when_selection_search_parameters_are_read(t *testing.T) {
	// when
	names, err := modelSearchFilterParameters()

	// then
	require.NoError(t, err)
	for _, filter := range []string{"method", "resolution_min", "organism", "model_type", "compound", "r_free_max", "metric_missing", "flagged"} {
		assert.True(t, names[filter], filter)
	}
	for paging := range modelSearchPagingParameters {
		assert.False(t, names[paging], paging)
	}
}

func Test_should_answer_422_naming_each_field_when_a_selection_request_is_invalid(t *testing.T) {
	tooManyIDs := make([]string, maxSelectionModelIDs+1)
	for index := range tooManyIDs {
		tooManyIDs[index] = fmt.Sprintf("dpdb_test_m_%05d", index)
	}
	tooManySearches := make([]string, maxSelectionSearches+1)
	cases := []struct {
		name        string
		body        any
		wantPointer string
		wantCode    validation.Code
	}{
		{name: "neither models nor searches", body: map[string]any{}, wantPointer: "/model_ids", wantCode: validation.CodeRequired},
		{name: "empty lists", body: map[string]any{"model_ids": []string{}, "searches": []string{}}, wantPointer: "/model_ids", wantCode: validation.CodeRequired},
		{name: "only excluded models", body: map[string]any{"exclude_model_ids": []string{"dpdb_a_m_001"}}, wantPointer: "/model_ids", wantCode: validation.CodeRequired},
		{name: "empty model ID", body: map[string]any{"model_ids": []string{"dpdb_a_m_001", " "}}, wantPointer: "/model_ids/1", wantCode: validation.CodeRequired},
		{name: "model ID too long", body: map[string]any{"model_ids": []string{strings.Repeat("m", maxSelectionModelIDLength+1)}}, wantPointer: "/model_ids/0", wantCode: validation.CodeOutOfRange},
		{name: "too many model IDs", body: map[string]any{"model_ids": tooManyIDs}, wantPointer: "/model_ids", wantCode: validation.CodeOutOfRange},
		{name: "too many excluded IDs", body: map[string]any{"searches": []string{""}, "exclude_model_ids": tooManyIDs}, wantPointer: "/exclude_model_ids", wantCode: validation.CodeOutOfRange},
		{name: "too many searches", body: map[string]any{"searches": tooManySearches}, wantPointer: "/searches", wantCode: validation.CodeOutOfRange},
		{name: "search too long", body: map[string]any{"searches": []string{"", strings.Repeat("a", maxSelectionSearchLength+1)}}, wantPointer: "/searches/1", wantCode: validation.CodeOutOfRange},
		{name: "misspelled search filter", body: map[string]any{"searches": []string{"modeltype=Ensemble"}}, wantPointer: "/searches/0", wantCode: validation.CodeInvalidValue},
		{name: "paging parameter in a search", body: map[string]any{"searches": []string{"cursor=abc"}}, wantPointer: "/searches/0", wantCode: validation.CodeInvalidValue},
		{name: "unknown body key", body: map[string]any{"searches": []string{""}, "search": "model_type=Ensemble"}, wantPointer: "/search", wantCode: validation.CodeUnknownField},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// given
			body, err := json.Marshal(tc.body)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()

			// when
			selectionsTestServer(true).CreateSelection(recorder, newSelectionRequest(string(body)))

			// then
			assert.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
			errorObjects := decodeErrorObjects(t, recorder)
			require.Len(t, errorObjects, 1)
			require.NotNil(t, errorObjects[0].Source)
			assert.Equal(t, tc.wantPointer, errorObjects[0].Source.Pointer)
			require.NotNil(t, errorObjects[0].Code)
			assert.Equal(t, string(tc.wantCode), *errorObjects[0].Code)
		})
	}
}

func Test_should_answer_400_when_a_selection_request_body_is_not_json(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()

	// when
	selectionsTestServer(true).CreateSelection(recorder, newSelectionRequest("{"))

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func Test_should_answer_400_naming_the_filters_when_a_selection_search_has_filters_and_model_search_is_off(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()

	// when
	selectionsTestServer(false).CreateSelection(recorder,
		newSelectionRequest(`{"searches": ["", "model_type=Ensemble&resolution_max=2"]}`))

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "model_type")
	assert.Contains(t, *errorObject.Detail, "resolution_max")
}

func Test_should_answer_404_on_every_selections_route_when_selections_are_off(t *testing.T) {
	// given
	server := &Server{}
	routes := map[string]func(http.ResponseWriter, *http.Request){
		"save": server.CreateSelection,
		"read": func(w http.ResponseWriter, r *http.Request) {
			server.GetSelection(w, r, "6f1d2c1e-0000-4000-8000-000000000001")
		},
		"list models": func(w http.ResponseWriter, r *http.Request) {
			server.ListSelectionModels(w, r, "6f1d2c1e-0000-4000-8000-000000000001", ListSelectionModelsParams{})
		},
	}
	for name, route := range routes {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			// when
			route(recorder, newSelectionRequest(`{"searches": [""]}`))

			// then
			assert.Equal(t, http.StatusNotFound, recorder.Code)
			errorObject := decodeSingleErrorObject(t, recorder)
			require.NotNil(t, errorObject.Code)
			assert.Equal(t, selectionsOffCode, *errorObject.Code)
		})
	}
}

func Test_should_answer_404_when_a_selection_id_is_not_a_uuid(t *testing.T) {
	// given
	recorder := httptest.NewRecorder()

	// when
	selectionsTestServer(true).GetSelection(recorder, httptest.NewRequest(http.MethodGet, "/v1/selections/x", nil), "not-a-uuid")

	// then
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func Test_should_answer_429_with_retry_after_when_a_client_saves_too_many_selections(t *testing.T) {
	// given
	server := selectionsTestServer(true).WithSelections(SelectionsPolicy{
		Enabled: true, MaxModels: 1000, MaxStoredModels: 100000, FreezeTimeout: time.Minute, MaxInFlight: 1,
		Requests: 1, InternalRequests: 1000, Window: time.Minute, TrustedProxies: clusterProxies,
	})
	// An invalid request still counts against the limit.
	server.CreateSelection(httptest.NewRecorder(), newSelectionRequest(`{}`))
	recorder := httptest.NewRecorder()

	// when
	server.CreateSelection(recorder, newSelectionRequest(`{}`))

	// then
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.NotEmpty(t, recorder.Header().Get("Retry-After"))
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Code)
	assert.Equal(t, rateLimitedErrorCode, *errorObject.Code)
}

func Test_should_answer_503_with_retry_after_when_every_save_slot_is_busy(t *testing.T) {
	// given
	server := selectionsTestServer(true)
	release, started := server.selections.tryStartSave()
	require.True(t, started)
	defer release()
	recorder := httptest.NewRecorder()

	// when
	server.CreateSelection(recorder, newSelectionRequest(`{"searches": [""]}`))

	// then
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Equal(t, "5", recorder.Header().Get("Retry-After"))
}

func Test_should_answer_each_freeze_failure_with_its_status_and_code_when_a_selection_save_fails(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		wantStatus     int
		wantCode       string
		wantRetryAfter bool
	}{
		{name: "empty", err: db.ErrSelectionEmpty, wantStatus: http.StatusUnprocessableEntity, wantCode: selectionEmptyCode},
		{name: "too large", err: fmt.Errorf("wrapped: %w", db.ErrSelectionTooLarge), wantStatus: http.StatusUnprocessableEntity, wantCode: selectionTooLargeCode},
		{name: "budget used up", err: db.ErrSelectionsFull, wantStatus: http.StatusServiceUnavailable, wantCode: selectionsFullCode, wantRetryAfter: true},
		{name: "timeout", err: db.ErrSelectionFreezeTimeout, wantStatus: http.StatusGatewayTimeout, wantCode: readTimeoutErrorCode, wantRetryAfter: true},
		{name: "invalid search", err: db.ErrInvalidModelSearch, wantStatus: http.StatusBadRequest, wantCode: "BAD_REQUEST"},
		{name: "anything else", err: errors.New("connection reset"), wantStatus: http.StatusInternalServerError, wantCode: "INTERNAL_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// given
			recorder := httptest.NewRecorder()

			// when
			selectionsTestServer(true).writeSelectionFreezeError(recorder, newSelectionRequest(`{}`), tc.err, CreateSelectionRequest{})

			// then
			assert.Equal(t, tc.wantStatus, recorder.Code)
			errorObject := decodeSingleErrorObject(t, recorder)
			require.NotNil(t, errorObject.Code)
			assert.Equal(t, tc.wantCode, *errorObject.Code)
			assert.Equal(t, tc.wantRetryAfter, recorder.Header().Get("Retry-After") != "")
		})
	}
}

func Test_should_point_at_the_first_place_of_each_unknown_model_id_when_a_selection_names_unknown_models(t *testing.T) {
	// given
	requested := &[]string{"dpdb_a_m_001", " dpdb_gone_m_001 ", "dpdb_gone_m_001", "dpdb_never_m_001"}

	// when
	fieldErrors := unknownSelectionModelErrors([]string{"dpdb_gone_m_001", "dpdb_never_m_001"}, requested)

	// then
	require.Len(t, fieldErrors, 2)
	assert.Equal(t, validation.Pointer("/model_ids/1"), fieldErrors[0].Pointer)
	assert.Equal(t, validation.Pointer("/model_ids/3"), fieldErrors[1].Pointer)
	assert.Contains(t, fieldErrors[1].Detail, "dpdb_never_m_001")
}

func Test_should_list_at_most_twenty_unknown_model_ids_when_a_selection_names_many_unknown_models(t *testing.T) {
	// given
	unknown := make([]string, maxUnknownSelectionModelErrors+5)
	for index := range unknown {
		unknown[index] = fmt.Sprintf("dpdb_gone_m_%03d", index)
	}

	// when
	fieldErrors := unknownSelectionModelErrors(unknown, &unknown)

	// then
	assert.Len(t, fieldErrors, maxUnknownSelectionModelErrors)
}

func Test_should_keep_each_model_id_once_in_request_order_when_selection_model_ids_are_read(t *testing.T) {
	// when
	ids, fieldErrors := selectionModelIDs(validation.Root.Field("model_ids"),
		&[]string{" dpdb_b_m_001", "dpdb_a_m_001", "dpdb_b_m_001 "})

	// then
	assert.Empty(t, fieldErrors)
	assert.Equal(t, []string{"dpdb_b_m_001", "dpdb_a_m_001"}, ids)
}

func Test_should_require_the_freeze_timeout_to_end_well_before_the_write_timeout_when_the_server_starts(t *testing.T) {
	assert.NoError(t, CheckSelectionFreezeTimeout(time.Minute, 3*time.Minute))
	assert.NoError(t, CheckSelectionFreezeTimeout(150*time.Second, 3*time.Minute))
	assert.Error(t, CheckSelectionFreezeTimeout(151*time.Second, 3*time.Minute))
}

func Test_should_build_absolute_links_when_a_selection_document_is_built(t *testing.T) {
	// given
	selection := domainmodels.Selection{ModelCount: 2, EntryCount: 1, CreatedAt: time.Now().UTC()}

	// when
	document := selectionsTestServer(true).selectionDocument(selection)

	// then
	self := "https://dynamicpdb.test/api/v1/selections/" + selection.ID.String()
	assert.Equal(t, self, document.Data.Links.Self)
	assert.Equal(t, self+"/models", document.Data.Links.Models)
	assert.Equal(t, []string{}, document.Data.Attributes.Request.Searches)
}

func Test_should_give_the_save_time_as_the_catalog_date_when_a_selection_document_is_built(t *testing.T) {
	// given
	savedAt := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	selection := domainmodels.Selection{ModelCount: 1, EntryCount: 1, CreatedAt: savedAt}

	// when
	document := selectionsTestServer(true).selectionDocument(selection)

	// then
	assert.Equal(t, savedAt, document.Data.Attributes.CatalogDate)
	assert.Equal(t, savedAt, document.Data.Attributes.CreatedAt)
}

func Test_should_map_every_role_and_source_when_selection_files_are_answered(t *testing.T) {
	// given
	sha256 := strings.Repeat("a", 64)
	checkedAt := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)
	file := domainmodels.SelectionFile{
		ID: uuid.New(),
		Roles: []domainmodels.SelectionFileRole{
			domainmodels.SelectionFileRolePrimary, domainmodels.SelectionFileRoleModel,
			domainmodels.SelectionFileRoleRunInput, domainmodels.SelectionFileRoleRunOutput,
			domainmodels.SelectionFileRoleEntry,
		},
		Name: "1abc.cif", Level: domainmodels.ArtifactLevelL2, Type: domainmodels.ArtifactTypeModel,
		RecordedSHA256: &sha256, RecordedSHA256Source: domainmodels.SelectionSHA256SourceComputed,
		RecordedAfterSave: true,
		Checksum: domainmodels.ArtifactChecksum{Status: domainmodels.ChecksumStatusVerified, SHA256: &sha256,
			CheckedAt: &checkedAt},
	}
	sources := map[domainmodels.SelectionSHA256Source]SelectionSHA256Source{
		domainmodels.SelectionSHA256SourceDeclared:    SelectionSHA256SourceDeclared,
		domainmodels.SelectionSHA256SourceComputed:    SelectionSHA256SourceComputed,
		domainmodels.SelectionSHA256SourceNotRecorded: SelectionSHA256SourceNotRecorded,
	}

	// when
	response, err := selectionFilesResponse([]domainmodels.SelectionFile{file})

	// then
	require.NoError(t, err)
	require.Len(t, response, 1)
	assert.Equal(t, []SelectionFileRole{SelectionFileRolePrimary, SelectionFileRoleModel, SelectionFileRoleRunInput,
		SelectionFileRoleRunOutput, SelectionFileRoleEntry}, response[0].Roles)
	assert.Equal(t, SelectionSHA256SourceComputed, response[0].RecordedSha256Source)
	assert.True(t, response[0].RecordedAfterSave)
	assert.Equal(t, ArtifactChecksumStatusVerified, response[0].Checksum.Status)
	for source, want := range sources {
		got, err := selectionSHA256SourceResponse(source)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func Test_should_fail_when_a_selection_file_has_an_unknown_role_or_source(t *testing.T) {
	// given
	unknownRole := domainmodels.SelectionFile{ID: uuid.New(), Roles: []domainmodels.SelectionFileRole{"owner"},
		RecordedSHA256Source: domainmodels.SelectionSHA256SourceDeclared}
	unknownSource := domainmodels.SelectionFile{ID: uuid.New(), Roles: []domainmodels.SelectionFileRole{
		domainmodels.SelectionFileRolePrimary}, RecordedSHA256Source: "guessed"}

	// when
	_, roleErr := selectionFilesResponse([]domainmodels.SelectionFile{unknownRole})
	_, sourceErr := selectionFilesResponse([]domainmodels.SelectionFile{unknownSource})

	// then
	assert.ErrorContains(t, roleErr, "unknown selection file role: owner")
	assert.ErrorContains(t, sourceErr, "unknown selection sha256 source: guessed")
}
