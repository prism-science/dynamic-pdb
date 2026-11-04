package e2etest

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/httpapi"
)

// The deposition forms upload a file before the entry or model exists, so it has no artifact
// row yet. POST /v1/files/extract reads such an upload by its artifact ID and object URL.

// putUploadForTest stores contents where an upload of artifactID would put them, and returns
// the object URL the upload would return.
func (s *ArtifactExtractSuite) putUploadForTest(artifactID uuid.UUID, fixture string, filename string) string {
	contents, err := os.ReadFile(filepath.Join("..", "..", "lib", "cif", "testdata", fixture))
	s.Require().NoError(err)
	objectPath := "/dynamic-pdb/entry-" + artifactID.String() + "/artifacts/" + artifactID.String() + "/" + filename
	s3Stub.PutObject(objectPath, contents)
	return s3Stub.URL() + objectPath
}

func (s *ArtifactExtractSuite) Test_should_return_header_values_with_file_source_when_upload_is_extracted_before_it_is_saved() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "upload-extract-owner", 8450)
	artifactID := uuid.New()
	objectURL := s.putUploadForTest(artifactID, "193L.cif", "model.cif")

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := postJSONWithToken(s.T(), "/v1/files/extract", map[string]any{
		"artifact_id": artifactID, "object_url": objectURL, "sha256": testArtifactSHA256,
	}, token)

	// then
	document := decodeJSONResponse[httpapi.ArtifactExtractionDocument](s.T(), response, http.StatusOK)
	s.Equal(artifactID, document.Data.Id)
	attributes := document.Data.Attributes
	s.Equal("file:"+testArtifactSHA256, attributes.Source)
	values := map[string]any{}
	for _, value := range attributes.Values {
		s.Equal("file:"+testArtifactSHA256, value.Source, value.Name)
		values[value.Name] = value.Value
	}
	s.Equal("193L", values["entry_id"])
	s.Equal([]any{"Vaney, M.C.", "Maignan, S.", "Ries-Kautt, M.", "Ducruix, A."}, values["authors"])
	s.InDelta(0.184, values["r_work"], 1e-9)
	s.InDelta(0.226, values["r_free"], 1e-9)
	s.InDelta(1.33, values["resolution"], 1e-9)
	s.Equal("X-RAY DIFFRACTION", values["method"])
	s.Equal("P 43 21 2", values["space_group"])
	s.Contains(values, "atom_count")
}

func (s *ArtifactExtractSuite) Test_should_reject_upload_extraction_when_url_or_checksum_is_wrong() {
	// given
	token := issueEntryTokenForGitHubIDForTest(s.T(), "upload-extract-bad-owner", 8451)
	artifactID := uuid.New()
	objectURL := s.putUploadForTest(artifactID, "193L.cif", "model.cif")
	pdbURL := s.putUploadForTest(artifactID, "1A62.pdb", "model.pdb")
	cases := map[string]struct {
		body   map[string]any
		status int
		detail string
	}{
		"another artifact's folder": {
			body:   map[string]any{"artifact_id": uuid.New(), "object_url": objectURL, "sha256": testArtifactSHA256},
			status: http.StatusUnprocessableEntity,
			detail: "artifact URI is not an uploaded file of this artifact",
		},
		"not an mmCIF file": {
			body:   map[string]any{"artifact_id": artifactID, "object_url": pdbURL, "sha256": testArtifactSHA256},
			status: http.StatusUnprocessableEntity,
			detail: "only mmCIF files can be extracted",
		},
		"checksum not lowercase hexadecimal": {
			body:   map[string]any{"artifact_id": artifactID, "object_url": objectURL, "sha256": "ABC"},
			status: http.StatusUnprocessableEntity,
			detail: "sha256 must be 64 lowercase hexadecimal characters",
		},
		"nothing uploaded at the URL": {
			body: map[string]any{
				"artifact_id": artifactID, "object_url": s3Stub.URL() + "/dynamic-pdb/entry-x/artifacts/" + artifactID.String() + "/missing.cif",
				"sha256": testArtifactSHA256,
			},
			status: http.StatusNotFound,
			detail: "nothing is stored at the artifact URI",
		},
	}

	for name, testCase := range cases {
		s.Run(name, func() {
			// when
			//nolint:bodyclose // assertErrorResponse closes the response body.
			response := postJSONWithToken(s.T(), "/v1/files/extract", testCase.body, token)

			// then
			s.Equal(testCase.status, response.StatusCode)
			assertErrorResponse(s.T(), response, errorCodeForStatus(testCase.status), testCase.detail)
		})
	}
}

func errorCodeForStatus(status int) string {
	switch status {
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusUnprocessableEntity:
		return "UNPROCESSABLE_ENTITY"
	default:
		panic("errorCodeForStatus: no error code for status " + http.StatusText(status))
	}
}

func (s *ArtifactExtractSuite) Test_should_return_401_when_upload_extraction_has_no_token() {
	// when
	response := postJSON(s.T(), "/v1/files/extract", map[string]any{
		"artifact_id": uuid.New(), "object_url": "https://example.org/a.cif", "sha256": testArtifactSHA256,
	})
	defer func() { s.Require().NoError(response.Body.Close()) }()

	// then
	s.Equal(http.StatusUnauthorized, response.StatusCode)
}
