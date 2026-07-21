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
	description := "Created from UI"
	authors := []string{"Fermi, G.", "Perutz, M.F."}
	affiliation := "MRC Laboratory of Molecular Biology, Cambridge"
	fastaMetadata := readFastaMetadataForTest(s.T(), "testdata/fasta/pdb_4hhb_human_deoxyhemoglobin.fasta")
	thumbnailImageURL := "https://example.com/entry.png"
	name := "entry-" + uuid.NewString()
	sequenceEntityID := uuid.New()
	programEntityID := uuid.New()
	modelEntityID := uuid.New()
	metricsEntityID := uuid.New()

	// when
	resp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
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
		"experiments": []map[string]any{
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

	entriesResp := getWithToken(s.T(), "/v1/entries", "")
	defer entriesResp.Body.Close()
	s.Equal(http.StatusOK, entriesResp.StatusCode)

	var entriesBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(entriesResp.Body).Decode(&entriesBody))
	entry := entryByName(entriesBody.Items, name)
	s.Require().NotNil(entry)
	s.NotEqual(uuid.Nil, uuid.UUID(entry.Id))
	s.Require().NotNil(entry.Description)
	s.Equal(description, *entry.Description)
	s.Require().NotNil(entry.ThumbnailImageUrl)
	s.Equal(thumbnailImageURL, *entry.ThumbnailImageUrl)

	entryPath := "/v1/entries/" + uuid.UUID(entry.Id).String()
	experimentsResp := getWithToken(s.T(), entryPath+"/experiments", tokenResponse.AccessToken)
	defer experimentsResp.Body.Close()
	s.Equal(http.StatusOK, experimentsResp.StatusCode)

	var experimentsBody httpapi.ExperimentListResponse
	s.Require().NoError(json.NewDecoder(experimentsResp.Body).Decode(&experimentsBody))
	s.Len(experimentsBody.Items, 1)
	s.Equal("X-ray refinement", experimentsBody.Items[0].Name)
	s.Require().NotNil(experimentsBody.Items[0].Description)
	s.Equal("Refinement against crystallographic density", *experimentsBody.Items[0].Description)

	entitiesResp := getWithToken(s.T(), entryPath+"/entities", tokenResponse.AccessToken)
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

func (s *EntriesSuite) Test_should_filter_entries_when_search_text_matches_nested_graph() {
	// given
	githubClient.On("GetUser", mock.Anything, "gh-token").
		Return(github.User{ID: 4202, Login: "search-creator", Name: "Search Creator", Email: "search@example.com"}, nil)
	githubClient.On("ListOrgs", mock.Anything, "gh-token").
		Return([]github.Organization{{ID: 1, Login: "Astera-org"}}, nil)

	tokenResponse := issueTokenForTest(s.T(), "gh-token")
	searchToken := "crambin-" + uuid.NewString()
	affiliationSearchToken := "metadata-lab-" + uuid.NewString()
	modelEntityID := uuid.New()

	createResp := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"name":        "entry " + searchToken,
		"description": "Searchable entry for nested graph tests",
		"experiments": []map[string]any{
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
	textSearchResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape(searchToken), "")
	defer textSearchResp.Body.Close()

	experimentSearchResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape("qFit"), "")
	defer experimentSearchResp.Body.Close()

	entitySearchResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape("model"), "")
	defer entitySearchResp.Body.Close()

	affiliationSearchResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape(affiliationSearchToken), "")
	defer affiliationSearchResp.Body.Close()

	missingResp := getWithToken(s.T(), "/v1/entries?query="+url.QueryEscape("missing-"+searchToken), "")
	defer missingResp.Body.Close()

	// then
	s.Equal(http.StatusOK, textSearchResp.StatusCode)
	s.Equal(http.StatusOK, experimentSearchResp.StatusCode)
	s.Equal(http.StatusOK, entitySearchResp.StatusCode)
	s.Equal(http.StatusOK, affiliationSearchResp.StatusCode)
	s.Equal(http.StatusOK, missingResp.StatusCode)

	var textSearchBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(textSearchResp.Body).Decode(&textSearchBody))
	s.Require().NotNil(entryByName(textSearchBody.Items, "entry "+searchToken))

	var experimentSearchBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(experimentSearchResp.Body).Decode(&experimentSearchBody))
	s.Require().NotNil(entryByName(experimentSearchBody.Items, "entry "+searchToken))

	var entitySearchBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(entitySearchResp.Body).Decode(&entitySearchBody))
	s.Require().NotNil(entryByName(entitySearchBody.Items, "entry "+searchToken))

	var affiliationSearchBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(affiliationSearchResp.Body).Decode(&affiliationSearchBody))
	s.Require().NotNil(entryByName(affiliationSearchBody.Items, "entry "+searchToken))

	var missingBody httpapi.EntryListResponse
	s.Require().NoError(json.NewDecoder(missingResp.Body).Decode(&missingBody))
	s.Nil(entryByName(missingBody.Items, "entry "+searchToken))
}

func (s *EntriesSuite) Test_should_return_401_when_create_entry_called_without_token() {
	// given

	// when
	resp := postJSON(s.T(), "/v1/entries", map[string]string{
		"name": "unauthorized-entry",
	})
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, resp.StatusCode)
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

func entryByName(entries []httpapi.Entry, name string) *httpapi.Entry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

func entityByID(entities []httpapi.Entity, id uuid.UUID) *httpapi.Entity {
	for i := range entities {
		if uuid.UUID(entities[i].Id) == id {
			return &entities[i]
		}
	}
	return nil
}
