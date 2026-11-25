package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_should_set_both_version_headers_on_every_response_errors_included(t *testing.T) {
	// given
	handler := VersionHeadersMiddleware("1.1.0", "0.3.0")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no such entry")
	}))
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/entries/dpdb_missing00", nil))

	// then
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Equal(t, "1.1.0", recorder.Header().Get(APIVersionHeader))
	assert.Equal(t, "0.3.0", recorder.Header().Get(SchemaVersionHeader))
}
