package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_read_related_dataset_filters_with_unstated_kept_apart(t *testing.T) {
	// given
	params := ListModelsAcrossEntriesParams{
		HasRelatedDatasets: ptr(true),
		RelatedDatasetRelation: &[]RelatedDatasetRelationFilter{
			RelatedDatasetRelationFilterSimilarSample, RelatedDatasetRelationFilterUnstated,
		},
	}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, ptr(true), search.filters.HasRelatedDatasets)
	assert.Equal(t, []domainmodels.RelatedDatasetRelation{domainmodels.RelatedDatasetRelationSimilarSample},
		search.filters.RelatedDatasetRelations)
	assert.True(t, search.filters.RelatedDatasetRelationUnstated)
	assert.Equal(t, []string{"has_related_datasets", "related_dataset_relation"}, search.relatedDatasetParameters)
}

func Test_should_name_the_parameter_when_a_related_dataset_relation_is_unknown(t *testing.T) {
	// given
	params := ListModelsAcrossEntriesParams{
		RelatedDatasetRelation: &[]RelatedDatasetRelationFilter{"sibling"},
	}

	// when
	_, err := modelSearchFromParams(params)

	// then
	var paramError *modelSearchParamError
	require.ErrorAs(t, err, &paramError)
	assert.Equal(t, "related_dataset_relation", paramError.parameter)
	assert.Contains(t, paramError.problem, `"unstated"`)
}

func Test_should_refuse_related_dataset_filters_and_name_them_when_only_their_switch_is_off(t *testing.T) {
	// given: model search on, related dataset filters off.
	server := (&Server{}).WithModelSearch(ModelSearchPolicy{
		FiltersEnabled: true, CountCap: 10, Requests: 10, InternalRequests: 10, Window: time.Minute,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/v1/models?has_related_datasets=true&related_dataset_relation=source_data", nil)

	// when
	server.ListModelsAcrossEntries(recorder, request, ListModelsAcrossEntriesParams{
		HasRelatedDatasets:     ptr(true),
		RelatedDatasetRelation: &[]RelatedDatasetRelationFilter{RelatedDatasetRelationFilterSourceData},
	})

	// then: refused before the database is read.
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "related dataset filters are not enabled")
	assert.Contains(t, *errorObject.Detail, "has_related_datasets, related_dataset_relation")
}

func Test_should_answer_400_naming_the_related_dataset_filters_when_a_selection_search_has_them_and_they_are_off(t *testing.T) {
	// given: model search on, related dataset filters off
	recorder := httptest.NewRecorder()

	// when
	selectionsTestServer(true).CreateSelection(recorder,
		newSelectionRequest(`{"searches": ["model_type=Ensemble", "has_related_datasets=true"]}`))

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "related dataset filters are not enabled")
	assert.Contains(t, *errorObject.Detail, "has_related_datasets")
	assert.Contains(t, *errorObject.Detail, "search 1")
}
