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
			"entry": map[string]any{"title": fmt.Sprintf("%s entry %d", searchToken, index), "method": testEntryMethod},
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
				"method": testEntryMethod,
				"title":  fmt.Sprintf("sequence cursor entry %d", index),
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
		"entry": map[string]any{"title": "cursor models entry", "method": testEntryMethod},
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
			"entry": map[string]any{"title": fmt.Sprintf("%s entry %d", searchToken, index), "method": testEntryMethod},
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

func (s *ListPaginationSuite) Test_should_return_each_entry_artifact_once_when_following_links_next() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-artifacts-owner", 8305)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"method":    testEntryMethod,
			"title":     "cursor artifacts entry",
			"artifacts": artifactRequestsForTest(3, "L0", "txt"),
		},
	})
	path := "/v1/entries/" + entry.EntryId + "/artifacts"
	unpaged := getJSONPageForTest[httpapi.ArtifactCollectionDocument](s.T(), path, "")

	// when
	pages := walkPagesByLinksForTest(s.T(), path+"?limit=2", "", func(page httpapi.ArtifactCollectionDocument) *httpapi.CollectionLinks {
		return page.Links
	})

	// then
	s.Require().Len(pages, 2)
	s.Require().Len(unpaged.Data, 3)
	walked := make([]httpapi.ArtifactData, 0, len(unpaged.Data))
	for _, page := range pages {
		walked = append(walked, page.Data...)
	}
	s.Equal(artifactIDsForTest(unpaged.Data), artifactIDsForTest(walked))
	s.Nil(pages[1].Meta, "the last page has no next cursor")
}

func (s *ListPaginationSuite) Test_should_return_each_model_artifact_once_with_runs_on_every_page_when_following_next_cursor() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-model-artifacts-owner", 8306)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "cursor model artifacts entry", "method": testEntryMethod},
	})
	model := createAndActivateModelForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title":     "cursor model artifacts model",
			"artifacts": artifactRequestsForTest(3, "L2", "cif"),
		},
	})
	path := "/v1/entries/" + entry.EntryId + "/models/" + model.ModelId + "/artifacts"
	unpaged := getJSONPageForTest[httpapi.ModelArtifactCollectionDocument](s.T(), path, "")

	// when
	walked := make([]httpapi.ArtifactData, 0, len(unpaged.Data))
	pageCount := 0
	cursor := ""
	for range maxPagesForTest {
		page := getJSONPageForTest[httpapi.ModelArtifactCollectionDocument](
			s.T(), path+"?limit=2&cursor="+url.QueryEscape(cursor), "",
		)
		pageCount++
		walked = append(walked, page.Data...)
		s.Equal(unpaged.Meta.Runs, page.Meta.Runs, "runs cover the whole model revision on every page")
		if page.Meta.NextCursor == nil {
			s.Nil(page.Links)
			break
		}
		s.Require().NotNil(page.Links)
		cursor = *page.Meta.NextCursor
	}

	// then
	s.Equal(2, pageCount)
	s.Require().Len(unpaged.Data, 3)
	s.Equal(artifactIDsForTest(unpaged.Data), artifactIDsForTest(walked))
}

func (s *ListPaginationSuite) Test_should_return_each_revision_group_once_newest_first_when_following_links_next() {
	// given
	// The e2e database outlives a run, so a fresh author keeps earlier runs' groups out.
	authorToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-groups-author-"+uuid.NewString(), 8_000_000+rand.Int64N(1_000_000))
	authorID := tokenUserIDForTest(s.T(), authorToken)
	createdNewestFirst := make([]string, 0, 3)
	for index := range 3 {
		entry := createEntryForTest(s.T(), authorToken, map[string]any{
			"entry": map[string]any{"title": fmt.Sprintf("cursor group entry %d", index), "method": testEntryMethod},
		})
		createdNewestFirst = append([]string{entry.EntryId}, createdNewestFirst...)
	}

	// when
	authorPages := walkPagesByLinksForTest(
		s.T(),
		fmt.Sprintf("/v1/users/%s/entries/revisions?state=in_review&limit=2", authorID),
		authorToken,
		func(page httpapi.EntryRevisionGroupCollectionDocument) *httpapi.CollectionLinks { return page.Links },
	)
	reviewerEntryIDs := walkReviewerGroupsUntilSeenForTest(s.T(), createdNewestFirst)

	// then
	s.Require().Len(authorPages, 2)
	s.Equal(createdNewestFirst, revisionGroupEntryIDsForTest(authorPages))
	s.Len(uniqueStringsForTest(reviewerEntryIDs), len(reviewerEntryIDs), "no group repeats across pages")
	s.Equal(createdNewestFirst, filterStringsForTest(reviewerEntryIDs, createdNewestFirst))
}

func (s *ListPaginationSuite) Test_should_return_each_entry_revision_once_newest_first_when_following_links_next() {
	// given
	authorToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-entry-revisions-author", 8308)
	authorID := tokenUserIDForTest(s.T(), authorToken)
	entry := createAndActivateEntryForTest(s.T(), authorToken, map[string]any{
		"entry": map[string]any{"title": "cursor entry revisions", "method": testEntryMethod},
	})
	createdNewestFirst := []uuid.UUID{entry.RevisionId}
	for index := range 2 {
		revision := createEntryRevisionForTest(s.T(), authorToken, entry.EntryId, map[string]any{
			"entry": map[string]any{"title": fmt.Sprintf("cursor entry revision %d", index)},
		})
		createdNewestFirst = append([]uuid.UUID{revision.RevisionId}, createdNewestFirst...)
	}

	// when
	pages := walkPagesByLinksForTest(
		s.T(),
		fmt.Sprintf("/v1/users/%s/entries/%s/revisions?limit=2", authorID, entry.EntryId),
		authorToken,
		func(page httpapi.EntryRevisionSummaryCollectionDocument) *httpapi.CollectionLinks { return page.Links },
	)

	// then
	s.Require().Len(pages, 2)
	walked := make([]uuid.UUID, 0, len(createdNewestFirst))
	for _, page := range pages {
		for _, item := range page.Data {
			walked = append(walked, item.Id)
		}
	}
	s.Equal(createdNewestFirst, walked)
}

func (s *ListPaginationSuite) Test_should_return_each_model_revision_once_oldest_first_when_following_links_next() {
	// given
	authorToken := issueEntryTokenForGitHubIDForTest(s.T(), "cursor-model-revisions-author", 8309)
	entry := createAndActivateEntryForTest(s.T(), authorToken, map[string]any{
		"entry": map[string]any{"title": "cursor model revisions entry", "method": testEntryMethod},
	})
	model := createAndActivateModelForTest(s.T(), authorToken, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "cursor model revisions model"},
	})
	createdOldestFirst := []uuid.UUID{model.RevisionId}
	for index := range 2 {
		revision := createModelRevisionForTest(s.T(), authorToken, entry.EntryId, model.ModelId, map[string]any{
			"model": map[string]any{"title": fmt.Sprintf("cursor model revision %d", index)},
		})
		createdOldestFirst = append(createdOldestFirst, revision.RevisionId)
	}

	// when
	pages := walkPagesByLinksForTest(
		s.T(),
		fmt.Sprintf("/v1/entries/%s/models/%s/revisions?limit=2", entry.EntryId, model.ModelId),
		adminToken,
		func(page httpapi.ModelRevisionSummaryCollectionDocument) *httpapi.CollectionLinks { return page.Links },
	)

	// then
	s.Require().Len(pages, 2)
	walked := make([]uuid.UUID, 0, len(createdOldestFirst))
	for _, page := range pages {
		for _, item := range page.Data {
			walked = append(walked, item.Id)
		}
	}
	s.Equal(createdOldestFirst, walked)
}

func (s *ListPaginationSuite) Test_should_reject_cursor_from_another_list_on_revision_and_artifact_lists() {
	modelsPage := getModelPageForTest(s.T(), "/v1/models?limit=1")
	s.Require().NotNil(modelsPage.Meta, "the e2e database holds more than one model")
	modelsCursor := url.QueryEscape(modelsPage.Meta.NextCursor)
	entry := createAndActivateEntryForTest(
		s.T(),
		issueEntryTokenForGitHubIDForTest(s.T(), "cursor-reject-owner", 8310),
		map[string]any{"entry": map[string]any{"title": "cursor reject entry", "method": testEntryMethod}},
	)
	cases := map[string]string{
		"revision groups":      "/v1/entries/revisions?state=in_review&cursor=" + modelsCursor,
		"entry revisions":      "/v1/entries/" + entry.EntryId + "/revisions?cursor=" + modelsCursor,
		"entry artifacts":      "/v1/entries/" + entry.EntryId + "/artifacts?cursor=" + modelsCursor,
		"similar entries":      "/v1/entries/" + entry.EntryId + "/similar-entries?cursor=" + modelsCursor,
		"artifacts and offset": "/v1/entries/" + entry.EntryId + "/artifacts?offset=0&cursor=" + modelsCursor,
	}
	for name, path := range cases {
		s.Run(name, func() {
			// given / when
			//nolint:bodyclose // assertErrorResponse closes the response body.
			response := getWithToken(s.T(), path, adminToken)

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

// walkPagesByLinksForTest follows links.next from firstPath and returns every page.
func walkPagesByLinksForTest[T any](
	t *testing.T,
	firstPath string,
	token string,
	nextLinks func(page T) *httpapi.CollectionLinks,
) []T {
	t.Helper()
	pages := make([]T, 0)
	path := firstPath
	for range maxPagesForTest {
		page := getJSONPageForTest[T](t, path, token)
		pages = append(pages, page)
		links := nextLinks(page)
		if links == nil {
			return pages
		}
		path = pathFromNextLinkForTest(t, links.Next)
	}
	require.FailNow(t, "pagination did not reach its last page", "after %d pages", maxPagesForTest)
	return nil
}

func getJSONPageForTest[T any](t *testing.T, path string, token string) T {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, path, token)
	return decodeJSONResponse[T](t, response, http.StatusOK)
}

// walkReviewerGroupsUntilSeenForTest pages the reviewer's in-review groups two at a time
// until every wanted entry has appeared. It stops early because the shared e2e database
// holds every earlier run's groups too.
func walkReviewerGroupsUntilSeenForTest(t *testing.T, wanted []string) []string {
	t.Helper()
	seen := make([]string, 0)
	path := "/v1/entries/revisions?state=in_review&limit=2"
	for range maxPagesForTest {
		page := getJSONPageForTest[httpapi.EntryRevisionGroupCollectionDocument](t, path, adminToken)
		seen = append(seen, revisionGroupEntryIDsForTest([]httpapi.EntryRevisionGroupCollectionDocument{page})...)
		if len(filterStringsForTest(seen, wanted)) >= len(wanted) || page.Links == nil {
			return seen
		}
		path = pathFromNextLinkForTest(t, page.Links.Next)
	}
	require.FailNow(t, "reviewer groups did not include every wanted entry", "after %d pages", maxPagesForTest)
	return nil
}

func artifactRequestsForTest(count int, level string, format string) []map[string]any {
	requests := make([]map[string]any, 0, count)
	for index := range count {
		name := fmt.Sprintf("cursor artifact %d", index)
		requests = append(requests, artifactRequest(uuid.New(), name, level, format, "s3://cursor/"+uuid.NewString(), nil))
	}
	return requests
}

func artifactIDsForTest(data []httpapi.ArtifactData) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(data))
	for _, item := range data {
		ids = append(ids, item.Id)
	}
	return ids
}

func revisionGroupEntryIDsForTest(pages []httpapi.EntryRevisionGroupCollectionDocument) []string {
	ids := make([]string, 0)
	for _, page := range pages {
		for _, item := range page.Data {
			ids = append(ids, item.Id)
		}
	}
	return ids
}

func uniqueStringsForTest(values []string) map[string]struct{} {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}
	return unique
}

// filterStringsForTest keeps the values that are in wanted, in their original order.
func filterStringsForTest(values []string, wanted []string) []string {
	wantedSet := uniqueStringsForTest(wanted)
	filtered := make([]string, 0, len(wanted))
	for _, value := range values {
		if _, ok := wantedSet[value]; ok {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
