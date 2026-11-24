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

func Test_should_read_the_residue_filter_when_example_1_is_searched(t *testing.T) {
	// given: example 1, "a lysine with more than three conformations".
	params := ListModelsAcrossEntriesParams{ResidueType: &[]string{"LYS"}, ResidueConformersMin: ptr(4)}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ResidueFilter{Types: []string{"LYS"}, MinConformers: 4, MinResidues: 1}, search.filters.Residues)
	assert.Equal(t, []string{"residue_type", "residue_conformers_min"}, search.parameters)
	assert.Equal(t, []string{"residue_type", "residue_conformers_min"}, search.residueParameters)
	assert.Empty(t, search.chainParameters)
}

func Test_should_count_every_residue_of_the_types_when_only_residue_type_is_given(t *testing.T) {
	// given: several types, any of them counts.
	params := ListModelsAcrossEntriesParams{ResidueType: &[]string{"LYS", " arg "}, ResiduesMin: ptr(3)}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ResidueFilter{Types: []string{"LYS", "arg"}, MinConformers: 1, MinResidues: 3}, search.filters.Residues)
	assert.Equal(t, []string{"residue_type", "residues_min"}, search.residueParameters)
}

func Test_should_read_the_residue_filter_when_a_saved_search_query_sets_it(t *testing.T) {
	// given
	query := "residue_type=LYS&residue_conformers_min=4&residues_min=2"

	// when
	search, text, err := modelSearchFromQuery(context.Background(), query)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, &db.ResidueFilter{Types: []string{"LYS"}, MinConformers: 4, MinResidues: 2}, search.filters.Residues)
	assert.Equal(t, "residue_conformers_min=4&residue_type=LYS&residues_min=2", text, "parameters sorted by name")
}

func Test_should_name_the_parameter_when_residue_params_are_invalid(t *testing.T) {
	cases := []struct {
		name          string
		params        ListModelsAcrossEntriesParams
		wantParameter string
	}{
		{name: "conformers without a type", params: ListModelsAcrossEntriesParams{ResidueConformersMin: ptr(4)}, wantParameter: "residue_conformers_min"},
		{name: "residues without a type", params: ListModelsAcrossEntriesParams{ResiduesMin: ptr(2)}, wantParameter: "residues_min"},
		{name: "empty type", params: ListModelsAcrossEntriesParams{ResidueType: &[]string{" "}}, wantParameter: "residue_type"},
		{
			name:          "no conformers",
			params:        ListModelsAcrossEntriesParams{ResidueType: &[]string{"LYS"}, ResidueConformersMin: ptr(0)},
			wantParameter: "residue_conformers_min",
		},
		{
			name:          "more conformers than a residue can have",
			params:        ListModelsAcrossEntriesParams{ResidueType: &[]string{"LYS"}, ResidueConformersMin: ptr(17)},
			wantParameter: "residue_conformers_min",
		},
		{
			name:          "no residues",
			params:        ListModelsAcrossEntriesParams{ResidueType: &[]string{"LYS"}, ResiduesMin: ptr(0)},
			wantParameter: "residues_min",
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

func Test_should_refuse_residue_filters_and_name_them_when_only_their_switch_is_off(t *testing.T) {
	// given: model search and chain filters on, residue filters off.
	server := (&Server{}).WithModelSearch(ModelSearchPolicy{
		FiltersEnabled: true, ChainFiltersEnabled: true, CountCap: 10, Requests: 10, InternalRequests: 10, Window: time.Minute,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models?residue_type=LYS&residue_conformers_min=4", nil)

	// when
	server.ListModelsAcrossEntries(recorder, request, ListModelsAcrossEntriesParams{
		ResidueType: &[]string{"LYS"}, ResidueConformersMin: ptr(4),
	})

	// then: refused before the database is read.
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "residue filters are not enabled")
	assert.Contains(t, *errorObject.Detail, "residue_type, residue_conformers_min")
}

func Test_should_refuse_residue_filters_as_any_filter_when_model_search_is_off(t *testing.T) {
	// given
	server := (&Server{}).WithModelSearch(ModelSearchPolicy{
		ResidueFiltersEnabled: true, CountCap: 10, Requests: 10, InternalRequests: 10, Window: time.Minute,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models?residue_type=LYS", nil)

	// when
	server.ListModelsAcrossEntries(recorder, request, ListModelsAcrossEntriesParams{ResidueType: &[]string{"LYS"}})

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "model search filters are not enabled")
	assert.Contains(t, *errorObject.Detail, "residue_type")
}

func Test_should_answer_400_naming_the_residue_filters_when_a_selection_search_has_them_and_they_are_off(t *testing.T) {
	// given: model search on, residue filters off.
	recorder := httptest.NewRecorder()

	// when
	selectionsTestServer(true).CreateSelection(recorder,
		newSelectionRequest(`{"searches": ["model_type=Deposited", "model_type=Deposited&residue_type=LYS&residue_conformers_min=4"]}`))

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "residue filters are not enabled")
	assert.Contains(t, *errorObject.Detail, "residue_type, residue_conformers_min")
	assert.Contains(t, *errorObject.Detail, "search 1")
	assert.NotContains(t, *errorObject.Detail, "model_type")
}
