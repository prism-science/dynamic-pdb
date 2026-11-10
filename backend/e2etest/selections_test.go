package e2etest

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

// SelectionsSuite covers frozen selections on a server that has them on. The e2e database is
// shared, so each test selects the records it made by ID or by a search only they match.
type SelectionsSuite struct {
	baseSuite
}

func TestSelections(t *testing.T) {
	suite.Run(t, new(SelectionsSuite))
}

// saveSelectionForTest saves a selection and checks its Location header.
func saveSelectionForTest(t *testing.T, body any) httpapi.SelectionDocument {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(t, selectionsServer.URL, http.MethodPost, "/v1/selections", body, "")
	location := response.Header.Get("Location")
	document := decodeJSONResponse[httpapi.SelectionDocument](t, response, http.StatusCreated)
	require.Equal(t, testPublicBaseURL+"/v1/selections/"+document.Data.Id, location)
	return document
}

// selectionModelsForTest follows the selection's model pages and returns every frozen model.
func selectionModelsForTest(t *testing.T, selectionID string, limit int) ([]httpapi.SelectionModelAttributes, int) {
	t.Helper()
	models := make([]httpapi.SelectionModelAttributes, 0)
	path := "/v1/selections/" + selectionID + "/models?limit=" + strconv.Itoa(limit)
	for pages := 1; pages <= 50; pages++ {
		//nolint:bodyclose // decodeJSONResponse closes the response body.
		response := jsonRequestToServerWithToken(t, selectionsServer.URL, http.MethodGet, path, nil, "")
		page := decodeJSONResponse[httpapi.SelectionModelCollectionDocument](t, response, http.StatusOK)
		for _, item := range page.Data {
			require.Equal(t, item.Attributes.ModelId, item.Id)
			models = append(models, item.Attributes)
		}
		if page.Links == nil {
			return models, pages
		}
		path = pathFromNextLinkForTest(t, page.Links.Next)
	}
	require.FailNow(t, "selection models did not reach their last page")
	return nil, 0
}

func (s *SelectionsSuite) Test_should_keep_the_frozen_revisions_when_a_model_and_its_entry_get_new_revisions_after_a_selection_is_saved() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "selection-revisions-owner", 8911)
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{"title": "frozen entry"}})
	revised := createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model to revise", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	unchanged := createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model left as it is", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	saved := saveSelectionForTest(s.T(), map[string]any{"model_ids": []string{unchanged.ModelId, revised.ModelId}})

	// when the model and its entry get new published revisions
	createAndActivateModelRevisionForTest(s.T(), token, entry.EntryId, revised.ModelId, map[string]any{
		"model": map[string]any{"title": "revised model"},
	})
	activateEntryRevisionForTest(s.T(), createEntryRevisionForTest(s.T(), token, entry.EntryId, map[string]any{
		"entry": map[string]any{"title": "revised entry"},
	}))

	// then the saved selection keeps the revisions it froze
	s.Equal(2, saved.Data.Attributes.ModelCount)
	s.Equal(1, saved.Data.Attributes.EntryCount)
	frozen, _ := selectionModelsForTest(s.T(), saved.Data.Id, 100)
	noFiles := []httpapi.SelectionFile{}
	s.Equal([]httpapi.SelectionModelAttributes{
		{ModelId: revised.ModelId, EntryId: entry.EntryId, ModelRevisionNumber: 1, EntryRevisionNumber: 1, Files: noFiles},
		{ModelId: unchanged.ModelId, EntryId: entry.EntryId, ModelRevisionNumber: 1, EntryRevisionNumber: 1, Files: noFiles},
	}, frozen)
	// and a new selection freezes the new revisions
	resaved := saveSelectionForTest(s.T(), map[string]any{"model_ids": []string{revised.ModelId}})
	refrozen, _ := selectionModelsForTest(s.T(), resaved.Data.Id, 100)
	s.Equal([]httpapi.SelectionModelAttributes{
		{ModelId: revised.ModelId, EntryId: entry.EntryId, ModelRevisionNumber: 2, EntryRevisionNumber: 2, Files: noFiles},
	}, refrozen)
}

func (s *SelectionsSuite) Test_should_keep_its_files_and_their_sha256_when_a_model_gets_a_new_file_after_a_selection_is_saved() {
	// given a published model with a declared-checksum coordinate file and structure factors
	// without one, in a published entry with a sequence file
	token := issueEntryTokenForGitHubIDForTest(s.T(), "selection-files-owner", 8912)
	originalID, structureFactorsID, fastaID := uuid.New(), uuid.New(), uuid.New()
	originalSHA256 := strings.Repeat("a", 64)
	original := artifactRequest(originalID, "original.cif", "L2", "mmcif", "https://files.example/original.cif", nil)
	original["type"], original["sha256"] = "model", originalSHA256
	structureFactors := artifactRequest(structureFactorsID, "sf.cif", "L1", "cif", "https://files.example/sf.cif", nil)
	structureFactors["type"] = "structure_factors"
	fasta := artifactRequest(fastaID, "entry.fasta", "L0", "fasta", "https://files.example/entry.fasta", nil)
	fasta["type"] = "fasta"
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{
		"entry": map[string]any{"title": "pinned files entry", "artifacts": []map[string]any{fasta}},
	})
	model := createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title": "pinned files model", "primary_artifact_id": originalID,
			"metadata":  map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{original, structureFactors},
		},
	})
	saved := saveSelectionForTest(s.T(), map[string]any{"model_ids": []string{model.ModelId}})
	s.Equal(saved.Data.Attributes.CreatedAt, saved.Data.Attributes.CatalogDate)

	// when the model gets a new published revision with a new coordinate file
	replacementID := uuid.New()
	replacement := artifactRequest(replacementID, "revised.cif", "L2", "mmcif", "https://files.example/revised.cif", nil)
	replacement["type"], replacement["sha256"] = "model", strings.Repeat("b", 64)
	createAndActivateModelRevisionForTest(s.T(), token, entry.EntryId, model.ModelId, map[string]any{
		"model": map[string]any{
			"title": "revised model", "primary_artifact_id": replacementID,
			"artifacts": []map[string]any{replacement},
		},
	})

	// then the saved selection still names the original files, with the SHA-256 it recorded
	frozen, _ := selectionModelsForTest(s.T(), saved.Data.Id, 100)
	s.Require().Len(frozen, 1)
	s.Equal(1, frozen[0].ModelRevisionNumber)
	files := frozen[0].Files
	s.Require().Len(files, 3)
	s.Equal(originalID, files[0].Id)
	s.Equal([]httpapi.SelectionFileRole{httpapi.SelectionFileRolePrimary}, files[0].Roles)
	s.Equal(&originalSHA256, files[0].RecordedSha256)
	s.Equal(httpapi.SelectionSHA256SourceDeclared, files[0].RecordedSha256Source)
	s.Equal(structureFactorsID, files[1].Id)
	s.Equal([]httpapi.SelectionFileRole{httpapi.SelectionFileRoleModel}, files[1].Roles)
	s.Nil(files[1].RecordedSha256, "no checksum was declared and the checksum job has not read the file")
	s.Equal(httpapi.SelectionSHA256SourceNotRecorded, files[1].RecordedSha256Source)
	s.Equal(httpapi.ArtifactChecksumStatusPending, files[1].Checksum.Status)
	s.Equal(fastaID, files[2].Id)
	s.Equal([]httpapi.SelectionFileRole{httpapi.SelectionFileRoleEntry}, files[2].Roles)
	// and a new selection names the new file
	resaved := saveSelectionForTest(s.T(), map[string]any{"model_ids": []string{model.ModelId}})
	refrozen, _ := selectionModelsForTest(s.T(), resaved.Data.Id, 100)
	s.Require().Len(refrozen, 1)
	s.Require().NotEmpty(refrozen[0].Files)
	s.Equal(replacementID, refrozen[0].Files[0].Id)
	s.Equal(2, refrozen[0].ModelRevisionNumber)
}

func (s *SelectionsSuite) Test_should_save_every_result_of_a_search_minus_excluded_models_when_a_selection_holds_a_search() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "selection-search-owner", 8912)
	ligand := randomCompoundIDForTest()
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{
		"title": "selection search entry", "ligands": []string{ligand}, "cofactors": []string{},
	}})
	modelIDs := make([]string, 0, 3)
	for range 3 {
		model := createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{
			"model": map[string]any{"title": "selection search model", "metadata": map[string]any{"model_type": "Deposited"}},
		})
		modelIDs = append(modelIDs, model.ModelId)
	}
	search := "?model_type=Deposited&ligand_or_cofactor=" + ligand

	// when
	saved := saveSelectionForTest(s.T(), map[string]any{
		"searches": []string{search}, "exclude_model_ids": []string{modelIDs[1]},
	})

	// then
	s.Equal(2, saved.Data.Attributes.ModelCount)
	s.Equal(httpapi.SelectionRequest{
		ModelIds:        []string{},
		Searches:        []string{"ligand_or_cofactor=" + ligand + "&model_type=Deposited"},
		ExcludeModelIds: []string{modelIDs[1]},
	}, saved.Data.Attributes.Request)
	frozen, pages := selectionModelsForTest(s.T(), saved.Data.Id, 1)
	s.Equal(2, pages)
	s.Require().Len(frozen, 2)
	s.Equal(modelIDs[0], frozen[0].ModelId)
	s.Equal(modelIDs[2], frozen[1].ModelId)
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	read := decodeJSONResponse[httpapi.SelectionDocument](s.T(), jsonRequestToServerWithToken(
		s.T(), selectionsServer.URL, http.MethodGet, "/v1/selections/"+saved.Data.Id, nil, ""), http.StatusOK)
	s.Equal(saved.Data.Attributes.Request, read.Data.Attributes.Request)
	s.Equal(saved.Data.Links, read.Data.Links)
	s.True(saved.Data.Attributes.CreatedAt.Equal(read.Data.Attributes.CreatedAt))
}

func (s *SelectionsSuite) Test_should_answer_422_pointing_at_the_model_id_when_a_selection_names_a_model_that_is_not_published() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "selection-unknown-owner", 8913)
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{"title": "selection unknown entry"}})
	published := createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "published model", "metadata": map[string]any{"model_type": "Deposited"}},
	})
	unpublished := createModelForTest(s.T(), token, entry.EntryId, map[string]any{
		"model": map[string]any{"title": "model in review", "metadata": map[string]any{"model_type": "Deposited"}},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodPost, "/v1/selections",
		map[string]any{"model_ids": []string{published.ModelId, unpublished.ModelId}}, "")

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Require().Len(errors, 1)
	s.Require().NotNil(errors[0].Source)
	s.Equal("/model_ids/1", errors[0].Source.Pointer)
}

func (s *SelectionsSuite) Test_should_answer_404_when_a_selection_is_unknown_or_selections_are_off() {
	// given
	unknownPath := "/v1/selections/" + uuid.NewString()

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	unknown := jsonRequestToServerWithToken(s.T(), selectionsServer.URL, http.MethodGet, unknownPath, nil, "")
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	off := jsonRequestToServerWithToken(s.T(), testServer.URL, http.MethodPost, "/v1/selections",
		map[string]any{"searches": []string{""}}, "")

	// then
	decodeErrorResponseForTest(s.T(), unknown, http.StatusNotFound)
	offErrors := decodeErrorResponseForTest(s.T(), off, http.StatusNotFound)
	s.Require().Len(offErrors, 1)
	s.Require().NotNil(offErrors[0].Code)
	s.Equal("SELECTIONS_OFF", *offErrors[0].Code)
}
