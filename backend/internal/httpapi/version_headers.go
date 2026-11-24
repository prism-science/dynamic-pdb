package httpapi

import "net/http"

// The response headers that name the versions a response follows: the API's (openapi.yaml
// info.version) and the Dynamic PDB Schema's (info.x-dynamic-pdb-schema-version).
const (
	APIVersionHeader    = "Dynamic-PDB-API-Version"
	SchemaVersionHeader = "Dynamic-PDB-Schema-Version"
)

// VersionHeaders are the headers a cross-origin page may read, for the CORS setup.
var VersionHeaders = []string{APIVersionHeader, SchemaVersionHeader}

// VersionHeadersMiddleware sets both version headers on every response, errors included. The
// versions come from the embedded spec (api.Versions), read once at startup.
func VersionHeadersMiddleware(apiVersion, schemaVersion string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(APIVersionHeader, apiVersion)
			w.Header().Set(SchemaVersionHeader, schemaVersion)
			next.ServeHTTP(w, r)
		})
	}
}
