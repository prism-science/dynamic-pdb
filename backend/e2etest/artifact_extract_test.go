package e2etest

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
)

const testArtifactSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type ArtifactExtractSuite struct {
	baseSuite
}

func TestArtifactExtract(t *testing.T) {
	suite.Run(t, new(ArtifactExtractSuite))
}

func (s *ArtifactExtractSuite) storeArtifact(createdBy uuid.UUID, fixture string, format string) models.Artifact {
	contents, err := os.ReadFile(filepath.Join("..", "..", "lib", "cif", "testdata", fixture))
	s.Require().NoError(err)
	artifactID := uuid.New()
	objectPath := "/dynamic-pdb/entry-" + artifactID.String() + "/artifacts/" + artifactID.String() + "/model.cif"
	s3Stub.PutObject(objectPath, contents)
	uri := s3Stub.URL() + objectPath
	sha256 := testArtifactSHA256
	size := int64(len(contents))
	created, err := testDatabase.Artifacts.Create(s.T().Context(), models.Artifact{
		ID:        artifactID,
		Name:      "model.cif",
		Level:     models.ArtifactLevelL0,
		Type:      models.ArtifactTypeModel,
		URI:       &uri,
		SHA256:    &sha256,
		Format:    &format,
		SizeBytes: &size,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
	})
	s.Require().NoError(err)
	return *created
}

func (s *ArtifactExtractSuite) userIDFromToken(token string) uuid.UUID {
	userID, err := auth.NewJWT(testJWTSecret, testJWTIssuer, testJWTTTL).Parse(token)
	s.Require().NoError(err)
	return userID
}

func (s *ArtifactExtractSuite) Test_should_return_values_tagged_with_file_source_when_creator_extracts_artifact() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "extract-owner", 8101)
	artifact := s.storeArtifact(s.userIDFromToken(token), "6OIM.cif", "mmcif")

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+artifact.ID.String()+"/extract", nil, token)
	document := decodeJSONResponse[httpapi.ArtifactExtractionDocument](s.T(), response, http.StatusOK)

	// then
	attributes := document.Data.Attributes
	s.Equal("file:"+testArtifactSHA256, attributes.Source)
	s.False(attributes.Truncated)
	names := map[string]httpapi.ExtractedValue{}
	for _, value := range attributes.Values {
		s.Equal("file:"+testArtifactSHA256, value.Source, value.Name)
		names[value.Name] = value
	}
	s.EqualValues(134, names["atom_count"].Value)
	s.Contains(names, "mutations")
	s.Contains(names, "cofactors")
	s.Len(attributes.Warnings, 1)
	s.Contains(attributes.Warnings[0].Detail, "reference lists are draft")
}

func (s *ArtifactExtractSuite) Test_should_return_403_when_user_is_neither_creator_nor_reviewer() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "extract-owner-2", 8102)
	otherToken := issueEntryTokenForGitHubIDForTest(s.T(), "extract-other", 8103)
	artifact := s.storeArtifact(s.userIDFromToken(ownerToken), "6OIM.cif", "mmcif")

	// when
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+artifact.ID.String()+"/extract", nil, otherToken)
	defer response.Body.Close()

	// then
	s.Equal(http.StatusForbidden, response.StatusCode)
}

func (s *ArtifactExtractSuite) Test_should_allow_reviewer_when_artifact_belongs_to_someone_else() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "extract-owner-3", 8104)
	artifact := s.storeArtifact(s.userIDFromToken(ownerToken), "3CLN.cif", "cif")

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+artifact.ID.String()+"/extract", nil, adminToken)
	document := decodeJSONResponse[httpapi.ArtifactExtractionDocument](s.T(), response, http.StatusOK)

	// then
	s.NotEmpty(document.Data.Attributes.Values)
}

func (s *ArtifactExtractSuite) Test_should_return_401_when_request_has_no_token() {
	// when
	response := postJSON(s.T(), "/v1/artifacts/"+uuid.NewString()+"/extract", nil)
	defer response.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, response.StatusCode)
}

func (s *ArtifactExtractSuite) Test_should_return_404_when_artifact_does_not_exist() {
	// when
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+uuid.NewString()+"/extract", nil, adminToken)
	defer response.Body.Close()

	// then
	s.Equal(http.StatusNotFound, response.StatusCode)
}

func (s *ArtifactExtractSuite) Test_should_return_422_when_artifact_is_not_mmcif() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "extract-owner-4", 8105)
	artifact := s.storeArtifact(s.userIDFromToken(token), "6OIM.cif", "pdb")

	// when
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+artifact.ID.String()+"/extract", nil, token)
	defer response.Body.Close()

	// then
	s.Equal(http.StatusUnprocessableEntity, response.StatusCode)
}

func (s *ArtifactExtractSuite) Test_should_return_404_when_object_is_missing_from_storage() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "extract-owner-5", 8106)
	artifact := s.storeArtifact(s.userIDFromToken(token), "6OIM.cif", "mmcif")
	missingID := uuid.New()
	missingURI := strings.Replace(*artifact.URI, artifact.ID.String(), missingID.String(), 2)
	missing := s.createArtifactWithURI(artifact, missingID, missingURI)

	// when
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+missing.ID.String()+"/extract", nil, token)
	defer response.Body.Close()

	// then
	s.Equal(http.StatusNotFound, response.StatusCode)
}

func (s *ArtifactExtractSuite) Test_should_return_422_when_uri_points_into_another_artifact_folder() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "extract-owner-6", 8107)
	victim := s.storeArtifact(s.userIDFromToken(token), "6OIM.cif", "mmcif")
	thief := s.createArtifactWithURI(victim, uuid.New(), *victim.URI)

	// when
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+thief.ID.String()+"/extract", nil, token)
	defer response.Body.Close()

	// then
	s.Equal(http.StatusUnprocessableEntity, response.StatusCode)
}

func (s *ArtifactExtractSuite) Test_should_report_truncated_and_keep_values_when_file_exceeds_read_limit() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "extract-owner-7", 8108)
	artifact := s.storeArtifact(s.userIDFromToken(token), "6OIM.cif", "mmcif")
	contents, err := os.ReadFile(filepath.Join("..", "..", "lib", "cif", "testdata", "6OIM.cif"))
	s.Require().NoError(err)
	padding := strings.Repeat("# padding padding padding padding padding padding padding\n", 80_000)
	objectPath := strings.TrimPrefix(*artifact.URI, s3Stub.URL())
	s3Stub.PutObject(objectPath, append(contents, []byte("\n"+padding)...))

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(s.T(), "/v1/artifacts/"+artifact.ID.String()+"/extract", nil, token)
	document := decodeJSONResponse[httpapi.ArtifactExtractionDocument](s.T(), response, http.StatusOK)

	// then
	attributes := document.Data.Attributes
	s.True(attributes.Truncated)
	s.EqualValues(4*1024*1024, attributes.BytesRead)
	s.Greater(attributes.ObjectSize, attributes.BytesRead)
	found := false
	for _, value := range attributes.Values {
		found = found || value.Name == "atom_count"
	}
	s.True(found)
}

func (s *ArtifactExtractSuite) createArtifactWithURI(template models.Artifact, id uuid.UUID, uri string) models.Artifact {
	created, err := testDatabase.Artifacts.Create(s.T().Context(), models.Artifact{
		ID:        id,
		Name:      "other.cif",
		Level:     models.ArtifactLevelL0,
		Type:      models.ArtifactTypeModel,
		URI:       &uri,
		SHA256:    template.SHA256,
		Format:    template.Format,
		CreatedBy: template.CreatedBy,
		CreatedAt: time.Now().UTC(),
	})
	s.Require().NoError(err)
	return *created
}

func (s *ArtifactExtractSuite) Test_should_return_429_with_retry_after_when_user_exceeds_extraction_rate() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "extract-rate-owner", 8110)
	artifact := s.storeArtifact(s.userIDFromToken(token), "6OIM.cif", "mmcif")
	otherToken := issueEntryTokenForGitHubIDForTest(s.T(), "extract-rate-other", 8111)
	otherArtifact := s.storeArtifact(s.userIDFromToken(otherToken), "6OIM.cif", "mmcif")
	path := "/v1/artifacts/" + artifact.ID.String() + "/extract"
	statuses := []int{}

	// when
	for range 4 {
		response := jsonRequestToServerWithToken(s.T(), limitedExtractionServer.URL, http.MethodPost, path, nil, token)
		statuses = append(statuses, response.StatusCode)
		if response.StatusCode == http.StatusTooManyRequests {
			s.NotEmpty(response.Header.Get("Retry-After"))
		}
		s.Require().NoError(response.Body.Close())
	}
	otherResponse := jsonRequestToServerWithToken(s.T(), limitedExtractionServer.URL, http.MethodPost,
		"/v1/artifacts/"+otherArtifact.ID.String()+"/extract", nil, otherToken)
	defer otherResponse.Body.Close()

	// then
	s.Equal([]int{http.StatusOK, http.StatusOK, http.StatusOK, http.StatusTooManyRequests}, statuses)
	s.Equal(http.StatusOK, otherResponse.StatusCode, "another user keeps their own budget")
}

func (s *ArtifactExtractSuite) Test_should_return_503_with_retry_after_when_every_extraction_slot_is_busy() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "extract-busy-owner", 8112)
	first := s.storeArtifact(s.userIDFromToken(token), "6OIM.cif", "mmcif")
	secondToken := issueEntryTokenForGitHubIDForTest(s.T(), "extract-busy-other", 8113)
	second := s.storeArtifact(s.userIDFromToken(secondToken), "3CLN.cif", "mmcif")
	started, release := s3Stub.HoldObjectReads()
	defer release()
	firstDone := make(chan int, 1)
	go func() {
		response := jsonRequestToServerWithToken(s.T(), limitedExtractionServer.URL, http.MethodPost,
			"/v1/artifacts/"+first.ID.String()+"/extract", nil, token)
		firstDone <- response.StatusCode
		_ = response.Body.Close()
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		s.FailNow("the first extraction never reached storage")
	}

	// when
	busy := jsonRequestToServerWithToken(s.T(), limitedExtractionServer.URL, http.MethodPost,
		"/v1/artifacts/"+second.ID.String()+"/extract", nil, secondToken)
	defer busy.Body.Close()
	release()

	// then
	s.Equal(http.StatusServiceUnavailable, busy.StatusCode)
	s.Equal("5", busy.Header.Get("Retry-After"))
	select {
	case status := <-firstDone:
		s.Equal(http.StatusOK, status, "the extraction holding the slot still completes")
	case <-time.After(10 * time.Second):
		s.FailNow("the first extraction never finished")
	}
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	again := jsonRequestToServerWithToken(s.T(), limitedExtractionServer.URL, http.MethodPost,
		"/v1/artifacts/"+second.ID.String()+"/extract", nil, secondToken)
	s.Equal(http.StatusOK, again.StatusCode, "the slot is free again once the first request finished")
	s.Require().NoError(again.Body.Close())
}
