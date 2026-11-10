package e2etest

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
)

// ModelSearchSuite covers the search filters of GET /v1/models on a server that has them on.
// The e2e database is shared, so each test checks the records it made, not whole result lists.
type ModelSearchSuite struct {
	baseSuite
}

func TestModelSearch(t *testing.T) {
	suite.Run(t, new(ModelSearchSuite))
}

func getModelSearchPageForTest(t *testing.T, path string, token string) httpapi.ModelSearchCollectionDocument {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(t, modelSearchServer.URL, http.MethodGet, path, nil, token)
	return decodeJSONResponse[httpapi.ModelSearchCollectionDocument](t, response, http.StatusOK)
}

// walkModelSearchForTest follows links.next from firstPath as an anonymous caller and returns
// every page.
func walkModelSearchForTest(t *testing.T, firstPath string) []httpapi.ModelSearchCollectionDocument {
	t.Helper()
	return walkModelSearchWithTokenForTest(t, firstPath, "")
}

// walkModelSearchWithTokenForTest follows links.next from firstPath as the token's user and
// returns every page.
func walkModelSearchWithTokenForTest(t *testing.T, firstPath string, token string) []httpapi.ModelSearchCollectionDocument {
	t.Helper()
	pages := make([]httpapi.ModelSearchCollectionDocument, 0)
	path := firstPath
	for range 50 {
		page := getModelSearchPageForTest(t, path, token)
		pages = append(pages, page)
		if page.Links == nil {
			return pages
		}
		path = pathFromNextLinkForTest(t, page.Links.Next)
	}
	require.FailNow(t, "model search did not reach its last page")
	return nil
}

func modelSearchResultsByIDForTest(pages []httpapi.ModelSearchCollectionDocument) map[string]httpapi.ModelSearchAttributes {
	results := make(map[string]httpapi.ModelSearchAttributes)
	for _, page := range pages {
		for _, item := range page.Data {
			results[item.Id] = item.Attributes
		}
	}
	return results
}

func (s *ModelSearchSuite) createEntryWithModelsForTest(token string, entry map[string]any, modelCount int) []string {
	created := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": entry})
	modelIDs := make([]string, 0, modelCount)
	for range modelCount {
		model := createAndActivateModelForTest(s.T(), token, created.EntryId, map[string]any{
			"model": map[string]any{"title": "search model", "metadata": map[string]any{"model_type": "Deposited"}},
		})
		modelIDs = append(modelIDs, model.ModelId)
	}
	return modelIDs
}

// Example 2 of the researchers' request, without its chain part (S2): models whose entry lists
// ADP in Ligands or Cofactors, each with the entry's related raw datasets.
func (s *ModelSearchSuite) Test_should_return_models_of_entries_listing_adp_with_related_datasets_when_searched_by_ligand_or_cofactor() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-adp-owner", 8811)
	inLigands := s.createEntryWithModelsForTest(token, map[string]any{
		"title": "ADP in ligands", "ligands": []string{"ADP", "MG"}, "cofactors": []string{},
		"related_datasets": []map[string]any{
			sbgridDatasetForTest("10.15785/SBGRID/1303"),
			{
				"accession": "EMPIAR-10028", "landing_url": "https://www.ebi.ac.uk/empiar/EMPIAR-10028/",
				"data_type": "EM raw micrographs", "repository": "EMPIAR",
			},
		},
	}, 2)
	inCofactors := s.createEntryWithModelsForTest(token, map[string]any{
		"title": "ADP in cofactors", "ligands": []string{}, "cofactors": []string{"ADP"},
	}, 1)
	otherCompound := s.createEntryWithModelsForTest(token, map[string]any{
		"title": "ATP only", "ligands": []string{"ATP"}, "cofactors": []string{},
	}, 1)
	unknownLists := s.createEntryWithModelsForTest(token, map[string]any{"title": "lists unknown"}, 1)

	// when
	pages := walkModelSearchForTest(s.T(), "/v1/models?ligand_or_cofactor=ADP&limit=2")

	// then
	results := modelSearchResultsByIDForTest(pages)
	for _, modelID := range slices.Concat(inLigands, inCofactors) {
		s.Contains(results, modelID)
	}
	for _, modelID := range slices.Concat(otherCompound, unknownLists) {
		s.NotContains(results, modelID)
	}
	withDatasets := results[inLigands[0]]
	s.Require().NotNil(withDatasets.Entry)
	s.Require().NotNil(withDatasets.Entry.RelatedDatasets)
	datasets := *withDatasets.Entry.RelatedDatasets
	s.Require().Len(datasets, 2)
	s.Equal(new("10.15785/SBGRID/1303"), datasets[0].Doi)
	s.Nil(datasets[1].Doi)
	s.Equal(new("EMPIAR-10028"), datasets[1].Accession)
	s.Equal(new("https://www.ebi.ac.uk/empiar/EMPIAR-10028/"), datasets[1].LandingUrl)
	s.Equal(new(1), withDatasets.Entry.RevisionNumber)
	s.Equal(new(1), withDatasets.RevisionNumber)
	s.Require().NotNil(withDatasets.FlaggedForReview)
	s.False(*withDatasets.FlaggedForReview)
	s.Nil(withDatasets.ReviewFlags, "an anonymous caller does not get the flags")
	s.Require().NotNil(pages[0].Meta)
	s.Require().NotNil(pages[0].Meta.Total)
	s.True(pages[0].Meta.Total.Exact)
	s.GreaterOrEqual(pages[0].Meta.Total.Count, 3)
	s.Require().Greater(len(pages), 1)
	for _, page := range pages[1:] {
		if page.Meta != nil {
			s.Nil(page.Meta.Total, "only the first page counts the result")
		}
	}
	s.Equal(pages[0].Meta.Total.Count, len(results))
}

func (s *ModelSearchSuite) Test_should_keep_every_filter_in_next_link_when_search_has_more_pages() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-links-owner", 8812)
	s.createEntryWithModelsForTest(token, map[string]any{
		"title": "linked pages", "ligands": []string{"GTP"}, "cofactors": []string{},
	}, 3)

	// when
	page := getModelSearchPageForTest(s.T(), "/v1/models?ligand_or_cofactor=GTP&model_type=Deposited&limit=1", "")

	// then
	s.Require().NotNil(page.Links)
	next, err := url.Parse(page.Links.Next)
	s.Require().NoError(err)
	s.Equal("GTP", next.Query().Get("ligand_or_cofactor"))
	s.Equal("Deposited", next.Query().Get("model_type"))
	s.NotEmpty(next.Query().Get("cursor"))
}

func (s *ModelSearchSuite) Test_should_list_an_open_review_flag_to_a_reviewer_when_the_flagged_record_is_searched() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-flag-owner", 8813)
	reviewerToken := issueReviewerTokenForTest(s.T(), "model-search-flag-reviewer", 8818)
	created := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{"title": "flagged entry"}})
	model := createAndActivateModelForTest(s.T(), token, created.EntryId, map[string]any{
		"model": map[string]any{"title": "flagged model", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	s.Require().NoError(testDatabase.MigrationReviews.Open(context.Background(), models.MigrationReview{
		ObjectType: "entry", ObjectID: created.EntryId, Issue: "sync_validation_failed", Details: map[string]any{},
	}))

	// when
	pages := walkModelSearchWithTokenForTest(s.T(), "/v1/models?flagged=true&model_type=Deposited&limit=100", reviewerToken)

	// then
	results := modelSearchResultsByIDForTest(pages)
	s.Require().Contains(results, model.ModelId)
	s.Require().NotNil(results[model.ModelId].FlaggedForReview)
	s.True(*results[model.ModelId].FlaggedForReview)
	flags := results[model.ModelId].ReviewFlags
	s.Require().NotNil(flags)
	s.Require().Len(*flags, 1)
	s.Equal("entry", (*flags)[0].ObjectType)
	s.Equal(created.EntryId, (*flags)[0].ObjectId)
	s.Equal("sync_validation_failed", (*flags)[0].Issue)
}

func (s *ModelSearchSuite) Test_should_refuse_filters_and_answer_as_before_when_model_search_is_off() {
	// given
	filtered := "/v1/models?ligand_or_cofactor=ADP"

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	refused := decodeJSONResponse[httpapi.Error](s.T(), getWithToken(s.T(), filtered, ""), http.StatusBadRequest)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	unfiltered := decodeJSONResponse[httpapi.ModelSearchCollectionDocument](s.T(), getWithToken(s.T(), "/v1/models?limit=1", ""), http.StatusOK)

	// then
	s.Require().Len(refused.Errors, 1)
	s.Require().NotNil(refused.Errors[0].Detail)
	s.Contains(*refused.Errors[0].Detail, "ligand_or_cofactor")
	if unfiltered.Meta != nil {
		s.Nil(unfiltered.Meta.Total)
	}
	for _, item := range unfiltered.Data {
		s.Nil(item.Attributes.Entry)
		s.Nil(item.Attributes.FlaggedForReview)
		s.Nil(item.Attributes.ReviewFlags)
	}
}

func (s *ModelSearchSuite) Test_should_answer_400_naming_the_parameter_when_a_filter_value_is_unknown() {
	// given
	path := "/v1/models?model_type=Hologram"

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), modelSearchServer.URL, http.MethodGet, path, nil, "")
	refused := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusBadRequest)

	// then
	s.Require().Len(refused.Errors, 1)
	s.Require().NotNil(refused.Errors[0].Detail)
	s.Contains(*refused.Errors[0].Detail, "model_type")
}

func randomCompoundIDForTest() string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	code := []byte("Z")
	for range 4 {
		code = append(code, letters[rand.IntN(len(letters))])
	}
	return string(code)
}

func (s *ModelSearchSuite) Test_should_page_through_every_match_once_when_a_search_has_few_matches() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-few-owner", 8814)
	compound := randomCompoundIDForTest()
	created := s.createEntryWithModelsForTest(token, map[string]any{
		"title": "few matches", "ligands": []string{compound}, "cofactors": []string{},
	}, 3)

	// when
	pages := walkModelSearchForTest(s.T(), "/v1/models?cursor=&limit=1&ligand_or_cofactor="+compound)

	// then
	walked := make([]string, 0)
	for _, page := range pages {
		for _, item := range page.Data {
			walked = append(walked, item.Id)
		}
	}
	s.Equal(created, walked)
	s.Require().NotNil(pages[0].Meta)
	s.Require().NotNil(pages[0].Meta.Total, "an empty cursor is the first page")
	s.Equal(httpapi.ModelSearchTotal{Count: 3, Exact: true}, *pages[0].Meta.Total)
}
