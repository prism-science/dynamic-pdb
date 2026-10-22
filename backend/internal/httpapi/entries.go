package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

var (
	errInvalidRequest = errors.New("invalid request")
	errConflict       = errors.New("conflict")
)

const (
	defaultListLimit              = 50
	maxListLimit                  = 100
	maxListFilterValues           = 100
	maxEntrySearchQueryLength     = 256
	maxEntryPDBIDLength           = 64
	minProteinSequenceQueryLength = 8
	proteinSequenceAlphabet       = "ACDEFGHIKLMNPQRSTVWYX"
	entryDataSyncMinimumInterval  = 7 * 24 * time.Hour
	entryDataSyncScheduleJitter   = 7 * 24 * time.Hour
)

func (s *Server) ListEntries(w http.ResponseWriter, r *http.Request, params ListEntriesParams) {
	filters, page, err := entryFiltersFromParams(params)
	if err != nil {
		writeListParamsError(w, err, "invalid entry filters")
		return
	}
	activeState := domainmodels.RevisionStateActive
	activeEntryState := domainmodels.EntryStateActive
	filters.State = &activeState
	filters.EntryState = &activeEntryState

	revisions, proteinSequenceSearch, err := s.listEntryRevisionsForSearch(r.Context(), filters, page)
	if errors.Is(err, errInvalidListCursor) {
		writeListParamsError(w, err, "invalid entry filters")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "list entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entries")
		return
	}
	revisions, hasNextPage := trimToPage(revisions, page)

	document := EntryCollectionDocument{Data: make([]EntryInfoData, 0, len(revisions))}
	for _, revision := range revisions {
		item, err := entryInfoAttributesFromRevision(revision)
		if err != nil {
			slog.ErrorContext(r.Context(), "build entry info response failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build entry list response")
			return
		}
		document.Data = append(document.Data, EntryInfoData{
			Type:       jsonAPITypeEntries,
			Id:         item.Id,
			Attributes: item,
		})
	}
	if hasNextPage && len(revisions) > 0 {
		last := revisions[len(revisions)-1]
		document.Links, document.Meta, err = s.nextPageLinks(r, listCursor{
			Kind:                  listCursorKindEntries,
			CreatedAt:             last.CreatedAt,
			ID:                    last.ID,
			ProteinSequenceSearch: proteinSequenceSearch,
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "build entry list links failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build entry list response")
			return
		}
	}
	writeJSON(w, http.StatusOK, document)
}

// listEntryRevisionsForSearch lists entry revisions for filters. A text query that matches
// nothing and looks like a protein sequence is retried as a sequence match. It reports
// whether the rows come from that sequence match, so the next page's cursor continues it.
func (s *Server) listEntryRevisionsForSearch(
	ctx context.Context,
	filters db.EntryRevisionFilters,
	page listPage,
) ([]domainmodels.EntryRevision, bool, error) {
	if page.Cursor != nil && page.Cursor.ProteinSequenceSearch {
		proteinSequence, ok := proteinSequenceFromSearchQuery(filters.Query)
		if !ok {
			return nil, false, fmt.Errorf("%w: protein sequence cursor needs a protein sequence query", errInvalidListCursor)
		}
		revisions, err := s.listEntryRevisionsByProteinSequence(ctx, filters, proteinSequence)
		return revisions, true, err
	}

	revisions, err := s.database.Entries.List(ctx, filters)
	if err != nil {
		return nil, false, fmt.Errorf("list entry revisions: %w", err)
	}
	// Under a cursor, an empty page is the end of the text matches, not a reason to switch
	// to sequence matches. Deprecated offset requests keep their old fallback at any depth.
	if len(revisions) > 0 || page.Cursor != nil {
		return revisions, false, nil
	}
	proteinSequence, ok := proteinSequenceFromSearchQuery(filters.Query)
	if !ok {
		return revisions, false, nil
	}
	revisions, err = s.listEntryRevisionsByProteinSequence(ctx, filters, proteinSequence)
	return revisions, true, err
}

func (s *Server) listEntryRevisionsByProteinSequence(
	ctx context.Context,
	filters db.EntryRevisionFilters,
	proteinSequence string,
) ([]domainmodels.EntryRevision, error) {
	filters.Query = ""
	filters.ProteinSequence = proteinSequence
	revisions, err := s.database.Entries.List(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list entry revisions by protein sequence: %w", err)
	}
	return revisions, nil
}

func (s *Server) CreateEntry(w http.ResponseWriter, r *http.Request) {
	var req CreateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	schemaErr := s.checkSchema(r, createEntryRequestErrors(req))
	if s.writeMutationError(r.Context(), w, schemaErr, "create entry") {
		return
	}

	result, err := s.createInitialEntryRevision(r.Context(), req, user.ID)
	if s.writeMutationError(r.Context(), w, err, "create entry") {
		return
	}
	writeJSON(w, http.StatusCreated, CreateEntryRevisionDocument{
		Data: CreateEntryRevisionData{
			Type:       jsonAPITypeEntryRevisionResults,
			Id:         result.RevisionId,
			Attributes: result,
		},
	})
}

func (s *Server) createInitialEntryRevision(
	ctx context.Context,
	req CreateEntryRequest,
	createdBy uuid.UUID,
) (CreateEntryRevisionAttributes, error) {
	if err := validateArtifactRequests(req.Entry.Artifacts, artifactAttachmentScopeEntry); err != nil {
		return CreateEntryRevisionAttributes{}, fmt.Errorf("validate initial entry artifacts: %w", err)
	}
	title := strings.TrimSpace(req.Entry.Title)
	if title == "" {
		return CreateEntryRevisionAttributes{}, invalidRequest("entry title is required")
	}
	entryID, err := domainmodels.NewEntryID()
	if err != nil {
		return CreateEntryRevisionAttributes{}, fmt.Errorf("generate entry id: %w", err)
	}
	metadata, err := entryMetadataFromAPIFields(req.Entry)
	if err != nil {
		return CreateEntryRevisionAttributes{}, fmt.Errorf("decode initial entry metadata: %w", err)
	}
	now := time.Now().UTC()
	attributes := CreateEntryRevisionAttributes{
		EntryId:      entryID,
		State:        CreateEntryRevisionAttributesStateInReview,
		ModelResults: make([]ModelOperationResult, 0),
	}

	err = s.database.Do(ctx, func(ctx context.Context) error {
		revisions, err := s.database.Entries.List(ctx, db.EntryRevisionFilters{EntryID: &entryID})
		if err != nil {
			return fmt.Errorf("check entry id: %w", err)
		}
		if len(revisions) > 0 {
			return fmt.Errorf("entry id already exists: %w", errConflict)
		}
		if err := s.ensurePDBReferenceAvailable(ctx, metadata, nil); err != nil {
			return fmt.Errorf("validate initial entry PDB reference: %w", err)
		}

		revision, err := s.database.Entries.Create(ctx, domainmodels.EntryRevision{
			ID:                uuid.New(),
			EntryID:           entryID,
			State:             domainmodels.RevisionStateInReview,
			EntryState:        domainmodels.EntryStateActive,
			Title:             &title,
			ThumbnailImageURL: trimmedStringPtr(req.Entry.ThumbnailImageUrl),
			Metadata:          metadata,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return fmt.Errorf("create entry revision: %w", err)
		}
		attributes.RevisionId = revision.ID
		if strings.TrimSpace(metadata.ExternalRefs[domainmodels.EntrySourcePDB]) != "" {
			if err := s.database.DataSyncJobs.Schedule(ctx, domainmodels.DataSyncJob{
				EntryID:     entryID,
				ScheduledAt: nextEntryDataSyncScheduledAt(now),
			}); err != nil {
				return fmt.Errorf("schedule entry data sync: %w", err)
			}
		}

		if req.Entry.Artifacts != nil {
			for _, artifactRequest := range *req.Entry.Artifacts {
				artifact, err := s.createArtifact(ctx, artifactRequest, createdBy, now)
				if err != nil {
					return fmt.Errorf("create initial entry artifact: %w", err)
				}
				if err := s.database.Artifacts.AttachToEntryRevision(ctx, revision.ID, artifact.ID); err != nil {
					return fmt.Errorf("attach artifact to entry revision: %w", err)
				}
			}
		}
		if req.ModelOperations != nil {
			for _, operation := range *req.ModelOperations {
				if operation.Op != AddModelOperationOpAdd {
					return invalidRequest("model operation must be add")
				}
				model, err := s.createInitialModelRevisionForPendingEntry(
					ctx,
					entryID,
					createModelDataFromAddOperation(operation.Data),
					createdBy,
				)
				if err != nil {
					return fmt.Errorf("create model from add operation: %w", err)
				}
				attributes.ModelResults = append(attributes.ModelResults, ModelOperationResult{
					Op:              ModelOperationResultOpAdd,
					ModelId:         model.ModelId,
					ModelRevisionId: model.RevisionId,
				})
			}
		}

		return nil
	})
	if err != nil {
		return CreateEntryRevisionAttributes{}, fmt.Errorf("create initial entry graph: %w", err)
	}
	return attributes, nil
}

func nextEntryDataSyncScheduledAt(now time.Time) time.Time {
	// Scheduling jitter does not require cryptographic randomness.
	//nolint:gosec
	jitter := time.Duration(rand.Int64N(int64(entryDataSyncScheduleJitter)))
	return now.Add(entryDataSyncMinimumInterval + jitter)
}

func applyEntryRevisionChange(
	revision *domainmodels.EntryRevision,
	change EntryRevisionChange,
	fields map[string]json.RawMessage,
) error {
	if err := validateRevisionFields(fields, entryRevisionFields); err != nil {
		return err
	}
	if raw, present := fields["title"]; present {
		if isJSONNull(raw) || change.Title == nil {
			return invalidRequest("entry title cannot be null")
		}
		title := strings.TrimSpace(*change.Title)
		if title == "" {
			return invalidRequest("entry title cannot be empty")
		}
		revision.Title = &title
	}
	if raw, present := fields["thumbnail_image_url"]; present {
		if isJSONNull(raw) {
			revision.ThumbnailImageURL = nil
		} else {
			revision.ThumbnailImageURL = trimmedStringPtr(change.ThumbnailImageUrl)
		}
	}
	metadata, err := entryMetadataFromAPIFields(change)
	if err != nil {
		return fmt.Errorf("decode changed entry fields: %w", err)
	}
	if _, present := fields["external_refs"]; present {
		revision.Metadata.ExternalRefs = metadata.ExternalRefs
	}
	if _, present := fields["details"]; present {
		revision.Metadata.Details = metadata.Details
	}
	if _, present := fields["resolution"]; present {
		revision.Metadata.Resolution = metadata.Resolution
	}
	if _, present := fields["method"]; present {
		revision.Metadata.Method = metadata.Method
	}
	if _, present := fields["space_group"]; present {
		revision.Metadata.SpaceGroup = metadata.SpaceGroup
	}
	if _, present := fields["crystallography"]; present {
		revision.Metadata.Crystallography = metadata.Crystallography
	}
	return nil
}

func (s *Server) CreateModel(w http.ResponseWriter, r *http.Request, entryID string) {
	var request CreateModelRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	schemaErr := s.checkSchema(r, createModelRequestErrors(validation.Root.Field("model"), request.Model))
	if s.writeMutationError(r.Context(), w, schemaErr, "create model") {
		return
	}

	result, err := s.createInitialModelRevision(r.Context(), entryID, request.Model, user.ID)
	if s.writeMutationError(r.Context(), w, err, "create model") {
		return
	}
	writeJSON(w, http.StatusCreated, CreateModelRevisionDocument{
		Data: CreateModelRevisionData{
			Type:       jsonAPITypeModelRevisionResults,
			Id:         result.RevisionId,
			Attributes: result,
		},
	})
}

func (s *Server) createInitialModelRevision(
	ctx context.Context,
	entryID string,
	data CreateModelData,
	createdBy uuid.UUID,
) (CreateModelRevisionAttributes, error) {
	return s.createInitialModelRevisionForEntry(ctx, entryID, data, createdBy, true)
}

func (s *Server) createInitialModelRevisionForPendingEntry(
	ctx context.Context,
	entryID string,
	data CreateModelData,
	createdBy uuid.UUID,
) (CreateModelRevisionAttributes, error) {
	return s.createInitialModelRevisionForEntry(ctx, entryID, data, createdBy, false)
}

func (s *Server) createInitialModelRevisionForEntry(
	ctx context.Context,
	entryID string,
	data CreateModelData,
	createdBy uuid.UUID,
	requireActiveEntry bool,
) (CreateModelRevisionAttributes, error) {
	if err := validateArtifactRequests(data.Artifacts, artifactAttachmentScopeModel); err != nil {
		return CreateModelRevisionAttributes{}, fmt.Errorf("validate initial model artifacts: %w", err)
	}
	title := strings.TrimSpace(data.Title)
	if title == "" {
		return CreateModelRevisionAttributes{}, invalidRequest("model title is required")
	}
	metadata, err := modelMetadataFromRequest(data.Metadata)
	if err != nil {
		return CreateModelRevisionAttributes{}, fmt.Errorf("decode model metadata: %w", err)
	}
	now := time.Now().UTC()
	attributes := CreateModelRevisionAttributes{
		EntryId:        entryID,
		IdempotencyKey: modelRevisionIdempotencyKeyFromCreateModelData(data),
		State:          RevisionStateInReview,
	}
	err = s.database.Do(ctx, func(ctx context.Context) error {
		if attributes.IdempotencyKey != nil {
			activeState := domainmodels.RevisionStateActive
			existing, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
				State:          &activeState,
				IdempotencyKey: attributes.IdempotencyKey,
			})
			if err == nil {
				attributes = createModelRevisionAttributesFromRevision(*existing)
				return nil
			}
			if !errors.Is(err, db.ErrModelRevisionNotFound) {
				return fmt.Errorf("check model revision idempotency key: %w", err)
			}
		}
		if requireActiveEntry {
			activeState := domainmodels.RevisionStateActive
			activeEntryState := domainmodels.EntryStateActive
			if _, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
				EntryID:    &entryID,
				State:      &activeState,
				EntryState: &activeEntryState,
			}); err != nil {
				return fmt.Errorf("get active entry revision: %w", err)
			}
		}
		modelID, err := s.database.Models.NextID(ctx, entryID)
		if err != nil {
			return fmt.Errorf("generate model id: %w", err)
		}
		attributes.ModelId = modelID
		if _, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{ModelID: &modelID}); err == nil {
			return fmt.Errorf("model id already exists: %w", errConflict)
		} else if !errors.Is(err, db.ErrModelRevisionNotFound) {
			return fmt.Errorf("check model id: %w", err)
		}
		artifacts, err := s.createArtifacts(ctx, data.Artifacts, createdBy, now)
		if err != nil {
			return fmt.Errorf("create model artifacts: %w", err)
		}
		revision, err := s.database.Models.Create(ctx, entryID, domainmodels.ModelRevision{
			ID:                uuid.New(),
			ModelID:           modelID,
			PrimaryArtifactID: data.PrimaryArtifactId,
			State:             domainmodels.RevisionStateInReview,
			ModelState:        domainmodels.ModelStateActive,
			Title:             &title,
			ThumbnailImageURL: trimmedStringPtr(data.ThumbnailImageUrl),
			Metadata:          metadata,
			IdempotencyKey:    attributes.IdempotencyKey,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return fmt.Errorf("create model revision: %w", err)
		}
		attributes.RevisionId = revision.ID
		if err := s.attachModelArtifacts(ctx, revision.ID, artifacts); err != nil {
			return fmt.Errorf("attach model artifacts: %w", err)
		}
		if data.Metrics != nil {
			if err := s.createModelMetrics(ctx, revision.ID, *data.Metrics, now); err != nil {
				return fmt.Errorf("create model metrics: %w", err)
			}
		}
		if data.Runs != nil {
			if err := s.createModelRuns(ctx, revision.ID, *data.Runs, createdBy, now); err != nil {
				return fmt.Errorf("create model runs: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return CreateModelRevisionAttributes{}, fmt.Errorf("create initial model revision: %w", err)
	}
	return attributes, nil
}

func createModelDataFromAddOperation(data AddModelData) CreateModelData {
	return CreateModelData(data)
}

func (s *Server) createArtifacts(
	ctx context.Context,
	requests *[]CreateArtifactRequest,
	createdBy uuid.UUID,
	now time.Time,
) ([]domainmodels.Artifact, error) {
	if requests == nil {
		return nil, nil
	}
	artifacts := make([]domainmodels.Artifact, 0, len(*requests))
	for _, request := range *requests {
		artifact, err := s.createArtifact(ctx, request, createdBy, now)
		if err != nil {
			return nil, fmt.Errorf("create model artifact: %w", err)
		}
		artifacts = append(artifacts, *artifact)
	}
	return artifacts, nil
}

func (s *Server) attachModelArtifacts(
	ctx context.Context,
	modelRevisionID uuid.UUID,
	artifacts []domainmodels.Artifact,
) error {
	for _, artifact := range artifacts {
		if err := s.database.Artifacts.AttachToModelRevision(ctx, modelRevisionID, artifact.ID); err != nil {
			return fmt.Errorf("attach artifact to model revision: %w", err)
		}
	}
	return nil
}

func (s *Server) CreateModelRevision(w http.ResponseWriter, r *http.Request, entryID, modelID string) {
	payload, err := decodeCreateModelRevisionPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	schemaErr := s.checkSchema(r, modelRevisionChangeErrors(payload))
	if s.writeMutationError(r.Context(), w, schemaErr, "create model revision") {
		return
	}

	result, err := s.createModelRevisionGraph(r.Context(), entryID, modelID, payload, user.ID)
	if s.writeMutationError(r.Context(), w, err, "create model revision") {
		return
	}
	writeJSON(w, http.StatusCreated, CreateModelRevisionDocument{
		Data: CreateModelRevisionData{
			Type:       jsonAPITypeModelRevisionResults,
			Id:         result.RevisionId,
			Attributes: result,
		},
	})
}

func (s *Server) createModelRevisionGraph(
	ctx context.Context,
	entryID, modelID string,
	payload createModelRevisionPayload,
	createdBy uuid.UUID,
) (CreateModelRevisionAttributes, error) {
	if err := validateRevisionFields(payload.ModelFields, modelRevisionFields); err != nil {
		return CreateModelRevisionAttributes{}, err
	}
	if len(payload.ModelFields) == 0 {
		return CreateModelRevisionAttributes{}, invalidRequest("model change must contain at least one field")
	}
	data := payload.Request.Model
	if err := validateArtifactRequests(data.Artifacts, artifactAttachmentScopeModel); err != nil {
		return CreateModelRevisionAttributes{}, fmt.Errorf("validate model artifacts: %w", err)
	}
	for _, field := range []string{"artifacts", "metrics", "runs"} {
		if raw, present := payload.ModelFields[field]; present && isJSONNull(raw) {
			return CreateModelRevisionAttributes{}, invalidRequest("model %s cannot be null", field)
		}
	}
	now := time.Now().UTC()
	attributes := CreateModelRevisionAttributes{
		EntryId:        entryID,
		IdempotencyKey: modelRevisionIdempotencyKeyFromChange(data),
		ModelId:        modelID,
		State:          RevisionStateInReview,
	}
	err := s.database.Do(ctx, func(ctx context.Context) error {
		base, err := s.activeModelRevisionForMutation(ctx, entryID, modelID)
		if err != nil {
			return fmt.Errorf("get active model revision: %w", err)
		}
		attributes.BaseRevisionId = &base.ID
		if attributes.IdempotencyKey != nil {
			activeState := domainmodels.RevisionStateActive
			existing, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
				State:          &activeState,
				IdempotencyKey: attributes.IdempotencyKey,
			})
			if err == nil {
				attributes = createModelRevisionAttributesFromRevision(*existing)
				return nil
			}
			if !errors.Is(err, db.ErrModelRevisionNotFound) {
				return fmt.Errorf("check model revision idempotency key: %w", err)
			}
		}
		artifacts, err := s.createArtifacts(ctx, data.Artifacts, createdBy, now)
		if err != nil {
			return fmt.Errorf("create model artifacts: %w", err)
		}
		revision := domainmodels.ModelRevision{
			ID:                uuid.New(),
			EntryID:           entryID,
			ModelID:           base.ModelID,
			ParentRevisionID:  &base.ID,
			PrimaryArtifactID: base.PrimaryArtifactID,
			State:             domainmodels.RevisionStateInReview,
			ModelState:        domainmodels.ModelStateActive,
			Title:             base.Title,
			ThumbnailImageURL: base.ThumbnailImageURL,
			Metadata:          base.Metadata,
			IdempotencyKey:    attributes.IdempotencyKey,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if raw, present := payload.ModelFields["title"]; present {
			if isJSONNull(raw) || data.Title == nil {
				return invalidRequest("model title cannot be null")
			}
			title := strings.TrimSpace(*data.Title)
			if title == "" {
				return invalidRequest("model title cannot be empty")
			}
			revision.Title = &title
		}
		if raw, present := payload.ModelFields["thumbnail_image_url"]; present {
			if isJSONNull(raw) {
				revision.ThumbnailImageURL = nil
			} else {
				revision.ThumbnailImageURL = trimmedStringPtr(data.ThumbnailImageUrl)
			}
		}
		if raw, present := payload.ModelFields["primary_artifact_id"]; present {
			if isJSONNull(raw) {
				revision.PrimaryArtifactID = nil
			} else {
				revision.PrimaryArtifactID = data.PrimaryArtifactId
			}
		}
		if raw, present := payload.ModelFields["metadata"]; present {
			if isJSONNull(raw) || data.Metadata == nil {
				return invalidRequest("model metadata cannot be null")
			}
			metadata, err := modelMetadataFromRequest(data.Metadata)
			if err != nil {
				return fmt.Errorf("decode model metadata: %w", err)
			}
			revision.Metadata = metadata
		}
		created, err := s.database.Models.Create(ctx, entryID, revision)
		if err != nil {
			return fmt.Errorf("create model revision: %w", err)
		}
		attributes.RevisionId = created.ID
		if err := s.copyOrAttachModelRevisionContents(
			ctx,
			base.ID,
			created.ID,
			data.Artifacts != nil,
			artifacts,
			data.Metrics,
			data.Runs,
			createdBy,
			now,
		); err != nil {
			return fmt.Errorf("populate model revision: %w", err)
		}
		return nil
	})
	if err != nil {
		return CreateModelRevisionAttributes{}, fmt.Errorf("create model revision graph: %w", err)
	}
	return attributes, nil
}

func (s *Server) activeModelRevisionForMutation(
	ctx context.Context,
	entryID, modelID string,
) (*domainmodels.ModelRevision, error) {
	activeState := domainmodels.RevisionStateActive
	revision, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
		EntryID: &entryID,
		ModelID: &modelID,
		State:   &activeState,
	})
	if err != nil {
		return nil, fmt.Errorf("get active model revision: %w", err)
	}
	if revision.ModelState != domainmodels.ModelStateActive {
		return nil, fmt.Errorf("model is deleted: %w", errConflict)
	}
	return revision, nil
}

func (s *Server) copyOrAttachModelRevisionContents(
	ctx context.Context,
	fromRevisionID, toRevisionID uuid.UUID,
	replaceArtifacts bool,
	artifacts []domainmodels.Artifact,
	metrics *[]CreateMetricRequest,
	runs *[]CreateRunRequest,
	createdBy uuid.UUID,
	now time.Time,
) error {
	if !replaceArtifacts {
		if err := s.database.Artifacts.CopyModelRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
			return fmt.Errorf("copy model revision artifacts: %w", err)
		}
	} else if err := s.attachModelArtifacts(ctx, toRevisionID, artifacts); err != nil {
		return fmt.Errorf("attach replacement model artifacts: %w", err)
	}
	if metrics == nil {
		if err := s.database.Metrics.CopyModelRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
			return fmt.Errorf("copy model revision metrics: %w", err)
		}
	} else if err := s.createModelMetrics(ctx, toRevisionID, *metrics, now); err != nil {
		return fmt.Errorf("create replacement model metrics: %w", err)
	}
	if runs == nil {
		if err := s.database.Runs.CopyModelRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
			return fmt.Errorf("copy model revision runs: %w", err)
		}
	} else if err := s.createModelRuns(ctx, toRevisionID, *runs, createdBy, now); err != nil {
		return fmt.Errorf("create replacement model runs: %w", err)
	}
	return nil
}

func (s *Server) createModelMetrics(
	ctx context.Context,
	modelRevisionID uuid.UUID,
	requests []CreateMetricRequest,
	now time.Time,
) error {
	for _, request := range requests {
		metric, err := s.createMetric(ctx, request, now)
		if err != nil {
			return fmt.Errorf("create model metric: %w", err)
		}
		if err := s.database.Metrics.AttachToModelRevision(ctx, modelRevisionID, metric.ID); err != nil {
			return fmt.Errorf("attach metric to model revision: %w", err)
		}
	}
	return nil
}

func (s *Server) createModelRuns(
	ctx context.Context,
	modelRevisionID uuid.UUID,
	requests []CreateRunRequest,
	createdBy uuid.UUID,
	now time.Time,
) error {
	for _, request := range requests {
		run, err := s.createRun(ctx, request, createdBy, now)
		if err != nil {
			return fmt.Errorf("create model run: %w", err)
		}
		if err := s.database.Runs.AttachToModelRevision(ctx, modelRevisionID, run.ID); err != nil {
			return fmt.Errorf("attach run to model revision: %w", err)
		}
		if request.Artifacts != nil {
			for _, artifactRequest := range *request.Artifacts {
				if err := s.attachArtifactToRun(ctx, run.ID, artifactRequest); err != nil {
					return fmt.Errorf("attach artifact to model run: %w", err)
				}
			}
		}
	}
	return nil
}

func (s *Server) ensurePDBReferenceAvailable(
	ctx context.Context,
	metadata domainmodels.EntryMetadata,
	excludeEntryID *string,
) error {
	pdbID := strings.TrimSpace(metadata.ExternalRefs[domainmodels.EntrySourcePDB])
	if pdbID == "" {
		return nil
	}
	activeState := domainmodels.RevisionStateActive
	activeEntryState := domainmodels.EntryStateActive
	limit := 2
	revisions, err := s.database.Entries.List(ctx, db.EntryRevisionFilters{
		State:      &activeState,
		EntryState: &activeEntryState,
		PDBIDs:     []string{pdbID},
		Limit:      &limit,
	})
	if err != nil {
		return fmt.Errorf("check active PDB reference: %w", err)
	}
	for _, revision := range revisions {
		if excludeEntryID == nil || revision.EntryID != *excludeEntryID {
			return fmt.Errorf("active PDB reference already exists: %w", errConflict)
		}
	}
	return nil
}

func (s *Server) GetEntry(w http.ResponseWriter, r *http.Request, entryID string) {
	revision, err := s.activeEntryRevision(r.Context(), entryID)
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}
	sequences, err := s.database.ProteinSequences.List(r.Context(), db.ProteinSequenceFilters{EntryRevisionID: &revision.ID})
	if err != nil {
		slog.ErrorContext(r.Context(), "list entry protein sequences failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}
	polymerEntities, err := s.database.PolymerEntities.List(r.Context(), db.PolymerEntityFilters{
		EntryRevisionID: &revision.ID,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "list entry polymer entities failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}
	properties, err := entryPropertiesFromModel(revision.Metadata)
	if err != nil {
		slog.ErrorContext(r.Context(), "build entry response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}
	included := make([]EntryProteinSequenceData, 0, len(sequences))
	sequenceIdentifiers := make([]EntryProteinSequenceIdentifier, 0, len(sequences))
	for _, sequence := range sequences {
		included = append(included, EntryProteinSequenceData{
			Type: EntryProteinSequenceDataTypeProteinSequences,
			Id:   sequence.ID,
			Attributes: EntryProteinSequenceAttributes{
				RecordIndex: sequence.RecordIndex,
				Header:      sequence.Header,
				Sequence:    sequence.Sequence,
				CreatedAt:   sequence.CreatedAt,
			},
			Relationships: EntryProteinSequenceRelationships{
				SourceArtifact: EntryProteinSequenceSourceArtifactRelationship{
					Data: EntrySourceArtifactIdentifier{Type: Artifacts, Id: sequence.SourceArtifactID},
				},
			},
		})
		sequenceIdentifiers = append(sequenceIdentifiers, EntryProteinSequenceIdentifier{
			Type: EntryProteinSequenceIdentifierTypeProteinSequences,
			Id:   sequence.ID,
		})
	}
	writeJSON(w, http.StatusOK, EntryDocument{
		Data: EntryData{
			Type: Entries,
			Id:   revision.EntryID,
			Attributes: EntryAttributes{
				Title:             revision.Title,
				ThumbnailImageUrl: revision.ThumbnailImageURL,
				ExternalRefs:      properties.ExternalRefs,
				Details:           properties.Details,
				Resolution:        properties.Resolution,
				Method:            properties.Method,
				SpaceGroup:        properties.SpaceGroup,
				Crystallography:   properties.Crystallography,
				PublishedAt:       revision.PublishedAt,
				CreatedAt:         revision.CreatedAt,
				UpdatedAt:         revision.UpdatedAt,
				PolymerEntities:   polymerEntitiesFromModels(polymerEntities),
			},
			Relationships: EntryRelationships{
				CreatedBy: EntryCreatedByRelationship{
					Data: EntryCreatedByIdentifier{Type: Users, Id: revision.CreatedBy},
				},
				ProteinSequences: EntryProteinSequencesRelationship{
					Data: sequenceIdentifiers,
				},
			},
		},
		Included: &included,
	})
}

func (s *Server) ListSimilarEntries(
	w http.ResponseWriter,
	r *http.Request,
	entryID string,
	params ListSimilarEntriesParams,
) {
	if params.Limit != nil && *params.Limit < 0 {
		writeError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit must be non-negative")
		return
	}
	if params.Offset != nil && *params.Offset < 0 {
		writeError(w, http.StatusBadRequest, "INVALID_OFFSET", "offset must be non-negative")
		return
	}
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindSimilarEntries)
	if err != nil {
		writeListParamsError(w, err, "invalid pagination")
		return
	}
	if _, err := s.activeEntryRevision(r.Context(), entryID); errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	} else if err != nil {
		slog.ErrorContext(r.Context(), "get entry for similar entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list similar entries")
		return
	}
	entries, err := s.database.ProteinSequenceSimilarities.ListSimilarEntries(r.Context(), db.SimilarEntryFilters{
		EntryID: entryID,
		Limit:   page.fetchLimit(),
		Offset:  page.Offset,
		After:   page.afterSimilarEntry(),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "list similar entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list similar entries")
		return
	}
	entries, hasNextPage := trimToPage(entries, page)
	data := make([]SimilarEntryData, 0, len(entries))
	for _, entry := range entries {
		info, err := entryInfoAttributesFromRevision(entry.Entry)
		if err != nil {
			slog.ErrorContext(r.Context(), "build similar entry response failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list similar entries")
			return
		}
		matches := make([]ProteinSequenceSimilarityMatch, 0, len(entry.Matches))
		for _, match := range entry.Matches {
			matches = append(matches, ProteinSequenceSimilarityMatch{
				SourceSequenceId: match.SourceSequenceID,
				SimilarSequence:  proteinSequenceFromModel(match.SimilarSequence),
				Score:            match.Similarity.Score,
				Tool:             match.Similarity.Tool,
				Metadata:         mapFromNil(match.Similarity.Metadata),
				CreatedAt:        match.Similarity.CreatedAt,
			})
		}
		item := SimilarEntryAttributes{Entry: info, Score: entry.Score, Matches: matches}
		data = append(data, SimilarEntryData{
			Type:       jsonAPITypeSimilarEntries,
			Id:         item.Entry.Id,
			Attributes: item,
		})
	}
	links, pageMeta, err := nextPageLinksAfter(s, r, entries, hasNextPage, func(last domainmodels.SimilarEntry) listCursor {
		return listCursor{Kind: listCursorKindSimilarEntries, Score: &last.Score, EntryID: last.Entry.EntryID}
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "build similar entry list links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list similar entries")
		return
	}
	meta := &SimilarEntryCollectionMeta{
		Limit:  &page.Limit,
		Offset: page.Offset,
	}
	if pageMeta != nil {
		meta.NextCursor = &pageMeta.NextCursor
	}
	writeJSON(w, http.StatusOK, SimilarEntryCollectionDocument{Data: data, Links: links, Meta: meta})
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
	activeState := domainmodels.RevisionStateActive
	activeEntryState := domainmodels.EntryStateActive
	activeModelState := domainmodels.ModelStateActive
	filters.State = &activeState
	filters.EntryState = &activeEntryState
	filters.ModelState = &activeModelState
	filters.ExcludeResidueData = true

	revisions, err := s.database.Models.List(r.Context(), filters)
	if err != nil {
		slog.ErrorContext(r.Context(), "list models across entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
		return
	}
	s.writeModelListPage(w, r, revisions, page)
}

func (s *Server) ListModels(w http.ResponseWriter, r *http.Request, entryID string, params ListModelsParams) {
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindModels)
	if err != nil {
		writeListParamsError(w, err, "invalid model filters")
		return
	}
	if _, err := s.activeEntryRevision(r.Context(), entryID); errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeJSON(w, http.StatusOK, modelCollectionDocumentFromAttributes([]ModelAttributes{}))
		return
	} else if err != nil {
		slog.ErrorContext(r.Context(), "get entry for models failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
		return
	}
	activeState := domainmodels.RevisionStateActive
	activeModelState := domainmodels.ModelStateActive
	revisions, err := s.database.Models.List(r.Context(), db.ModelRevisionFilters{
		EntryID:            &entryID,
		State:              &activeState,
		ModelState:         &activeModelState,
		Limit:              page.fetchLimit(),
		Offset:             page.Offset,
		After:              page.after(),
		ExcludeResidueData: true,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "list models failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
		return
	}
	s.writeModelListPage(w, r, revisions, page)
}

// writeModelListPage writes one page of a model list. revisions holds the extra row
// requested by page.fetchLimit, if it exists.
func (s *Server) writeModelListPage(
	w http.ResponseWriter,
	r *http.Request,
	revisions []domainmodels.ModelRevision,
	page listPage,
) {
	revisions, hasNextPage := trimToPage(revisions, page)
	response, err := s.modelAttributesFromRevisions(r.Context(), revisions)
	if err != nil {
		slog.ErrorContext(r.Context(), "build model list response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
		return
	}
	document := modelCollectionDocumentFromAttributes(response)
	if hasNextPage && len(revisions) > 0 {
		last := revisions[len(revisions)-1]
		document.Links, document.Meta, err = s.nextPageLinks(r, listCursor{
			Kind:      listCursorKindModels,
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "build model list links failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
			return
		}
	}
	writeJSON(w, http.StatusOK, document)
}

func (s *Server) GetModel(w http.ResponseWriter, r *http.Request, entryID, modelID string) {
	revision, err := s.activeModelRevision(r.Context(), entryID, modelID)
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get model failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model")
		return
	}
	metrics, err := s.database.Metrics.List(r.Context(), db.MetricFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		slog.ErrorContext(r.Context(), "list model metrics failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model")
		return
	}
	model, err := modelAttributesFromRevision(*revision, metrics)
	if err != nil {
		slog.ErrorContext(r.Context(), "build model response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model")
		return
	}
	model.ResidueData = ptr(residueDataFromModels(revision.Metadata.ResidueData))
	writeJSON(w, http.StatusOK, ModelDocument{
		Data: ModelData{
			Type:       jsonAPITypeModels,
			Id:         model.Id,
			Attributes: model,
		},
	})
}

func (s *Server) ListArtifacts(w http.ResponseWriter, r *http.Request, entryID string, params ListArtifactsParams) {
	filters, page, err := artifactFiltersFromParams(params)
	if err != nil {
		writeListParamsError(w, err, "invalid artifact filters")
		return
	}
	revision, err := s.activeEntryRevision(r.Context(), entryID)
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeJSON(w, http.StatusOK, ArtifactCollectionDocument{Data: []ArtifactData{}})
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get entry for artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list artifacts")
		return
	}
	filters.EntryRevisionID = &revision.ID
	artifacts, err := s.database.Artifacts.List(r.Context(), filters)
	if err != nil {
		slog.ErrorContext(r.Context(), "list entry artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list artifacts")
		return
	}
	data, links, meta, err := s.artifactListPage(r, artifacts, page)
	if err != nil {
		slog.ErrorContext(r.Context(), "build artifact response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list artifacts")
		return
	}
	writeJSON(w, http.StatusOK, ArtifactCollectionDocument{Data: data, Links: links, Meta: meta})
}

func (s *Server) ListModelArtifacts(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID string,
	params ListModelArtifactsParams,
) {
	filters, page, err := modelArtifactFiltersFromParams(params)
	if err != nil {
		writeListParamsError(w, err, "invalid artifact filters")
		return
	}
	revision, err := s.activeModelRevision(r.Context(), entryID, modelID)
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get model for artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	filters.ModelRevisionID = &revision.ID
	artifacts, err := s.database.Artifacts.List(r.Context(), filters)
	if err != nil {
		slog.ErrorContext(r.Context(), "list model artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	runs, err := s.database.Runs.List(r.Context(), db.RunFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		slog.ErrorContext(r.Context(), "list model runs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	links, err := s.database.Runs.ListArtifactLinks(r.Context(), revision.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "list model run artifact links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	artifactData, pageLinks, pageMeta, err := s.artifactListPage(r, artifacts, page)
	if err != nil {
		slog.ErrorContext(r.Context(), "build artifact response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	runItems := make([]Run, 0, len(runs))
	for _, run := range runs {
		runItems = append(runItems, runFromModel(run))
	}
	relationItems := make([]RunArtifact, 0, len(links))
	for _, link := range links {
		relationItems = append(relationItems, runArtifactFromLink(link))
	}
	meta := ModelArtifactCollectionMeta{
		Runs:      runItems,
		Relations: relationItems,
	}
	if pageMeta != nil {
		meta.NextCursor = &pageMeta.NextCursor
	}
	writeJSON(w, http.StatusOK, ModelArtifactCollectionDocument{
		Data:  artifactData,
		Links: pageLinks,
		Meta:  meta,
	})
}

// artifactListPage trims artifacts to the page and builds their response items, plus
// the next page's links when another page exists.
func (s *Server) artifactListPage(
	r *http.Request,
	artifacts []domainmodels.Artifact,
	page listPage,
) ([]ArtifactData, *CollectionLinks, *CursorPageMeta, error) {
	artifacts, hasNextPage := trimToPage(artifacts, page)
	items, err := artifactAttributesFromModels(artifacts)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build artifact attributes: %w", err)
	}
	data := make([]ArtifactData, 0, len(items))
	for _, item := range items {
		data = append(data, ArtifactData{
			Type:       jsonAPITypeArtifacts,
			Id:         item.Id,
			Attributes: item,
		})
	}
	links, meta, err := nextPageLinksAfter(s, r, artifacts, hasNextPage, func(last domainmodels.Artifact) listCursor {
		return listCursor{Kind: listCursorKindArtifacts, CreatedAt: last.CreatedAt, ID: last.ID}
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build artifact list links: %w", err)
	}
	return data, links, meta, nil
}

func (s *Server) createArtifact(
	ctx context.Context,
	req CreateArtifactRequest,
	createdBy uuid.UUID,
	now time.Time,
) (*domainmodels.Artifact, error) {
	if req.Id == uuid.Nil {
		return nil, invalidRequest("artifact id is required")
	}
	artifact := artifactFromRequest(req)
	if artifact.Name == "" {
		return nil, invalidRequest("artifact name is required")
	}
	// In log-only mode a request without a type still reaches here and is stored as other.
	artifactType, err := artifactTypeFromRequest(req.Type)
	if err != nil {
		return nil, fmt.Errorf("validate artifact %s: %w", req.Id, err)
	}
	artifact.Type = artifactType
	artifact.CreatedBy = createdBy
	artifact.CreatedAt = now
	created, err := s.database.Artifacts.Create(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}
	return created, nil
}

// artifactFromRequest maps an artifact request without checking it, so schema checks
// and createArtifact see the same values. A missing type stays empty so the schema
// check can report it; createArtifact applies the default.
func artifactFromRequest(req CreateArtifactRequest) domainmodels.Artifact {
	metadata := map[string]any{}
	if req.Metadata != nil {
		metadata = *req.Metadata
	}
	var artifactType domainmodels.ArtifactType
	if req.Type != nil {
		artifactType = domainmodels.ArtifactType(*req.Type)
	}
	return domainmodels.Artifact{
		ID: req.Id, Name: strings.TrimSpace(req.Name), Level: domainmodels.ArtifactLevel(req.Level),
		Type: artifactType,
		URI:  trimmedStringPtr(req.Uri), SHA256: trimmedStringPtr(req.Sha256),
		Format: trimmedStringPtr(req.Format), SizeBytes: req.SizeBytes, Metadata: metadata,
	}
}

func (s *Server) createMetric(ctx context.Context, req CreateMetricRequest, now time.Time) (*domainmodels.Metric, error) {
	if req.Id == uuid.Nil {
		return nil, invalidRequest("metric id is required")
	}
	key := metricKeyFromRequest(req.Key)
	if key == "" {
		return nil, invalidRequest("metric key is required")
	}
	if req.Value == nil {
		return nil, invalidRequest("metric value is required")
	}
	metric, err := s.database.Metrics.Create(ctx, domainmodels.Metric{
		ID: req.Id, Key: key, Value: *req.Value, CreatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("create metric: %w", err)
	}
	return metric, nil
}

func (s *Server) createRun(
	ctx context.Context,
	req CreateRunRequest,
	createdBy uuid.UUID,
	now time.Time,
) (*domainmodels.Run, error) {
	if req.Id == uuid.Nil {
		return nil, invalidRequest("run id is required")
	}
	run := runFromRequest(req)
	if run.Name == "" {
		return nil, invalidRequest("run name is required")
	}
	run.CreatedBy = createdBy
	run.CreatedAt = now
	run.UpdatedAt = now
	created, err := s.database.Runs.Create(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return created, nil
}

func metricKeyFromRequest(key string) domainmodels.MetricKey {
	return domainmodels.MetricKey(strings.TrimSpace(key))
}

// runFromRequest maps a run request without checking it, so schema checks and
// createRun see the same values.
func runFromRequest(req CreateRunRequest) domainmodels.Run {
	parameters := map[string]any{}
	if req.Parameters != nil {
		parameters = *req.Parameters
	}
	metadata := map[string]any{}
	if req.Metadata != nil {
		metadata = *req.Metadata
	}
	return domainmodels.Run{
		ID: req.Id, Name: strings.TrimSpace(req.Name), SoftwareName: trimmedStringPtr(req.SoftwareName),
		SoftwareVersion: trimmedStringPtr(req.SoftwareVersion), Command: trimmedStringPtr(req.Command),
		Parameters: parameters, Metadata: metadata, StartedAt: req.StartedAt, FinishedAt: req.FinishedAt,
	}
}

func (s *Server) attachArtifactToRun(ctx context.Context, runID uuid.UUID, req CreateRunArtifactRequest) error {
	if req.ArtifactId == uuid.Nil {
		return invalidRequest("run artifact artifact_id is required")
	}
	if req.Direction == "" {
		return invalidRequest("run artifact direction is required")
	}
	if err := s.database.Runs.AttachArtifact(ctx, runID, req.ArtifactId, domainmodels.RunArtifactDirection(req.Direction)); err != nil {
		return fmt.Errorf("attach artifact to run: %w", err)
	}
	return nil
}

func (s *Server) saveProteinSequencesIfFASTA(
	ctx context.Context,
	entryRevisionID uuid.UUID,
	artifact domainmodels.Artifact,
) error {
	if !artifact.IsFASTA() {
		return nil
	}
	records, err := artifact.FASTARecords()
	if err != nil {
		return fmt.Errorf("read artifact FASTA records: %w", err)
	}
	if err := s.database.ProteinSequences.Create(ctx, entryRevisionID, artifact.ID, records); err != nil {
		return fmt.Errorf("save artifact protein sequences: %w", err)
	}
	return nil
}

func modelCollectionDocumentFromAttributes(models []ModelAttributes) ModelCollectionDocument {
	items := make([]ModelData, 0, len(models))
	for _, item := range models {
		items = append(items, ModelData{
			Type:       jsonAPITypeModels,
			Id:         item.Id,
			Attributes: item,
		})
	}
	return ModelCollectionDocument{Data: items}
}

func createModelRevisionAttributesFromRevision(revision domainmodels.ModelRevision) CreateModelRevisionAttributes {
	return CreateModelRevisionAttributes{
		BaseRevisionId: revision.ParentRevisionID,
		EntryId:        revision.EntryID,
		IdempotencyKey: revision.IdempotencyKey,
		ModelId:        revision.ModelID,
		RevisionId:     revision.ID,
		State:          RevisionState(revision.State),
	}
}

func (s *Server) modelAttributesFromRevisions(
	ctx context.Context,
	revisions []domainmodels.ModelRevision,
) ([]ModelAttributes, error) {
	revisionIDs := make([]uuid.UUID, 0, len(revisions))
	for _, revision := range revisions {
		revisionIDs = append(revisionIDs, revision.ID)
	}
	metricsByRevision, err := s.database.Metrics.ListByModelRevisionIDs(ctx, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("list model metrics: %w", err)
	}

	items := make([]ModelAttributes, 0, len(revisions))
	for _, revision := range revisions {
		item, err := modelAttributesFromRevision(revision, metricsByRevision[revision.ID])
		if err != nil {
			return nil, fmt.Errorf("build model response: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func entryInfoAttributesFromRevision(revision domainmodels.EntryRevision) (EntryInfoAttributes, error) {
	properties, err := entryPropertiesFromModel(revision.Metadata)
	if err != nil {
		return EntryInfoAttributes{}, fmt.Errorf("build entry fields response: %w", err)
	}
	return EntryInfoAttributes{Id: revision.EntryID, CreatedBy: revision.CreatedBy, Title: revision.Title,
		ThumbnailImageUrl: revision.ThumbnailImageURL, ExternalRefs: properties.ExternalRefs,
		Details: properties.Details, Resolution: properties.Resolution, Method: properties.Method,
		SpaceGroup: properties.SpaceGroup, Crystallography: properties.Crystallography,
		PublishedAt: revision.PublishedAt, CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt}, nil
}

func modelAttributesFromRevision(revision domainmodels.ModelRevision, metrics []domainmodels.Metric) (ModelAttributes, error) {
	metadata, err := modelMetadataForResponse(revision.Metadata)
	if err != nil {
		return ModelAttributes{}, fmt.Errorf("build model metadata response: %w", err)
	}
	return ModelAttributes{Id: revision.ModelID, EntryId: revision.EntryID, CreatedBy: revision.CreatedBy,
		Title: revision.Title, ThumbnailImageUrl: revision.ThumbnailImageURL,
		IdempotencyKey: revision.IdempotencyKey, Metadata: metadata, PrimaryArtifactId: revision.PrimaryArtifactID, PublishedAt: revision.PublishedAt,
		CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt, Metrics: metricsFromModels(metrics)}, nil
}

func artifactAttributesFromModels(artifacts []domainmodels.Artifact) ([]ArtifactAttributes, error) {
	items := make([]ArtifactAttributes, 0, len(artifacts))
	for _, artifact := range artifacts {
		item, err := artifactAttributesFromModel(artifact)
		if err != nil {
			return nil, fmt.Errorf("build artifact response: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func artifactAttributesFromModel(artifact domainmodels.Artifact) (ArtifactAttributes, error) {
	metadata, err := metadataFromValue(artifact.Metadata)
	if err != nil {
		return ArtifactAttributes{}, fmt.Errorf("build artifact metadata response: %w", err)
	}
	return ArtifactAttributes{Id: artifact.ID, Name: artifact.Name, Level: ArtifactLevel(artifact.Level), Type: ArtifactType(artifact.Type), Uri: artifact.URI,
		Sha256: artifact.SHA256, Format: artifact.Format, SizeBytes: artifact.SizeBytes, Metadata: metadata,
		CreatedBy: artifact.CreatedBy, CreatedAt: artifact.CreatedAt}, nil
}

type artifactAttachmentScope string

const (
	artifactAttachmentScopeEntry artifactAttachmentScope = "entry"
	artifactAttachmentScopeModel artifactAttachmentScope = "model"
)

func artifactTypeFromRequest(requested *ArtifactType) (domainmodels.ArtifactType, error) {
	if requested == nil {
		return domainmodels.ArtifactTypeOther, nil
	}
	typeValue := domainmodels.ArtifactType(*requested)
	if err := typeValue.Validate(); err != nil {
		return "", invalidRequest("unsupported artifact type %q", *requested)
	}
	return typeValue, nil
}

func validateArtifactRequests(requests *[]CreateArtifactRequest, scope artifactAttachmentScope) error {
	if requests == nil {
		return nil
	}
	counts := make(map[domainmodels.ArtifactType]int)
	for _, request := range *requests {
		artifactType, err := artifactTypeFromRequest(request.Type)
		if err != nil {
			return fmt.Errorf("validate artifact %s: %w", request.Id, err)
		}
		if scope == artifactAttachmentScopeEntry &&
			artifactType != domainmodels.ArtifactTypeFASTA &&
			artifactType != domainmodels.ArtifactTypeOther {
			return invalidRequest("artifact type %q cannot be attached to an entry revision", artifactType)
		}
		if scope == artifactAttachmentScopeModel && artifactType == domainmodels.ArtifactTypeFASTA {
			return invalidRequest("artifact type %q cannot be attached to a model revision", artifactType)
		}
		if artifactType == domainmodels.ArtifactTypeFASTA &&
			(request.Format == nil || strings.TrimSpace(*request.Format) != "fasta") {
			return invalidRequest("artifact type %q requires FASTA format", artifactType)
		}
		counts[artifactType]++
	}

	uniqueTypes := []domainmodels.ArtifactType{domainmodels.ArtifactTypeModel}
	if scope == artifactAttachmentScopeEntry {
		uniqueTypes = []domainmodels.ArtifactType{domainmodels.ArtifactTypeFASTA}
	} else {
		uniqueTypes = append(uniqueTypes, domainmodels.ArtifactTypeStructureFactors)
	}
	for _, artifactType := range uniqueTypes {
		if counts[artifactType] > 1 {
			return invalidRequest("only one artifact of type %q can be attached to a %s revision", artifactType, scope)
		}
	}
	return nil
}

func metricsFromModels(metrics []domainmodels.Metric) []Metric {
	items := make([]Metric, 0, len(metrics))
	for _, metric := range metrics {
		items = append(items, Metric{Id: metric.ID, Key: string(metric.Key), Value: metric.Value, CreatedAt: metric.CreatedAt})
	}
	return items
}

func residueDataFromModels(residues []domainmodels.ResidueData) []ResidueData {
	items := make([]ResidueData, 0, len(residues))
	for _, residue := range residues {
		items = append(items, ResidueData{
			LabelAsymId:     residue.LabelAsymID,
			LabelSeqId:      residue.LabelSeqID,
			LabelCompId:     trimmedStringValuePtr(residue.LabelCompID),
			AuthAsymId:      residue.AuthAsymID,
			AuthSeqId:       residue.AuthSeqID,
			PdbxPdbInsCode:  residue.PDBxPDBInsCode,
			LabelAltId:      residue.LabelAltID,
			UniprotPosition: residue.UniProtPosition,
			Rscc:            residue.RSCC,
			BIso:            residue.BIso,
			Occupancy:       residue.Occupancy,
			ConformerCount:  residue.ConformerCount,
			Rmsf:            residue.RMSF,
		})
	}
	return items
}

func runFromModel(run domainmodels.Run) Run {
	return Run{Id: run.ID, Name: run.Name, SoftwareName: run.SoftwareName, SoftwareVersion: run.SoftwareVersion,
		Command: run.Command, Parameters: mapFromNil(run.Parameters), Metadata: mapFromNil(run.Metadata),
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, CreatedBy: run.CreatedBy,
		CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
}

func runArtifactFromLink(link db.RunArtifactLink) RunArtifact {
	return RunArtifact{RunId: link.RunID, ArtifactId: link.ArtifactID, Direction: RunArtifactDirection(link.Direction)}
}

func proteinSequencesFromModels(sequences []domainmodels.ProteinSequence) []ProteinSequence {
	items := make([]ProteinSequence, 0, len(sequences))
	for _, sequence := range sequences {
		items = append(items, proteinSequenceFromModel(sequence))
	}
	return items
}

func proteinSequenceFromModel(sequence domainmodels.ProteinSequence) ProteinSequence {
	return ProteinSequence{Id: sequence.ID, SourceArtifactId: sequence.SourceArtifactID,
		RecordIndex: sequence.RecordIndex, Header: sequence.Header, Sequence: sequence.Sequence,
		CreatedAt: sequence.CreatedAt}
}

func polymerEntitiesFromModels(entities []domainmodels.PolymerEntity) []PolymerEntity {
	items := make([]PolymerEntity, 0, len(entities))
	for _, entity := range entities {
		sourceOrganisms := make([]PolymerEntityOrganism, 0, len(entity.Metadata.SourceOrganisms))
		for _, organism := range entity.Metadata.SourceOrganisms {
			sourceOrganisms = append(sourceOrganisms, PolymerEntityOrganism{
				ScientificName: organism.ScientificName,
				NcbiTaxonomyId: organism.NCBITaxonomyID,
			})
		}

		uniProtMappings := make([]PolymerEntityUniProtMapping, 0, len(entity.Metadata.UniProtMappings))
		for _, mapping := range entity.Metadata.UniProtMappings {
			uniProtMappings = append(uniProtMappings, PolymerEntityUniProtMapping{
				Accession:  mapping.Accession,
				Source:     PolymerEntityUniProtMappingSource(mapping.Source),
				UnpRelease: mapping.UniProtRelease,
			})
		}

		items = append(items, PolymerEntity{
			Id:              entity.ID,
			LabelEntityId:   entity.Metadata.LabelEntityID,
			Description:     entity.Metadata.Description,
			SourceOrganisms: sourceOrganisms,
			Construct:       entity.Metadata.Construct,
			Mutations:       entity.Metadata.Mutations,
			UniprotMappings: uniProtMappings,
			CreatedAt:       entity.CreatedAt,
		})
	}
	return items
}

func metadataFromValue(value any) (map[string]interface{}, error) {
	if value == nil {
		return map[string]interface{}{}, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	metadata := map[string]interface{}{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	return metadata, nil
}

func modelMetadataForResponse(metadata domainmodels.ModelMetadata) (map[string]interface{}, error) {
	metadata.ResidueData = nil
	return metadataFromValue(metadata)
}

func mapFromNil(value map[string]any) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return value
}

func entryFiltersFromParams(params ListEntriesParams) (db.EntryRevisionFilters, listPage, error) {
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindEntries)
	if err != nil {
		return db.EntryRevisionFilters{}, listPage{}, fmt.Errorf("entry list page: %w", err)
	}
	filters := db.EntryRevisionFilters{Limit: page.fetchLimit(), Offset: page.Offset, After: page.after()}
	if params.Query != nil {
		filters.Query = boundedString(strings.TrimSpace(*params.Query), maxEntrySearchQueryLength)
	}
	if params.PdbId != nil {
		filters.PDBIDs = boundedSearchFilterValues(*params.PdbId)
	}
	return filters, page, nil
}

func modelFiltersFromParams(params ListModelsAcrossEntriesParams) (db.ModelRevisionFilters, listPage, error) {
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindModels)
	if err != nil {
		return db.ModelRevisionFilters{}, listPage{}, fmt.Errorf("model list page: %w", err)
	}
	filters := db.ModelRevisionFilters{Limit: page.fetchLimit(), Offset: page.Offset, After: page.after()}
	return filters, page, nil
}

func proteinSequenceFromSearchQuery(value string) (string, bool) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(value), ""))
	if len(normalized) < minProteinSequenceQueryLength {
		return "", false
	}
	for _, symbol := range normalized {
		if !strings.ContainsRune(proteinSequenceAlphabet, symbol) {
			return "", false
		}
	}
	return normalized, true
}

func artifactFiltersFromParams(params ListArtifactsParams) (db.ArtifactFilters, listPage, error) {
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindArtifacts)
	if err != nil {
		return db.ArtifactFilters{}, listPage{}, fmt.Errorf("artifact list page: %w", err)
	}
	return artifactPageFilters(page, params.Levels), page, nil
}

func modelArtifactFiltersFromParams(params ListModelArtifactsParams) (db.ArtifactFilters, listPage, error) {
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindArtifacts)
	if err != nil {
		return db.ArtifactFilters{}, listPage{}, fmt.Errorf("model artifact list page: %w", err)
	}
	return artifactPageFilters(page, params.Levels), page, nil
}

func artifactPageFilters(page listPage, levels *[]ArtifactLevel) db.ArtifactFilters {
	filters := db.ArtifactFilters{Limit: page.fetchLimit(), Offset: page.Offset, After: page.after()}
	if levels != nil {
		filters.Levels = boundedArtifactLevels(*levels)
	}
	return filters
}

func (s *Server) activeEntryRevision(ctx context.Context, entryID string) (*domainmodels.EntryRevision, error) {
	activeState := domainmodels.RevisionStateActive
	activeEntry := domainmodels.EntryStateActive
	revision, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID: &entryID, State: &activeState, EntryState: &activeEntry,
	})
	if err != nil {
		return nil, fmt.Errorf("get active entry revision: %w", err)
	}
	return revision, nil
}

func (s *Server) activeModelRevision(
	ctx context.Context,
	entryID, modelID string,
) (*domainmodels.ModelRevision, error) {
	if _, err := s.activeEntryRevision(ctx, entryID); errors.Is(err, db.ErrEntryRevisionNotFound) {
		return nil, db.ErrModelRevisionNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get active entry for model: %w", err)
	}
	activeState := domainmodels.RevisionStateActive
	activeModel := domainmodels.ModelStateActive
	revision, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
		EntryID: &entryID, ModelID: &modelID, State: &activeState, ModelState: &activeModel,
	})
	if err != nil {
		return nil, fmt.Errorf("get active model revision: %w", err)
	}
	return revision, nil
}

func entryMetadataFromAPIFields(fields any) (domainmodels.EntryMetadata, error) {
	data, err := json.Marshal(fields)
	if err != nil {
		return domainmodels.EntryMetadata{}, invalidPayloadRequest("encode entry fields", err)
	}
	var result domainmodels.EntryMetadata
	if err := json.Unmarshal(data, &result); err != nil {
		return domainmodels.EntryMetadata{}, invalidPayloadRequest("decode entry fields", err)
	}
	return result, nil
}

func entryPropertiesFromModel(metadata domainmodels.EntryMetadata) (EntryProperties, error) {
	data, err := json.Marshal(metadata)
	if err != nil {
		return EntryProperties{}, fmt.Errorf("marshal entry fields: %w", err)
	}
	var result EntryProperties
	if err := json.Unmarshal(data, &result); err != nil {
		return EntryProperties{}, fmt.Errorf("unmarshal entry fields: %w", err)
	}
	return result, nil
}

func modelMetadataFromRequest(metadata *map[string]interface{}) (domainmodels.ModelMetadata, error) {
	if metadata == nil {
		return domainmodels.ModelMetadata{}, nil
	}
	var result domainmodels.ModelMetadata
	if err := decodeMetadataRequest(*metadata, &result); err != nil {
		return domainmodels.ModelMetadata{}, invalidPayloadRequest("decode model metadata", err)
	}
	return result, nil
}

func decodeMetadataRequest(metadata map[string]interface{}, dest any) error {
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal metadata: %w", err)
	}
	return nil
}

func trimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func trimmedStringValuePtr(value string) *string {
	return trimmedStringPtr(&value)
}

func modelRevisionIdempotencyKeyFromCreateModelData(data CreateModelData) *string {
	return trimmedStringPtr(data.IdempotencyKey)
}

func modelRevisionIdempotencyKeyFromChange(change ModelRevisionChange) *string {
	return trimmedStringPtr(change.IdempotencyKey)
}

func limitOrDefault(limit *int) *int {
	if limit == nil {
		return ptr(defaultListLimit)
	}
	return ptr(min(*limit, maxListLimit))
}

func boundedSearchFilterValues(values []string) []string {
	valueCount := min(len(values), maxListFilterValues)
	bounded := make([]string, 0, valueCount)
	for _, value := range values[:valueCount] {
		bounded = append(bounded, boundedString(strings.TrimSpace(value), maxEntryPDBIDLength))
	}
	return bounded
}

func boundedArtifactLevels(values []ArtifactLevel) []domainmodels.ArtifactLevel {
	valueCount := min(len(values), maxListFilterValues)
	bounded := make([]domainmodels.ArtifactLevel, 0, valueCount)
	for _, value := range values[:valueCount] {
		bounded = append(bounded, domainmodels.ArtifactLevel(value))
	}
	return bounded
}

func boundedString(value string, maxLength int) string {
	runeCount := 0
	for index := range value {
		if runeCount == maxLength {
			return value[:index]
		}
		runeCount++
	}
	return value
}

func ptr[T any](value T) *T { return &value }

type createEntryRevisionPayload struct {
	Request     CreateEntryRevisionRequest
	EntryFields map[string]json.RawMessage
}

type rawCreateEntryRevisionPayload struct {
	Entry map[string]json.RawMessage `json:"entry"`
}

func decodeCreateEntryRevisionPayload(r *http.Request) (createEntryRevisionPayload, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return createEntryRevisionPayload{}, fmt.Errorf("read entry revision request: %w", err)
	}
	var request CreateEntryRevisionRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return createEntryRevisionPayload{}, fmt.Errorf("decode entry revision request: %w", err)
	}
	var raw rawCreateEntryRevisionPayload
	if err := json.Unmarshal(data, &raw); err != nil {
		return createEntryRevisionPayload{}, fmt.Errorf("decode entry revision field presence: %w", err)
	}
	return createEntryRevisionPayload{
		Request:     request,
		EntryFields: raw.Entry,
	}, nil
}

type createModelRevisionPayload struct {
	Request     CreateModelRevisionRequest
	ModelFields map[string]json.RawMessage
}

type rawCreateModelRevisionPayload struct {
	Model map[string]json.RawMessage `json:"model"`
}

func decodeCreateModelRevisionPayload(r *http.Request) (createModelRevisionPayload, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return createModelRevisionPayload{}, fmt.Errorf("read model revision request: %w", err)
	}
	var request CreateModelRevisionRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return createModelRevisionPayload{}, fmt.Errorf("decode model revision request: %w", err)
	}
	var raw rawCreateModelRevisionPayload
	if err := json.Unmarshal(data, &raw); err != nil {
		return createModelRevisionPayload{}, fmt.Errorf("decode model revision field presence: %w", err)
	}
	return createModelRevisionPayload{Request: request, ModelFields: raw.Model}, nil
}

var entryRevisionFields = map[string]struct{}{
	"title": {}, "thumbnail_image_url": {}, "external_refs": {}, "details": {},
	"resolution": {}, "method": {}, "space_group": {}, "crystallography": {},
}

var modelRevisionFields = map[string]struct{}{
	"title": {}, "thumbnail_image_url": {}, "metadata": {},
	"idempotency_key": {}, "primary_artifact_id": {}, "artifacts": {}, "runs": {}, "metrics": {},
}

func validateRevisionFields(fields map[string]json.RawMessage, allowed map[string]struct{}) error {
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			return invalidRequest("revision field %q is not allowed", field)
		}
	}
	return nil
}

func isJSONNull(value json.RawMessage) bool {
	return strings.TrimSpace(string(value)) == "null"
}

func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidRequest, fmt.Sprintf(format, args...))
}

func invalidPayloadRequest(description string, err error) error {
	return fmt.Errorf("%s: %w", description, invalidRequest("%v", err))
}

func isUniqueConstraint(err error, constraint string) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == constraint
}

func (s *Server) writeMutationError(ctx context.Context, w http.ResponseWriter, err error, action string) bool {
	var validationError *validation.Error
	switch {
	case err == nil:
		return false
	case errors.Is(err, db.ErrEntryRevisionNotFound), errors.Is(err, db.ErrModelRevisionNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry or model not found")
	case errors.As(err, &validationError):
		writeFieldErrors(w, validationError.Fields)
	case errors.Is(err, errInvalidRequest):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, errConflict), errors.Is(err, db.ErrEntryRevisionConflict), errors.Is(err, db.ErrModelRevisionConflict),
		isUniqueConstraint(err, "entry_revisions_active_pdb_ref_idx"):
		writeError(w, http.StatusConflict, "CONFLICT", "revision conflicts with the current active state")
	default:
		slog.ErrorContext(ctx, action+" failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+action)
	}
	return true
}
