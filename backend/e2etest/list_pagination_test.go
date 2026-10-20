package e2etest

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

// maxPagesForTest stops a pagination walk that never reaches its last page.
const maxPagesForTest = 10

type ListPaginationSuite struct {
	baseSuite
}

func TestListPagination(t *testing.T) {
	suite.Run(t, new(ListPaginationSuite))
}

func (s *ListPaginationSuite) Test_should_return_each_matching_entry_once_when_following_links_next() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-entries-owner", 8301)
	searchToken := "cursorwalk" + strings.ReplaceAll(uuid.NewString(), "-", "")
	createdNewestFirst := make([]string, 0, 5)
	for index := range 5 {
		entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
			"entry": map[string]any{"title": fmt.Sprintf("%s entry %d", searchToken, index)},
		})
		createdNewestFirst = append([]string{entry.EntryId}, createdNewestFirst...)
	}

	// when
	pages := walkEntryPagesByLinksForTest(s.T(), "/v1/entries?limit=2&query="+searchToken)

	// then
	s.Require().Len(pages, 3)
	s.Equal(createdNewestFirst, entryIDsFromPagesForTest(pages))
	for _, page := range pages[:2] {
		s.Require().NotNil(page.Links)
		s.Require().NotNil(page.Meta)
		s.True(strings.HasPrefix(page.Links.Next, testPublicBaseURL+"/v1/entries?"))
	}
	s.Nil(pages[2].Links, "the last page has no next link")
	s.Nil(pages[2].Meta, "the last page has no next cursor")
}

func (s *ListPaginationSuite) Test_should_keep_matching_protein_sequences_when_paging_a_sequence_search() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-sequence-owner", 8302)
	sequence := randomProteinSequenceForTest(16)
	created := make([]string, 0, 3)
	for index := range 3 {
		entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
			"entry": map[string]any{
				"title": fmt.Sprintf("sequence cursor entry %d", index),
				"artifacts": []map[string]any{
					artifactRequest(uuid.New(), "entry FASTA", "L0", "fasta", "s3://entry/sequence.fasta", map[string]any{
						"records": []map[string]any{{"header": "entry", "sequence": "MK" + sequence + "GG"}},
					}),
				},
			},
		})
		created = append(created, entry.EntryId)
	}

	// when
	pages := walkEntryPagesByLinksForTest(s.T(), "/v1/entries?limit=2&query="+sequence)

	// then
	s.Require().Len(pages, 2)
	s.ElementsMatch(created, entryIDsFromPagesForTest(pages))
}

func (s *ListPaginationSuite) Test_should_return_each_entry_model_once_in_order_when_following_next_cursor() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-models-owner", 8303)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "cursor models entry"},
	})
	createdOldestFirst := make([]string, 0, 5)
	for index := range 5 {
		model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
			"model": map[string]any{"title": fmt.Sprintf("cursor model %d", index)},
		})
		createdOldestFirst = append(createdOldestFirst, model.ModelId)
	}

	// when
	walked := make([]string, 0, len(createdOldestFirst))
	path := "/v1/entries/" + entry.EntryId + "/models?limit=2"
	cursor := ""
	for range maxPagesForTest {
		page := getModelPageForTest(s.T(), path+"&cursor="+url.QueryEscape(cursor))
		for _, item := range page.Data {
			walked = append(walked, item.Id)
		}
		if page.Meta == nil {
			break
		}
		cursor = page.Meta.NextCursor
	}

	// then
	s.Equal(createdOldestFirst, walked)
}

func (s *ListPaginationSuite) Test_should_continue_offset_request_with_next_cursor() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-offset-owner", 8304)
	searchToken := "cursoroffset" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for index := range 3 {
		createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
			"entry": map[string]any{"title": fmt.Sprintf("%s entry %d", searchToken, index)},
		})
	}

	// when
	offsetPage := getEntryPageForTest(s.T(), "/v1/entries?limit=1&offset=1&query="+searchToken)
	s.Require().NotNil(offsetPage.Links)
	nextPage := getEntryPageForTest(s.T(), pathFromNextLinkForTest(s.T(), offsetPage.Links.Next))

	// then
	s.NotContains(offsetPage.Links.Next, "offset=")
	s.Require().Len(offsetPage.Data, 1)
	s.Require().Len(nextPage.Data, 1)
	s.NotEqual(offsetPage.Data[0].Id, nextPage.Data[0].Id)
	s.Nil(nextPage.Links)
}

func (s *ListPaginationSuite) Test_should_reject_invalid_cursor_requests() {
	modelsPage := getModelPageForTest(s.T(), "/v1/models?limit=1")
	s.Require().NotNil(modelsPage.Meta, "the e2e database holds more than one model")
	cases := map[string]string{
		"malformed cursor":         "/v1/entries?cursor=not-a-cursor",
		"cursor with offset":       "/v1/models?offset=0&cursor=" + url.QueryEscape(modelsPage.Meta.NextCursor),
		"cursor from another list": "/v1/entries?cursor=" + url.QueryEscape(modelsPage.Meta.NextCursor),
	}
	for name, path := range cases {
		s.Run(name, func() {
			// given / when
			//nolint:bodyclose // assertErrorResponse closes the response body.
			response := getWithToken(s.T(), path, "")

			// then
			s.Equal(http.StatusBadRequest, response.StatusCode)
			assertErrorResponse(s.T(), response, "BAD_REQUEST", "invalid cursor")
		})
	}
}

func walkEntryPagesByLinksForTest(t *testing.T, firstPath string) []httpapi.EntryCollectionDocument {
	t.Helper()
	pages := make([]httpapi.EntryCollectionDocument, 0)
	path := firstPath
	for range maxPagesForTest {
		page := getEntryPageForTest(t, path)
		pages = append(pages, page)
		if page.Links == nil {
			return pages
		}
		path = pathFromNextLinkForTest(t, page.Links.Next)
	}
	require.FailNow(t, "pagination did not reach its last page", "after %d pages", maxPagesForTest)
	return nil
}

// pathFromNextLinkForTest maps a public links.next URL onto the test server, which is
// reached without the ingress /api prefix.
func pathFromNextLinkForTest(t *testing.T, next string) string {
	t.Helper()
	require.True(t, strings.HasPrefix(next, testPublicBaseURL+"/"), "next link %q", next)
	return strings.TrimPrefix(next, testPublicBaseURL)
}

func getEntryPageForTest(t *testing.T, path string) httpapi.EntryCollectionDocument {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, path, "")
	return decodeJSONResponse[httpapi.EntryCollectionDocument](t, response, http.StatusOK)
}

func getModelPageForTest(t *testing.T, path string) httpapi.ModelCollectionDocument {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, path, "")
	return decodeJSONResponse[httpapi.ModelCollectionDocument](t, response, http.StatusOK)
}

func entryIDsFromPagesForTest(pages []httpapi.EntryCollectionDocument) []string {
	ids := make([]string, 0)
	for _, page := range pages {
		for _, item := range page.Data {
			ids = append(ids, item.Id)
		}
	}
	return ids
}

// randomProteinSequenceForTest returns letters a search falls back to sequence matching
// for, and which no other entry in the shared test database contains.
func randomProteinSequenceForTest(length int) string {
	const aminoAcids = "ACDEFGHIKLMNPQRSTVWY"
	var builder strings.Builder
	for range length {
		builder.WriteByte(aminoAcids[rand.IntN(len(aminoAcids))])
	}
	return builder.String()
}
