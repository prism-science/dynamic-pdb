package e2etest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
)

// EntryMethodSuite covers the depositor method list: a create or entry revision that
// sends a method must use models.StructureMethods, and a create may keep the archive's
// method for the PDB ID it names. It also covers the other create refusal the New entry
// form shows beside a field: a PDB ID another active entry has.
type EntryMethodSuite struct {
	baseSuite
}

func TestEntryMethod(t *testing.T) {
	suite.Run(t, new(EntryMethodSuite))
}

func (s *EntryMethodSuite) Test_should_refuse_a_method_off_the_list_when_an_entry_is_created() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "method-list-create-owner", 8951)

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{"title": "entry with a typed method", "method": "X-ray diffraction"},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)
	s.Equal([]keyErrorResponse{{Pointer: "/entry/method", Code: "INVALID_VALUE"}},
		keyErrorsFromResponse(s.T(), errors))
	s.Equal(`method "X-ray diffraction" is not one of "X-ray crystallography", "CryoEM"`,
		stringValue(errors[0].Detail))
}

func (s *EntryMethodSuite) Test_should_keep_the_archive_method_when_an_entry_for_its_pdb_id_is_created() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "method-list-archive-owner", 8952)

	// when
	created := createEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{
			"title":         "entry that keeps the archive method",
			"method":        "NEUTRON DIFFRACTION",
			"external_refs": map[string]string{"pdb": neutronArchivePDBID},
		},
	})

	// then
	revisions, err := testDatabase.Entries.List(s.T().Context(), db.EntryRevisionFilters{EntryID: &created.EntryId})
	s.Require().NoError(err)
	s.Require().Len(revisions, 1)
	s.Require().NotNil(revisions[0].Metadata.Method)
	s.Equal(models.StructureMethod("NEUTRON DIFFRACTION"), *revisions[0].Metadata.Method)
	s.Equal(models.FieldSourceRCSB, revisions[0].FieldSources["/metadata/method"].Source)
}

func (s *EntryMethodSuite) Test_should_refuse_a_method_that_differs_from_the_archive_when_an_entry_for_its_pdb_id_is_created() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "method-list-archive-typed-owner", 8953)

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{
			"title":         "entry with a method the archive does not give",
			"method":        "Neutron diffraction",
			"external_refs": map[string]string{"pdb": neutronArchivePDBID},
		},
	}, ownerToken)

	// then
	s.Equal([]keyErrorResponse{{Pointer: "/entry/method", Code: "INVALID_VALUE"}},
		keyErrorsFromResponse(s.T(), decodeErrorResponseForTest(s.T(), response, http.StatusUnprocessableEntity)))
}

func (s *EntryMethodSuite) Test_should_refuse_a_method_off_the_list_when_an_entry_revision_sends_it() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "method-list-revision-owner", 8954)
	entry := createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry whose method is changed"},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	refused := postJSONWithToken(s.T(), "/v1/entries/"+entry.EntryId+"/revisions", map[string]any{
		"entry": map[string]any{"method": "NEUTRON DIFFRACTION"},
	}, ownerToken)
	accepted := createEntryRevisionForTest(s.T(), ownerToken, entry.EntryId, map[string]any{
		"entry": map[string]any{"method": "CryoEM"},
	})

	// then
	s.Equal([]keyErrorResponse{{Pointer: "/entry/method", Code: "INVALID_VALUE"}},
		keyErrorsFromResponse(s.T(), decodeErrorResponseForTest(s.T(), refused, http.StatusUnprocessableEntity)))
	s.NotEmpty(accepted.RevisionId)
}

func (s *EntryMethodSuite) Test_should_store_a_method_off_the_list_when_mode_is_log_only() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "method-list-log-only-owner", 8955)

	// when
	//nolint:bodyclose // decodeJSONResponse closes the response body.
	response := jsonRequestToServerWithToken(s.T(), logOnlyTestServer.URL, http.MethodPost, "/v1/entries", map[string]any{
		"entry": map[string]any{"title": "entry stored in log-only mode", "method": "X-ray diffraction"},
	}, ownerToken)

	// then
	decodeJSONResponse[map[string]any](s.T(), response, http.StatusCreated)
}

func (s *EntryMethodSuite) Test_should_point_to_the_pdb_id_when_another_active_entry_has_it() {
	// given
	ownerToken := issueEntryTokenForGitHubIDForTest(s.T(), "pdb-id-taken-owner", 8956)
	pdbID := unusedPDBIDForTest(s.T())
	createAndActivateEntryForTest(s.T(), ownerToken, map[string]any{
		"entry": map[string]any{"title": "entry that has the PDB ID", "external_refs": map[string]string{"pdb": pdbID}},
	})

	// when
	//nolint:bodyclose // decodeErrorResponseForTest closes the response body.
	response := postJSONWithToken(s.T(), "/v1/entries", map[string]any{
		"entry": map[string]any{
			"title":         "second entry for the PDB ID",
			"method":        "X-ray crystallography",
			"external_refs": map[string]string{"pdb": pdbID},
		},
	}, ownerToken)

	// then
	errors := decodeErrorResponseForTest(s.T(), response, http.StatusConflict)
	s.Equal([]keyErrorResponse{{Pointer: "/entry/external_refs/pdb", Code: "CONFLICT"}},
		keyErrorsFromResponse(s.T(), errors))
	s.Equal("another entry already has this PDB ID; add your models to that entry instead",
		stringValue(errors[0].Detail))
}
