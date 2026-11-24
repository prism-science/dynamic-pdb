package e2etest

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

const compoundReviewReasonForTest = "could be a chain residue"

// compoundCopiesForTest are copies of ccdID in a model file: confirmed ones, then ones the reader
// marked needs review.
func compoundCopiesForTest(ccdID string, confirmed, needingReview int) []cif.CompoundCopy {
	copies := make([]cif.CompoundCopy, 0, confirmed+needingReview)
	for index := range confirmed + needingReview {
		compoundCopy := cif.CompoundCopy{
			CCDID: ccdID, AuthAsymID: "A", AuthSeqID: strconv.Itoa(index + 1), Source: cif.SourcePDBHetatm,
		}
		if index >= confirmed {
			compoundCopy.NeedsReview = true
			compoundCopy.ReviewReason = compoundReviewReasonForTest
		}
		copies = append(copies, compoundCopy)
	}
	return copies
}

// saveCompoundsForTest stores a read of a model file, as the model-compounds job does.
func saveCompoundsForTest(t require.TestingT, artifactID uuid.UUID, copies []cif.CompoundCopy) {
	_, err := testDatabase.SaveModeledCompounds(context.Background(), db.SaveModeledCompoundsParams{
		ArtifactID: artifactID,
		CreatedBy:  uuid.MustParse(models.SystemUserID),
		Result:     cif.ModeledCompounds{Copies: copies, Complete: true, Warnings: []string{}},
		ReadAt:     time.Now().UTC(),
	})
	require.NoError(t, err)
}

// createModelWithOwnFileForTest adds an active model of metadata's type to entryID with its own
// model file in our bucket, so the model-compounds job reads it. A computed model gets the run that
// made its file from the entry's file. It returns the model ID and its file's ID.
func (s *ModelSearchSuite) createModelWithOwnFileForTest(
	token, entryID string,
	entryFileID uuid.UUID,
	metadata map[string]any,
) (string, uuid.UUID) {
	modelFileID := uuid.New()
	model := map[string]any{
		"title":               "compound review model",
		"primary_artifact_id": modelFileID,
		"metadata":            metadata,
		"artifacts": []map[string]any{{
			"id": modelFileID, "name": "model.cif", "level": "L2", "type": "model", "format": "mmcif",
			"uri": storedModelURI(modelFileID),
		}},
	}
	if metadata["model_type"] != "Deposited" {
		model["runs"] = []map[string]any{refinementRunForTest(uuid.New(), entryFileID, modelFileID)}
	}
	created := createAndActivateModelForTest(s.T(), token, entryID, map[string]any{"model": model})
	return created.ModelId, modelFileID
}

// createArchiveReRefinementWithCopyForTest adds a re-refinement deposited as an Ensemble (origin
// archive, its file on files.rcsb.org, which is its primary file) whose file the checksum job copied
// into our bucket, so the model-compounds job reads that copy. It returns the model ID and its file's ID.
func (s *ModelSearchSuite) createArchiveReRefinementWithCopyForTest(token, entryID, originalPDBID string) (string, uuid.UUID) {
	ctx := context.Background()
	windowStart := time.Now().UTC().Add(-time.Minute)
	pdbID := unusedPDBIDForTest(s.T())
	request := archiveEnsembleForTest(pdbID, originalPDBID, coordinateFileForTest(pdbID))
	model, ok := request["model"].(map[string]any)
	s.Require().True(ok)
	artifacts, ok := model["artifacts"].([]map[string]any)
	s.Require().True(ok)
	artifactID, ok := artifacts[0]["id"].(uuid.UUID)
	s.Require().True(ok)
	model["primary_artifact_id"] = artifactID
	created := createAndActivateModelForTest(s.T(), token, entryID, request)
	recordChecksumCopyForTest(s.T(), artifactID)
	_, err := testDatabase.ModeledCompounds.AddMissingSince(ctx, windowStart, true)
	s.Require().NoError(err)
	return created.ModelId, artifactID
}

// Researcher question 6, on every model representation: by default copies that need review match
// (and are listed); with exclude_compounds_needing_review a model matches only through a copy that
// does not, and nothing is listed. A file not read yet never matches; the curated filter is
// unchanged.
func (s *ModelSearchSuite) Test_should_leave_out_only_models_whose_copies_all_need_review_when_the_user_excludes_them() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "model-search-compound-review-owner", 8830)
	compound := randomCompoundIDForTest()
	originalPDBID := unusedPDBIDForTest(s.T())
	entryFileID := uuid.New()
	entry := createAndActivateEntryForTest(s.T(), token, map[string]any{"entry": map[string]any{
		"title": "compound review entry", "ligands": []string{compound}, "cofactors": []string{},
		"external_refs": map[string]string{"pdb": originalPDBID},
		"artifacts": []map[string]any{
			artifactRequest(entryFileID, "data", "L0", "structure_factors_cif", "s3://entry/compound-review-sf.cif", nil),
		},
	}})
	deposited, depositedFile := s.createModelWithOwnFileForTest(token, entry.EntryId, entryFileID,
		map[string]any{"model_type": "Deposited"})
	saveCompoundsForTest(s.T(), depositedFile, compoundCopiesForTest(compound, 1, 0))
	singleConformer, singleConformerFile := s.createModelWithOwnFileForTest(token, entry.EntryId, entryFileID,
		map[string]any{"model_type": "Single Conformer"})
	saveCompoundsForTest(s.T(), singleConformerFile, compoundCopiesForTest(compound, 2, 0))
	multiconformer, multiconformerFile := s.createModelWithOwnFileForTest(token, entry.EntryId, entryFileID,
		map[string]any{"model_type": "Multiconformer"})
	saveCompoundsForTest(s.T(), multiconformerFile, compoundCopiesForTest(compound, 0, 1))
	ensemble, ensembleFile := s.createModelWithOwnFileForTest(token, entry.EntryId, entryFileID,
		map[string]any{"model_type": "Ensemble"})
	saveCompoundsForTest(s.T(), ensembleFile, compoundCopiesForTest(compound, 1, 2))
	rerefined, rerefinedFile := s.createArchiveReRefinementWithCopyForTest(token, entry.EntryId, originalPDBID)
	saveCompoundsForTest(s.T(), rerefinedFile, compoundCopiesForTest(compound, 0, 2))
	notRead, _ := s.createModelWithOwnFileForTest(token, entry.EntryId, entryFileID,
		map[string]any{"model_type": "Deposited"})
	needsReview := []httpapi.CompoundNeedingReview{{CcdId: compound, ReviewReason: compoundReviewReasonForTest}}
	none := []httpapi.CompoundNeedingReview{}

	// when
	byDefault := modelSearchResultsByIDForTest(walkModelSearchForTest(s.T(), "/v1/models?limit=100&compound="+compound))
	excluded := modelSearchResultsByIDForTest(walkModelSearchForTest(s.T(),
		"/v1/models?limit=100&compound="+compound+"&exclude_compounds_needing_review=true"))
	explicitDefault := modelSearchResultsByIDForTest(walkModelSearchForTest(s.T(),
		"/v1/models?limit=100&compound="+compound+"&exclude_compounds_needing_review=false"))
	curated := modelSearchResultsByIDForTest(walkModelSearchForTest(s.T(), "/v1/models?limit=100&ligand_or_cofactor="+compound))

	// then
	s.Equal(map[string][]httpapi.CompoundNeedingReview{
		deposited: none, singleConformer: none, multiconformer: needsReview, ensemble: needsReview, rerefined: needsReview,
	}, compoundsNeedingReviewByModelForTest(byDefault))
	s.Equal(map[string][]httpapi.CompoundNeedingReview{
		deposited: none, singleConformer: none, ensemble: none,
	}, compoundsNeedingReviewByModelForTest(excluded))
	s.Equal(compoundsNeedingReviewByModelForTest(byDefault), compoundsNeedingReviewByModelForTest(explicitDefault))
	s.NotContains(byDefault, notRead, "a file not read yet is unknown, not absent: it never matches")
	s.ElementsMatch([]string{deposited, singleConformer, multiconformer, ensemble, rerefined, notRead}, keysOfForTest(curated))
}

func compoundsNeedingReviewByModelForTest(
	results map[string]httpapi.ModelSearchAttributes,
) map[string][]httpapi.CompoundNeedingReview {
	byModel := make(map[string][]httpapi.CompoundNeedingReview, len(results))
	for modelID, attributes := range results {
		if attributes.CompoundsNeedingReview == nil {
			byModel[modelID] = nil
			continue
		}
		byModel[modelID] = *attributes.CompoundsNeedingReview
	}
	return byModel
}

func keysOfForTest[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func (s *ModelSearchSuite) Test_should_answer_400_naming_the_option_when_needs_review_is_excluded_without_a_compound() {
	// given
	path := "/v1/models?exclude_compounds_needing_review=true&model_type=Deposited"

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), modelSearchServer.URL, http.MethodGet, path, nil, "")
	refused := decodeJSONResponse[httpapi.Error](s.T(), response, http.StatusBadRequest)

	// then
	s.Require().Len(refused.Errors, 1)
	s.Require().NotNil(refused.Errors[0].Detail)
	s.Contains(*refused.Errors[0].Detail, "exclude_compounds_needing_review")
	s.Contains(*refused.Errors[0].Detail, "compound")
}
