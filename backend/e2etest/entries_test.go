package e2etest

import (
	"encoding/json"
	"io"
	"net/http"
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
				"name":  "5GY3 FASTA sequence",
				"payload": map[string]any{
					"file_url": "https://example.com/5gy3.fasta",
					"type":     "fasta",
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
						"name":  "5GY3 refined model",
						"payload": map[string]any{
							"file_url": "https://example.com/5gy3-refined.cif",
						},
					},
					{
						"id":    metricsEntityID,
						"type":  "metrics",
						"level": "L3",
						"name":  "5GY3 refinement metrics",
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
