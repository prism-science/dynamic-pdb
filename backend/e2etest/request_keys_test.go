package e2etest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

// RequestKeysSuite covers keys a write request type does not define and keys starting
// with "__". testServer enforces that rule, as dev does; requestKeysLogOnlyServer only logs
// it, as production does while the rule rolls out.
type RequestKeysSuite struct {
	baseSuite
}

func TestRequestKeys(t *testing.T) {
	suite.Run(t, new(RequestKeysSuite))
}

// keyErrorResponse is the pointer and code of one error in a 422 response.
type keyErrorResponse struct {
	Pointer string
	Code    string
}

func keyErrorsFromResponse(t *testing.T, errors []httpapi.ErrorObject) []keyErrorResponse {
	t.Helper()
	result := make([]keyErrorResponse, 0, len(errors))
	for _, item := range errors {
		require.NotNil(t, item.Source, "every key error names its field")
		result = append(result, keyErrorResponse{Pointer: item.Source.Pointer, Code: stringValue(item.Code)})
	}
	return result
}

func (s *RequestKeysSuite) inReviewRevisionsOf(ownerToken string) []httpapi.EntryRevisionGroupAttributes {
	ownerID := tokenUserIDForTest(s.T(), ownerToken)
	//nolint:bodyclose // decodeEntryRevisionGroupsForTest closes the response body.
	response := getWithToken(s.T(), fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review", ownerID), ownerToken)
	return decodeEntryRevisionGroupsForTest(s.T(), response)
}

func (s *RequestKeysSuite) Test_should_reject_unknown_and_reserved_keys_and_write_nothing_when_entry_is_created() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "request-keys-create-entry-owner", 8851)

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"__trace": "x",
		"entry": map[string]any{
			"title":  "entry that must not be saved",
			"method": "X-ray diffraction",
			"colour": "blue",
			"crystallography": map[string]any{"crystals": []map[string]any{{
				"id": "1", "growth": map[string]any{"ph": 7, "ph_note": "buffered"},
			}}},
		},
		"model_operations": []map[string]any{{
			"op": "add",
			"data": map[string]any{
				"title":    "model that must not be saved",
				"metadata": map[string]any{"model_type": "Deposited"},
				"metrics":  []map[string]any{{"id": uuid.New(), "key": "r_free", "value": 0.2, "__unit": "%"}},
			},
		}},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Equal([]keyErrorResponse{
		{Pointer: "/__trace", Code: "RESERVED_KEY"},
		{Pointer: "/entry/colour", Code: "UNKNOWN_FIELD"},
		{Pointer: "/entry/crystallography/crystals/0/growth/ph_note", Code: "UNKNOWN_FIELD"},
		{Pointer: "/model_operations/0/data/metrics/0/__unit", Code: "RESERVED_KEY"},
	}, keyErrorsFromResponse(s.T(), errors))
	s.Empty(s.inReviewRevisionsOf(ownerToken))
}

func (s *RequestKeysSuite) Test_should_list_key_and_value_errors_together_when_request_has_both() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "request-keys-both-owner", 8852)

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{
			"title": "entry that must not be saved", "method": "X-ray diffraction", "resolution": -2, "colour": "blue",
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Equal([]keyErrorResponse{
		{Pointer: "/entry/colour", Code: "UNKNOWN_FIELD"},
		{Pointer: "/entry/resolution", Code: "OUT_OF_RANGE"},
	}, keyErrorsFromResponse(s.T(), errors))
}

func (s *RequestKeysSuite) Test_should_reject_unknown_key_and_write_nothing_when_model_is_created() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "request-keys-create-model-owner", 8853)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "request keys model entry", "method": "X-ray diffraction"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models", map[string]any{
		"model": map[string]any{
			"title":    "model that must not be saved",
			"metadata": map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{{
				"id": uuid.New(), "name": "model.cif", "level": "L0", "type": "model", "stored_uri": "s3://x",
			}},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Equal([]keyErrorResponse{
		{Pointer: "/model/artifacts/0/stored_uri", Code: "UNKNOWN_FIELD"},
	}, keyErrorsFromResponse(s.T(), errors))
	s.Empty(listEntryModelsForTest(s.T(), entry.EntryId))
}

func (s *RequestKeysSuite) Test_should_reject_top_level_and_nested_unknown_keys_with_422_when_entry_revision_is_proposed() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "request-keys-entry-revision-owner", 8854)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "request keys revision entry", "method": "X-ray diffraction"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/revisions", map[string]any{
		"entry": map[string]any{
			"title":            "changed title",
			"colour":           "blue",
			"polymer_entities": []map[string]any{{"label_entity_id": "1", "tag": "his"}},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Equal([]keyErrorResponse{
		{Pointer: "/entry/colour", Code: "UNKNOWN_FIELD"},
		{Pointer: "/entry/polymer_entities/0/tag", Code: "UNKNOWN_FIELD"},
	}, keyErrorsFromResponse(s.T(), errors))
	s.Empty(s.inReviewRevisionsOf(ownerToken))
}

func (s *RequestKeysSuite) Test_should_reject_top_level_unknown_key_with_422_when_model_revision_is_proposed() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "request-keys-model-revision-owner", 8855)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "request keys model revision entry", "method": "X-ray diffraction"},
	})
	model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model to change", "metadata": map[string]any{"model_type": "Deposited"}},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), fmt.Sprintf("/v1/entries/%s/models/%s/revisions", entry.EntryId, model.ModelId),
		map[string]any{"model": map[string]any{"title": "changed model title", "colour": "blue"}}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Equal([]keyErrorResponse{
		{Pointer: "/model/colour", Code: "UNKNOWN_FIELD"},
	}, keyErrorsFromResponse(s.T(), errors))
	s.Empty(s.inReviewRevisionsOf(ownerToken))
}

func (s *RequestKeysSuite) Test_should_save_entry_without_unknown_key_when_request_keys_are_only_logged() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "request-keys-log-only-owner", 8856)

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), requestKeysLogOnlyServer.URL, http.MethodPost, "/v1/entries",
		map[string]any{"entry": map[string]any{
			"title": "entry saved while request keys are only logged", "method": "X-ray diffraction",
			"colour": "blue", "__trace": "x",
		}}, ownerToken)

	// then
	created := decodeJSONResponse[httpapi.CreateEntryRevisionDocument](s.T(), response, http.StatusCreated).Data.Attributes
	s.NotNil(entryRevisionGroupByEntryID(s.inReviewRevisionsOf(ownerToken), created.EntryId))
}

func (s *RequestKeysSuite) Test_should_keep_400_for_top_level_unknown_key_when_request_keys_are_only_logged() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "request-keys-log-only-revision-owner", 8857)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "request keys log-only revision entry", "method": "X-ray diffraction"},
	})

	// when
	//nolint:bodyclose // assertErrorResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), requestKeysLogOnlyServer.URL, http.MethodPost,
		"/v1/entries/"+entry.EntryId+"/revisions",
		map[string]any{"entry": map[string]any{"title": "changed title", "colour": "blue"}}, ownerToken)

	// then
	s.Equal(http.StatusBadRequest, response.StatusCode)
	assertErrorResponse(s.T(), response, "BAD_REQUEST", `invalid request: revision field "colour" is not allowed`)
	s.Empty(s.inReviewRevisionsOf(ownerToken))
}
