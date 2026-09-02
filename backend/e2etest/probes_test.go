package e2etest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

type ProbesSuite struct {
	baseSuite
}

func TestProbes(t *testing.T) {
	suite.Run(t, new(ProbesSuite))
}

func (s *ProbesSuite) Test_should_return_200_when_livez_called_without_auth() {
	// given / when
	resp := getProbe(s.T(), "/livez")
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusOK, resp.StatusCode)
	s.Equal("ok", decodeProbe(s.T(), resp).Status)
}

func (s *ProbesSuite) Test_should_return_200_when_readyz_called_without_auth() {
	// given / when
	resp := getProbe(s.T(), "/readyz")
	defer resp.Body.Close()

	// then
	s.Equal(http.StatusOK, resp.StatusCode)
	s.Equal("ok", decodeProbe(s.T(), resp).Status)
}

func getProbe(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(testServer.URL + path)
	require.NoError(t, err)
	return resp
}

func decodeProbe(t *testing.T, resp *http.Response) httpapi.ProbeMeta {
	t.Helper()
	var document httpapi.ProbeDocument
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&document))
	return document.Meta
}
