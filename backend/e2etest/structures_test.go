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

type StructuresSuite struct {
	baseSuite
}

func TestStructures(t *testing.T) {
	suite.Run(t, new(StructuresSuite))
}

func (s *StructuresSuite) Test_should_create_structure_when_request_is_valid() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 4201, Login: "structure-creator", Name: "Structure Creator", Email: "structure@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	tokenResponse := issueTokenForTest(s.T(), "gh-token")
	creatorID, err := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL).Parse(tokenResponse.AccessToken)
	s.Require().NoError(err)
	description := "Created from UI"
	authors := []string{"Fermi, G.", "Perutz, M.F."}
	affiliation := "MRC Laboratory of Molecular Biology, Cambridge"
	fastaMetadata := readFastaMetadataForTest(s.T(), "testdata/fasta/pdb_4hhb_human_deoxyhemoglobin.fasta")
	thumbnailImageURL := "https://example.com/structure.png"
	name := "structure-" + uuid.NewString()
	sequenceEntityID := uuid.New()
	programEntityID := uuid.New()
	modelEntityID := uuid.New()
	metricsEntityID := uuid.New()

	// when
	resp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"name":                name,
		"description":         description,
		"thumbnail_image_url": thumbnailImageURL,
		"entities": []map[string]any{
			{
				"id":    sequenceEntityID,
				"type":  "data",
				"level": "L0",
				"name":  "4HHB human deoxyhemoglobin FASTA",
				"payload": map[string]any{
					"file_url":    "https://www.rcsb.org/fasta/entry/4HHB/download",
					"type":        "fasta",
					"authors":     authors,
					"affiliation": affiliation,
					"metadata":    fastaMetadata,
				},
			},
		},
		"models": []map[string]any{
			{
				"name":                "X-ray refinement",
				"description":         "Refinement against crystallographic density",
				"thumbnail_image_url": thumbnailImageURL,
				"entities": []map[string]any{
					{
						"id":   programEntityID,
						"type": "program",
						"name": "phenix.refine 1.21.2",
						"payload": map[string]any{
							"name":        "phenix.refine",
							"version":     "1.21.2",
							"description": "Automated reciprocal-space refinement",
						},
					},
					{
						"id":    modelEntityID,
						"type":  "model",
						"level": "L2",
						"name":  "4HHB refined deoxyhemoglobin model",
						"payload": map[string]any{
							"file_url":    "https://www.ebi.ac.uk/pdbe/entry-files/download/4hhb.cif",
							"authors":     authors,
							"affiliation": affiliation,
						},
					},
					{
						"id":    metricsEntityID,
						"type":  "metrics",
						"level": "L3",
						"name":  "4HHB refinement metrics",
						"payload": map[string]any{
							"r_free": 0.214,
							"r_work": 0.187,
							"rscc":   0.91,
							"cc":     0.93,
						},
					},
				},
				"relations": []map[string]any{
					{
						"source_entity_id": sequenceEntityID,
						"target_entity_id": programEntityID,
						"relation_type":    "input_to",
					},
					{
						"source_entity_id": modelEntityID,
						"target_entity_id": programEntityID,
						"relation_type":    "output_of",
					},
					{
						"source_entity_id": metricsEntityID,
						"target_entity_id": modelEntityID,
						"relation_type":    "metrics_for",
					},
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

	structuresResp := getWithToken(s.T(), "/v1/structures", "")
	defer structuresResp.Body.Close()
	s.Equal(http.StatusOK, structuresResp.StatusCode)

	var structuresBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(structuresResp.Body).Decode(&structuresBody))
	structure := structureByName(structuresBody.Items, name)
	s.Require().NotNil(structure)
	s.NotEqual(uuid.Nil, structure.Id)
	s.Equal(creatorID, structure.CreatedBy)
	s.Require().NotNil(structure.Description)
	s.Equal(description, *structure.Description)
	s.Require().NotNil(structure.ThumbnailImageUrl)
	s.Equal(thumbnailImageURL, *structure.ThumbnailImageUrl)

	structurePath := "/v1/structures/" + structure.Id.String()

	structureResp := getWithToken(s.T(), structurePath, "")
	defer structureResp.Body.Close()
	s.Equal(http.StatusOK, structureResp.StatusCode)
	var structureBody httpapi.Structure
	s.Require().NoError(json.NewDecoder(structureResp.Body).Decode(&structureBody))
	s.Equal(structure.Id, structureBody.Id)

	modelsResp := getWithToken(s.T(), structurePath+"/models", "")
	defer modelsResp.Body.Close()
	s.Equal(http.StatusOK, modelsResp.StatusCode)

	var modelsBody httpapi.ModelListResponse
	s.Require().NoError(json.NewDecoder(modelsResp.Body).Decode(&modelsBody))
	s.Len(modelsBody.Items, 1)
	s.Equal("X-ray refinement", modelsBody.Items[0].Name)
	s.Equal(creatorID, modelsBody.Items[0].CreatedBy)
	s.Require().NotNil(modelsBody.Items[0].Description)
	s.Equal("Refinement against crystallographic density", *modelsBody.Items[0].Description)

	modelResp := getWithToken(s.T(), structurePath+"/models/"+modelsBody.Items[0].Id.String(), "")
	defer modelResp.Body.Close()
	s.Equal(http.StatusOK, modelResp.StatusCode)
	var modelBody httpapi.Model
	s.Require().NoError(json.NewDecoder(modelResp.Body).Decode(&modelBody))
	s.Equal(modelsBody.Items[0].Id, modelBody.Id)

	entitiesResp := getWithToken(s.T(), structurePath+"/entities", "")
	defer entitiesResp.Body.Close()
	s.Equal(http.StatusOK, entitiesResp.StatusCode)

	var entitiesBody httpapi.EntityListResponse
	s.Require().NoError(json.NewDecoder(entitiesResp.Body).Decode(&entitiesBody))
	s.Len(entitiesBody.Items, 4)
	s.Len(entitiesBody.Relations, 3)

	sequenceEntity := entityByID(entitiesBody.Items, sequenceEntityID)
	s.Require().NotNil(sequenceEntity)
	sequencePayload, err := sequenceEntity.Payload.AsDataPayload()
	s.Require().NoError(err)
	s.Require().NotNil(sequencePayload.Authors)
	s.Equal(authors, *sequencePayload.Authors)
	s.Require().NotNil(sequencePayload.Affiliation)
	s.Equal(affiliation, *sequencePayload.Affiliation)
	s.Require().NotNil(sequencePayload.Metadata)
	sequenceMetadata := *sequencePayload.Metadata
	s.Equal("4HHB", sequenceMetadata["pdb_id"])
	s.Equal(float64(4), sequenceMetadata["chains"])
	s.Equal([]interface{}{"A", "C", "B", "D"}, sequenceMetadata["chain_ids"])
	s.Equal(float64(287), sequenceMetadata["length"])
	s.Equal("Homo sapiens", sequenceMetadata["organism"])
	s.Contains(sequenceMetadata["sequence"], "VHLTPEEKSAVTALWGKVNVDEVGGEALGRLLVVYPWTQR")

	modelEntity := entityByID(entitiesBody.Items, modelEntityID)
	s.Require().NotNil(modelEntity)
	modelPayload, err := modelEntity.Payload.AsModelPayload()
	s.Require().NoError(err)
	s.Require().NotNil(modelPayload.Authors)
	s.Equal(authors, *modelPayload.Authors)
	s.Require().NotNil(modelPayload.Affiliation)
	s.Equal(affiliation, *modelPayload.Affiliation)

	metricsEntity := entityByID(entitiesBody.Items, metricsEntityID)
	s.Require().NotNil(metricsEntity)
	metricsPayload, err := metricsEntity.Payload.AsMetricsPayload()
	s.Require().NoError(err)
	s.Require().NotNil(metricsPayload.Cc)
	s.InDelta(0.93, *metricsPayload.Cc, 0.0001)

	relationTypes := make(map[httpapi.EntityRelationType]bool)
	for _, relation := range entitiesBody.Relations {
		relationTypes[relation.RelationType] = true
	}
	s.True(relationTypes[httpapi.InputTo])
	s.True(relationTypes[httpapi.OutputOf])
	s.True(relationTypes[httpapi.MetricsFor])
}

func (s *StructuresSuite) Test_should_filter_structures_when_search_text_matches_nested_graph() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 4202, Login: "search-creator", Name: "Search Creator", Email: "search@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	tokenResponse := issueTokenForTest(s.T(), "gh-token")
	searchToken := "crambin-" + uuid.NewString()
	affiliationSearchToken := "metadata-lab-" + uuid.NewString()
	proteinSequence := proteinSequenceTokenForTest(uuid.New())
	sequenceEntityID := uuid.New()
	modelEntityID := uuid.New()

	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"name":        "structure " + searchToken,
		"description": "Searchable structure for nested graph tests",
		"entities": []map[string]any{
			{
				"id":   sequenceEntityID,
				"type": "data",
				"name": "Protein FASTA",
				"payload": map[string]any{
					"file_url": "https://files.example/protein.fasta",
					"type":     "fasta",
					"metadata": map[string]any{
						"sequence": "M" + proteinSequence + "K",
					},
				},
			},
		},
		"models": []map[string]any{
			{
				"name":        "qFit refinement",
				"description": "Refinement against crystallographic density",
				"entities": []map[string]any{
					{
						"id":    modelEntityID,
						"type":  "model",
						"level": "L2",
						"name":  "Refined model " + searchToken,
						"payload": map[string]any{
							"file_url":    "https://example.com/" + searchToken + ".cif",
							"authors":     []string{"Search Author"},
							"affiliation": "Open metadata institute " + affiliationSearchToken,
						},
					},
				},
			},
		},
	}, tokenResponse.AccessToken)
	defer createResp.Body.Close()
	s.Equal(http.StatusCreated, createResp.StatusCode)

	// when
	textSearchResp := getWithToken(s.T(), "/v1/structures?query="+url.QueryEscape(searchToken), "")
	defer textSearchResp.Body.Close()

	modelSearchResp := getWithToken(s.T(), "/v1/structures?query="+url.QueryEscape("qFit"), "")
	defer modelSearchResp.Body.Close()

	entitySearchResp := getWithToken(s.T(), "/v1/structures?query="+url.QueryEscape("model"), "")
	defer entitySearchResp.Body.Close()

	affiliationSearchResp := getWithToken(s.T(), "/v1/structures?query="+url.QueryEscape(affiliationSearchToken), "")
	defer affiliationSearchResp.Body.Close()

	sequenceSearchResp := getWithToken(s.T(), "/v1/structures?query="+url.QueryEscape(strings.ToLower(proteinSequence)), "")
	defer sequenceSearchResp.Body.Close()

	missingResp := getWithToken(s.T(), "/v1/structures?query="+url.QueryEscape("missing-"+searchToken), "")
	defer missingResp.Body.Close()

	// then
	s.Equal(http.StatusOK, textSearchResp.StatusCode)
	s.Equal(http.StatusOK, modelSearchResp.StatusCode)
	s.Equal(http.StatusOK, entitySearchResp.StatusCode)
	s.Equal(http.StatusOK, affiliationSearchResp.StatusCode)
	s.Equal(http.StatusOK, sequenceSearchResp.StatusCode)
	s.Equal(http.StatusOK, missingResp.StatusCode)

	var textSearchBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(textSearchResp.Body).Decode(&textSearchBody))
	s.Require().NotNil(structureByName(textSearchBody.Items, "structure "+searchToken))

	var modelSearchBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(modelSearchResp.Body).Decode(&modelSearchBody))
	s.Require().NotNil(structureByName(modelSearchBody.Items, "structure "+searchToken))

	var entitySearchBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(entitySearchResp.Body).Decode(&entitySearchBody))
	s.Require().NotNil(structureByName(entitySearchBody.Items, "structure "+searchToken))

	var affiliationSearchBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(affiliationSearchResp.Body).Decode(&affiliationSearchBody))
	s.Require().NotNil(structureByName(affiliationSearchBody.Items, "structure "+searchToken))

	var sequenceSearchBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(sequenceSearchResp.Body).Decode(&sequenceSearchBody))
	s.Require().NotNil(structureByName(sequenceSearchBody.Items, "structure "+searchToken))

	var missingBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(missingResp.Body).Decode(&missingBody))
	s.Nil(structureByName(missingBody.Items, "structure "+searchToken))
}

func (s *StructuresSuite) Test_should_return_401_when_create_structure_called_without_token() {
	// given

	// when
	resp := postJSON(s.T(), "/v1/structures", map[string]string{
		"name": "unauthorized-structure",
	})
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *StructuresSuite) Test_should_return_structure_when_get_structure_called() {
	// given
	token := issueStructureTokenForTest(s.T(), "structure-get-token")
	structureID := uuid.New()
	description := "Structure loaded by id"
	thumbnailURL := "https://example.com/structure-get.png"
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":                  structureID,
		"name":                "structure-get-" + uuid.NewString(),
		"description":         description,
		"thumbnail_image_url": thumbnailURL,
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	resp := getWithToken(s.T(), "/v1/structures/"+structureID.String(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusOK, resp.StatusCode)

	var body httpapi.Structure
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(structureID, body.Id)
	s.Require().NotNil(body.Description)
	s.Equal(description, *body.Description)
	s.Require().NotNil(body.ThumbnailImageUrl)
	s.Equal(thumbnailURL, *body.ThumbnailImageUrl)
}

func (s *StructuresSuite) Test_should_return_404_when_get_structure_misses() {
	// given
	token := issueStructureTokenForTest(s.T(), "structure-missing-token")

	// when
	resp := getWithToken(s.T(), "/v1/structures/"+uuid.NewString(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusNotFound, resp.StatusCode)

	var body httpapi.Error
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal("NOT_FOUND", body.Code)
}

func (s *StructuresSuite) Test_should_return_model_when_get_model_called() {
	// given
	token := issueStructureTokenForTest(s.T(), "model-get-token")
	structureID := uuid.New()
	modelID := uuid.New()
	description := "Model loaded by id"
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":   structureID,
		"name": "structure-with-model-" + uuid.NewString(),
		"models": []map[string]any{
			{
				"id":          modelID,
				"name":        "model loaded by id",
				"description": description,
			},
		},
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	resp := getWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models/"+modelID.String(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusOK, resp.StatusCode)

	var body httpapi.Model
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal(modelID, body.Id)
	s.Equal(structureID, body.StructureId)
	s.Require().NotNil(body.Description)
	s.Equal(description, *body.Description)
}

func (s *StructuresSuite) Test_should_return_404_when_get_model_misses() {
	// given
	token := issueStructureTokenForTest(s.T(), "model-missing-token")

	// when
	resp := getWithToken(s.T(), "/v1/structures/"+uuid.NewString()+"/models/"+uuid.NewString(), token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusNotFound, resp.StatusCode)

	var body httpapi.Error
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal("NOT_FOUND", body.Code)
}

func (s *StructuresSuite) Test_should_delete_structure_when_caller_is_creator() {
	// given
	token := issueStructureTokenForTest(s.T(), "structure-delete-owner-token")
	structureID := uuid.New()
	modelID := uuid.New()
	modelEntityID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":   structureID,
		"name": "structure-delete-owner-" + uuid.NewString(),
		"models": []map[string]any{
			{
				"id":   modelID,
				"name": "model removed with structure",
				"entities": []map[string]any{
					modelEntityRequest(modelEntityID, "model entity removed with structure"),
				},
			},
		},
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/structures/"+structureID.String(), token)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusNoContent, deleteResp.StatusCode)
	body, err := io.ReadAll(deleteResp.Body)
	s.Require().NoError(err)
	s.Empty(body)

	getStructureResp := getWithToken(s.T(), "/v1/structures/"+structureID.String(), token)
	defer getStructureResp.Body.Close()
	s.Equal(http.StatusNotFound, getStructureResp.StatusCode)

	getModelResp := getWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models/"+modelID.String(), token)
	defer getModelResp.Body.Close()
	s.Equal(http.StatusNotFound, getModelResp.StatusCode)
}

func (s *StructuresSuite) Test_should_add_model_to_existing_structure() {
	// given
	ownerToken := issueStructureTokenForTest(s.T(), "model-add-owner-token")
	structureID := uuid.New()
	baselineEntityID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":   structureID,
		"name": "structure-add-model-" + uuid.NewString(),
		"entities": []map[string]any{
			dataEntityRequest(baselineEntityID, "baseline sequence"),
		},
	}, ownerToken)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// A second user contributes the model, and its program declares the
	// structure-level file that already existed as an input.
	contributorToken := issueStructureTokenForGitHubIDForTest(s.T(), "model-add-contributor-token", 8110)
	contributorID, err := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL).Parse(contributorToken)
	s.Require().NoError(err)
	modelID := uuid.New()
	modelEntityID := uuid.New()
	programEntityID := uuid.New()
	metricsEntityID := uuid.New()

	// when
	resp := postJSONWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models", map[string]any{
		"id":          modelID,
		"name":        "added refinement",
		"description": "Added to a structure that already existed",
		"entities": []map[string]any{
			modelEntityRequest(modelEntityID, "added model entity"),
			{
				"id":   programEntityID,
				"type": "program",
				"name": "phenix.refine 1.21.2",
				"payload": map[string]any{
					"name":        "phenix.refine",
					"version":     "1.21.2",
					"description": "Automated reciprocal-space refinement",
				},
			},
			{
				"id":    metricsEntityID,
				"type":  "metrics",
				"level": "L3",
				"name":  "added metrics",
				"payload": map[string]any{
					"r_free": 0.231,
				},
			},
		},
		"relations": []map[string]any{
			{
				"source_entity_id": baselineEntityID,
				"target_entity_id": programEntityID,
				"relation_type":    "input_to",
			},
			{
				"source_entity_id": modelEntityID,
				"target_entity_id": programEntityID,
				"relation_type":    "output_of",
			},
			{
				"source_entity_id": metricsEntityID,
				"target_entity_id": modelEntityID,
				"relation_type":    "metrics_for",
			},
		},
	}, contributorToken)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusCreated, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	s.Require().NoError(err)
	s.Empty(body)

	modelResp := getWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models/"+modelID.String(), "")
	defer modelResp.Body.Close()
	s.Equal(http.StatusOK, modelResp.StatusCode)
	var modelBody httpapi.Model
	s.Require().NoError(json.NewDecoder(modelResp.Body).Decode(&modelBody))
	s.Equal("added refinement", modelBody.Name)
	s.Equal(contributorID, modelBody.CreatedBy)

	entitiesResp := getWithToken(s.T(), "/v1/structures/"+structureID.String()+"/entities", "")
	defer entitiesResp.Body.Close()
	s.Equal(http.StatusOK, entitiesResp.StatusCode)
	var entitiesBody httpapi.EntityListResponse
	s.Require().NoError(json.NewDecoder(entitiesResp.Body).Decode(&entitiesBody))
	s.Require().NotNil(entityByID(entitiesBody.Items, modelEntityID))
	s.Require().NotNil(entityByID(entitiesBody.Items, programEntityID))
	s.Len(entitiesBody.Relations, 3)

	// The relation from the pre-existing structure-level file is what this endpoint
	// adds over creating a whole structure.
	linkedBaseline := false
	for _, relation := range entitiesBody.Relations {
		if relation.SourceEntityId == baselineEntityID &&
			relation.TargetEntityId == programEntityID &&
			relation.RelationType == httpapi.InputTo {
			linkedBaseline = true
		}
	}
	s.True(linkedBaseline)
}

func (s *StructuresSuite) Test_should_return_404_when_adding_model_to_missing_structure() {
	// given
	token := issueStructureTokenForTest(s.T(), "model-add-missing-structure-token")

	// when
	resp := postJSONWithToken(s.T(), "/v1/structures/"+uuid.NewString()+"/models", map[string]any{
		"name": "model for a missing structure",
	}, token)
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusNotFound, resp.StatusCode)

	var body httpapi.Error
	s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
	s.Equal("NOT_FOUND", body.Code)
}

func (s *StructuresSuite) Test_should_return_401_when_adding_model_without_token() {
	// given

	// when
	resp := postJSON(s.T(), "/v1/structures/"+uuid.NewString()+"/models", map[string]any{
		"name": "unauthorized model",
	})
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *StructuresSuite) Test_should_return_400_when_added_model_is_invalid() {
	// given
	token := issueStructureTokenForTest(s.T(), "model-add-invalid-token")
	structureID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":   structureID,
		"name": "structure-add-model-invalid-" + uuid.NewString(),
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	modelsPath := "/v1/structures/" + structureID.String() + "/models"
	foreignEntityID := uuid.New()

	tests := []struct {
		name    string
		request func() map[string]any
		message string
	}{
		{
			name: "missing model name",
			request: func() map[string]any {
				return map[string]any{"name": "   "}
			},
			message: "model name is required",
		},
		{
			name: "relation to an entity outside the structure",
			request: func() map[string]any {
				modelEntityID := uuid.New()
				return map[string]any{
					"name": "model with a foreign relation",
					"entities": []map[string]any{
						modelEntityRequest(modelEntityID, "model entity"),
					},
					"relations": []map[string]any{
						{
							"source_entity_id": foreignEntityID,
							"target_entity_id": modelEntityID,
							"relation_type":    "input_to",
						},
					},
				}
			},
			message: "is not part of this structure",
		},
		{
			name: "entity id already used by the structure",
			request: func() map[string]any {
				reusedEntityID := uuid.New()
				reuseStructureID := uuid.New()
				reuseResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
					"id":   reuseStructureID,
					"name": "structure-add-model-reuse-" + uuid.NewString(),
					"entities": []map[string]any{
						dataEntityRequest(reusedEntityID, "already deposited"),
					},
				}, token)
				defer reuseResp.Body.Close()
				s.Require().Equal(http.StatusCreated, reuseResp.StatusCode)

				return map[string]any{
					"path": "/v1/structures/" + reuseStructureID.String() + "/models",
					"name": "model reusing an entity id",
					"entities": []map[string]any{
						dataEntityRequest(reusedEntityID, "duplicate id"),
					},
				}
			},
			message: "duplicate entity id",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// when
			request := tt.request()
			path := modelsPath
			if override, ok := request["path"].(string); ok {
				path = override
				delete(request, "path")
			}
			resp := postJSONWithToken(s.T(), path, request, token)
			defer resp.Body.Close()

			// then
			s.Equal(http.StatusBadRequest, resp.StatusCode)

			var body httpapi.Error
			s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
			s.Equal("BAD_REQUEST", body.Code)
			s.Contains(body.Message, tt.message)
		})
	}
}

func (s *StructuresSuite) Test_should_return_403_when_non_creator_deletes_structure() {
	// given
	ownerToken := issueStructureTokenForTest(s.T(), "structure-delete-forbidden-owner")
	otherToken := issueStructureTokenForGitHubIDForTest(s.T(), "structure-delete-forbidden-other", 8101)
	structureID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":   structureID,
		"name": "structure-delete-forbidden-" + uuid.NewString(),
	}, ownerToken)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/structures/"+structureID.String(), otherToken)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusForbidden, deleteResp.StatusCode)

	getStructureResp := getWithToken(s.T(), "/v1/structures/"+structureID.String(), ownerToken)
	defer getStructureResp.Body.Close()
	s.Equal(http.StatusOK, getStructureResp.StatusCode)
}

func (s *StructuresSuite) Test_should_delete_model_when_caller_is_creator() {
	// given
	token := issueStructureTokenForTest(s.T(), "model-delete-owner-token")
	structureID := uuid.New()
	modelID := uuid.New()
	modelEntityID := uuid.New()
	searchToken := "modeldelete" + strings.ReplaceAll(uuid.NewString(), "-", "")
	structureName := "structure-kept-after-model-delete-" + uuid.NewString()
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":   structureID,
		"name": structureName,
		"models": []map[string]any{
			{
				"id":   modelID,
				"name": "model " + searchToken,
				"entities": []map[string]any{
					modelEntityRequest(modelEntityID, "entity "+searchToken),
				},
			},
		},
	}, token)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models/"+modelID.String(), token)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusNoContent, deleteResp.StatusCode)
	body, err := io.ReadAll(deleteResp.Body)
	s.Require().NoError(err)
	s.Empty(body)

	getStructureResp := getWithToken(s.T(), "/v1/structures/"+structureID.String(), token)
	defer getStructureResp.Body.Close()
	s.Equal(http.StatusOK, getStructureResp.StatusCode)

	getModelResp := getWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models/"+modelID.String(), token)
	defer getModelResp.Body.Close()
	s.Equal(http.StatusNotFound, getModelResp.StatusCode)

	entitiesResp := getWithToken(s.T(), "/v1/structures/"+structureID.String()+"/entities", token)
	defer entitiesResp.Body.Close()
	s.Equal(http.StatusOK, entitiesResp.StatusCode)
	var entitiesBody httpapi.EntityListResponse
	s.Require().NoError(json.NewDecoder(entitiesResp.Body).Decode(&entitiesBody))
	s.Nil(entityByID(entitiesBody.Items, modelEntityID))

	searchResp := getWithToken(s.T(), "/v1/structures?query="+url.QueryEscape(searchToken), "")
	defer searchResp.Body.Close()
	s.Equal(http.StatusOK, searchResp.StatusCode)
	var searchBody httpapi.StructureListResponse
	s.Require().NoError(json.NewDecoder(searchResp.Body).Decode(&searchBody))
	s.Nil(structureByName(searchBody.Items, structureName))
}

func (s *StructuresSuite) Test_should_return_403_when_non_creator_deletes_model() {
	// given
	ownerToken := issueStructureTokenForTest(s.T(), "model-delete-forbidden-owner")
	otherToken := issueStructureTokenForGitHubIDForTest(s.T(), "model-delete-forbidden-other", 8102)
	structureID := uuid.New()
	modelID := uuid.New()
	createResp := postJSONWithToken(s.T(), "/v1/structures", map[string]any{
		"id":   structureID,
		"name": "structure-with-forbidden-model-delete-" + uuid.NewString(),
		"models": []map[string]any{
			{
				"id":   modelID,
				"name": "model kept after forbidden delete",
			},
		},
	}, ownerToken)
	defer createResp.Body.Close()
	s.Require().Equal(http.StatusCreated, createResp.StatusCode)

	// when
	deleteResp := deleteWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models/"+modelID.String(), otherToken)
	defer deleteResp.Body.Close()

	// then
	s.Equal(http.StatusForbidden, deleteResp.StatusCode)

	getModelResp := getWithToken(s.T(), "/v1/structures/"+structureID.String()+"/models/"+modelID.String(), ownerToken)
	defer getModelResp.Body.Close()
	s.Equal(http.StatusOK, getModelResp.StatusCode)
}

func (s *StructuresSuite) Test_should_return_400_when_create_structure_graph_is_invalid() {
	// given
	token := issueStructureTokenForTest(s.T(), "structure-validation-token")

	tests := []struct {
		name    string
		request func() map[string]any
		message string
	}{
		{
			name: "nil structure id",
			request: func() map[string]any {
				return map[string]any{
					"id":   uuid.Nil,
					"name": "invalid nil id",
				}
			},
			message: "structure id is required",
		},
		{
			name: "duplicate structure entity id",
			request: func() map[string]any {
				entityID := uuid.New()
				return map[string]any{
					"name": "duplicate structure entity",
					"entities": []map[string]any{
						dataEntityRequest(entityID, "first duplicate entity"),
						dataEntityRequest(entityID, "second duplicate entity"),
					},
				}
			},
			message: "duplicate entity id",
		},
		{
			name: "duplicate entity id across structure and model",
			request: func() map[string]any {
				entityID := uuid.New()
				return map[string]any{
					"name":     "duplicate graph entity",
					"entities": []map[string]any{dataEntityRequest(entityID, "structure entity")},
					"models": []map[string]any{
						{
							"name":     "model one",
							"entities": []map[string]any{modelEntityRequest(entityID, "model entity")},
						},
					},
				}
			},
			message: "duplicate entity id",
		},
		{
			name: "empty model name",
			request: func() map[string]any {
				return map[string]any{
					"name": "empty model",
					"models": []map[string]any{
						{"name": "   "},
					},
				}
			},
			message: "model name is required",
		},
		{
			name: "empty entity name",
			request: func() map[string]any {
				return map[string]any{
					"name":     "empty entity",
					"entities": []map[string]any{dataEntityRequest(uuid.New(), "   ")},
				}
			},
			message: "entity name is required",
		},
		{
			name: "invalid metrics payload",
			request: func() map[string]any {
				return map[string]any{
					"name": "invalid metrics payload",
					"models": []map[string]any{
						{
							"name": "model with invalid metrics",
							"entities": []map[string]any{
								{
									"id":      uuid.New(),
									"type":    "metrics",
									"level":   "L3",
									"name":    "bad metrics",
									"payload": map[string]any{"r_free": "not-a-number"},
								},
							},
						},
					},
				}
			},
			message: "decode metrics payload",
		},
		{
			name: "unexpected entity type",
			request: func() map[string]any {
				return map[string]any{
					"name": "unexpected entity type",
					"entities": []map[string]any{
						{
							"id":      uuid.New(),
							"type":    "unknown",
							"name":    "unknown entity",
							"payload": map[string]any{},
						},
					},
				}
			},
			message: "unexpected entity type",
		},
		{
			name: "relation self reference",
			request: func() map[string]any {
				entityID := uuid.New()
				return map[string]any{
					"name": "self relation",
					"models": []map[string]any{
						{
							"name":     "model with self relation",
							"entities": []map[string]any{modelEntityRequest(entityID, "self related model")},
							"relations": []map[string]any{
								{
									"source_entity_id": entityID,
									"target_entity_id": entityID,
									"relation_type":    "output_of",
								},
							},
						},
					},
				}
			},
			message: "must be different",
		},
		{
			name: "relation source missing",
			request: func() map[string]any {
				targetID := uuid.New()
				return map[string]any{
					"name": "missing relation source",
					"models": []map[string]any{
						{
							"name":     "model with missing relation source",
							"entities": []map[string]any{modelEntityRequest(targetID, "target model")},
							"relations": []map[string]any{
								{
									"source_entity_id": uuid.New(),
									"target_entity_id": targetID,
									"relation_type":    "output_of",
								},
							},
						},
					},
				}
			},
			message: "relation source entity is not part of this structure",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// when
			resp := postJSONWithToken(s.T(), "/v1/structures", tt.request(), token)
			defer resp.Body.Close()

			// then
			s.Equal(http.StatusBadRequest, resp.StatusCode)

			var body httpapi.Error
			s.Require().NoError(json.NewDecoder(resp.Body).Decode(&body))
			s.Equal("BAD_REQUEST", body.Code)
			s.Contains(body.Message, tt.message)
		})
	}
}

func issueStructureTokenForTest(t *testing.T, accessToken string) string {
	t.Helper()

	return issueStructureTokenForGitHubIDForTest(t, accessToken, 8000)
}

func issueStructureTokenForGitHubIDForTest(t *testing.T, accessToken string, githubID int64) string {
	t.Helper()

	githubClient.On("GetUser", mock.Anything, accessToken).
		Return(github.User{ID: githubID, Login: accessToken, Name: "Structure Test", Email: accessToken + "@example.com"}, nil)
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

func readFastaMetadataForTest(t *testing.T, path string) map[string]any {
	t.Helper()

	body, err := os.ReadFile(path)
	require.NoError(t, err)

	var sequence strings.Builder
	var pdbID string
	chainIDs := make([]string, 0, 4)

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			if pdbID == "" {
				fields := strings.Fields(strings.TrimPrefix(line, ">"))
				require.NotEmpty(t, fields)
				pdbID = strings.Split(fields[0], "_")[0]
			}
			chainIDs = append(chainIDs, chainIDsFromFastaHeader(line)...)
			continue
		}
		sequence.WriteString(line)
	}

	require.NotEmpty(t, pdbID)
	require.NotEmpty(t, chainIDs)
	require.NotEmpty(t, sequence.String())

	return map[string]any{
		"pdb_id":    pdbID,
		"chains":    len(chainIDs),
		"chain_ids": chainIDs,
		"length":    sequence.Len(),
		"organism":  "Homo sapiens",
		"sequence":  sequence.String(),
	}
}

func chainIDsFromFastaHeader(header string) []string {
	for _, segment := range strings.Split(header, "|") {
		segment = strings.TrimSpace(segment)
		if !strings.HasPrefix(segment, "Chains ") {
			continue
		}

		chainList := strings.TrimPrefix(segment, "Chains ")
		chainIDs := make([]string, 0)
		for _, chainID := range strings.Split(chainList, ",") {
			chainID = strings.TrimSpace(chainID)
			if chainID != "" {
				chainIDs = append(chainIDs, chainID)
			}
		}
		return chainIDs
	}

	return nil
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

func structureByName(structures []httpapi.Structure, name string) *httpapi.Structure {
	for i := range structures {
		if structures[i].Name == name {
			return &structures[i]
		}
	}
	return nil
}

func entityByID(entities []httpapi.Entity, id uuid.UUID) *httpapi.Entity {
	for i := range entities {
		if entities[i].Id == id {
			return &entities[i]
		}
	}
	return nil
}

func dataEntityRequest(id uuid.UUID, name string) map[string]any {
	return map[string]any{
		"id":    id,
		"type":  "data",
		"level": "L0",
		"name":  name,
		"payload": map[string]any{
			"file_url": "s3://dynamic-pdb/test/" + id.String() + ".fasta",
			"type":     "fasta",
		},
	}
}

func modelEntityRequest(id uuid.UUID, name string) map[string]any {
	return map[string]any{
		"id":    id,
		"type":  "model",
		"level": "L2",
		"name":  name,
		"payload": map[string]any{
			"file_url": "s3://dynamic-pdb/test/" + id.String() + ".cif",
		},
	}
}
