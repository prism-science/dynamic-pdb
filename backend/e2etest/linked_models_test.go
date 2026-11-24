package e2etest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/httpapi"
)

func coordinateFileForTest(pdbID string) string {
	return "https://files.rcsb.org/download/" + pdbID + ".cif"
}

// archiveEnsembleForTest is a model request for a re-refinement deposited as an ensemble.
func archiveEnsembleForTest(pdbID, originalPDBID, fileURI string) map[string]any {
	artifact := artifactRequest(uuid.New(), "model", "L2", "cif", fileURI, nil)
	artifact["type"] = "model"
	return map[string]any{
		"model": map[string]any{
			"title": "6-conformation model",
			"metadata": map[string]any{
				"model_type":    "Ensemble",
				"purpose":       "Refinement",
				"origin":        "archive",
				"member_count":  6,
				"external_refs": map[string]string{"pdb": pdbID},
				"related_entries": []map[string]any{
					{"db_name": "PDB", "db_id": originalPDBID, "content_type": "re-refinement"},
				},
			},
			"artifacts": []map[string]any{artifact},
		},
	}
}

func (s *SchemaValidationSuite) Test_should_keep_ensemble_type_and_need_no_run_when_model_is_an_archive_deposition() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "linked-model-owner", 9101)
	originalPDBID, reRefinementPDBID := unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T())
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{
		"entry": map[string]any{"title": "original deposition", "external_refs": map[string]string{"pdb": originalPDBID}},
	})

	// when
	model := createAndActivateModelForTest(s.T(), token, entry.EntryId,
		archiveEnsembleForTest(reRefinementPDBID, originalPDBID, coordinateFileForTest(reRefinementPDBID)))

	// then
	stored := getModelForTest(s.T(), entry.EntryId, model.ModelId)
	s.Equal("Ensemble", stored.Metadata["model_type"])
	s.Equal("archive", stored.Metadata["origin"])
	s.Equal(float64(6), stored.Metadata["member_count"])
	s.Equal([]any{map[string]any{"db_name": "PDB", "db_id": originalPDBID, "content_type": "re-refinement"}},
		stored.Metadata["related_entries"])
}

func (s *SchemaValidationSuite) Test_should_refuse_archive_origin_when_model_does_not_link_rcsb_coordinate_file() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "linked-model-own-file-owner", 9102)
	originalPDBID, reRefinementPDBID := unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T())
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{
		"entry": map[string]any{"title": "original deposition", "external_refs": map[string]string{"pdb": originalPDBID}},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/models",
		archiveEnsembleForTest(reRefinementPDBID, originalPDBID, "s3://model/own.cif"), token)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Equal("/model/metadata/origin", errors[0].Source.Pointer)
}

func (s *SchemaValidationSuite) Test_should_find_the_entry_by_the_pdb_id_of_its_model() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "linked-model-find-owner", 9103)
	originalPDBID, reRefinementPDBID := unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T())
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{
		"entry": map[string]any{"title": "original deposition", "external_refs": map[string]string{"pdb": originalPDBID}},
	})
	createAndActivateModelForTest(s.T(), token, entry.EntryId,
		archiveEnsembleForTest(reRefinementPDBID, originalPDBID, coordinateFileForTest(reRefinementPDBID)))

	// when
	byModelPDBID := listEntryIDsForTest(s.T(), "pdb_id="+reRefinementPDBID)
	byEntryPDBID := listEntryIDsForTest(s.T(), "pdb_id="+originalPDBID)
	bySearch := listEntryIDsForTest(s.T(), "query="+reRefinementPDBID)

	// then
	s.Equal([]string{entry.EntryId}, byModelPDBID)
	s.Equal([]string{entry.EntryId}, byEntryPDBID)
	s.Equal([]string{entry.EntryId}, bySearch)
}

func (s *SchemaValidationSuite) Test_should_name_the_parent_entry_when_a_new_entry_takes_the_pdb_id_of_a_model() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "linked-model-duplicate-owner", 9104)
	originalPDBID, reRefinementPDBID := unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T())
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{
		"entry": map[string]any{"title": "original deposition", "external_refs": map[string]string{"pdb": originalPDBID}},
	})
	model := createAndActivateModelForTest(s.T(), token, entry.EntryId,
		archiveEnsembleForTest(reRefinementPDBID, originalPDBID, coordinateFileForTest(reRefinementPDBID)))

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{
			"title":         "second record of the re-refinement",
			"method":        "X-ray crystallography",
			"external_refs": map[string]string{"pdb": reRefinementPDBID},
		},
	}, token)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusConflict)
	s.Contains(stringValue(errors[0].Detail), entry.EntryId)
	s.Contains(stringValue(errors[0].Detail), model.ModelId)
}

func (s *SchemaValidationSuite) Test_should_name_the_entry_when_a_model_takes_a_pdb_id_another_entry_holds() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "linked-model-other-entry-owner", 9105)
	heldPDBID := unusedPDBIDForTest(s.T())
	holder := createAndActivateEntryForTest(s.T(), token, map[string]any{
		"entry": map[string]any{"title": "holder", "external_refs": map[string]string{"pdb": heldPDBID}},
	})
	other := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{"title": "other"}})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries/"+other.EntryId+"/models",
		archiveEnsembleForTest(heldPDBID, heldPDBID, coordinateFileForTest(heldPDBID)), token)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusConflict)
	s.Equal("/model/metadata/external_refs/pdb", errors[0].Source.Pointer)
	s.Contains(stringValue(errors[0].Detail), holder.EntryId)
}

// matchedModelsForTest lists GET /v1/entries for query and returns each result's matched_models
// (nil when the response has none), by entry ID.
func matchedModelsForTest(t *testing.T, query string) map[string]*[]httpapi.MatchedModel {
	t.Helper()
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := getWithToken(t, "/v1/entries?"+query, "")
	document := decodeJSONResponse[httpapi.EntryCollectionDocument](t, response, http.StatusOK)
	matched := make(map[string]*[]httpapi.MatchedModel, len(document.Data))
	for _, resource := range document.Data {
		matched[resource.Id] = resource.Attributes.MatchedModels
	}
	return matched
}

// Researcher question 4: searching any PDB ID of an entry that holds several depositions returns
// that entry with the model of that PDB ID selected, on every search path. Nothing is merged.
func (s *SchemaValidationSuite) Test_should_select_the_model_of_the_pdb_id_when_an_entry_with_several_depositions_is_searched() {
	// given: one entry with a computed Single Conformer that names a PDB ID (made first), its
	// Deposited model, a re-refinement deposited as an Ensemble, and computed models without one.
	token := issueEntryTokenForGitHubIDForTest(s.T(), "linked-model-select-owner", 9106)
	originalPDBID, reRefinementPDBID, singleConformerPDBID :=
		unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T()), unusedPDBIDForTest(s.T())
	entryFileID := uuid.New()
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{
		"title": "several depositions", "external_refs": map[string]string{"pdb": originalPDBID},
		"artifacts": []map[string]any{
			artifactRequest(entryFileID, "data", "L0", "structure_factors_cif", "s3://entry/linked-select-sf.cif", nil),
		},
	}})
	computedModel := func(modelType string, externalRefs map[string]string) string {
		modelFileID := uuid.New()
		metadata := map[string]any{"model_type": modelType}
		if externalRefs != nil {
			metadata["external_refs"] = externalRefs
		}
		return createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{"model": map[string]any{
			"title": modelType + " model", "metadata": metadata,
			"artifacts": []map[string]any{artifactRequest(modelFileID, "model", "L2", "cif", "s3://model/linked-select.cif", nil)},
			"runs":      []map[string]any{refinementRunForTest(uuid.New(), entryFileID, modelFileID)},
		}}).ModelId
	}
	singleConformer := computedModel("Single Conformer", map[string]string{"pdb": singleConformerPDBID})
	deposited := createAndActivateModelForTest(s.T(), token, entry.EntryId, map[string]any{"model": map[string]any{
		"title": "deposited model", "metadata": map[string]any{
			"model_type": "Deposited", "external_refs": map[string]string{"pdb": originalPDBID}},
	}}).ModelId
	rerefined := createAndActivateModelForTest(s.T(), token, entry.EntryId,
		archiveEnsembleForTest(reRefinementPDBID, originalPDBID, coordinateFileForTest(reRefinementPDBID))).ModelId
	computedModel("Multiconformer", nil)
	computedModel("Ensemble", nil)
	other := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{"title": "unrelated entry"}})

	// when
	byReRefinement := matchedModelsForTest(s.T(), "pdb_id="+reRefinementPDBID)
	byQuery := matchedModelsForTest(s.T(), "query="+strings.ToLower(reRefinementPDBID))
	byEntryPDBID := matchedModelsForTest(s.T(), "pdb_id="+originalPDBID)
	bySeveral := matchedModelsForTest(s.T(), "pdb_id="+singleConformerPDBID+"&pdb_id="+originalPDBID)
	byText := matchedModelsForTest(s.T(), "query=several+depositions")
	unsearched := matchedModelsForTest(s.T(), "limit=100")
	modelSearch := getModelSearchPageForTest(s.T(), "/v1/models?pdb_id="+strings.ToLower(reRefinementPDBID), "")

	// then
	s.Equal(map[string]*[]httpapi.MatchedModel{
		entry.EntryId: {{ModelId: rerefined, PdbId: reRefinementPDBID}},
	}, byReRefinement)
	s.Equal(byReRefinement, byQuery)
	s.Equal(map[string]*[]httpapi.MatchedModel{
		entry.EntryId: {{ModelId: deposited, PdbId: originalPDBID}},
	}, byEntryPDBID)
	s.Equal(map[string]*[]httpapi.MatchedModel{entry.EntryId: {
		{ModelId: deposited, PdbId: originalPDBID},
		{ModelId: singleConformer, PdbId: singleConformerPDBID},
	}}, bySeveral, "archive models first, then in list order")
	s.Require().Contains(byText, entry.EntryId)
	s.Equal(&[]httpapi.MatchedModel{}, byText[entry.EntryId], "text that is no PDB ID selects nothing")
	for entryID, matched := range unsearched {
		s.Nil(matched, "a list without pdb_id or query has no matched_models (%s)", entryID)
	}
	s.Require().Len(modelSearch.Data, 1)
	s.Equal(rerefined, modelSearch.Data[0].Id)
	s.Require().NotNil(modelSearch.Data[0].Attributes.Entry)
	s.Equal(entry.EntryId, modelSearch.Data[0].Attributes.Entry.Id)
	// Nothing merged or re-anchored: every model is still a model of its own entry.
	for _, modelID := range []string{deposited, rerefined, singleConformer} {
		s.Equal(entry.EntryId, getModelForTest(s.T(), entry.EntryId, modelID).EntryId)
	}
	s.Empty(listEntryModelsForTest(s.T(), other.EntryId))
}
