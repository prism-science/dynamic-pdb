package e2etest

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// createModelWithConformersForTest adds an active Deposited model with its own model file to entry,
// and stores the conformer counts the coordinate-index job would read from the file.
func (s *ModelSearchSuite) createModelWithConformersForTest(token string, entryID string, counts ...models.ResidueConformerCount) string {
	artifactID := uuid.New()
	model := createAndActivateModelForTest(s.T(), token, entryID, map[string]any{
		"model": map[string]any{
			"title":               "residue search model",
			"primary_artifact_id": artifactID,
			"metadata":            map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{{
				"id": artifactID, "name": "model.cif", "level": "L2", "type": "model", "format": "mmcif",
				"uri": storedModelURI(artifactID),
			}},
		},
	})
	residues, multi := 0, 0
	for _, count := range counts {
		residues += count.Residues
		if count.ConformerCount >= 2 {
			multi += count.Residues
		}
	}
	key := models.ResidueKeyLabel
	s.Require().NoError(testDatabase.SaveCoordinateIndex(context.Background(), db.SaveCoordinateIndexParams{
		ArtifactID:    artifactID,
		ReaderVersion: cif.CoordinateIndexVersion,
		ReadAt:        time.Now().UTC(),
		Resolve: func(models.ModelFileFASTA) (db.ResolvedIndex, error) {
			return db.ResolvedIndex{}, nil
		},
		Conformers: func(ensembleType bool) (models.FileConformers, error) {
			return models.FileConformers{
				Version: cif.ResidueCountVersion, Scope: models.ConformerScopeCounted, Key: &key,
				Residues: &residues, MultiConformerResidues: &multi, ReadAsEnsembleType: ensembleType, Counts: counts,
			}, nil
		},
	}))
	return model.ModelId
}

// Example 1 of the researchers' request: models with a lysine with more than three conformations.
// A random compound ID in Ligands keeps the shared e2e database's other models out.
func (s *ModelSearchSuite) Test_should_return_models_with_a_lysine_of_four_conformers_when_example_1_is_searched() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-residues-owner", 8816)
	compound := randomCompoundIDForTest()
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{
		"title": "residues with the compound", "ligands": []string{compound}, "cofactors": []string{},
	}})
	four := s.createModelWithConformersForTest(token, entry.EntryId,
		models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 1, Residues: 20},
		models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 4, Residues: 1})
	five := s.createModelWithConformersForTest(token, entry.EntryId,
		models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 5, Residues: 2})
	three := s.createModelWithConformersForTest(token, entry.EntryId,
		models.ResidueConformerCount{ResidueType: "LYS", ConformerCount: 3, Residues: 8},
		models.ResidueConformerCount{ResidueType: "ARG", ConformerCount: 4, Residues: 1})

	// when
	pages := walkModelSearchForTest(s.T(), fmt.Sprintf(
		"/v1/models?ligand_or_cofactor=%s&residue_type=lys&residue_conformers_min=4&limit=1", compound))

	// then
	results := modelSearchResultsByIDForTest(pages)
	s.Len(results, 2)
	s.Contains(results, four)
	s.Contains(results, five)
	s.NotContains(results, three)
	s.Require().NotNil(pages[0].Meta)
	s.Equal(&httpapi.ModelSearchTotal{Count: 2, Exact: true}, pages[0].Meta.Total)
	s.Contains(pages[0].Links.Next, "residue_type=lys")
	s.Contains(pages[0].Links.Next, "residue_conformers_min=4")

	// and two such lysines: only the model with two.
	pages = walkModelSearchForTest(s.T(), fmt.Sprintf(
		"/v1/models?ligand_or_cofactor=%s&residue_type=LYS&residue_conformers_min=4&residues_min=2", compound))
	results = modelSearchResultsByIDForTest(pages)
	s.Len(results, 1)
	s.Contains(results, five)
}

func (s *ModelSearchSuite) Test_should_answer_400_naming_the_parameter_when_residue_conformers_min_has_no_residue_type() {
	// given
	path := "/v1/models?residue_conformers_min=4"

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), modelSearchServer.URL, http.MethodGet, path, nil, "")
	refused := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusBadRequest)

	// then
	s.Require().Len(refused.Errors, 1)
	s.Require().NotNil(refused.Errors[0].Detail)
	s.Contains(*refused.Errors[0].Detail, "residue_conformers_min")
	s.Contains(*refused.Errors[0].Detail, "needs residue_type")
}
