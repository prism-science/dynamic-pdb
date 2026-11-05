package e2etest

import (
	"net/http"

	"github.com/google/uuid"
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
