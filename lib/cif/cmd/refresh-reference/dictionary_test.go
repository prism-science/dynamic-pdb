package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRCSB(server *httptest.Server) *rcsbDictionary {
	return &rcsbDictionary{
		httpClient: server.Client(),
		searchURL:  server.URL + "/search",
		graphqlURL: server.URL + "/graphql",
		retryDelay: time.Millisecond,
	}
}

func Test_should_read_search_hits_and_treat_no_content_as_no_hits(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Query struct {
				Parameters struct {
					Value string `json:"value"`
				} `json:"parameters"`
			} `json:"query"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body.Query.Parameters.Value == "other" {
			writer.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(writer, `{"total_count": 2, "result_set": [{"identifier": "MSE"}, {"identifier": "PRD_000001"}]}`)
	}))
	defer server.Close()
	dictionary := testRCSB(server)

	// when
	ids, idsErr := dictionary.idsOfType(context.Background(), "L-peptide linking")
	none, noneErr := dictionary.countOfType(context.Background(), "other")

	// then
	require.NoError(t, idsErr)
	assert.Equal(t, []string{"MSE", "PRD_000001"}, ids)
	require.NoError(t, noneErr)
	assert.Zero(t, none)
}

func Test_should_read_chem_comp_records_and_skip_unknown_ids(t *testing.T) {
	// given
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"data": {"chem_comps": [
			{"chem_comp": {"id": "ZN", "type": "non-polymer", "formula": "Zn", "pdbx_release_status": "REL", "pdbx_replaced_by": null},
			 "rcsb_chem_comp_info": {"atom_count": 1}},
			null
		]}}`)
	}))
	defer server.Close()

	// when
	records, err := testRCSB(server).chemComps(context.Background(), []string{"ZN", "TDP"})

	// then
	require.NoError(t, err)
	assert.Equal(t, map[string]chemComp{
		"ZN": {ID: "ZN", Type: "non-polymer", Formula: "Zn", ReleaseStatus: "REL", AtomCount: 1},
	}, records)
}

func Test_should_retry_when_rcsb_is_briefly_unavailable(t *testing.T) {
	// given
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(writer, `{"total_count": 51512}`)
	}))
	defer server.Close()

	// when
	count, err := testRCSB(server).countWithType(context.Background())

	// then
	require.NoError(t, err)
	assert.Equal(t, 51512, count)
	assert.Equal(t, int32(2), calls.Load())
}

func Test_should_fail_without_retry_when_rcsb_rejects_the_query(t *testing.T) {
	// given
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, `{"message": "search is not enabled"}`)
	}))
	defer server.Close()

	// when
	_, err := testRCSB(server).countWithType(context.Background())

	// then
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 400")
	assert.Equal(t, int32(1), calls.Load())
}
