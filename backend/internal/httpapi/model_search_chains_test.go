package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
)

func Test_should_read_the_protein_chain_filter_when_its_params_are_given(t *testing.T) {
	// given: example 2's chain part, "more than four protein chains, each longer than 300".
	params := ListModelsAcrossEntriesParams{ProteinChainsMin: ptr(5), ProteinChainLengthMin: ptr(301)}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ProteinChainFilter{Mode: db.ProteinChainsAtLeast, MinChains: 5, MinLength: ptr(301)}, search.filters.ProteinChains)
	assert.Equal(t, []string{"protein_chains_min", "protein_chain_length_min"}, search.parameters)
	assert.Equal(t, []string{"protein_chains_min", "protein_chain_length_min"}, search.chainParameters)
}

func Test_should_count_one_chain_when_only_a_length_bound_is_given(t *testing.T) {
	// given
	params := ListModelsAcrossEntriesParams{ProteinChainLengthMin: ptr(100), ProteinChainLengthMax: ptr(200)}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ProteinChainFilter{
		Mode: db.ProteinChainsAtLeast, MinChains: 1, MinLength: ptr(100), MaxLength: ptr(200),
	}, search.filters.ProteinChains)
}

func Test_should_read_the_every_mode_when_protein_chain_mode_is_every(t *testing.T) {
	// given: "at least two protein chains, all longer than 300 residues".
	mode := ProteinChainModeEvery
	params := ListModelsAcrossEntriesParams{ProteinChainMode: &mode, ProteinChainsMin: ptr(2), ProteinChainLengthMin: ptr(301)}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ProteinChainFilter{Mode: db.ProteinChainsEvery, MinChains: 2, MinLength: ptr(301)}, search.filters.ProteinChains)
	assert.Equal(t, []string{"protein_chain_mode", "protein_chains_min", "protein_chain_length_min"}, search.chainParameters)
}

func Test_should_match_files_with_a_protein_chain_when_only_the_at_least_mode_is_given(t *testing.T) {
	// given
	mode := ProteinChainModeAtLeast
	params := ListModelsAcrossEntriesParams{ProteinChainMode: &mode}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ProteinChainFilter{Mode: db.ProteinChainsAtLeast, MinChains: 1}, search.filters.ProteinChains)
}

func Test_should_read_the_mode_when_a_saved_search_query_sets_it(t *testing.T) {
	// given
	query := "protein_chain_mode=every&protein_chain_length_min=301"

	// when
	search, text, err := modelSearchFromQuery(context.Background(), query)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ProteinChainFilter{Mode: db.ProteinChainsEvery, MinChains: 1, MinLength: ptr(301)}, search.filters.ProteinChains)
	assert.Equal(t, "protein_chain_length_min=301&protein_chain_mode=every", text, "parameters sorted by name")
}

func Test_should_name_the_parameter_when_protein_chain_params_are_invalid(t *testing.T) {
	cases := []struct {
		name          string
		params        ListModelsAcrossEntriesParams
		wantParameter string
	}{
		{name: "no chains", params: ListModelsAcrossEntriesParams{ProteinChainsMin: ptr(0)}, wantParameter: "protein_chains_min"},
		{name: "negative length", params: ListModelsAcrossEntriesParams{ProteinChainLengthMin: ptr(-1)}, wantParameter: "protein_chain_length_min"},
		{name: "zero length", params: ListModelsAcrossEntriesParams{ProteinChainLengthMax: ptr(0)}, wantParameter: "protein_chain_length_max"},
		{
			name:          "minimum above maximum",
			params:        ListModelsAcrossEntriesParams{ProteinChainLengthMin: ptr(300), ProteinChainLengthMax: ptr(200)},
			wantParameter: "protein_chain_length_min",
		},
		{
			name:          "every without a length bound",
			params:        ListModelsAcrossEntriesParams{ProteinChainMode: ptr(ProteinChainModeEvery), ProteinChainsMin: ptr(3)},
			wantParameter: "protein_chain_mode",
		},
		{
			name:          "unknown mode",
			params:        ListModelsAcrossEntriesParams{ProteinChainMode: ptr(ProteinChainMode("most")), ProteinChainLengthMin: ptr(3)},
			wantParameter: "protein_chain_mode",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			_, err := modelSearchFromParams(tc.params)

			// then
			var paramError *modelSearchParamError
			require.ErrorAs(t, err, &paramError)
			assert.Equal(t, tc.wantParameter, paramError.parameter)
		})
	}
}

func Test_should_refuse_chain_filters_and_name_them_when_only_their_switch_is_off(t *testing.T) {
	// given: model search on, protein chain filters off.
	server := (&Server{}).WithModelSearch(ModelSearchPolicy{
		FiltersEnabled: true, CountCap: 10, Requests: 10, InternalRequests: 10, Window: time.Minute,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models?protein_chains_min=5&protein_chain_length_min=301", nil)

	// when
	server.ListModelsAcrossEntries(recorder, request, ListModelsAcrossEntriesParams{
		ProteinChainsMin: ptr(5), ProteinChainLengthMin: ptr(301),
	})

	// then: refused before the database is read.
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "protein chain filters are not enabled")
	assert.Contains(t, *errorObject.Detail, "protein_chains_min, protein_chain_length_min")
}

func Test_should_refuse_chain_filters_as_any_filter_when_model_search_is_off(t *testing.T) {
	// given
	server := (&Server{}).WithModelSearch(ModelSearchPolicy{
		ChainFiltersEnabled: true, CountCap: 10, Requests: 10, InternalRequests: 10, Window: time.Minute,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models?protein_chain_length_max=50", nil)

	// when
	server.ListModelsAcrossEntries(recorder, request, ListModelsAcrossEntriesParams{ProteinChainLengthMax: ptr(50)})

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "model search filters are not enabled")
	assert.Contains(t, *errorObject.Detail, "protein_chain_length_max")
}
