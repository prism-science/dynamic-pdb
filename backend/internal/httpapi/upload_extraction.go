package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// uploadExtractionOffCode is the error code of the 404 the endpoint answers while it is
// turned off, so the forms can tell it from a file that is not stored.
const uploadExtractionOffCode = "EXTRACTION_OFF"

// sha256Pattern is a SHA-256 in lowercase hexadecimal, as artifacts store it.
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// WithUploadExtraction turns POST /v1/files/extract on. The deposition forms call it to show
// the values of an uploaded mmCIF before the entry or model is saved; off, it answers 404 and
// the forms keep their own preview.
func (s *Server) WithUploadExtraction(enabled bool) *Server {
	s.uploadExtractionEnabled = enabled
	return s
}

// ExtractUploadedFile extracts the values of a file uploaded under an artifact ID that has no
// artifact yet. ReadArtifactHead reads only an object inside that artifact's folder, so the
// caller must name the URL the upload returned. Uploads record no owner, so any signed-in user
// may extract an object whose artifact ID and URL they know; those objects already have public
// URLs. The per-user and per-instance limits are the artifact endpoint's.
func (s *Server) ExtractUploadedFile(w http.ResponseWriter, r *http.Request) {
	if !s.uploadExtractionEnabled {
		writeError(w, http.StatusNotFound, uploadExtractionOffCode, "extracting uploaded files before they are saved is turned off")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	var request ExtractUploadedFileRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if s.extractionLimits.userLimited(w, r, user.ID.String()) {
		writeError(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "too many extraction requests: wait before extracting again")
		return
	}
	if problem := uploadExtractionProblem(request); problem != "" {
		writeError(w, http.StatusUnprocessableEntity, "UNPROCESSABLE_ENTITY", problem)
		return
	}
	s.writeObjectExtraction(w, r, request.ArtifactId, strings.TrimSpace(request.ObjectUrl), "file:"+request.Sha256)
}

// uploadExtractionProblem says why an upload cannot be extracted, or "" when it can. That the
// URL lies in the artifact's folder is checked where the object is read.
func uploadExtractionProblem(request ExtractUploadedFileRequest) string {
	if request.ArtifactId == uuid.Nil {
		return "artifact_id is required"
	}
	if !sha256Pattern.MatchString(request.Sha256) {
		return "sha256 must be 64 lowercase hexadecimal characters"
	}
	parsed, err := url.Parse(strings.TrimSpace(request.ObjectUrl))
	if err != nil || parsed.Path == "" {
		return "object_url is not a URL"
	}
	if !isMMCIFFormat(strings.TrimPrefix(path.Ext(parsed.Path), ".")) {
		return "only mmCIF files can be extracted"
	}
	return ""
}
