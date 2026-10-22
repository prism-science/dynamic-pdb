package e2etest

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/httpapi"
)

const archiveFixtureDirectory = "../../lib/archive/testdata"

// serveArchiveFixture answers RCSB core API requests for 4HHB from the recorded fixtures.
func serveArchiveFixture(w http.ResponseWriter, r *http.Request) {
	fixtures := map[string]string{
		"/rest/v1/core/entry/4HHB":            "entry_4HHB.json",
		"/rest/v1/core/polymer_entity/4HHB/1": "polymer_entity_4HHB_1.json",
		"/rest/v1/core/polymer_entity/4HHB/2": "polymer_entity_4HHB_1.json",
	}
	name, ok := fixtures[strings.TrimSpace(r.URL.Path)]
	if !ok {
		http.NotFound(w, r)
		return
	}
	contents, err := os.ReadFile(filepath.Join(archiveFixtureDirectory, name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(contents); err != nil {
		slog.Error("write archive fixture response", "error", err)
	}
}

type ArchiveSuite struct {
	baseSuite
}

func TestArchive(t *testing.T) {
	suite.Run(t, new(ArchiveSuite))
}

func (s *ArchiveSuite) Test_should_return_401_when_archive_lookup_has_no_token() {
	// given / when
	response := getWithToken(s.T(), "/v1/archive/pdb/4HHB", "")
	defer response.Body.Close()

	// then
	s.Equal(http.StatusUnauthorized, response.StatusCode)
}

func (s *ArchiveSuite) Test_should_return_proposed_values_when_pdb_entry_exists() {
	// given / when
	response := getWithToken(s.T(), "/v1/archive/pdb/4hhb", adminToken)
	defer response.Body.Close()

	// then
	s.Require().Equal(http.StatusOK, response.StatusCode)
	var document httpapi.ArchivePdbDocument
	require.NoError(s.T(), json.NewDecoder(response.Body).Decode(&document))
	s.Equal("4HHB", document.Data.Id)
	s.Equal([]string{"1HHB"}, document.Data.Attributes.Entry.Supersedes)
	s.Len(document.Data.Attributes.PolymerEntities, 2)
}

func (s *ArchiveSuite) Test_should_return_404_when_rcsb_has_no_entry() {
	// given / when
	response := getWithToken(s.T(), "/v1/archive/pdb/9ZZZ", adminToken)
	defer response.Body.Close()

	// then
	s.Equal(http.StatusNotFound, response.StatusCode)
}
