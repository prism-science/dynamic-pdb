package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

func Test_should_read_every_filter_when_model_search_params_are_given(t *testing.T) {
	// given
	params := ListModelsAcrossEntriesParams{
		Method:           &[]string{" X-ray crystallography "},
		ResolutionMin:    ptr(1.0),
		ResolutionMax:    ptr(2.5),
		Organism:         &[]string{"Homo sapiens", "562"},
		Uniprot:          &[]string{"P69905"},
		LigandOrCofactor: &[]string{"ADP"},
		ModelType:        &[]StructureModelType{StructureModelTypeEnsemble},
		Purpose:          &[]ModelPurpose{ModelPurposeRefinement},
		Source:           &[]ModelSearchSource{ModelSearchSourcePDBArchive},
		DataLevel:        &[]ArtifactLevel{L2},
		Compound:         &[]string{"ATP"},
		RFreeMax:         ptr(0.25),
		ClashscoreMin:    ptr(1.0),
		MetricMissing:    &[]MetricKey{MetricKeyRMSF},
		Flagged:          ptr(true),
	}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	require.NotNil(t, search)
	assert.Equal(t, db.ModelSearch{
		Methods:            []string{"X-ray crystallography"},
		ResolutionMin:      ptr(1.0),
		ResolutionMax:      ptr(2.5),
		Organisms:          []string{"Homo sapiens", "562"},
		UniProtAccessions:  []string{"P69905"},
		LigandsOrCofactors: []string{"ADP"},
		ModelTypes:         []domainmodels.StructureModelType{domainmodels.StructureModelTypeEnsemble},
		Purposes:           []domainmodels.ModelPurpose{domainmodels.ModelPurposeRefinement},
		Sources:            []domainmodels.ModelSearchSource{domainmodels.ModelSearchSourcePDBArchive},
		DataLevels:         []domainmodels.ArtifactLevel{domainmodels.ArtifactLevelL2},
		Compounds:          []string{"ATP"},
		MetricBounds: []db.MetricBound{
			{Key: domainmodels.MetricKeyRFree, Max: ptr(0.25)},
			{Key: domainmodels.MetricKeyClashscore, Min: ptr(1.0)},
		},
		MissingMetrics: []domainmodels.MetricKey{domainmodels.MetricKeyRMSF},
		Flagged:        ptr(true),
	}, search.filters)
	assert.ElementsMatch(t, []string{
		"method", "resolution_min", "resolution_max", "organism", "uniprot", "ligand_or_cofactor",
		"model_type", "purpose", "source", "data_level", "compound", "r_free_max", "clashscore_min",
		"metric_missing", "flagged",
	}, search.parameters)
}

func Test_should_return_no_search_when_model_list_params_set_no_filter(t *testing.T) {
	// given
	params := ListModelsAcrossEntriesParams{Limit: ptr(10), Cursor: ptr("cursor")}

	// when
	search, err := modelSearchFromParams(params)

	// then
	require.NoError(t, err)
	assert.Nil(t, search)
}

func Test_should_name_the_parameter_when_model_search_params_are_invalid(t *testing.T) {
	tooMany := make([]string, maxModelSearchValues+1)
	for index := range tooMany {
		tooMany[index] = "ADP"
	}
	cases := []struct {
		name          string
		params        ListModelsAcrossEntriesParams
		wantParameter string
	}{
		{name: "unknown model type", params: ListModelsAcrossEntriesParams{ModelType: &[]StructureModelType{"Hologram"}}, wantParameter: "model_type"},
		{name: "unknown purpose", params: ListModelsAcrossEntriesParams{Purpose: &[]ModelPurpose{"Decoration"}}, wantParameter: "purpose"},
		{name: "unknown source", params: ListModelsAcrossEntriesParams{Source: &[]ModelSearchSource{"elsewhere"}}, wantParameter: "source"},
		{name: "unknown data level", params: ListModelsAcrossEntriesParams{DataLevel: &[]ArtifactLevel{"L9"}}, wantParameter: "data_level"},
		{name: "unknown missing metric", params: ListModelsAcrossEntriesParams{MetricMissing: &[]MetricKey{"r_factor"}}, wantParameter: "metric_missing"},
		{name: "empty value", params: ListModelsAcrossEntriesParams{Compound: &[]string{"ADP", "  "}}, wantParameter: "compound"},
		{name: "value too long", params: ListModelsAcrossEntriesParams{Organism: &[]string{strings.Repeat("a", maxModelSearchValueLength+1)}}, wantParameter: "organism"},
		{name: "too many values", params: ListModelsAcrossEntriesParams{LigandOrCofactor: &tooMany}, wantParameter: "ligand_or_cofactor"},
		{name: "resolution minimum above maximum", params: ListModelsAcrossEntriesParams{ResolutionMin: ptr(3.0), ResolutionMax: ptr(2.0)}, wantParameter: "resolution_min"},
		{name: "metric minimum above maximum", params: ListModelsAcrossEntriesParams{RFreeMin: ptr(0.3), RFreeMax: ptr(0.2)}, wantParameter: "r_free_min"},
		{name: "metric not finite", params: ListModelsAcrossEntriesParams{RmsfMax: ptr(math.Inf(1))}, wantParameter: "rmsf_max"},
		{name: "resolution not a number", params: ListModelsAcrossEntriesParams{ResolutionMax: ptr(math.NaN())}, wantParameter: "resolution_max"},
		{name: "taxonomy ID of zero", params: ListModelsAcrossEntriesParams{Organism: &[]string{"Homo sapiens", "000"}}, wantParameter: "organism"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			_, err := modelSearchFromParams(tc.params)

			// then
			var paramError *modelSearchParamError
			require.ErrorAs(t, err, &paramError)
			assert.Equal(t, tc.wantParameter, paramError.parameter)
			assert.Contains(t, err.Error(), tc.wantParameter)
		})
	}
}

// metricBoundParams must name every metric, and openapi.yaml must define both parameters of each.
func Test_should_cover_every_metric_with_documented_parameters_when_metric_bounds_are_read(t *testing.T) {
	// given
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/openapi.yaml")
	require.NoError(t, err)
	operation := spec.Paths.Find("/v1/models").Get
	require.NotNil(t, operation)

	// when
	bounds := metricBoundParams(ListModelsAcrossEntriesParams{})

	// then
	keys := make([]domainmodels.MetricKey, 0, len(bounds))
	for _, bound := range bounds {
		keys = append(keys, bound.key)
		for _, suffix := range []string{"_min", "_max"} {
			assert.NotNil(t, operation.Parameters.GetByInAndName("query", string(bound.key)+suffix),
				"openapi.yaml has no parameter %s%s", bound.key, suffix)
		}
	}
	assert.Equal(t, domainmodels.MetricKeys, keys)
}

func Test_should_read_each_metric_bound_from_its_own_parameters_when_metric_bounds_are_read(t *testing.T) {
	// given
	params := ListModelsAcrossEntriesParams{
		RWorkMin: ptr(1.0), RWorkMax: ptr(2.0), RFreeMin: ptr(3.0), RFreeMax: ptr(4.0), CcMin: ptr(5.0), CcMax: ptr(6.0),
		RsccMin: ptr(7.0), RsccMax: ptr(8.0), ClashscoreMin: ptr(9.0), ClashscoreMax: ptr(10.0),
		RamachandranOutliersMin: ptr(11.0), RamachandranOutliersMax: ptr(12.0),
		SideChainOutliersMin: ptr(13.0), SideChainOutliersMax: ptr(14.0),
		MolprobityScoreMin: ptr(15.0), MolprobityScoreMax: ptr(16.0), WilsonBMin: ptr(17.0), WilsonBMax: ptr(18.0),
		RmsfMin: ptr(19.0), RmsfMax: ptr(20.0),
	}

	// when
	bounds := metricBoundParams(params)

	// then
	for index, bound := range bounds {
		require.NotNil(t, bound.min, bound.key)
		require.NotNil(t, bound.max, bound.key)
		assert.InDelta(t, float64(2*index+1), *bound.min, 0, bound.key)
		assert.InDelta(t, float64(2*index+2), *bound.max, 0, bound.key)
	}
}

func Test_should_refuse_filters_and_name_them_when_model_search_is_off(t *testing.T) {
	// given
	server := &Server{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models?model_type=Ensemble&r_free_max=0.2", nil)

	// when
	server.ListModelsAcrossEntries(recorder, request, ListModelsAcrossEntriesParams{
		ModelType: &[]StructureModelType{StructureModelTypeEnsemble},
		RFreeMax:  ptr(0.2),
	})

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "not enabled")
	assert.Contains(t, *errorObject.Detail, "model_type")
	assert.Contains(t, *errorObject.Detail, "r_free_max")
}

func Test_should_refuse_invalid_filters_before_reading_models_when_model_search_is_on(t *testing.T) {
	// given
	server := (&Server{}).WithModelSearch(ModelSearchPolicy{
		FiltersEnabled: true, CountCap: 10, Requests: 10, InternalRequests: 10, Window: time.Minute,
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/models?resolution_min=3&resolution_max=2", nil)

	// when
	server.ListModelsAcrossEntries(recorder, request, ListModelsAcrossEntriesParams{
		ResolutionMin: ptr(3.0), ResolutionMax: ptr(2.0),
	})

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "resolution_min")
}

func Test_should_answer_429_with_guidance_when_client_runs_too_many_counted_searches(t *testing.T) {
	// given
	limiter := newModelSearchLimiter(ModelSearchPolicy{
		Requests: 2, InternalRequests: 100, Window: time.Minute, TrustedProxies: clusterProxies,
	})
	for range 2 {
		require.False(t, limiter.respondOnLimit(httptest.NewRecorder(), newRequest("/v1/models", "192.0.2.1:1234")))
	}
	recorder := httptest.NewRecorder()

	// when
	limited := limiter.respondOnLimit(recorder, newRequest("/v1/models", "192.0.2.1:1234"))

	// then
	assert.True(t, limited)
	assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	assert.NotEmpty(t, recorder.Header().Get("Retry-After"))
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Code)
	assert.Equal(t, rateLimitedErrorCode, *errorObject.Code)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "links.next")
	assert.False(t, limiter.respondOnLimit(httptest.NewRecorder(), newRequest("/v1/models", "192.0.2.2:1234")))
}

func Test_should_answer_504_with_guidance_when_search_runs_past_its_deadline(t *testing.T) {
	// given
	server := &Server{}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/models?model_type=Ensemble", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()

	// when
	server.writeModelSearchFailure(recorder, request, "count models", errors.New("canceling statement due to user request"))

	// then
	assert.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	assert.Equal(t, "10", recorder.Header().Get("Retry-After"))
	errorObject := decodeSingleErrorObject(t, recorder)
	require.NotNil(t, errorObject.Code)
	assert.Equal(t, readTimeoutErrorCode, *errorObject.Code)
	require.NotNil(t, errorObject.Detail)
	assert.Contains(t, *errorObject.Detail, "Add filters")
}

func Test_should_answer_500_when_search_fails_before_its_deadline(t *testing.T) {
	// given
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recorder := httptest.NewRecorder()

	// when
	server.writeModelSearchFailure(recorder, request, "list models", errors.New("connection refused"))

	// then
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Retry-After"))
}

func Test_should_copy_every_model_field_when_search_attributes_are_built(t *testing.T) {
	// given
	model := ModelAttributes{
		Id: "dpdb_abcd1234_m_001", EntryId: "dpdb_abcd1234", Title: ptr("model"), RevisionNumber: ptr(3),
		Metrics: []Metric{}, IdempotencyKey: ptr("key"), ThumbnailImageUrl: ptr("https://example.com/t.png"),
		CreatedAt: time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 11, 9, 1, 0, 0, 0, time.UTC),
	}

	// when
	attributes, err := modelSearchAttributesFromModel(model)

	// then
	require.NoError(t, err)
	assert.Equal(t, model.Id, attributes.Id)
	assert.Equal(t, model.EntryId, attributes.EntryId)
	assert.Equal(t, model.Title, attributes.Title)
	assert.Equal(t, model.RevisionNumber, attributes.RevisionNumber)
	assert.Equal(t, model.IdempotencyKey, attributes.IdempotencyKey)
	assert.Equal(t, model.ThumbnailImageUrl, attributes.ThumbnailImageUrl)
	assert.Equal(t, model.CreatedAt, attributes.CreatedAt)
	assert.Equal(t, model.UpdatedAt, attributes.UpdatedAt)
	assert.Nil(t, attributes.Entry)
	assert.Nil(t, attributes.ReviewFlags)
}

func Test_should_answer_400_when_the_database_layer_refuses_the_search(t *testing.T) {
	// given
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recorder := httptest.NewRecorder()

	// when
	server.writeModelSearchFailure(recorder, request, "list models", fmt.Errorf("build: %w", db.ErrInvalidModelSearch))

	// then
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}
