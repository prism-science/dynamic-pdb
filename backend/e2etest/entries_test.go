package e2etest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/integrations/github"
)

type EntriesSuite struct {
	baseSuite
}

func TestEntries(t *testing.T) {
	suite.Run(t, new(EntriesSuite))
}

func (s *EntriesSuite) Test_should_create_entry_when_request_is_valid() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 4201, Login: "entry-creator", Name: "Entry Creator", Email: "entry@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	tokenResponse := issueTokenForTest(s.T(), "gh-token")
	creatorID, err := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL).Parse(tokenResponse.AccessToken)
	s.Require().NoError(err)
	description := "Created from UI"
	authors := []string{"Fermi, G.", "Perutz, M.F."}
	affiliation := "MRC Laboratory of Molecular Biology, Cambridge"
	fastaMetadata := readFastaMetadataForTest(s.T(), "testdata/fasta/pdb_4hhb_human_deoxyhemoglobin.fasta")
	thumbnailImageURL := "https://example.com/entry.png"
	name := "entry-" + uuid.NewString()
	sequenceArtifactID := uuid.New()
	modelID := uuid.New()
	modelArtifactID := uuid.New()
	runID := uuid.New()
	rFreeMetricID := uuid.New()
	rWorkMetricID := uuid.New()

	// when
	resp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"name":                name,
		"description":         description,
		"thumbnail_image_url": thumbnailImageURL,
		"metadata": map[string]any{
			"external_refs": map[string]string{"pdb": "4HHB"},
			"organism":      "Homo sapiens",
			"method":        "X-ray crystallography",
		},
		"artifacts": []map[string]any{
			artifactRequest(
				sequenceArtifactID,
				"4HHB human deoxyhemoglobin FASTA",
				"L0",
				"fasta",
				"https://www.rcsb.org/fasta/entry/4HHB/download",
				fastaMetadata,
			),
		},
		"models": []map[string]any{
			{
				"id":                  modelID,
				"name":                "X-ray refinement",
				"description":         "Refinement against crystallographic density",
				"thumbnail_image_url": thumbnailImageURL,
				"primary_artifact_id": modelArtifactID,
				"metadata": map[string]any{
					"authors":     authors,
					"affiliation": affiliation,
					"purpose":     "Refinement",
					"model_type":  "Single Conformer",
				},
				"artifacts": []map[string]any{
					artifactRequest(
						modelArtifactID,
						"4HHB refined deoxyhemoglobin model",
						"L2",
						"cif",
						"https://www.ebi.ac.uk/pdbe/entry-files/download/4hhb.cif",
						map[string]any{"authors": authors},
					),
				},
				"runs": []map[string]any{
					{
						"id":               runID,
						"name":             "phenix.refine 1.21.2",
						"software_name":    "phenix.refine",
						"software_version": "1.21.2",
						"command":          "phenix.refine model.cif data.mtz",
						"artifacts": []map[string]any{
							{"artifact_id": sequenceArtifactID, "direction": "input"},
							{"artifact_id": modelArtifactID, "direction": "output"},
						},
					},
				},
				"metrics": []map[string]any{
					{"id": rFreeMetricID, "key": "r_free", "value": 0.214},
					{"id": rWorkMetricID, "key": "r_work", "value": 0.187},
				},
			},
		},
	}, tokenResponse.AccessToken)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusCreated, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	s.Empty(body)

	entriesResp := getWithToken(s.T(), "/v1/entries", "")
	defer entriesResp.Body.Close()
	s.Equal(http.StatusOK, entriesResp.StatusCode)

	var entriesBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(entriesResp.Body).Decode(&entriesBody))
	entry := entryInfoByName(entriesBody.Items, name)
	s.Require().NotNil(entry)
	s.NotEqual(uuid.Nil, entry.Id)
	s.Equal(creatorID, entry.CreatedBy)
	s.Require().NotNil(entry.Description)
	s.Equal(description, *entry.Description)
	s.Require().NotNil(entry.ThumbnailImageUrl)
	s.Equal(thumbnailImageURL, *entry.ThumbnailImageUrl)

	entryPath := "/v1/entries/" + entry.Id.String()
	entryResp := getWithToken(s.T(), entryPath, "")
	defer entryResp.Body.Close()
	s.Equal(http.StatusOK, entryResp.StatusCode)

	var entryBody httpapi.Entry
	s.Require().NoError(json.NewDecoder(entryResp.Body).Decode(&entryBody))
	s.Equal(entry.Id, entryBody.Id)
	s.Equal("Homo sapiens", entryBody.Metadata["organism"])
	s.Require().Len(entryBody.ProteinSequences, 2)
	s.Equal(sequenceArtifactID, entryBody.ProteinSequences[0].SourceArtifactId)
	s.Contains(entryBody.ProteinSequences[0].Sequence, "VLSPADKTNVKAAWGKVGAHAGEYGAEALERM")

	artifactsResp := getWithToken(s.T(), entryPath+"/artifacts", "")
	defer artifactsResp.Body.Close()
	s.Equal(http.StatusOK, artifactsResp.StatusCode)

	var artifactsBody httpapi.ArtifactListResponse
	s.Require().NoError(json.NewDecoder(artifactsResp.Body).Decode(&artifactsBody))
	s.Require().Len(artifactsBody.Items, 1)
	sequenceArtifact := artifactByID(artifactsBody.Items, sequenceArtifactID)
	s.Require().NotNil(sequenceArtifact)
	s.Equal(httpapi.L0, sequenceArtifact.Level)
	s.Require().NotNil(sequenceArtifact.Format)
	s.Equal("fasta", *sequenceArtifact.Format)

	modelsResp := getWithToken(s.T(), entryPath+"/models", "")
	defer modelsResp.Body.Close()
	s.Equal(http.StatusOK, modelsResp.StatusCode)

	var modelsBody httpapi.ModelListResponse
	s.Require().NoError(json.NewDecoder(modelsResp.Body).Decode(&modelsBody))
	s.Require().Len(modelsBody.Items, 1)
	s.Equal(modelID, modelsBody.Items[0].Id)
	s.Equal("X-ray refinement", modelsBody.Items[0].Name)
	s.Equal(creatorID, modelsBody.Items[0].CreatedBy)
	s.Require().NotNil(modelsBody.Items[0].PrimaryArtifactId)
	s.Equal(modelArtifactID, *modelsBody.Items[0].PrimaryArtifactId)
	s.Require().NotNil(metricByKey(modelsBody.Items[0].Metrics, "r_free"))

	modelResp := getWithToken(s.T(), entryPath+"/models/"+modelID.String(), "")
	defer modelResp.Body.Close()
	s.Equal(http.StatusOK, modelResp.StatusCode)

	var modelBody httpapi.Model
	s.Require().NoError(json.NewDecoder(modelResp.Body).Decode(&modelBody))
	s.Equal(modelID, modelBody.Id)
	s.Equal(authors[0], modelBody.Metadata["authors"].([]any)[0])
	s.Require().NotNil(metricByKey(modelBody.Metrics, "r_work"))

	modelArtifactsResp := getWithToken(s.T(), entryPath+"/models/"+modelID.String()+"/artifacts", "")
	defer modelArtifactsResp.Body.Close()
	s.Equal(http.StatusOK, modelArtifactsResp.StatusCode)

	var modelArtifactsBody httpapi.ModelArtifactListResponse
	s.Require().NoError(json.NewDecoder(modelArtifactsResp.Body).Decode(&modelArtifactsBody))
	s.Require().NotNil(artifactByID(modelArtifactsBody.Items, modelArtifactID))
	s.Require().NotNil(runByID(modelArtifactsBody.Runs, runID))
	s.True(runArtifactLinkExists(modelArtifactsBody.Relations, runID, sequenceArtifactID, httpapi.Input))
	s.True(runArtifactLinkExists(modelArtifactsBody.Relations, runID, modelArtifactID, httpapi.Output))
}

func (s *EntriesSuite) Test_should_filter_entries_when_search_text_matches_revisions_or_sequence() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 4202, Login: "search-creator", Name: "Search Creator", Email: "search@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	tokenResponse := issueTokenForTest(s.T(), "gh-token")
	entrySearchToken := "crambin-" + uuid.NewString()
	modelSearchToken := "qfit-" + uuid.NewString()
	affiliationSearchToken := "metadata-lab-" + uuid.NewString()
	proteinSequence := proteinSequenceTokenForTest(uuid.New())
	entryName := "entry " + entrySearchToken

	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"name": entryName,
		"artifacts": []map[string]any{
			artifactRequest(uuid.New(), "Protein FASTA", "L0", "fasta", "https://files.example/protein.fasta", map[string]any{
				"records": []map[string]any{{"header": "search protein", "sequence": "M" + proteinSequence + "K"}},
			}),
		},
		"models": []map[string]any{
			{
				"name": "refinement " + modelSearchToken,
				"metadata": map[string]any{
					"authors":     []string{"Search Author"},
					"affiliation": "Open metadata institute " + affiliationSearchToken,
				},
			},
		},
	}, tokenResponse.AccessToken)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	queries := []string{
		entrySearchToken,
		modelSearchToken,
		affiliationSearchToken,
		strings.ToLower(proteinSequence),
	}

	for _, query := range queries {
		// when
		searchResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape(query), "")
		defer searchResp.Body.Close()

		// then
		s.Equal(http.StatusOK, searchResp.StatusCode)
		var searchBody httpapi.EntryListResponse
		s.Require().NoError(json.NewDecoder(searchResp.Body).Decode(&searchBody))
		s.Require().NotNil(entryInfoByName(searchBody.Items, entryName))
	}

	// when
	missingResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape("missing-"+entrySearchToken), "")
	defer missingResp.Body.Close()

	// then
	s.Equal(http.StatusOK, missingResp.StatusCode)
	var missingBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(missingResp.Body).Decode(&missingBody))
	s.Nil(entryInfoByName(missingBody.Items, entryName))
}

func (s *EntriesSuite) Test_should_return_401_when_create_entry_called_without_token() {
	// given

	// when
	resp := postJSON(s.T(), "/v1/entries", map[string]string{"name": "unauthorized-entry"})
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *EntriesSuite) Test_should_return_entry_when_get_entry_called() {
	// given
	token := issueEntryTokenForTest(s.T(), "entry-get-token")
	entryID := uuid.New()
	description := "Entry loaded by id"
	thumbnailURL := "https://example.com/entry-get.png"
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id":                  entryID,
		"name":                "entry-get-" + uuid.NewString(),
		"description":         description,
		"thumbnail_image_url": thumbnailURL,
		"metadata":            map[string]any{"space_group": "P 21 21 21"},
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	resp := getWithToken(s.T(), "/v1/entries/"+entryID.String(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusOK, resp.StatusCode)
	var body httpapi.Entry
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(entryID, body.Id)
	s.Require().NotNil(body.Description)
	s.Equal(description, *body.Description)
	s.Require().NotNil(body.ThumbnailImageUrl)
	s.Equal(thumbnailURL, *body.ThumbnailImageUrl)
	s.Equal("P 21 21 21", body.Metadata["space_group"])
}

func (s *EntriesSuite) Test_should_return_404_when_get_entry_misses() {
	// given
	token := issueEntryTokenForTest(s.T(), "entry-missing-token")

	// when
	resp := getWithToken(s.T(), "/v1/entries/"+uuid.NewString(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusNotFound, resp.StatusCode)
	assertErrorResponse(s.T(), resp, "NOT_FOUND", "entry not found")
}

func (s *EntriesSuite) Test_should_return_model_when_get_model_called() {
	// given
	token := issueEntryTokenForTest(s.T(), "model-get-token")
	entryID := uuid.New()
	modelID := uuid.New()
	metricID := uuid.New()
	description := "Model loaded by id"
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id":   entryID,
		"name": "entry-with-model-" + uuid.NewString(),
		"models": []map[string]any{
			{
				"id":          modelID,
				"name":        "model loaded by id",
				"description": description,
				"metrics":     []map[string]any{{"id": metricID, "key": "r_free", "value": 0.231}},
			},
		},
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	resp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusOK, resp.StatusCode)
	var body httpapi.Model
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(modelID, body.Id)
	s.Equal(entryID, body.EntryId)
	s.Require().NotNil(body.Description)
	s.Equal(description, *body.Description)
	metric := metricByKey(body.Metrics, "r_free")
	s.Require().NotNil(metric)
	s.Equal(metricID, metric.Id)
	s.InDelta(0.231, metric.Value, 0.0001)
}

func (s *EntriesSuite) Test_should_return_404_when_get_model_misses() {
	// given
	token := issueEntryTokenForTest(s.T(), "model-missing-token")

	// when
	resp := getWithToken(s.T(), "/v1/entries/"+uuid.NewString()+"/models/"+uuid.NewString(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusNotFound, resp.StatusCode)
	assertErrorResponse(s.T(), resp, "NOT_FOUND", "model not found")
}

func (s *EntriesSuite) Test_should_delete_entry_when_caller_is_creator() {
	// given
	token := issueEntryTokenForTest(s.T(), "entry-delete-owner-token")
	entryID := uuid.New()
	modelID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id":        entryID,
		"name":      "entry-delete-owner-" + uuid.NewString(),
		"artifacts": []map[string]any{artifactRequest(uuid.New(), "entry artifact", "L0", "mtz", "s3://entry/data.mtz", nil)},
		"models":    []map[string]any{{"id": modelID, "name": "model removed with entry"}},
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/entries/"+entryID.String(), token)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusNoContent, deleteResp.StatusCode)
	body, err := io.ReadAll(deleteResp.Body)
	s.Require().NoError(err)
	s.Empty(body)

	getEntryResp := getWithToken(s.T(), "/v1/entries/"+entryID.String(), token)
	defer getEntryResp.Body.Close()
	s.Equal(http.StatusNotFound, getEntryResp.StatusCode)

	getModelResp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String(), token)
	defer getModelResp.Body.Close()
	s.Equal(http.StatusNotFound, getModelResp.StatusCode)

	listModelsResp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models", token)
	defer listModelsResp.Body.Close()
	s.Equal(http.StatusOK, listModelsResp.StatusCode)
	var listModelsBody httpapi.ModelListResponse
	s.Require().NoError(json.NewDecoder(listModelsResp.Body).Decode(&listModelsBody))
	s.Empty(listModelsBody.Items)

	modelArtifactsResp := getWithToken(
		s.T(),
		"/v1/entries/"+entryID.String()+"/models/"+modelID.String()+"/artifacts",
		token,
	)
	defer modelArtifactsResp.Body.Close()
	s.Equal(http.StatusNotFound, modelArtifactsResp.StatusCode)
}

func (s *EntriesSuite) Test_should_add_model_to_existing_entry() {
	// given
	ownerToken := issueEntryTokenForTest(s.T(), "model-add-owner-token")
	entryID := uuid.New()
	baselineArtifactID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id":        entryID,
		"name":      "entry-add-model-" + uuid.NewString(),
		"artifacts": []map[string]any{artifactRequest(baselineArtifactID, "baseline data", "L0", "mtz", "s3://entry/data.mtz", nil)},
	}, ownerToken)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	contributorToken := issueEntryTokenForGitHubIDForTest(s.T(), "model-add-contributor-token", 8110)
	contributorID, err := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL).Parse(contributorToken)
	s.Require().NoError(err)
	modelID := uuid.New()
	modelArtifactID := uuid.New()
	runID := uuid.New()
	metricID := uuid.New()

	// when
	resp := postJSONWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models", map[string]any{
		"id":                  modelID,
		"name":                "added refinement",
		"description":         "Added to an entry that already existed",
		"primary_artifact_id": modelArtifactID,
		"artifacts": []map[string]any{
			artifactRequest(modelArtifactID, "added model artifact", "L2", "cif", "s3://model/model.cif", nil),
		},
		"runs": []map[string]any{
			{
				"id":            runID,
				"name":          "phenix.refine",
				"software_name": "phenix.refine",
				"artifacts": []map[string]any{
					{"artifact_id": baselineArtifactID, "direction": "input"},
					{"artifact_id": modelArtifactID, "direction": "output"},
				},
			},
		},
		"metrics": []map[string]any{{"id": metricID, "key": "r_free", "value": 0.231}},
	}, contributorToken)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusCreated, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	s.Empty(body)

	modelResp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String(), "")
	defer modelResp.Body.Close()
	s.Equal(http.StatusOK, modelResp.StatusCode)
	var modelBody httpapi.Model
	s.Require().NoError(json.NewDecoder(modelResp.Body).Decode(&modelBody))
	s.Equal("added refinement", modelBody.Name)
	s.Equal(contributorID, modelBody.CreatedBy)
	s.Require().NotNil(metricByKey(modelBody.Metrics, "r_free"))

	artifactsResp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String()+"/artifacts", "")
	defer artifactsResp.Body.Close()
	s.Equal(http.StatusOK, artifactsResp.StatusCode)
	var artifactsBody httpapi.ModelArtifactListResponse
	s.Require().NoError(json.NewDecoder(artifactsResp.Body).Decode(&artifactsBody))
	s.Require().NotNil(artifactByID(artifactsBody.Items, modelArtifactID))
	s.Require().NotNil(runByID(artifactsBody.Runs, runID))
	s.True(runArtifactLinkExists(artifactsBody.Relations, runID, baselineArtifactID, httpapi.Input))
	s.True(runArtifactLinkExists(artifactsBody.Relations, runID, modelArtifactID, httpapi.Output))
}

func (s *EntriesSuite) Test_should_return_404_when_adding_model_to_missing_entry() {
	// given
	token := issueEntryTokenForTest(s.T(), "model-add-missing-entry-token")

	// when
	resp := postJSONWithToken(s.T(), "/v1/entries/"+uuid.NewString()+"/models", map[string]any{
		"name": "model for a missing entry",
	}, token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusNotFound, resp.StatusCode)
	assertErrorResponse(s.T(), resp, "NOT_FOUND", "entry not found")
}

func (s *EntriesSuite) Test_should_return_401_when_adding_model_without_token() {
	// given

	// when
	resp := postJSON(s.T(), "/v1/entries/"+uuid.NewString()+"/models", map[string]any{
		"name": "unauthorized model",
	})
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *EntriesSuite) Test_should_return_400_when_added_model_is_invalid() {
	// given
	token := issueEntryTokenForTest(s.T(), "model-add-invalid-token")
	entryID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id": entryID, "name": "entry-add-model-invalid-" + uuid.NewString(),
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	tests := []struct {
		name    string
		request map[string]any
		message string
	}{
		{name: "missing model name", request: map[string]any{"name": "   "}, message: "model name is required"},
		{name: "nil model id", request: map[string]any{"id": uuid.Nil, "name": "nil model id"}, message: "model id is required"},
		{
			name: "nil artifact id",
			request: map[string]any{
				"name":      "model with nil artifact",
				"artifacts": []map[string]any{{"id": uuid.Nil, "name": "artifact", "level": "L2"}},
			},
			message: "artifact id is required",
		},
		{
			name: "nil metric id",
			request: map[string]any{
				"name":    "model with nil metric",
				"metrics": []map[string]any{{"id": uuid.Nil, "key": "r_free", "value": 0.2}},
			},
			message: "metric id is required",
		},
		{
			name: "nil run id",
			request: map[string]any{
				"name": "model with nil run",
				"runs": []map[string]any{{"id": uuid.Nil, "name": "run"}},
			},
			message: "run id is required",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// when
			resp := postJSONWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models", tt.request, token)
			defer resp.Body.Close()

			// then
			s.Equal(http.StatusBadRequest, resp.StatusCode)
			assertErrorResponse(s.T(), resp, "BAD_REQUEST", tt.message)
		})
	}
}

func (s *EntriesSuite) Test_should_return_403_when_non_creator_deletes_entry() {
	// given
	ownerToken := issueEntryTokenForTest(s.T(), "entry-delete-forbidden-owner")
	otherToken := issueEntryTokenForGitHubIDForTest(s.T(), "entry-delete-forbidden-other", 8101)
	entryID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id": entryID, "name": "entry-delete-forbidden-" + uuid.NewString(),
	}, ownerToken)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/entries/"+entryID.String(), otherToken)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusForbidden, deleteResp.StatusCode)
	getEntryResp := getWithToken(s.T(), "/v1/entries/"+entryID.String(), ownerToken)
	defer getEntryResp.Body.Close()
	s.Equal(http.StatusOK, getEntryResp.StatusCode)
}

func (s *EntriesSuite) Test_should_delete_model_when_caller_is_creator() {
	// given
	token := issueEntryTokenForTest(s.T(), "model-delete-owner-token")
	entryID := uuid.New()
	modelID := uuid.New()
	modelArtifactID := uuid.New()
	searchToken := "modeldelete" + strings.ReplaceAll(uuid.NewString(), "-", "")
	entryName := "entry-kept-after-model-delete-" + uuid.NewString()
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id": entryID, "name": entryName,
		"models": []map[string]any{
			{
				"id":   modelID,
				"name": "model " + searchToken,
				"artifacts": []map[string]any{
					artifactRequest(modelArtifactID, "artifact "+searchToken, "L2", "cif", "s3://model/model.cif", nil),
				},
			},
		},
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String(), token)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusNoContent, deleteResp.StatusCode)
	body, err := io.ReadAll(deleteResp.Body)
	s.Require().NoError(err)
	s.Empty(body)

	getEntryResp := getWithToken(s.T(), "/v1/entries/"+entryID.String(), token)
	defer getEntryResp.Body.Close()
	s.Equal(http.StatusOK, getEntryResp.StatusCode)

	getModelResp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String(), token)
	defer getModelResp.Body.Close()
	s.Equal(http.StatusNotFound, getModelResp.StatusCode)

	modelArtifactsResp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String()+"/artifacts", token)
	defer modelArtifactsResp.Body.Close()
	s.Equal(http.StatusNotFound, modelArtifactsResp.StatusCode)

	searchResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape(searchToken), "")
	defer searchResp.Body.Close()
	s.Equal(http.StatusOK, searchResp.StatusCode)
	var searchBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(searchResp.Body).Decode(&searchBody))
	s.Nil(entryInfoByName(searchBody.Items, entryName))
}

func (s *EntriesSuite) Test_should_return_403_when_non_creator_deletes_model() {
	// given
	ownerToken := issueEntryTokenForTest(s.T(), "model-delete-forbidden-owner")
	otherToken := issueEntryTokenForGitHubIDForTest(s.T(), "model-delete-forbidden-other", 8102)
	entryID := uuid.New()
	modelID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"id": entryID, "name": "entry-with-forbidden-model-delete-" + uuid.NewString(),
		"models": []map[string]any{{"id": modelID, "name": "model kept after forbidden delete"}},
	}, ownerToken)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String(), otherToken)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusForbidden, deleteResp.StatusCode)
	getModelResp := getWithToken(s.T(), "/v1/entries/"+entryID.String()+"/models/"+modelID.String(), ownerToken)
	defer getModelResp.Body.Close()
	s.Equal(http.StatusOK, getModelResp.StatusCode)
}

func (s *EntriesSuite) Test_should_return_400_when_create_entry_graph_is_invalid() {
	// given
	token := issueEntryTokenForTest(s.T(), "entry-validation-token")

	tests := []struct {
		name    string
		request map[string]any
		message string
	}{
		{name: "nil entry id", request: map[string]any{"id": uuid.Nil, "name": "invalid nil id"}, message: "entry id is required"},
		{
			name: "empty model name",
			request: map[string]any{
				"name": "entry with empty model", "models": []map[string]any{{"name": "   "}},
			},
			message: "model name is required",
		},
		{
			name: "nil entry artifact id",
			request: map[string]any{
				"name":      "entry with nil artifact",
				"artifacts": []map[string]any{{"id": uuid.Nil, "name": "artifact", "level": "L0"}},
			},
			message: "artifact id is required",
		},
		{
			name: "empty artifact name",
			request: map[string]any{
				"name":      "entry with empty artifact",
				"artifacts": []map[string]any{{"id": uuid.New(), "name": "   ", "level": "L0"}},
			},
			message: "artifact name is required",
		},
		{
			name: "nil metric id",
			request: map[string]any{
				"name": "entry with nil metric",
				"models": []map[string]any{{
					"name": "model", "metrics": []map[string]any{{"id": uuid.Nil, "key": "r_free", "value": 0.2}},
				}},
			},
			message: "metric id is required",
		},
		{
			name: "empty metric key",
			request: map[string]any{
				"name": "entry with empty metric key",
				"models": []map[string]any{{
					"name": "model", "metrics": []map[string]any{{"id": uuid.New(), "key": "   ", "value": 0.2}},
				}},
			},
			message: "metric key is required",
		},
		{
			name: "nil run id",
			request: map[string]any{
				"name":   "entry with nil run",
				"models": []map[string]any{{"name": "model", "runs": []map[string]any{{"id": uuid.Nil, "name": "run"}}}},
			},
			message: "run id is required",
		},
		{
			name: "empty run name",
			request: map[string]any{
				"name":   "entry with empty run",
				"models": []map[string]any{{"name": "model", "runs": []map[string]any{{"id": uuid.New(), "name": "   "}}}},
			},
			message: "run name is required",
		},
		{
			name: "nil run artifact id",
			request: map[string]any{
				"name": "entry with nil run artifact",
				"models": []map[string]any{{
					"name": "model",
					"runs": []map[string]any{{
						"id": uuid.New(), "name": "run",
						"artifacts": []map[string]any{{"artifact_id": uuid.Nil, "direction": "input"}},
					}},
				}},
			},
			message: "run artifact artifact_id is required",
		},
		{
			name: "empty run artifact direction",
			request: map[string]any{
				"name": "entry with empty run direction",
				"models": []map[string]any{{
					"name": "model",
					"runs": []map[string]any{{
						"id": uuid.New(), "name": "run",
						"artifacts": []map[string]any{{"artifact_id": uuid.New(), "direction": ""}},
					}},
				}},
			},
			message: "run artifact direction is required",
		},
		{
			name: "invalid entry metadata",
			request: map[string]any{
				"name": "entry with invalid metadata", "metadata": map[string]any{"resolution": "not-a-number"},
			},
			message: "decode entry metadata",
		},
		{
			name: "invalid model metadata",
			request: map[string]any{
				"name":   "entry with invalid model metadata",
				"models": []map[string]any{{"name": "model", "metadata": map[string]any{"atom_count": "not-a-number"}}},
			},
			message: "decode model metadata",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// when
			resp := postJSONWithToken(s.T(), "/v1/entries", tt.request, token)
			defer resp.Body.Close()

			// then
			s.Equal(http.StatusBadRequest, resp.StatusCode)
			assertErrorResponse(s.T(), resp, "BAD_REQUEST", tt.message)
		})
	}
}

func issueEntryTokenForTest(t *testing.T, accessToken string) string {
	t.Helper()
	return issueEntryTokenForGitHubIDForTest(t, accessToken, 8000)
}

func issueEntryTokenForGitHubIDForTest(t *testing.T, accessToken string, githubID int64) string {
	t.Helper()
	githubClient.On("GetUser", mock.Anything, accessToken).
		Return(github.User{ID: githubID, Login: accessToken, Name: "Entry Test", Email: accessToken + "@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, accessToken).
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)
	return issueTokenForTest(t, accessToken).AccessToken
}

func issueTokenForTest(t *testing.T, accessToken string) httpapi.TokenResponse {
	t.Helper()
	resp := ExchangeGithubToken(t, accessToken)
	defer resp.Body.Close()

	var body httpapi.TokenResponse
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body
}

func readFastaMetadataForTest(t *testing.T, filePath string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(filePath)
	require.NoError(t, err)

	records := make([]map[string]any, 0, 2)
	var sequence strings.Builder
	var header string
	var pdbID string

	appendRecord := func() {
		if sequence.Len() == 0 {
			return
		}
		records = append(records, map[string]any{"header": header, "sequence": sequence.String()})
		sequence.Reset()
	}

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			appendRecord()
			header = strings.TrimSpace(strings.TrimPrefix(line, ">"))
			if pdbID == "" {
				fields := strings.Fields(header)
				require.NotEmpty(t, fields)
				pdbID = strings.Split(fields[0], "_")[0]
			}
			continue
		}
		sequence.WriteString(line)
	}
	appendRecord()

	require.NotEmpty(t, pdbID)
	require.NotEmpty(t, records)
	return map[string]any{"pdb_id": pdbID, "records": records, "organism": "Homo sapiens"}
}

func proteinSequenceTokenForTest(id uuid.UUID) string {
	const alphabet = "ACDEFGHIKLMNPQRSTVWY"

	var token strings.Builder
	token.Grow(len(id))
	for _, value := range id {
		token.WriteByte(alphabet[int(value)%len(alphabet)])
	}
	return token.String()
}

func entryInfoByName(entries []httpapi.EntryInfo, name string) *httpapi.EntryInfo {
	for index := range entries {
		if entries[index].Name == name {
			return &entries[index]
		}
	}
	return nil
}

func artifactByID(artifacts []httpapi.Artifact, id uuid.UUID) *httpapi.Artifact {
	for index := range artifacts {
		if artifacts[index].Id == id {
			return &artifacts[index]
		}
	}
	return nil
}

func runByID(runs []httpapi.Run, id uuid.UUID) *httpapi.Run {
	for index := range runs {
		if runs[index].Id == id {
			return &runs[index]
		}
	}
	return nil
}

func metricByKey(metrics []httpapi.Metric, key string) *httpapi.Metric {
	for index := range metrics {
		if metrics[index].Key == key {
			return &metrics[index]
		}
	}
	return nil
}

func runArtifactLinkExists(
	links []httpapi.RunArtifact,
	runID uuid.UUID,
	artifactID uuid.UUID,
	direction httpapi.RunArtifactDirection,
) bool {
	for _, link := range links {
		if link.RunId == runID && link.ArtifactId == artifactID && link.Direction == direction {
			return true
		}
	}
	return false
}

func artifactRequest(
	id uuid.UUID,
	name string,
	level string,
	format string,
	uri string,
	metadata map[string]any,
) map[string]any {
	request := map[string]any{
		"id": id, "name": name, "level": level, "format": format, "uri": uri,
	}
	if metadata != nil {
		request["metadata"] = metadata
	}
	return request
}

func assertErrorResponse(t *testing.T, response *http.Response, code, message string) {
	t.Helper()
	var body httpapi.Error
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	require.Equal(t, code, body.Code)
	require.Contains(t, body.Message, message)
}
