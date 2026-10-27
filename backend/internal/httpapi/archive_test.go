package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
)

type fakeArchiveLookup struct {
	result       archive.Result
	err          error
	requestedID  string
	deadlineLeft time.Duration
}

func (fake *fakeArchiveLookup) Get(ctx context.Context, pdbID string) (archive.Result, error) {
	fake.requestedID = pdbID
	if deadline, ok := ctx.Deadline(); ok {
		fake.deadlineLeft = time.Until(deadline)
	}
	return fake.result, fake.err
}

func archiveRequest(t *testing.T, server *Server, pdbID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/archive/pdb/"+pdbID, nil)
	recorder := httptest.NewRecorder()
	server.GetArchivePdb(recorder, request, pdbID)
	return recorder
}

func decodeArchiveDocument(t *testing.T, recorder *httptest.ResponseRecorder) ArchivePdbDocument {
	t.Helper()
	var document ArchivePdbDocument
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &document), recorder.Body.String())
	return document
}

func Test_should_return_proposed_values_with_provenance_when_pdb_entry_exists(t *testing.T) {
	// given
	lookup := &fakeArchiveLookup{result: archive.Result{
		PDBID: "4HHB",
		Entry: archive.EntryMapping{
			Annotations: archive.Annotations{
				Provenance: map[string]string{"resolution": "rcsb_entry_info.resolution_combined[0]"},
				Conflicts:  []archive.Conflict{{Field: "resolution", Chosen: "1.74", Others: []string{"2"}}},
			},
			Resolution:    new(1.74),
			LatestVersion: new("4.3"),
			Supersedes:    []string{"1HHB"},
		},
	}}
	server := (&Server{}).WithArchiveLookup(lookup)

	// when
	recorder := archiveRequest(t, server, "4hhb")

	// then
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, JSONAPIMediaType, recorder.Header().Get("Content-Type"))
	document := decodeArchiveDocument(t, recorder)
	assert.Equal(t, "4HHB", lookup.requestedID)
	assert.Equal(t, "4HHB", document.Data.Id)
	assert.Equal(t, ArchivePdbDataType("archive_pdb_lookups"), document.Data.Type)
	entry := document.Data.Attributes.Entry
	assert.Equal(t, new(1.74), entry.Resolution)
	assert.Equal(t, new("4.3"), entry.LatestVersion)
	assert.Equal(t, []string{"1HHB"}, entry.Supersedes)
	assert.Equal(t, "rcsb_entry_info.resolution_combined[0]", entry.Provenance["resolution"])
	assert.Equal(t, []ArchiveConflict{{Field: "resolution", Chosen: "1.74", Others: []string{"2"}}}, entry.Conflicts)
	assert.NotNil(t, entry.Warnings)
	assert.NotNil(t, document.Data.Attributes.PolymerEntities)
	assert.NotNil(t, document.Data.Attributes.Model.Metrics)
}

func Test_should_give_rcsb_ten_seconds_when_looking_up_pdb_entry(t *testing.T) {
	// given
	lookup := &fakeArchiveLookup{}
	server := (&Server{}).WithArchiveLookup(lookup)

	// when
	recorder := archiveRequest(t, server, "4HHB")

	// then
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Greater(t, lookup.deadlineLeft, 9*time.Second)
	assert.LessOrEqual(t, lookup.deadlineLeft, 10*time.Second)
}

func Test_should_propose_metric_software_when_archive_names_it(t *testing.T) {
	// given
	lookup := &fakeArchiveLookup{result: archive.Result{
		PDBID: "6LU7",
		Model: archive.ModelMapping{Metrics: []archive.Metric{
			{Key: "r_free", Value: 0.235, SoftwareName: new("PHENIX"), SoftwareVersion: new("1.17.1_3660")},
			{Key: "r_work", Value: 0.202},
		}},
	}}
	server := (&Server{}).WithArchiveLookup(lookup)

	// when
	recorder := archiveRequest(t, server, "6LU7")

	// then
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, []ArchiveMetric{
		{Key: "r_free", Value: 0.235, SoftwareName: new("PHENIX"), SoftwareVersion: new("1.17.1_3660")},
		{Key: "r_work", Value: 0.202},
	}, decodeArchiveDocument(t, recorder).Data.Attributes.Model.Metrics)
}

func Test_should_return_validation_warnings_instead_of_failing_when_proposed_values_break_schema(t *testing.T) {
	// given
	lookup := &fakeArchiveLookup{result: archive.Result{
		PDBID: "4HHB",
		Entry: archive.EntryMapping{Resolution: new(-1.0)},
		Model: archive.ModelMapping{
			AtomCount:         new(-5),
			UnmodeledFraction: new(1.5),
			Metrics:           []archive.Metric{{Key: "not_a_metric", Value: 1}},
		},
		PolymerEntities: []archive.PolymerEntityMapping{{
			AlignmentMappings: []archive.UniProtMapping{{Accession: "Q99999", Source: archive.UniProtSourceAlignment}},
		}},
		Warnings: []archive.Warning{{Field: "uniprot_release", Detail: "sifts down"}},
	}}
	server := (&Server{}).WithArchiveLookup(lookup)

	// when
	recorder := archiveRequest(t, server, "4HHB")

	// then
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	attributes := decodeArchiveDocument(t, recorder).Data.Attributes
	assert.Contains(t, attributes.Entry.Warnings, ArchiveWarning{
		Source: archiveWarningFromValidation, Pointer: new("/resolution"),
		Code: new("OUT_OF_RANGE"), Detail: "must be greater than 0",
	})
	modelPointers := warningPointers(attributes.Model.Warnings)
	assert.ElementsMatch(t, []string{"/atom_count", "/unmodeled_fraction", "/metrics/0/key"}, modelPointers)
	assert.Equal(t, []string{"/uniprot_mappings/0/unp_release"}, warningPointers(attributes.PolymerEntities[0].Warnings))
	require.Len(t, attributes.PolymerEntities[0].UniprotMappings, 1)
	assert.Equal(t, "alignment", attributes.PolymerEntities[0].UniprotMappings[0].Source)
	assert.Equal(t, []ArchiveWarning{{
		Source: archiveWarningFromArchive, Field: new("uniprot_release"), Detail: "sifts down",
	}}, attributes.Warnings)
}

func warningPointers(warnings []ArchiveWarning) []string {
	pointers := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning.Pointer != nil {
			pointers = append(pointers, *warning.Pointer)
		}
	}
	return pointers
}

func Test_should_report_archive_warnings_when_lookup_result_has_warnings(t *testing.T) {
	// given
	lookup := &fakeArchiveLookup{result: archive.Result{
		Entry: archive.EntryMapping{Annotations: archive.Annotations{
			Warnings: []archive.Warning{{Field: "method", Detail: "unrecognized"}},
		}},
	}}
	server := (&Server{}).WithArchiveLookup(lookup)

	// when
	recorder := archiveRequest(t, server, "4HHB")

	// then
	require.Equal(t, http.StatusOK, recorder.Code)
	entry := decodeArchiveDocument(t, recorder).Data.Attributes.Entry
	assert.Equal(t, []ArchiveWarning{{
		Source: archiveWarningFromArchive, Field: new("method"), Detail: "unrecognized",
	}}, entry.Warnings)
}

func Test_should_reject_malformed_pdb_id_when_looking_up_archive(t *testing.T) {
	for _, pdbID := range []string{"4HH", "4HHBX", "HHB4", "4H-B"} {
		t.Run(pdbID, func(t *testing.T) {
			// given
			lookup := &fakeArchiveLookup{}
			server := (&Server{}).WithArchiveLookup(lookup)

			// when
			recorder := archiveRequest(t, server, pdbID)

			// then
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Empty(t, lookup.requestedID)
		})
	}
}

func Test_should_map_status_code_when_lookup_fails(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{"should_return_not_found_when_rcsb_has_no_entry", fmt.Errorf("get entry: %w", archive.ErrEntryNotFound), http.StatusNotFound},
		{"should_return_bad_gateway_when_rcsb_has_no_listed_polymer_entity", fmt.Errorf("get polymer entity: %w", rcsb.ErrNotFound), http.StatusBadGateway},
		{"should_return_bad_gateway_when_rcsb_times_out", fmt.Errorf("get entry: %w", context.DeadlineExceeded), http.StatusBadGateway},
		{"should_return_bad_gateway_when_rcsb_fails", errors.New("unexpected HTTP status 500"), http.StatusBadGateway},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// given
			server := (&Server{}).WithArchiveLookup(&fakeArchiveLookup{err: testCase.err})

			// when
			recorder := archiveRequest(t, server, "4HHB")

			// then
			assert.Equal(t, testCase.expectedStatus, recorder.Code)
		})
	}
}

func Test_should_fail_when_archive_lookup_is_not_configured(t *testing.T) {
	// given
	server := &Server{}

	// when
	recorder := archiveRequest(t, server, "4HHB")

	// then
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func Test_should_require_bearer_token_when_looking_up_archive(t *testing.T) {
	// given
	server := (&Server{}).WithArchiveLookup(&fakeArchiveLookup{})
	router := chi.NewRouter()
	HandlerWithOptions(server, ChiServerOptions{
		BaseRouter: router,
		Middlewares: []MiddlewareFunc{
			AuthMiddleware(auth.NewJWT("secret", "issuer", time.Hour), nil),
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/archive/pdb/4HHB", nil)
	recorder := httptest.NewRecorder()

	// when
	router.ServeHTTP(recorder, request)

	// then
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
