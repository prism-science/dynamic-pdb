package httpapi

import (
	"net/http"
	"strings"
)

// JSONAPIMediaType is the media type used by JSON:API documents.
const JSONAPIMediaType = "application/vnd.api+json"

func MediaTypeMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == SpecPath {
				next.ServeHTTP(w, r)
				return
			}

			if requestHasBody(r) && !isJSONAPIContentType(r.Header.Get("Content-Type")) {
				writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "request Content-Type must be application/vnd.api+json")
				return
			}

			if !acceptsJSONAPIMediaType(r.Header.Values("Accept")) {
				writeError(w, http.StatusNotAcceptable, "NOT_ACCEPTABLE", "request Accept header must allow application/vnd.api+json")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func requestHasBody(r *http.Request) bool {
	if r.Body == nil || r.Body == http.NoBody {
		return false
	}
	if r.ContentLength == 0 {
		return false
	}
	return true
}

func isJSONAPIContentType(value string) bool {
	return strings.TrimSpace(value) == JSONAPIMediaType
}

func acceptsJSONAPIMediaType(values []string) bool {
	if len(values) == 0 {
		return true
	}

	seenValue := false

	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			mediaType := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
			if mediaType == "" {
				continue
			}
			seenValue = true
			if mediaType == JSONAPIMediaType || mediaType == "*/*" {
				return true
			}
		}
	}

	return !seenValue
}

const (
	// #nosec G101 -- JSON:API type name, not a credential.
	jsonAPITypeAuthTokens             = "auth_tokens"
	jsonAPITypeFileUploads            = "file_uploads"
	jsonAPITypeEntryRevisionResults   = "entry_revision_results"
	jsonAPITypeModelRevisionResults   = "model_revision_results"
	jsonAPITypeEntries                = "entries"
	jsonAPITypeSimilarEntries         = "similar_entries"
	jsonAPITypeModels                 = "models"
	jsonAPITypeArtifacts              = "artifacts"
	jsonAPITypeEntryRevisionGroups    = "entry_revision_groups"
	jsonAPITypeEntryRevisionSummaries = "entry_revision_summaries"
	jsonAPITypeModelRevisionSummaries = "model_revision_summaries"
	jsonAPITypeEntryRevisions         = "entry_revisions"
	jsonAPITypeModelRevisions         = "model_revisions"
)
