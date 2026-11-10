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

// createModelWithChainsForTest adds an active Deposited model with its own model file to entry,
// and stores the chains the coordinate-index job would read from the file: proteinLengths are
// its protein chains' deposited lengths (0 for unknown); a DNA chain is added too.
func (s *ModelSearchSuite) createModelWithChainsForTest(token string, entryID string, proteinLengths ...int) string {
	artifactID := uuid.New()
	model := createAndActivateModelForTest(s.T(), token, entryID, map[string]any{
		"model": map[string]any{
			"title":               "chain search model",
			"primary_artifact_id": artifactID,
			"metadata":            map[string]any{"model_type": "Deposited"},
			"artifacts": []map[string]any{{
				"id": artifactID, "name": "model.cif", "level": "L2", "type": "model", "format": "mmcif",
				"uri": storedModelURI(artifactID),
			}},
		},
	})
	chains := []models.IndexedChain{{AuthAsymID: "Z", ModeledResidues: 24, ProteinSource: models.ProteinSourceEntityPolyType}}
	for index, length := range proteinLengths {
		chain := models.IndexedChain{
			AuthAsymID: string(rune('A' + index)), ModeledResidues: 10, Protein: true, ProteinSource: models.ProteinSourceEntityPolyType,
		}
		if length > 0 {
			source := models.ChainLengthSourceEntityPolySeq
			chain.DepositedLength = &length
			chain.LengthSource = &source
		}
		chains = append(chains, chain)
	}
	s.Require().NoError(saveChainsForTest(artifactID, chains...))
	return model.ModelId
}

// saveChainsForTest saves a read of a model file with the given chains, as the coordinate-index
// job does.
func saveChainsForTest(artifactID uuid.UUID, chains ...models.IndexedChain) error {
	return testDatabase.SaveCoordinateIndex(context.Background(), db.SaveCoordinateIndexParams{
		ArtifactID:    artifactID,
		ReaderVersion: cif.CoordinateIndexVersion,
		ReadAt:        time.Now().UTC(),
		Resolve: func(models.ModelFileFASTA) (db.ResolvedIndex, error) {
			return db.ResolvedIndex{Chains: chains}, nil
		},
	})
}

// Example 2 of the researchers' request: models whose entry lists ADP in Ligands or Cofactors and
// that have more than four protein chains, each longer than 300 residues. A random compound ID
// stands in for ADP, so the shared e2e database holds no other matches.
func (s *ModelSearchSuite) Test_should_return_models_with_five_long_protein_chains_when_example_2_is_searched() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-chains-owner", 8815)
	compound := randomCompoundIDForTest()
	listing := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{
		"title": "chains with the compound", "ligands": []string{compound}, "cofactors": []string{},
	}})
	other := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{
		"title": "chains without the compound", "ligands": []string{}, "cofactors": []string{},
	}})
	fiveLong := s.createModelWithChainsForTest(token, listing.EntryId, 301, 450, 450, 900, 1200)
	sixLong := s.createModelWithChainsForTest(token, listing.EntryId, 400, 400, 400, 400, 400, 400)
	fourLong := s.createModelWithChainsForTest(token, listing.EntryId, 400, 400, 400, 400, 300)
	unknownLengths := s.createModelWithChainsForTest(token, listing.EntryId, 0, 0, 0, 0, 0)
	notRead := s.createModelWithChainsForTest(token, listing.EntryId)
	otherEntry := s.createModelWithChainsForTest(token, other.EntryId, 400, 400, 400, 400, 400)

	// when
	pages := walkModelSearchForTest(s.T(), fmt.Sprintf(
		"/v1/models?ligand_or_cofactor=%s&protein_chains_min=5&protein_chain_length_min=301&limit=1", compound))

	// then
	results := modelSearchResultsByIDForTest(pages)
	s.Len(results, 2)
	s.Contains(results, fiveLong)
	s.Contains(results, sixLong)
	for _, modelID := range []string{fourLong, unknownLengths, notRead, otherEntry} {
		s.NotContains(results, modelID)
	}
	s.Require().NotNil(pages[0].Meta)
	s.Equal(&httpapi.ModelSearchTotal{Count: 2, Exact: true}, pages[0].Meta.Total)
	s.Contains(pages[0].Links.Next, "protein_chains_min=5")
	s.Contains(pages[0].Links.Next, "protein_chain_length_min=301")
}

// The researchers' "every protein chain" mode: at least two protein chains, all longer than 300
// residues. Each model also has a short DNA chain, which is not a protein chain.
func (s *ModelSearchSuite) Test_should_return_models_whose_every_protein_chain_is_long_when_the_every_mode_is_searched() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-chains-every-owner", 8817)
	compound := randomCompoundIDForTest()
	listing := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{
		"title": "every chain long", "ligands": []string{compound}, "cofactors": []string{},
	}})
	allLong := s.createModelWithChainsForTest(token, listing.EntryId, 400, 400, 400)
	twoLong := s.createModelWithChainsForTest(token, listing.EntryId, 350, 350)
	oneShort := s.createModelWithChainsForTest(token, listing.EntryId, 400, 400, 120)
	oneUnknown := s.createModelWithChainsForTest(token, listing.EntryId, 400, 400, 0)
	oneChain := s.createModelWithChainsForTest(token, listing.EntryId, 900)

	// when
	pages := walkModelSearchForTest(s.T(), fmt.Sprintf(
		"/v1/models?ligand_or_cofactor=%s&protein_chain_mode=every&protein_chains_min=2&protein_chain_length_min=301&limit=1",
		compound))

	// then
	results := modelSearchResultsByIDForTest(pages)
	s.Len(results, 2)
	s.Contains(results, allLong)
	s.Contains(results, twoLong)
	for _, modelID := range []string{oneShort, oneUnknown, oneChain} {
		s.NotContains(results, modelID)
	}
	s.Require().NotNil(pages[0].Meta)
	s.Equal(&httpapi.ModelSearchTotal{Count: 2, Exact: true}, pages[0].Meta.Total)
	s.Contains(pages[0].Links.Next, "protein_chain_mode=every")
}

func (s *ModelSearchSuite) Test_should_answer_400_naming_the_mode_when_every_has_no_length_bound() {
	// given
	path := "/v1/models?protein_chain_mode=every&protein_chains_min=2"

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), modelSearchServer.URL, http.MethodGet, path, nil, "")
	refused := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusBadRequest)

	// then
	s.Require().Len(refused.Errors, 1)
	s.Require().NotNil(refused.Errors[0].Detail)
	s.Contains(*refused.Errors[0].Detail, "protein_chain_mode")
}

func (s *ModelSearchSuite) Test_should_answer_400_naming_the_parameter_when_a_protein_chain_bound_is_below_one() {
	// given
	path := "/v1/models?protein_chains_min=0"

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), modelSearchServer.URL, http.MethodGet, path, nil, "")
	refused := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusBadRequest)

	// then
	s.Require().Len(refused.Errors, 1)
	s.Require().NotNil(refused.Errors[0].Detail)
	s.Contains(*refused.Errors[0].Detail, "protein_chains_min")
}
