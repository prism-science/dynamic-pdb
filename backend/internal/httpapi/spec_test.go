package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/api"
)

func Test_should_return_openapi_yaml_when_openapi_description_is_called(t *testing.T) {
	// given
	handler := MediaTypeMiddleware()(Handler((*Server)(nil)))
	request := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	request.Header.Set("Accept", "application/yaml")
	recorder := httptest.NewRecorder()

	// when
	handler.ServeHTTP(recorder, request)

	// then
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, SpecMediaType, recorder.Header().Get("Content-Type"))
	assert.Equal(t, string(api.Spec), recorder.Body.String())
}
