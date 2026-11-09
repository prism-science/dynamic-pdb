package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

const (
	// The limits of one POST /v1/selections request, as openapi.yaml documents them.
	maxSelectionModelIDs      = 10000
	maxSelectionModelIDLength = 100
	maxSelectionSearches      = 10
	maxSelectionSearchLength  = 8192
	// maxUnknownSelectionModelErrors bounds the errors listed for model IDs with no active model.
	maxUnknownSelectionModelErrors = 20

	selectionsOffCode         = "SELECTIONS_OFF"
	selectionEmptyCode        = "SELECTION_EMPTY"
	selectionTooLargeCode     = "SELECTION_TOO_LARGE"
	selectionsFullCode        = "SELECTIONS_FULL"
	selectionBusyRetryAfter   = 5 * time.Second
	selectionsFullRetryAfter  = time.Hour
	selectionNotFoundDetail   = "selection not found"
	selectionFreezeActionName = "save selection"

	// SelectionFreezeTimeoutMargin is how long before the server's write timeout a save's
	// statement timeout must end, so a slow save still gets its 504 written.
	SelectionFreezeTimeoutMargin = 30 * time.Second
)

// SelectionsPolicy configures frozen selections (/v1/selections).
type SelectionsPolicy struct {
	// Enabled turns the routes on. Off, they answer 404 with code SELECTIONS_OFF.
	Enabled bool
	// MaxModels is the most models one selection may hold.
	MaxModels int
	// MaxStoredModels caps the rows of all stored selections together, counting each stored
	// request as rows of the same size.
	MaxStoredModels int
	// FreezeTimeout is the statement timeout of one save.
	FreezeTimeout time.Duration
	// MaxInFlight caps the saves running at once on this server instance.
	MaxInFlight int
	// Requests and InternalRequests cap saves per client per Window, for clients through the
	// ingress and in-cluster callers. Clients are told apart as by GlobalRateLimitMiddleware,
	// with TrustedProxies.
	Requests         int
	InternalRequests int
	Window           time.Duration
	TrustedProxies   []netip.Prefix
}

// selections is the server's selections setup; the zero value has the routes off.
type selections struct {
	enabled         bool
	maxModels       int
	maxStoredModels int
	freezeTimeout   time.Duration
	// saving holds one slot per save running on this server instance.
	saving  chan struct{}
	limiter *routeRateLimiter
}

// WithSelections configures frozen selections.
func (s *Server) WithSelections(policy SelectionsPolicy) *Server {
	s.selections = selections{
		enabled:         policy.Enabled,
		maxModels:       policy.MaxModels,
		maxStoredModels: policy.MaxStoredModels,
		freezeTimeout:   policy.FreezeTimeout,
		saving:          make(chan struct{}, policy.MaxInFlight),
		limiter: newRouteRateLimiter(policy.Requests, policy.InternalRequests, policy.Window, policy.TrustedProxies,
			fmt.Sprintf("too many selections saved from this client: each client may save %d selections per %s. "+
				"Wait the number of seconds in Retry-After", policy.Requests, policy.Window)),
	}
	return s
}

// CheckSelectionFreezeTimeout returns an error unless a save's statement timeout ends at least
// SelectionFreezeTimeoutMargin before the server's write timeout, so a save that runs out of time
// is answered with a 504 instead of a dropped connection.
func CheckSelectionFreezeTimeout(freezeTimeout, writeTimeout time.Duration) error {
	if freezeTimeout+SelectionFreezeTimeoutMargin > writeTimeout {
		return fmt.Errorf("selection freeze timeout %s must end at least %s before the server write timeout %s",
			freezeTimeout, SelectionFreezeTimeoutMargin, writeTimeout)
	}
	return nil
}

// tryStartSave takes a save slot without waiting. The returned release is safe to call more
// than once.
func (c *selections) tryStartSave() (release func(), started bool) {
	select {
	case c.saving <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-c.saving }) }, true
	default:
		return nil, false
	}
}

func writeSelectionsOff(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, selectionsOffCode, "saving and reading selections is turned off on this server")
}

// CreateSelection saves a frozen selection: the models the request matches, each with the model
// revision and entry revision it has now.
func (s *Server) CreateSelection(w http.ResponseWriter, r *http.Request) {
	if !s.selections.enabled {
		writeSelectionsOff(w)
		return
	}
	if s.selections.limiter.respondOnLimit(w, r) {
		return
	}
	var request CreateSelectionRequest
	_, rawBody, err := decodeRequestBody(r, &request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	keysErr := s.checkSchema(r, requestKeyErrors(rawBody, CreateSelectionRequest{}), nil)
	if s.writeMutationError(r.Context(), w, keysErr, selectionFreezeActionName) {
		return
	}
	freeze, err := s.selectionFreezeFromRequest(r.Context(), request)
	if err != nil {
		s.writeSelectionRequestError(w, r, err)
		return
	}
	release, started := s.selections.tryStartSave()
	if !started {
		w.Header().Set("Retry-After", retryAfterSeconds(selectionBusyRetryAfter))
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE",
			"the server is saving other selections: try again after Retry-After seconds")
		return
	}
	defer release()
	selection, err := s.database.Selections.Freeze(r.Context(), freeze)
	if err != nil {
		s.writeSelectionFreezeError(w, r, err, request)
		return
	}
	document := s.selectionDocument(*selection)
	w.Header().Set("Location", document.Data.Links.Self)
	writeJSON(w, http.StatusCreated, document)
}

// errSelectionSearchFiltersOff marks a search with filters where model search is off.
var errSelectionSearchFiltersOff = errors.New("model search filters are not enabled on this server")

// selectionFreezeFromRequest turns a save request into a freeze. A request that breaks the
// documented limits gets *validation.Error with every field error; a search with filters where
// model search is off gets errSelectionSearchFiltersOff.
func (s *Server) selectionFreezeFromRequest(ctx context.Context, request CreateSelectionRequest) (db.SelectionFreeze, error) {
	modelIDs, fieldErrors := selectionModelIDs(validation.Root.Field("model_ids"), request.ModelIds)
	excluded, excludedErrors := selectionModelIDs(validation.Root.Field("exclude_model_ids"), request.ExcludeModelIds)
	fieldErrors = append(fieldErrors, excludedErrors...)
	searches, searchTexts, searchErrors, err := s.selectionSearches(ctx, validation.Root.Field("searches"), request.Searches)
	if err != nil {
		return db.SelectionFreeze{}, err
	}
	fieldErrors = append(fieldErrors, searchErrors...)
	// Empty lists ask for nothing, as missing ones do. A list whose values all broke a rule
	// already has its errors.
	if len(fieldErrors) == 0 && len(modelIDs) == 0 && len(searches) == 0 {
		fieldErrors = append(fieldErrors, validation.FieldError{
			Pointer: validation.Root.Field("model_ids"),
			Code:    validation.CodeRequired,
			Detail:  "give model_ids, searches, or both",
		})
	}
	if len(fieldErrors) > 0 {
		return db.SelectionFreeze{}, &validation.Error{Fields: fieldErrors}
	}
	return db.SelectionFreeze{
		ID:              uuid.New(),
		ModelIDs:        modelIDs,
		Searches:        searches,
		ExcludeModelIDs: excluded,
		Request: domainmodels.SelectionRequest{
			ModelIDs:        modelIDs,
			Searches:        searchTexts,
			ExcludeModelIDs: excluded,
		},
		CreatedAt:        time.Now().UTC(),
		MaxModels:        s.selections.maxModels,
		MaxStoredModels:  s.selections.maxStoredModels,
		StatementTimeout: s.selections.freezeTimeout,
	}, nil
}

// selectionModelIDs reads a list of model IDs: each trimmed, none empty or too long, each kept
// once in the order given. A missing list is an empty one.
func selectionModelIDs(at validation.Pointer, values *[]string) ([]string, []validation.FieldError) {
	ids := []string{}
	if values == nil {
		return ids, nil
	}
	if len(*values) > maxSelectionModelIDs {
		return ids, []validation.FieldError{{
			Pointer: at,
			Code:    validation.CodeOutOfRange,
			Detail:  fmt.Sprintf("at most %d model IDs", maxSelectionModelIDs),
		}}
	}
	var fieldErrors []validation.FieldError
	seen := make(map[string]bool, len(*values))
	for index, value := range *values {
		id := strings.TrimSpace(value)
		switch {
		case id == "":
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: at.Index(index), Code: validation.CodeRequired, Detail: "a model ID is empty",
			})
		case len(id) > maxSelectionModelIDLength:
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: at.Index(index),
				Code:    validation.CodeOutOfRange,
				Detail:  fmt.Sprintf("a model ID is longer than %d characters", maxSelectionModelIDLength),
			})
		case !seen[id]:
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, fieldErrors
}

// selectionSearches reads the searches of a save request. It returns each search's filters, each
// search in a fixed form (once, in the order given), and the field errors of the searches. The
// error is errSelectionSearchFiltersOff, or a failure to read the openapi spec.
func (s *Server) selectionSearches(
	ctx context.Context,
	at validation.Pointer,
	values *[]string,
) ([]db.ModelSearch, []string, []validation.FieldError, error) {
	searches := []db.ModelSearch{}
	texts := []string{}
	if values == nil {
		return searches, texts, nil, nil
	}
	if len(*values) > maxSelectionSearches {
		return searches, texts, []validation.FieldError{{
			Pointer: at, Code: validation.CodeOutOfRange, Detail: fmt.Sprintf("at most %d searches", maxSelectionSearches),
		}}, nil
	}
	var fieldErrors []validation.FieldError
	seen := make(map[string]bool, len(*values))
	for index, value := range *values {
		if len(value) > maxSelectionSearchLength {
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: at.Index(index),
				Code:    validation.CodeOutOfRange,
				Detail:  fmt.Sprintf("a search is longer than %d characters", maxSelectionSearchLength),
			})
			continue
		}
		parsed, text, err := modelSearchFromQuery(ctx, strings.TrimSpace(value))
		var paramErr *modelSearchParamError
		switch {
		case errors.As(err, &paramErr):
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: at.Index(index), Code: validation.CodeInvalidValue, Detail: paramErr.Error(),
			})
			continue
		case err != nil:
			return nil, nil, nil, fmt.Errorf("read search %d: %w", index, err)
		}
		search := db.ModelSearch{}
		if parsed != nil {
			if !s.modelSearch.enabled {
				return nil, nil, nil, fmt.Errorf("%w; remove %s from search %d",
					errSelectionSearchFiltersOff, strings.Join(parsed.parameters, ", "), index)
			}
			search = parsed.filters
		}
		if !seen[text] {
			seen[text] = true
			searches = append(searches, search)
			texts = append(texts, text)
		}
	}
	return searches, texts, fieldErrors, nil
}

// writeSelectionRequestError answers a save request that selectionFreezeFromRequest refused.
func (s *Server) writeSelectionRequestError(w http.ResponseWriter, r *http.Request, err error) {
	var validationError *validation.Error
	switch {
	case errors.As(err, &validationError):
		writeFieldErrors(w, validationError.Fields)
	case errors.Is(err, errSelectionSearchFiltersOff):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	default:
		slog.ErrorContext(r.Context(), "read selection request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+selectionFreezeActionName)
	}
}

// writeSelectionFreezeError answers a save the database refused or could not finish.
func (s *Server) writeSelectionFreezeError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	request CreateSelectionRequest,
) {
	var unknown *db.UnknownSelectionModelsError
	switch {
	case errors.As(err, &unknown):
		writeFieldErrors(w, unknownSelectionModelErrors(unknown.ModelIDs, request.ModelIds))
	case errors.Is(err, db.ErrSelectionEmpty):
		writeError(w, http.StatusUnprocessableEntity, selectionEmptyCode,
			"no published model matches the selection, so nothing was saved")
	case errors.Is(err, db.ErrSelectionTooLarge):
		writeError(w, http.StatusUnprocessableEntity, selectionTooLargeCode, fmt.Sprintf(
			"more than %d models match, and one selection holds at most %d. Narrow the searches, "+
				"for example with a model type or a resolution range, or save several selections",
			s.selections.maxModels, s.selections.maxModels))
	case errors.Is(err, db.ErrSelectionsFull):
		slog.ErrorContext(r.Context(), "stored selections reached their budget: raise selections.max_stored_models "+
			"or give the database more space", "err", err)
		w.Header().Set("Retry-After", retryAfterSeconds(selectionsFullRetryAfter))
		writeError(w, http.StatusServiceUnavailable, selectionsFullCode,
			"the space for saved selections is used up; try again after Retry-After seconds")
	case errors.Is(err, db.ErrSelectionFreezeTimeout):
		slog.WarnContext(r.Context(), "selection save ran past its time limit", "err", err)
		w.Header().Set("Retry-After", strconv.Itoa(readTimeoutRetryAfterSeconds))
		writeError(w, http.StatusGatewayTimeout, readTimeoutErrorCode,
			"saving the selection did not finish within the server's time limit. Narrow the searches, "+
				"for example with a model type or a resolution range, and try again after Retry-After seconds")
	case errors.Is(err, db.ErrInvalidModelSearch):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid search: "+err.Error())
	default:
		slog.ErrorContext(r.Context(), selectionFreezeActionName+" failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+selectionFreezeActionName)
	}
}

// unknownSelectionModelErrors points at each requested model ID that has no active model, at
// its first place in the request, for up to maxUnknownSelectionModelErrors IDs.
func unknownSelectionModelErrors(unknownIDs []string, requested *[]string) []validation.FieldError {
	firstIndex := map[string]int{}
	if requested != nil {
		for index, value := range *requested {
			id := strings.TrimSpace(value)
			if _, found := firstIndex[id]; !found {
				firstIndex[id] = index
			}
		}
	}
	at := validation.Root.Field("model_ids")
	fieldErrors := make([]validation.FieldError, 0, min(len(unknownIDs), maxUnknownSelectionModelErrors))
	for _, id := range unknownIDs[:min(len(unknownIDs), maxUnknownSelectionModelErrors)] {
		pointer := at
		if index, found := firstIndex[id]; found {
			pointer = at.Index(index)
		}
		fieldErrors = append(fieldErrors, validation.FieldError{
			Pointer: pointer,
			Code:    validation.CodeInvalidValue,
			Detail:  fmt.Sprintf("no published model of a published entry has ID %q", id),
		})
	}
	return fieldErrors
}

// GetSelection returns a saved selection.
func (s *Server) GetSelection(w http.ResponseWriter, r *http.Request, selectionID string) {
	if !s.selections.enabled {
		writeSelectionsOff(w)
		return
	}
	selection, found := s.readSelection(w, r, selectionID)
	if !found {
		return
	}
	writeJSON(w, http.StatusOK, s.selectionDocument(*selection))
}

// ListSelectionModels lists the models of a saved selection with their frozen revisions.
func (s *Server) ListSelectionModels(
	w http.ResponseWriter,
	r *http.Request,
	selectionID string,
	params ListSelectionModelsParams,
) {
	if !s.selections.enabled {
		writeSelectionsOff(w)
		return
	}
	page, err := listPageFromParams(params.Limit, nil, params.Cursor, listCursorKindSelectionModels)
	if err != nil {
		writeListParamsError(w, err, "invalid limit")
		return
	}
	selection, found := s.readSelection(w, r, selectionID)
	if !found {
		return
	}
	afterOrdinal := 0
	if page.Cursor != nil {
		afterOrdinal = page.Cursor.Ordinal
	}
	items, err := s.database.Selections.ListModels(r.Context(), selection.ID, afterOrdinal, *page.fetchLimit())
	if err != nil {
		slog.ErrorContext(r.Context(), "list selection models failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list the selection's models")
		return
	}
	items, hasNextPage := trimToPage(items, page)
	links, meta, err := nextPageLinksAfter(s, r, items, hasNextPage, func(last domainmodels.SelectionModel) listCursor {
		return listCursor{Kind: listCursorKindSelectionModels, Ordinal: last.Ordinal}
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "build selection model links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list the selection's models")
		return
	}
	data := make([]SelectionModelData, 0, len(items))
	for _, item := range items {
		data = append(data, SelectionModelData{
			Type: jsonAPITypeSelectionModels,
			Id:   item.ModelID,
			Attributes: SelectionModelAttributes{
				ModelId:             item.ModelID,
				EntryId:             item.EntryID,
				ModelRevisionNumber: item.ModelRevisionNumber,
				EntryRevisionNumber: item.EntryRevisionNumber,
			},
		})
	}
	writeJSON(w, http.StatusOK, SelectionModelCollectionDocument{Data: data, Links: links, Meta: meta})
}

// readSelection returns the selection named by a path's ID, or writes the 404 or 500 and
// returns false. An ID that is not a UUID names no selection.
func (s *Server) readSelection(w http.ResponseWriter, r *http.Request, rawID string) (*domainmodels.Selection, bool) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", selectionNotFoundDetail)
		return nil, false
	}
	selection, err := s.database.Selections.Get(r.Context(), id)
	switch {
	case errors.Is(err, db.ErrSelectionNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", selectionNotFoundDetail)
		return nil, false
	case err != nil:
		slog.ErrorContext(r.Context(), "get selection failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to read the selection")
		return nil, false
	}
	return selection, true
}

func (s *Server) selectionDocument(selection domainmodels.Selection) SelectionDocument {
	id := selection.ID.String()
	self := s.publicBaseURL + "/v1/selections/" + id
	return SelectionDocument{Data: SelectionData{
		Type: jsonAPITypeSelections,
		Id:   id,
		Attributes: SelectionAttributes{
			ModelCount: selection.ModelCount,
			EntryCount: selection.EntryCount,
			CreatedAt:  selection.CreatedAt,
			Request: SelectionRequest{
				ModelIds:        nonNilStrings(selection.Request.ModelIDs),
				Searches:        nonNilStrings(selection.Request.Searches),
				ExcludeModelIds: nonNilStrings(selection.Request.ExcludeModelIDs),
			},
		},
		Links: SelectionLinks{Self: self, Models: self + "/models"},
	}}
}
