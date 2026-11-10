package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

const (
	// maxModelSearchValues and maxModelSearchValueLength bound one list filter of the model
	// search, as openapi.yaml documents.
	maxModelSearchValues      = 20
	maxModelSearchValueLength = 100
	// fewModelSearchMatches is the most matches a search may count for its pages to collect every
	// match before ordering them; a search with more matches walks models in list order, where
	// matches are frequent enough to fill a page soon.
	fewModelSearchMatches = 2000
)

// ModelSearchPolicy configures the search filters of GET /v1/models.
type ModelSearchPolicy struct {
	// FiltersEnabled turns on the filters, the capped total, and the entry and review warnings in
	// each result. Off, a filter parameter gets 400 and the list answers as before.
	FiltersEnabled bool
	// CountCap is where the total stops counting.
	CountCap int
	// Requests and InternalRequests cap counted searches per client per Window, for clients
	// through the ingress and in-cluster callers. Clients are told apart as by
	// GlobalRateLimitMiddleware, with TrustedProxies.
	Requests         int
	InternalRequests int
	Window           time.Duration
	TrustedProxies   []netip.Prefix
}

// modelSearch is the server's model search setup; the zero value has the filters off.
type modelSearch struct {
	enabled  bool
	countCap int
	limiter  *routeRateLimiter
}

// WithModelSearch configures the search filters of GET /v1/models.
func (s *Server) WithModelSearch(policy ModelSearchPolicy) *Server {
	s.modelSearch = modelSearch{
		enabled:  policy.FiltersEnabled,
		countCap: policy.CountCap,
		limiter:  newModelSearchLimiter(policy),
	}
	return s
}

// newModelSearchLimiter caps searches that count their results, per client, on top of the global
// limit. Paging with a cursor does not count against it.
func newModelSearchLimiter(policy ModelSearchPolicy) *routeRateLimiter {
	return newRouteRateLimiter(policy.Requests, policy.InternalRequests, policy.Window, policy.TrustedProxies,
		fmt.Sprintf("too many searches from this client: each client may run %d searches per %s that count their "+
			"results. Wait the number of seconds in Retry-After. To read more results of the same search, "+
			"follow links.next: paging with a cursor does not count against this limit",
			policy.Requests, policy.Window))
}

func (s *Server) ListModelsAcrossEntries(
	w http.ResponseWriter,
	r *http.Request,
	params ListModelsAcrossEntriesParams,
) {
	filters, page, err := modelFiltersFromParams(params)
	if err != nil {
		writeListParamsError(w, err, "invalid model filters")
		return
	}
	search, err := modelSearchFromParams(params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	activeState := domainmodels.RevisionStateActive
	activeEntryState := domainmodels.EntryStateActive
	activeModelState := domainmodels.ModelStateActive
	filters.State = &activeState
	filters.EntryState = &activeEntryState
	filters.ModelState = &activeModelState
	filters.ExcludeResidueData = true

	if !s.modelSearch.enabled {
		if search != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST",
				"model search filters are not enabled on this server; remove "+strings.Join(search.parameters, ", "))
			return
		}
		revisions, err := s.database.Models.List(r.Context(), filters)
		if err != nil {
			slog.ErrorContext(r.Context(), "list models across entries failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
			return
		}
		s.writeModelListPage(w, r, revisions, page)
		return
	}

	// Search results say what the caller may know about review flags, so they vary by caller.
	// Without the flagged filter, a failed role lookup only costs the caller the flags' issues.
	varyByCaller(w)
	seesReviewFlags, err := s.seesReviewFlags(r.Context())
	if err != nil {
		if usesFlaggedFilter(search) {
			slog.ErrorContext(r.Context(), "model search authorization failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to authorize request")
			return
		}
		slog.ErrorContext(r.Context(), "review flag access check failed; answering as for the public", "err", err)
		seesReviewFlags = false
	}
	if err := checkFlaggedFilter(search, seesReviewFlags); err != nil {
		writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error()+"; remove flagged")
		return
	}

	// The first page counts the whole result. It, and the pages of a search with few matches,
	// which read every match, count against the search limit; other pages are cheap.
	firstPage := page.Cursor == nil && page.Offset == nil
	if search != nil {
		filters.Search = &search.filters
		filters.SearchMatchesFew = page.Cursor != nil && page.Cursor.ModelSearchMatchesFew
	}
	if (firstPage || filters.SearchMatchesFew) && s.modelSearch.limiter.respondOnLimit(w, r) {
		return
	}
	var total *ModelSearchTotal
	if firstPage {
		count, exact, err := s.database.Models.CountCapped(r.Context(), filters, s.modelSearch.countCap)
		if err != nil {
			s.writeModelSearchFailure(w, r, "count models", err)
			return
		}
		total = &ModelSearchTotal{Count: count, Exact: exact}
		filters.SearchMatchesFew = search != nil && exact && count <= fewModelSearchMatches
	}
	revisions, err := s.database.Models.List(r.Context(), filters)
	if err != nil {
		s.writeModelSearchFailure(w, r, "list models", err)
		return
	}
	s.writeModelSearchPage(w, r, revisions, page, search, seesReviewFlags, total, filters.SearchMatchesFew)
}

// writeModelSearchFailure answers a failed search: 400 for a search the database layer refuses,
// 504 with guidance when it ran past the read deadline, else 500.
func (s *Server) writeModelSearchFailure(w http.ResponseWriter, r *http.Request, action string, err error) {
	if errors.Is(err, db.ErrInvalidModelSearch) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid model search: "+err.Error())
		return
	}
	if errors.Is(r.Context().Err(), context.DeadlineExceeded) {
		slog.WarnContext(r.Context(), "model search ran past the read deadline", "action", action, "err", err)
		w.Header().Set("Retry-After", strconv.Itoa(readTimeoutRetryAfterSeconds))
		writeError(w, http.StatusGatewayTimeout, readTimeoutErrorCode,
			"the search did not finish within the server's time limit. Add filters that narrow the "+
				"result, for example a model type or a resolution range, and try again after "+
				"Retry-After seconds")
		return
	}
	slog.ErrorContext(r.Context(), "model search failed", "action", action, "err", err)
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
}

// writeModelSearchPage writes one page of model search results. revisions holds the extra row
// requested by page.fetchLimit, if it exists; seesReviewFlags says whether results list their
// review flags; total is nil on pages after the first; matchesFew goes into the next page's
// cursor.
func (s *Server) writeModelSearchPage(
	w http.ResponseWriter,
	r *http.Request,
	revisions []domainmodels.ModelRevision,
	page listPage,
	search *parsedModelSearch,
	seesReviewFlags bool,
	total *ModelSearchTotal,
	matchesFew bool,
) {
	revisions, hasNextPage := trimToPage(revisions, page)
	items, err := s.modelSearchItems(r.Context(), revisions, search, seesReviewFlags)
	if err != nil {
		s.writeModelSearchFailure(w, r, "build model search results", err)
		return
	}
	document := ModelSearchCollectionDocument{Data: items}
	meta := ModelSearchCollectionMeta{Total: total}
	if hasNextPage && len(revisions) > 0 {
		last := revisions[len(revisions)-1]
		links, cursorMeta, err := s.nextPageLinks(r, listCursor{
			Kind:                  listCursorKindModels,
			CreatedAt:             last.CreatedAt,
			ID:                    last.ID,
			ModelSearchMatchesFew: matchesFew,
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "build model search links failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
			return
		}
		document.Links = links
		meta.NextCursor = &cursorMeta.NextCursor
	}
	if meta.Total != nil || meta.NextCursor != nil {
		document.Meta = &meta
	}
	writeJSON(w, http.StatusOK, document)
}

// modelSearchItems builds the search results of one page: each model with its entry's active
// revision, whether it is flagged for review (with the flags themselves when seesReviewFlags)
// and, for a compound search, the matched compounds that need review.
func (s *Server) modelSearchItems(
	ctx context.Context,
	revisions []domainmodels.ModelRevision,
	search *parsedModelSearch,
	seesReviewFlags bool,
) ([]ModelSearchData, error) {
	modelAttributes, err := s.modelAttributesFromRevisions(ctx, revisions)
	if err != nil {
		return nil, fmt.Errorf("build models of model search results: %w", err)
	}
	revisionIDs := make([]uuid.UUID, 0, len(revisions))
	entryIDs := make([]string, 0, len(revisions))
	primaryArtifactIDs := make([]uuid.UUID, 0, len(revisions))
	for _, revision := range revisions {
		revisionIDs = append(revisionIDs, revision.ID)
		entryIDs = append(entryIDs, revision.EntryID)
		if revision.PrimaryArtifactID != nil {
			primaryArtifactIDs = append(primaryArtifactIDs, *revision.PrimaryArtifactID)
		}
	}
	entries, err := s.activeEntryInfoByID(ctx, entryIDs)
	if err != nil {
		return nil, fmt.Errorf("read entries of model search results: %w", err)
	}
	flags, err := s.database.MigrationReviews.ListOpenForModelRevisions(ctx, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("list review flags of model search results: %w", err)
	}
	var compounds map[uuid.UUID][]domainmodels.CompoundNeedingReview
	if search != nil && len(search.filters.Compounds) > 0 {
		compounds, err = s.database.ModeledCompounds.ListNeedingReview(ctx, primaryArtifactIDs, search.filters.Compounds)
		if err != nil {
			return nil, fmt.Errorf("list compounds needing review of model search results: %w", err)
		}
	}

	items := make([]ModelSearchData, 0, len(revisions))
	for index, revision := range revisions {
		attributes, err := modelSearchAttributesFromModel(modelAttributes[index])
		if err != nil {
			return nil, fmt.Errorf("build model search result %s: %w", revision.ModelID, err)
		}
		if entry, found := entries[revision.EntryID]; found {
			attributes.Entry = &entry
		}
		flagged, reviewFlags := reviewFlagFields(flags[revision.ID], seesReviewFlags)
		attributes.FlaggedForReview = &flagged
		attributes.ReviewFlags = reviewFlags
		if compounds != nil {
			attributes.CompoundsNeedingReview = ptr([]CompoundNeedingReview{})
			if revision.PrimaryArtifactID != nil {
				attributes.CompoundsNeedingReview = ptr(compoundsNeedingReviewFromModels(compounds[*revision.PrimaryArtifactID]))
			}
		}
		items = append(items, ModelSearchData{Type: jsonAPITypeModels, Id: attributes.Id, Attributes: attributes})
	}
	return items, nil
}

// activeEntryInfoByID returns the active revision of each entry as entry info, by entry ID.
func (s *Server) activeEntryInfoByID(ctx context.Context, entryIDs []string) (map[string]EntryInfoAttributes, error) {
	entries := make(map[string]EntryInfoAttributes, len(entryIDs))
	if len(entryIDs) == 0 {
		return entries, nil
	}
	activeState := domainmodels.RevisionStateActive
	revisions, err := s.database.Entries.List(ctx, db.EntryRevisionFilters{EntryIDs: entryIDs, State: &activeState})
	if err != nil {
		return nil, fmt.Errorf("list entries of model search results: %w", err)
	}
	for _, revision := range revisions {
		info, err := entryInfoAttributesFromRevision(revision)
		if err != nil {
			return nil, fmt.Errorf("build entry of model search result: %w", err)
		}
		entries[revision.EntryID] = info
	}
	return entries, nil
}

// modelSearchAttributesFromModel copies every model list field into a search result. It goes
// through JSON, as ModelSearchAttributes is ModelAttributes plus the search fields, so a field
// added to ModelAttributes reaches search results without a change here.
func modelSearchAttributesFromModel(model ModelAttributes) (ModelSearchAttributes, error) {
	data, err := json.Marshal(model)
	if err != nil {
		return ModelSearchAttributes{}, fmt.Errorf("marshal model for search result: %w", err)
	}
	var attributes ModelSearchAttributes
	if err := json.Unmarshal(data, &attributes); err != nil {
		return ModelSearchAttributes{}, fmt.Errorf("unmarshal model for search result: %w", err)
	}
	return attributes, nil
}

func compoundsNeedingReviewFromModels(compounds []domainmodels.CompoundNeedingReview) []CompoundNeedingReview {
	items := make([]CompoundNeedingReview, 0, len(compounds))
	for _, compound := range compounds {
		items = append(items, CompoundNeedingReview{CcdId: compound.CCDID, ReviewReason: compound.ReviewReason})
	}
	return items
}

// parsedModelSearch is a request's search filters and the parameters that set them.
type parsedModelSearch struct {
	filters    db.ModelSearch
	parameters []string
}

// modelSearchParamError is a search parameter the request got wrong.
type modelSearchParamError struct {
	parameter string
	problem   string
}

func (e *modelSearchParamError) Error() string {
	return fmt.Sprintf("invalid model search parameter %s: %s", e.parameter, e.problem)
}

// modelSearchFromParams reads the search filters of GET /v1/models. It returns nil when the
// request sets none.
func modelSearchFromParams(params ListModelsAcrossEntriesParams) (*parsedModelSearch, error) {
	search := &parsedModelSearch{}
	var err error
	if search.filters.Methods, err = search.texts("method", params.Method); err != nil {
		return nil, err
	}
	if search.filters.Organisms, err = search.texts("organism", params.Organism); err != nil {
		return nil, err
	}
	if err := checkTaxonomyIDs(search.filters.Organisms); err != nil {
		return nil, err
	}
	if search.filters.UniProtAccessions, err = search.texts("uniprot", params.Uniprot); err != nil {
		return nil, err
	}
	if search.filters.LigandsOrCofactors, err = search.texts("ligand_or_cofactor", params.LigandOrCofactor); err != nil {
		return nil, err
	}
	if search.filters.Compounds, err = search.texts("compound", params.Compound); err != nil {
		return nil, err
	}
	if search.filters.ResolutionMin, search.filters.ResolutionMax, err = search.bounds(
		"resolution", params.ResolutionMin, params.ResolutionMax,
	); err != nil {
		return nil, err
	}
	if search.filters.ModelTypes, err = vocabularyValues[StructureModelType, domainmodels.StructureModelType](
		search, "model_type", params.ModelType,
	); err != nil {
		return nil, err
	}
	if search.filters.Purposes, err = vocabularyValues[ModelPurpose, domainmodels.ModelPurpose](
		search, "purpose", params.Purpose,
	); err != nil {
		return nil, err
	}
	if search.filters.Sources, err = vocabularyValues[ModelSearchSource, domainmodels.ModelSearchSource](
		search, "source", params.Source,
	); err != nil {
		return nil, err
	}
	if search.filters.DataLevels, err = vocabularyValues[ArtifactLevel, domainmodels.ArtifactLevel](
		search, "data_level", params.DataLevel,
	); err != nil {
		return nil, err
	}
	if search.filters.MissingMetrics, err = vocabularyValues[MetricKey, domainmodels.MetricKey](
		search, "metric_missing", params.MetricMissing,
	); err != nil {
		return nil, err
	}
	for _, metric := range metricBoundParams(params) {
		minimum, maximum, err := search.bounds(string(metric.key), metric.min, metric.max)
		if err != nil {
			return nil, err
		}
		if minimum != nil || maximum != nil {
			search.filters.MetricBounds = append(search.filters.MetricBounds,
				db.MetricBound{Key: metric.key, Min: minimum, Max: maximum})
		}
	}
	if params.Flagged != nil {
		search.parameters = append(search.parameters, "flagged")
		search.filters.Flagged = params.Flagged
	}
	if len(search.parameters) == 0 {
		return nil, nil
	}
	return search, nil
}

// modelSearchPagingParameters are the GET /v1/models parameters that choose a page of results,
// not which models match.
var modelSearchPagingParameters = map[string]bool{"limit": true, "offset": true, "cursor": true}

// modelSearchFilterParameters are the filter parameters of GET /v1/models, read from the spec, so
// a filter added there is accepted in a search query with no change here.
var modelSearchFilterParameters = sync.OnceValues(func() (map[string]bool, error) {
	spec, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("read the embedded openapi spec: %w", err)
	}
	path := spec.Paths.Find("/v1/models")
	if path == nil || path.Get == nil {
		return nil, errors.New("the embedded openapi spec has no GET /v1/models")
	}
	names := make(map[string]bool, len(path.Get.Parameters))
	for _, parameter := range path.Get.Parameters {
		if parameter.Value != nil && parameter.Value.In == "query" && !modelSearchPagingParameters[parameter.Value.Name] {
			names[parameter.Value.Name] = true
		}
	}
	return names, nil
})

// modelSearchFromQuery reads a search given as the query string of a GET /v1/models request,
// as POST /v1/selections takes it. It returns the search (nil when the query has no filter) and
// the query in a fixed form: parameters sorted by name. A query that GET /v1/models would read
// differently is refused with *modelSearchParamError:
//   - a parameter GET /v1/models does not define, so a misspelled filter never widens the
//     search to every model;
//   - a paging parameter, as the search holds every result, not a page.
//
// Values are read by GET /v1/models's own generated binder, then as modelSearchFromParams reads
// that request's parameters.
func modelSearchFromQuery(ctx context.Context, query string) (*parsedModelSearch, string, error) {
	values, err := url.ParseQuery(strings.TrimPrefix(query, "?"))
	if err != nil {
		return nil, "", &modelSearchParamError{parameter: "query", problem: "not a URL query string: " + err.Error()}
	}
	filterParameters, err := modelSearchFilterParameters()
	if err != nil {
		return nil, "", err
	}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		switch {
		case modelSearchPagingParameters[name]:
			return nil, "", &modelSearchParamError{
				parameter: name,
				problem:   "chooses a page of results; a saved search holds every result, so leave it out",
			}
		case !filterParameters[name]:
			return nil, "", &modelSearchParamError{parameter: name, problem: "is not a GET /v1/models filter"}
		}
	}
	params, err := bindModelSearchQuery(ctx, values)
	if err != nil {
		return nil, "", err
	}
	search, err := modelSearchFromParams(params)
	if err != nil {
		return nil, "", err
	}
	return search, values.Encode(), nil
}

// bindModelSearchQuery reads values with the generated parameter binder of GET /v1/models, so a
// search query is read exactly as that request's query.
func bindModelSearchQuery(ctx context.Context, values url.Values) (ListModelsAcrossEntriesParams, error) {
	var params ListModelsAcrossEntriesParams
	var bindErr error
	wrapper := ServerInterfaceWrapper{
		Handler: modelSearchParamsCapture{params: &params},
		ErrorHandlerFunc: func(_ http.ResponseWriter, _ *http.Request, err error) {
			bindErr = err
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "/v1/models?"+values.Encode(), nil)
	if err != nil {
		return params, fmt.Errorf("build model search request: %w", err)
	}
	wrapper.ListModelsAcrossEntries(discardResponseWriter{}, request)
	if bindErr != nil {
		var paramErr *InvalidParamFormatError
		if errors.As(bindErr, &paramErr) {
			return params, &modelSearchParamError{parameter: paramErr.ParamName, problem: paramErr.Err.Error()}
		}
		return params, &modelSearchParamError{parameter: "query", problem: bindErr.Error()}
	}
	return params, nil
}

// modelSearchParamsCapture keeps the parameters the generated GET /v1/models binder read.
type modelSearchParamsCapture struct {
	Unimplemented
	params *ListModelsAcrossEntriesParams
}

func (c modelSearchParamsCapture) ListModelsAcrossEntries(
	_ http.ResponseWriter,
	_ *http.Request,
	params ListModelsAcrossEntriesParams,
) {
	*c.params = params
}

// discardResponseWriter is the response of a request that only binds parameters.
type discardResponseWriter struct{}

func (discardResponseWriter) Header() http.Header            { return http.Header{} }
func (discardResponseWriter) Write(data []byte) (int, error) { return len(data), nil }
func (discardResponseWriter) WriteHeader(int)                {}

// texts reads a list filter of free text: each value trimmed, none empty or too long.
func (search *parsedModelSearch) texts(parameter string, values *[]string) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	if err := checkModelSearchValueCount(parameter, len(*values)); err != nil {
		return nil, err
	}
	texts := make([]string, 0, len(*values))
	for _, value := range *values {
		text := strings.TrimSpace(value)
		if text == "" {
			return nil, &modelSearchParamError{parameter: parameter, problem: "a value is empty"}
		}
		if len(text) > maxModelSearchValueLength {
			return nil, &modelSearchParamError{
				parameter: parameter,
				problem:   fmt.Sprintf("a value is longer than %d characters", maxModelSearchValueLength),
			}
		}
		texts = append(texts, text)
	}
	search.parameters = append(search.parameters, parameter)
	return texts, nil
}

// bounds reads the <name>_min and <name>_max pair of a numeric filter.
func (search *parsedModelSearch) bounds(name string, minimum, maximum *float64) (*float64, *float64, error) {
	for _, bound := range []struct {
		parameter string
		value     *float64
	}{{name + "_min", minimum}, {name + "_max", maximum}} {
		if bound.value == nil {
			continue
		}
		if math.IsNaN(*bound.value) || math.IsInf(*bound.value, 0) {
			return nil, nil, &modelSearchParamError{parameter: bound.parameter, problem: "not a finite number"}
		}
		search.parameters = append(search.parameters, bound.parameter)
	}
	if minimum != nil && maximum != nil && *minimum > *maximum {
		return nil, nil, &modelSearchParamError{
			parameter: name + "_min",
			problem:   fmt.Sprintf("%v is greater than %s_max %v", *minimum, name, *maximum),
		}
	}
	return minimum, maximum, nil
}

// vocabularyValues reads a list filter of one of the API's vocabularies, refusing values outside it.
func vocabularyValues[API ~string, Domain interface {
	~string
	Validate() error
}](search *parsedModelSearch, parameter string, values *[]API) ([]Domain, error) {
	if values == nil {
		return nil, nil
	}
	if err := checkModelSearchValueCount(parameter, len(*values)); err != nil {
		return nil, err
	}
	result := make([]Domain, 0, len(*values))
	for _, value := range *values {
		domainValue := Domain(value)
		if err := domainValue.Validate(); err != nil {
			return nil, &modelSearchParamError{parameter: parameter, problem: err.Error()}
		}
		result = append(result, domainValue)
	}
	search.parameters = append(search.parameters, parameter)
	return result, nil
}

// checkTaxonomyIDs refuses an organism value of digits only that is no NCBI taxonomy ID: IDs start at 1.
func checkTaxonomyIDs(organisms []string) error {
	for _, organism := range organisms {
		if strings.Trim(organism, "0123456789") == "" && strings.Trim(organism, "0") == "" {
			return &modelSearchParamError{parameter: "organism", problem: "NCBI taxonomy IDs start at 1"}
		}
	}
	return nil
}

func checkModelSearchValueCount(parameter string, count int) error {
	if count == 0 {
		return &modelSearchParamError{parameter: parameter, problem: "no value given"}
	}
	if count > maxModelSearchValues {
		return &modelSearchParamError{
			parameter: parameter,
			problem:   fmt.Sprintf("more than %d values", maxModelSearchValues),
		}
	}
	return nil
}

// metricBoundParam is the <key>_min and <key>_max pair of one metric.
type metricBoundParam struct {
	key domainmodels.MetricKey
	min *float64
	max *float64
}

// metricBoundParams lists every metric's bound parameters, in domainmodels.MetricKeys order.
func metricBoundParams(params ListModelsAcrossEntriesParams) []metricBoundParam {
	return []metricBoundParam{
		{key: domainmodels.MetricKeyRWork, min: params.RWorkMin, max: params.RWorkMax},
		{key: domainmodels.MetricKeyRFree, min: params.RFreeMin, max: params.RFreeMax},
		{key: domainmodels.MetricKeyCC, min: params.CcMin, max: params.CcMax},
		{key: domainmodels.MetricKeyRSCC, min: params.RsccMin, max: params.RsccMax},
		{key: domainmodels.MetricKeyClashscore, min: params.ClashscoreMin, max: params.ClashscoreMax},
		{key: domainmodels.MetricKeyRamachandranOutliers, min: params.RamachandranOutliersMin, max: params.RamachandranOutliersMax},
		{key: domainmodels.MetricKeySideChainOutliers, min: params.SideChainOutliersMin, max: params.SideChainOutliersMax},
		{key: domainmodels.MetricKeyMolProbityScore, min: params.MolprobityScoreMin, max: params.MolprobityScoreMax},
		{key: domainmodels.MetricKeyWilsonB, min: params.WilsonBMin, max: params.WilsonBMax},
		{key: domainmodels.MetricKeyRMSF, min: params.RmsfMin, max: params.RmsfMax},
	}
}
