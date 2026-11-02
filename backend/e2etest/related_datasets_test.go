package e2etest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

// RelatedDatasetsSuite covers the DOI references to datasets kept elsewhere that people add
// to an entry.
type RelatedDatasetsSuite struct {
	baseSuite
}

func TestRelatedDatasets(t *testing.T) {
	suite.Run(t, new(RelatedDatasetsSuite))
}

func sbgridDatasetForTest(doi string) map[string]any {
	return map[string]any{
		"doi": doi, "data_type": " diffraction image data ", "repository": "SBGrid Data Bank",
		"details": "Similar crystals, collected in October 2025.",
	}
}

func (s *RelatedDatasetsSuite) Test_should_store_bare_doi_when_entry_is_created_with_resolver_url() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "related-datasets-create-owner", 8711)

	// when
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"title":            "entry with related data",
			"related_datasets": []map[string]any{sbgridDatasetForTest("https://doi.org/10.15785/SBGRID/1303")},
		},
	})

	// then
	s.Equal([]httpapi.RelatedDataset{{
		Doi: "10.15785/SBGRID/1303", DataType: "diffraction image data",
		Repository: new("SBGrid Data Bank"), Details: new("Similar crystals, collected in October 2025."),
	}}, s.relatedDatasets(entry.EntryId))
}

func (s *RelatedDatasetsSuite) Test_should_keep_related_datasets_when_entry_revision_changes_only_title() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "related-datasets-title-owner", 8712)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"title":            "entry with related data",
			"related_datasets": []map[string]any{sbgridDatasetForTest("10.15785/SBGRID/1303")},
		},
	})

	// when
	next := createEntryRevisionForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"title": "retitled entry"},
	})
	activateEntryRevisionForTest(s.T(), next)

	// then
	datasets := s.relatedDatasets(entry.EntryId)
	s.Require().Len(datasets, 1)
	s.Equal("10.15785/SBGRID/1303", datasets[0].Doi)
}

func (s *RelatedDatasetsSuite) Test_should_replace_then_remove_related_datasets_when_entry_revisions_send_them() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "related-datasets-change-owner", 8713)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry gaining related data"},
	})

	// when
	added := createEntryRevisionForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"related_datasets": []map[string]any{
			sbgridDatasetForTest("10.15785/SBGRID/1303"),
			{"doi": "doi:10.5281/zenodo.1234", "data_type": "EM raw micrographs"},
		}},
	})
	activateEntryRevisionForTest(s.T(), added)
	afterAdd := s.relatedDatasets(entry.EntryId)
	emptied := createEntryRevisionForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"related_datasets": []map[string]any{}},
	})
	activateEntryRevisionForTest(s.T(), emptied)
	afterEmpty := s.relatedDatasets(entry.EntryId)
	readded := createEntryRevisionForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"related_datasets": []map[string]any{sbgridDatasetForTest("10.15785/SBGRID/1303")}},
	})
	activateEntryRevisionForTest(s.T(), readded)
	nulled := createEntryRevisionForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"related_datasets": nil},
	})
	activateEntryRevisionForTest(s.T(), nulled)
	afterNull := s.relatedDatasets(entry.EntryId)

	// then
	s.Require().Len(afterAdd, 2)
	s.Equal("10.15785/SBGRID/1303", afterAdd[0].Doi)
	s.Equal(httpapi.RelatedDataset{Doi: "10.5281/zenodo.1234", DataType: "EM raw micrographs"}, afterAdd[1])
	s.Empty(afterEmpty)
	s.Empty(afterNull)
}

func (s *RelatedDatasetsSuite) Test_should_return_422_with_pointers_when_related_datasets_are_malformed_in_log_only_mode() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "related-datasets-malformed-owner", 8714)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry refusing bad related data"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := jsonRequestToServerWithToken(
		s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries/"+entry.EntryId+"/revisions",
		map[string]any{"entry": map[string]any{"related_datasets": []map[string]any{
			{"doi": "https://zenodo.org/records/1234", "data_type": "raw data"},
			{"doi": "10.15785/SBGRID/1303", "data_type": "  "},
		}}},
		ownerToken,
	)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	pointers := make([]string, 0, len(errors))
	for _, fieldError := range errors {
		pointers = append(pointers, fieldError.Source.Pointer)
	}
	s.Equal([]string{"/entry/related_datasets/0/doi", "/entry/related_datasets/1/data_type"}, pointers)
	s.Empty(s.relatedDatasets(entry.EntryId))
}

func (s *RelatedDatasetsSuite) Test_should_return_422_when_new_entry_lists_the_same_doi_twice() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "related-datasets-duplicate-owner", 8715)

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{
			"title": "entry with a repeated dataset", "method": "X-ray crystallography",
			"related_datasets": []map[string]any{
				sbgridDatasetForTest("10.15785/SBGRID/1303"),
				sbgridDatasetForTest("https://doi.org/10.15785/sbgrid/1303"),
			},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Equal("/entry/related_datasets/1/doi", errors[0].Source.Pointer)
	s.Equal("DUPLICATE", stringValue(errors[0].Code))
}

// relatedDatasets reads the entry's related datasets as the public API returns them.
func (s *RelatedDatasetsSuite) relatedDatasets(entryID string) []httpapi.RelatedDataset {
	s.T().Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(s.T(), "/v1/entries/"+entryID, "")
	document := decodeJSONResponse[httpapi.EntryDocument](s.T(), response, http.StatusOK)
	if document.Data.Attributes.RelatedDatasets == nil {
		return nil
	}
	return *document.Data.Attributes.RelatedDatasets
}
