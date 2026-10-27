package rcsb

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_read_obsolete_status_and_successors_when_rcsb_removed_the_entry(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/v1/holdings/status/4YYU":
			writeText(t, w, `{
				"rcsb_repository_holdings_combined": {
					"id_code_replaced_by_latest": "8R76",
					"status": "REMOVED",
					"status_code": "OBS"
				}
			}`)
		case "/rest/v1/holdings/removed/4YYU":
			writeText(t, w, `{
				"rcsb_repository_holdings_removed": {
					"id_codes_replaced_by": ["8R76"],
					"remove_date": "2024-01-17T00:00:00.000+00:00"
				}
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv(dataBaseURLEnv, server.URL)
	client := NewClient()

	// when
	status, statusErr := client.GetHoldingsStatus(t.Context(), "4yyu")
	removed, removedErr := client.GetRemovedEntry(t.Context(), "4yyu")

	// then
	require.NoError(t, statusErr)
	require.NoError(t, removedErr)
	assert.Equal(t, HoldingsStatus{Status: "REMOVED", StatusCode: "OBS", ReplacedByLatest: "8R76"}, status)
	assert.Equal(t, []string{"8R76"}, removed.ReplacedBy)
	removeDate, err := time.Parse(time.RFC3339, removed.RemoveDate)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 1, 17, 0, 0, 0, 0, time.UTC), removeDate.UTC())
}

func Test_should_return_not_found_when_rcsb_has_no_holdings_for_the_id(t *testing.T) {
	// given
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv(dataBaseURLEnv, server.URL)
	client := NewClient()

	// when
	_, err := client.GetHoldingsStatus(t.Context(), "9ZZZ")

	// then
	assert.True(t, errors.Is(err, ErrNotFound))
}

func Test_should_read_revision_history_when_entry_is_decoded(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeText(t, w, `{
			"pdbx_audit_revision_history": [
				{"ordinal": 1, "major_revision": 1, "minor_revision": 0, "revision_date": "2003-10-31T00:00:00.000+00:00"},
				{"ordinal": 2, "major_revision": 2, "minor_revision": 3, "revision_date": "2024-11-20T00:00:00.000+00:00"}
			]
		}`)
	}))
	defer server.Close()
	t.Setenv(dataBaseURLEnv, server.URL)
	client := NewClient()

	// when
	details, err := client.GetEntryDetails(t.Context(), "1URC")

	// then
	require.NoError(t, err)
	assert.Equal(t, []EntryRevisionHistory{
		{Ordinal: 1, MajorRevision: 1, MinorRevision: 0, RevisionDate: "2003-10-31T00:00:00.000+00:00"},
		{Ordinal: 2, MajorRevision: 2, MinorRevision: 3, RevisionDate: "2024-11-20T00:00:00.000+00:00"},
	}, details.RevisionHistory)
}
