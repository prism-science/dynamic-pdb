package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

var (
	errInvalidRequest = errors.New("invalid request")
	errConflict       = errors.New("conflict")
)

const (
	defaultEntryListLimit         = 50
	minProteinSequenceQueryLength = 8
	proteinSequenceAlphabet       = "ACDEFGHIKLMNPQRSTVWYX"
)

func (s *Server) ListEntries(w http.ResponseWriter, r *http.Request, params ListEntriesParams) {
	filters, err := entryFiltersFromParams(params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid entry filters")
		return
	}
	activeState := domainmodels.RevisionStateActive
	activeEntryState := domainmodels.EntryStateActive
	filters.State = &activeState
	filters.EntryState = &activeEntryState

	revisions, err := s.database.Entries.List(r.Context(), filters)
	if err != nil {
		slog.Error("list entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entries")
		return
	}
	if len(revisions) == 0 {
		if proteinSequence, ok := proteinSequenceFromSearchQuery(filters.Query); ok {
			filters.Query = ""
			filters.ProteinSequence = proteinSequence
			revisions, err = s.database.Entries.List(r.Context(), filters)
			if err != nil {
				slog.Error("list entries by protein sequence failed", "err", err)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entries")
				return
			}
		}
	}

	items := make([]EntryInfo, 0, len(revisions))
	for _, revision := range revisions {
		item, err := entryInfoResponseFromRevision(revision)
		if err != nil {
			slog.Error("build entry info response failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build entry list response")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, EntryListResponse{Items: items})
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

	result, err := s.createInitialEntryRevision(r.Context(), req, user.ID)
	if s.writeMutationError(w, err, "create entry") {
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) createInitialEntryRevision(
	ctx context.Context,
	req CreateEntryRequest,
	createdBy uuid.UUID,
) (CreateEntryRevisionResponse, error) {
	name := strings.TrimSpace(req.Entry.Name)
	if name == "" {
		return CreateEntryRevisionResponse{}, invalidRequest("entry name is required")
	}
	entryID := uuid.New()
	if req.Entry.Id != nil {
		entryID = *req.Entry.Id
		if entryID == uuid.Nil {
			return CreateEntryRevisionResponse{}, invalidRequest("entry id is required")
		}
	}
	metadata, err := entryMetadataFromRequest(req.Entry.Metadata)
	if err != nil {
		return CreateEntryRevisionResponse{}, fmt.Errorf("decode initial entry metadata: %w", err)
	}
	now := time.Now().UTC()
	response := CreateEntryRevisionResponse{
		EntryId:      entryID,
		State:        CreateEntryRevisionResponseStateInReview,
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
			Name:              name,
			Description:       trimmedStringPtr(req.Entry.Description),
			ThumbnailImageURL: trimmedStringPtr(req.Entry.ThumbnailImageUrl),
			Metadata:          metadata,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return fmt.Errorf("create entry revision: %w", err)
		}
		response.RevisionId = revision.ID

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
				response.ModelResults = append(response.ModelResults, ModelOperationResult{
					Op:              ModelOperationResultOpAdd,
					ModelId:         model.ModelId,
					ModelRevisionId: model.RevisionId,
				})
			}
		}

		return nil
	})
	if err != nil {
		return CreateEntryRevisionResponse{}, fmt.Errorf("create initial entry graph: %w", err)
	}
	return response, nil
}

func applyEntryRevisionChange(
	revision *domainmodels.EntryRevision,
	change EntryRevisionChange,
	fields map[string]json.RawMessage,
) error {
	if err := validateRevisionFields(fields, entryRevisionFields); err != nil {
		return err
	}
	if raw, present := fields["name"]; present {
		if isJSONNull(raw) || change.Name == nil {
			return invalidRequest("entry name cannot be null")
		}
		name := strings.TrimSpace(*change.Name)
		if name == "" {
			return invalidRequest("entry name cannot be empty")
		}
		revision.Name = name
	}
	if raw, present := fields["description"]; present {
		if isJSONNull(raw) {
			revision.Description = nil
		} else {
			revision.Description = trimmedStringPtr(change.Description)
		}
	}
	if raw, present := fields["thumbnail_image_url"]; present {
		if isJSONNull(raw) {
			revision.ThumbnailImageURL = nil
		} else {
			revision.ThumbnailImageURL = trimmedStringPtr(change.ThumbnailImageUrl)
		}
	}
	if raw, present := fields["metadata"]; present {
		if isJSONNull(raw) || change.Metadata == nil {
			return invalidRequest("entry metadata cannot be null")
		}
		metadata, err := entryMetadataFromRequest(change.Metadata)
		if err != nil {
			return fmt.Errorf("decode changed entry metadata: %w", err)
		}
		revision.Metadata = metadata
	}
	return nil
}

func (s *Server) CreateModel(w http.ResponseWriter, r *http.Request, entryID uuid.UUID) {
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

	result, err := s.createInitialModelRevision(r.Context(), entryID, request.Model, user.ID)
	if s.writeMutationError(w, err, "create model") {
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) createInitialModelRevision(
	ctx context.Context,
	entryID uuid.UUID,
	data CreateModelData,
	createdBy uuid.UUID,
) (CreateModelRevisionResponse, error) {
	return s.createInitialModelRevisionForEntry(ctx, entryID, data, createdBy, true)
}

func (s *Server) createInitialModelRevisionForPendingEntry(
	ctx context.Context,
	entryID uuid.UUID,
	data CreateModelData,
	createdBy uuid.UUID,
) (CreateModelRevisionResponse, error) {
	return s.createInitialModelRevisionForEntry(ctx, entryID, data, createdBy, false)
}

func (s *Server) createInitialModelRevisionForEntry(
	ctx context.Context,
	entryID uuid.UUID,
	data CreateModelData,
	createdBy uuid.UUID,
	requireActiveEntry bool,
) (CreateModelRevisionResponse, error) {
	name := strings.TrimSpace(data.Name)
	if name == "" {
		return CreateModelRevisionResponse{}, invalidRequest("model name is required")
	}
	modelID := uuid.New()
	if data.Id != nil {
		modelID = *data.Id
		if modelID == uuid.Nil {
			return CreateModelRevisionResponse{}, invalidRequest("model id is required")
		}
	}
	metadata, err := modelMetadataFromRequest(data.Metadata)
	if err != nil {
		return CreateModelRevisionResponse{}, fmt.Errorf("decode model metadata: %w", err)
	}
	now := time.Now().UTC()
	response := CreateModelRevisionResponse{
		EntryId:        entryID,
		IdempotencyKey: modelRevisionIdempotencyKeyFromCreateModelData(data),
		ModelId:        modelID,
		State:          RevisionStateInReview,
	}
	err = s.database.Do(ctx, func(ctx context.Context) error {
		if response.IdempotencyKey != nil {
			activeState := domainmodels.RevisionStateActive
			existing, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
				State:          &activeState,
				IdempotencyKey: response.IdempotencyKey,
			})
			if err == nil {
				response = createModelRevisionResponseFromModel(*existing)
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
			Name:              name,
			Description:       trimmedStringPtr(data.Description),
			ThumbnailImageURL: trimmedStringPtr(data.ThumbnailImageUrl),
			Metadata:          metadata,
			IdempotencyKey:    response.IdempotencyKey,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return fmt.Errorf("create model revision: %w", err)
		}
		response.RevisionId = revision.ID
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
		return CreateModelRevisionResponse{}, fmt.Errorf("create initial model revision: %w", err)
	}
	return response, nil
}

func createModelDataFromAddOperation(data AddModelData) CreateModelData {
	return CreateModelData{
		Id:                data.ModelId,
		Name:              data.Name,
		Description:       data.Description,
		ThumbnailImageUrl: data.ThumbnailImageUrl,
		Metadata:          data.Metadata,
		IdempotencyKey:    data.IdempotencyKey,
		PrimaryArtifactId: data.PrimaryArtifactId,
		Artifacts:         data.Artifacts,
		Runs:              data.Runs,
		Metrics:           data.Metrics,
	}
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

func (s *Server) CreateModelRevision(w http.ResponseWriter, r *http.Request, entryID, modelID uuid.UUID) {
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

	result, err := s.createModelRevisionGraph(r.Context(), entryID, modelID, payload, user.ID)
	if s.writeMutationError(w, err, "create model revision") {
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) createModelRevisionGraph(
	ctx context.Context,
	entryID, modelID uuid.UUID,
	payload createModelRevisionPayload,
	createdBy uuid.UUID,
) (CreateModelRevisionResponse, error) {
	if err := validateRevisionFields(payload.ModelFields, modelRevisionFields); err != nil {
		return CreateModelRevisionResponse{}, err
	}
	if len(payload.ModelFields) == 0 {
		return CreateModelRevisionResponse{}, invalidRequest("model change must contain at least one field")
	}
	data := payload.Request.Model
	for _, field := range []string{"artifacts", "metrics", "runs"} {
		if raw, present := payload.ModelFields[field]; present && isJSONNull(raw) {
			return CreateModelRevisionResponse{}, invalidRequest("model %s cannot be null", field)
		}
	}
	now := time.Now().UTC()
	response := CreateModelRevisionResponse{
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
		response.BaseRevisionId = &base.ID
		if response.IdempotencyKey != nil {
			activeState := domainmodels.RevisionStateActive
			existing, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
				State:          &activeState,
				IdempotencyKey: response.IdempotencyKey,
			})
			if err == nil {
				response = createModelRevisionResponseFromModel(*existing)
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
			Name:              base.Name,
			Description:       base.Description,
			ThumbnailImageURL: base.ThumbnailImageURL,
			Metadata:          base.Metadata,
			IdempotencyKey:    response.IdempotencyKey,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if raw, present := payload.ModelFields["name"]; present {
			if isJSONNull(raw) || data.Name == nil {
				return invalidRequest("model name cannot be null")
			}
			name := strings.TrimSpace(*data.Name)
			if name == "" {
				return invalidRequest("model name cannot be empty")
			}
			revision.Name = name
		}
		if raw, present := payload.ModelFields["description"]; present {
			if isJSONNull(raw) {
				revision.Description = nil
			} else {
				revision.Description = trimmedStringPtr(data.Description)
			}
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
		response.RevisionId = created.ID
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
		return CreateModelRevisionResponse{}, fmt.Errorf("create model revision graph: %w", err)
	}
	return response, nil
}

func createModelRevisionResponseFromModel(revision domainmodels.ModelRevision) CreateModelRevisionResponse {
	return CreateModelRevisionResponse{
		BaseRevisionId: revision.ParentRevisionID,
		EntryId:        revision.EntryID,
		IdempotencyKey: revision.IdempotencyKey,
		ModelId:        revision.ModelID,
		RevisionId:     revision.ID,
		State:          RevisionState(revision.State),
	}
}

func (s *Server) activeModelRevisionForMutation(
	ctx context.Context,
	entryID, modelID uuid.UUID,
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
	excludeEntryID *uuid.UUID,
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

func (s *Server) GetEntry(w http.ResponseWriter, r *http.Request, entryID uuid.UUID) {
	revision, err := s.activeEntryRevision(r.Context(), entryID)
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	}
	if err != nil {
		slog.Error("get entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}
	sequences, err := s.database.ProteinSequences.List(r.Context(), db.ProteinSequenceFilters{EntryRevisionID: &revision.ID})
	if err != nil {
		slog.Error("list entry protein sequences failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}
	entry, err := entryResponseFromRevision(*revision, sequences)
	if err != nil {
		slog.Error("build entry response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) ListSimilarEntries(
	w http.ResponseWriter,
	r *http.Request,
	entryID uuid.UUID,
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
	if _, err := s.activeEntryRevision(r.Context(), entryID); errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	} else if err != nil {
		slog.Error("get entry for similar entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list similar entries")
		return
	}
	entries, err := s.database.ProteinSequenceSimilarities.ListSimilarEntries(r.Context(), db.SimilarEntryFilters{
		EntryID: entryID,
		Limit:   params.Limit,
		Offset:  params.Offset,
	})
	if err != nil {
		slog.Error("list similar entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list similar entries")
		return
	}
	items, err := similarEntryResponsesFromModels(entries)
	if err != nil {
		slog.Error("build similar entry response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list similar entries")
		return
	}
	writeJSON(w, http.StatusOK, SimilarEntryListResponse{Items: items, Limit: params.Limit, Offset: params.Offset})
}

func (s *Server) ListModels(w http.ResponseWriter, r *http.Request, entryID uuid.UUID, params ListModelsParams) {
	if params.Limit != nil && *params.Limit < 0 || params.Offset != nil && *params.Offset < 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid model filters")
		return
	}
	if _, err := s.activeEntryRevision(r.Context(), entryID); errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeJSON(w, http.StatusOK, ModelListResponse{Items: []Model{}})
		return
	} else if err != nil {
		slog.Error("get entry for models failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
		return
	}
	activeState := domainmodels.RevisionStateActive
	activeModelState := domainmodels.ModelStateActive
	revisions, err := s.database.Models.List(r.Context(), db.ModelRevisionFilters{
		EntryID:    &entryID,
		State:      &activeState,
		ModelState: &activeModelState,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
	if err != nil {
		slog.Error("list models failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
		return
	}
	items := make([]Model, 0, len(revisions))
	for _, revision := range revisions {
		metrics, err := s.database.Metrics.List(r.Context(), db.MetricFilters{ModelRevisionID: &revision.ID})
		if err != nil {
			slog.Error("list model metrics failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
			return
		}
		item, err := modelResponseFromRevision(revision, metrics)
		if err != nil {
			slog.Error("build model response failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, ModelListResponse{Items: items})
}

func (s *Server) GetModel(w http.ResponseWriter, r *http.Request, entryID, modelID uuid.UUID) {
	revision, err := s.activeModelRevision(r.Context(), entryID, modelID)
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
		return
	}
	if err != nil {
		slog.Error("get model failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model")
		return
	}
	metrics, err := s.database.Metrics.List(r.Context(), db.MetricFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		slog.Error("list model metrics failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model")
		return
	}
	model, err := modelResponseFromRevision(*revision, metrics)
	if err != nil {
		slog.Error("build model response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model")
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) ListArtifacts(w http.ResponseWriter, r *http.Request, entryID uuid.UUID, params ListArtifactsParams) {
	revision, err := s.activeEntryRevision(r.Context(), entryID)
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeJSON(w, http.StatusOK, ArtifactListResponse{Items: []Artifact{}})
		return
	}
	if err != nil {
		slog.Error("get entry for artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list artifacts")
		return
	}
	filters, err := artifactFiltersFromParams(params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid artifact filters")
		return
	}
	filters.EntryRevisionID = &revision.ID
	artifacts, err := s.database.Artifacts.List(r.Context(), filters)
	if err != nil {
		slog.Error("list entry artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list artifacts")
		return
	}
	items, err := artifactResponsesFromModels(artifacts)
	if err != nil {
		slog.Error("build artifact response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list artifacts")
		return
	}
	writeJSON(w, http.StatusOK, ArtifactListResponse{Items: items})
}

func (s *Server) ListModelArtifacts(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID uuid.UUID,
	params ListModelArtifactsParams,
) {
	revision, err := s.activeModelRevision(r.Context(), entryID, modelID)
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
		return
	}
	if err != nil {
		slog.Error("get model for artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	filters, err := modelArtifactFiltersFromParams(params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid artifact filters")
		return
	}
	filters.ModelRevisionID = &revision.ID
	artifacts, err := s.database.Artifacts.List(r.Context(), filters)
	if err != nil {
		slog.Error("list model artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	runs, err := s.database.Runs.List(r.Context(), db.RunFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		slog.Error("list model runs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	links, err := s.database.Runs.ListArtifactLinks(r.Context(), revision.ID)
	if err != nil {
		slog.Error("list model run artifact links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	artifactItems, err := artifactResponsesFromModels(artifacts)
	if err != nil {
		slog.Error("build artifact response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	runItems := make([]Run, 0, len(runs))
	for _, run := range runs {
		runItems = append(runItems, runResponseFromModel(run))
	}
	relationItems := make([]RunArtifact, 0, len(links))
	for _, link := range links {
		relationItems = append(relationItems, runArtifactResponseFromModel(link))
	}
	writeJSON(w, http.StatusOK, ModelArtifactListResponse{Items: artifactItems, Runs: runItems, Relations: relationItems})
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
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, invalidRequest("artifact name is required")
	}
	metadata := map[string]any{}
	if req.Metadata != nil {
		metadata = *req.Metadata
	}
	artifact, err := s.database.Artifacts.Create(ctx, domainmodels.Artifact{
		ID: req.Id, Name: name, Level: domainmodels.ArtifactLevel(req.Level),
		URI: trimmedStringPtr(req.Uri), SHA256: trimmedStringPtr(req.Sha256),
		Format: trimmedStringPtr(req.Format), SizeBytes: req.SizeBytes, Metadata: metadata,
		CreatedBy: createdBy, CreatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}
	return artifact, nil
}

func (s *Server) createMetric(ctx context.Context, req CreateMetricRequest, now time.Time) (*domainmodels.Metric, error) {
	if req.Id == uuid.Nil {
		return nil, invalidRequest("metric id is required")
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return nil, invalidRequest("metric key is required")
	}
	metric, err := s.database.Metrics.Create(ctx, domainmodels.Metric{
		ID: req.Id, Key: domainmodels.MetricKey(key), Value: req.Value, CreatedAt: now,
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
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, invalidRequest("run name is required")
	}
	parameters := map[string]any{}
	if req.Parameters != nil {
		parameters = *req.Parameters
	}
	metadata := map[string]any{}
	if req.Metadata != nil {
		metadata = *req.Metadata
	}
	run, err := s.database.Runs.Create(ctx, domainmodels.Run{
		ID: req.Id, Name: name, SoftwareName: trimmedStringPtr(req.SoftwareName),
		SoftwareVersion: trimmedStringPtr(req.SoftwareVersion), Command: trimmedStringPtr(req.Command),
		Parameters: parameters, Metadata: metadata, StartedAt: req.StartedAt, FinishedAt: req.FinishedAt,
		CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return run, nil
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

func entryFiltersFromParams(params ListEntriesParams) (db.EntryRevisionFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.EntryRevisionFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.EntryRevisionFilters{}, errors.New("offset must be non-negative")
	}
	limit := params.Limit
	if limit == nil {
		limit = ptr(defaultEntryListLimit)
	}
	filters := db.EntryRevisionFilters{Limit: limit, Offset: params.Offset}
	if params.Query != nil {
		filters.Query = strings.TrimSpace(*params.Query)
	}
	if params.PdbId != nil {
		filters.PDBIDs = *params.PdbId
	}
	return filters, nil
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

func artifactFiltersFromParams(params ListArtifactsParams) (db.ArtifactFilters, error) {
	if params.Limit != nil && *params.Limit < 0 || params.Offset != nil && *params.Offset < 0 {
		return db.ArtifactFilters{}, errors.New("invalid pagination")
	}
	filters := db.ArtifactFilters{Limit: params.Limit, Offset: params.Offset}
	if params.Levels != nil {
		filters.Levels = make([]domainmodels.ArtifactLevel, 0, len(*params.Levels))
		for _, level := range *params.Levels {
			filters.Levels = append(filters.Levels, domainmodels.ArtifactLevel(level))
		}
	}
	return filters, nil
}

func modelArtifactFiltersFromParams(params ListModelArtifactsParams) (db.ArtifactFilters, error) {
	if params.Limit != nil && *params.Limit < 0 || params.Offset != nil && *params.Offset < 0 {
		return db.ArtifactFilters{}, errors.New("invalid pagination")
	}
	filters := db.ArtifactFilters{Limit: params.Limit, Offset: params.Offset}
	if params.Levels != nil {
		filters.Levels = make([]domainmodels.ArtifactLevel, 0, len(*params.Levels))
		for _, level := range *params.Levels {
			filters.Levels = append(filters.Levels, domainmodels.ArtifactLevel(level))
		}
	}
	return filters, nil
}

func (s *Server) activeEntryRevision(ctx context.Context, entryID uuid.UUID) (*domainmodels.EntryRevision, error) {
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
	entryID, modelID uuid.UUID,
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

func entryInfoResponseFromRevision(revision domainmodels.EntryRevision) (EntryInfo, error) {
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return EntryInfo{}, fmt.Errorf("build entry metadata response: %w", err)
	}
	return EntryInfo{Id: revision.EntryID, CreatedBy: revision.CreatedBy, Name: revision.Name,
		Description: revision.Description, ThumbnailImageUrl: revision.ThumbnailImageURL, Metadata: &metadata,
		PublishedAt: revision.PublishedAt, CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt}, nil
}

func entryResponseFromRevision(
	revision domainmodels.EntryRevision,
	sequences []domainmodels.ProteinSequence,
) (Entry, error) {
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return Entry{}, fmt.Errorf("build entry metadata response: %w", err)
	}
	return Entry{Id: revision.EntryID, CreatedBy: revision.CreatedBy, Name: revision.Name,
		Description: revision.Description, ThumbnailImageUrl: revision.ThumbnailImageURL, Metadata: metadata,
		PublishedAt: revision.PublishedAt, CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt,
		ProteinSequences: proteinSequenceResponsesFromModels(sequences)}, nil
}

func modelResponseFromRevision(revision domainmodels.ModelRevision, metrics []domainmodels.Metric) (Model, error) {
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return Model{}, fmt.Errorf("build model metadata response: %w", err)
	}
	return Model{Id: revision.ModelID, EntryId: revision.EntryID, CreatedBy: revision.CreatedBy,
		Name: revision.Name, Description: revision.Description, ThumbnailImageUrl: revision.ThumbnailImageURL,
		IdempotencyKey: revision.IdempotencyKey, Metadata: metadata, PrimaryArtifactId: revision.PrimaryArtifactID, PublishedAt: revision.PublishedAt,
		CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt, Metrics: metricResponsesFromModels(metrics)}, nil
}

func artifactResponsesFromModels(artifacts []domainmodels.Artifact) ([]Artifact, error) {
	items := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		item, err := artifactResponseFromModel(artifact)
		if err != nil {
			return nil, fmt.Errorf("build artifact response: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func artifactResponseFromModel(artifact domainmodels.Artifact) (Artifact, error) {
	metadata, err := metadataResponseFromValue(artifact.Metadata)
	if err != nil {
		return Artifact{}, fmt.Errorf("build artifact metadata response: %w", err)
	}
	return Artifact{Id: artifact.ID, Name: artifact.Name, Level: ArtifactLevel(artifact.Level), Uri: artifact.URI,
		Sha256: artifact.SHA256, Format: artifact.Format, SizeBytes: artifact.SizeBytes, Metadata: metadata,
		CreatedBy: artifact.CreatedBy, CreatedAt: artifact.CreatedAt}, nil
}

func metricResponsesFromModels(metrics []domainmodels.Metric) []Metric {
	items := make([]Metric, 0, len(metrics))
	for _, metric := range metrics {
		items = append(items, Metric{Id: metric.ID, Key: string(metric.Key), Value: metric.Value, CreatedAt: metric.CreatedAt})
	}
	return items
}

func runResponseFromModel(run domainmodels.Run) Run {
	return Run{Id: run.ID, Name: run.Name, SoftwareName: run.SoftwareName, SoftwareVersion: run.SoftwareVersion,
		Command: run.Command, Parameters: mapFromNil(run.Parameters), Metadata: mapFromNil(run.Metadata),
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, CreatedBy: run.CreatedBy,
		CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
}

func runArtifactResponseFromModel(link db.RunArtifactLink) RunArtifact {
	return RunArtifact{RunId: link.RunID, ArtifactId: link.ArtifactID, Direction: RunArtifactDirection(link.Direction)}
}

func proteinSequenceResponsesFromModels(sequences []domainmodels.ProteinSequence) []ProteinSequence {
	items := make([]ProteinSequence, 0, len(sequences))
	for _, sequence := range sequences {
		items = append(items, proteinSequenceResponseFromModel(sequence))
	}
	return items
}

func proteinSequenceResponseFromModel(sequence domainmodels.ProteinSequence) ProteinSequence {
	return ProteinSequence{Id: sequence.ID, SourceArtifactId: sequence.SourceArtifactID,
		RecordIndex: sequence.RecordIndex, Header: sequence.Header, Sequence: sequence.Sequence,
		CreatedAt: sequence.CreatedAt}
}

func similarEntryResponsesFromModels(entries []domainmodels.SimilarEntry) ([]SimilarEntry, error) {
	items := make([]SimilarEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entryInfoResponseFromRevision(entry.Entry)
		if err != nil {
			return nil, fmt.Errorf("build similar entry info response: %w", err)
		}
		matches := make([]ProteinSequenceSimilarityMatch, 0, len(entry.Matches))
		for _, match := range entry.Matches {
			matches = append(matches, ProteinSequenceSimilarityMatch{SourceSequenceId: match.SourceSequenceID,
				SimilarSequence: proteinSequenceResponseFromModel(match.SimilarSequence), Score: match.Similarity.Score,
				Tool: match.Similarity.Tool, Metadata: mapFromNil(match.Similarity.Metadata), CreatedAt: match.Similarity.CreatedAt})
		}
		items = append(items, SimilarEntry{Entry: info, Score: entry.Score, Matches: matches})
	}
	return items, nil
}

func entryMetadataFromRequest(metadata *map[string]interface{}) (domainmodels.EntryMetadata, error) {
	if metadata == nil {
		return domainmodels.EntryMetadata{}, nil
	}
	var result domainmodels.EntryMetadata
	if err := decodeMetadataRequest(*metadata, &result); err != nil {
		return domainmodels.EntryMetadata{}, invalidPayloadRequest("decode entry metadata", err)
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

func metadataResponseFromValue(value any) (map[string]interface{}, error) {
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

func mapFromNil(value map[string]any) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return value
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

func modelRevisionIdempotencyKeyFromCreateModelData(data CreateModelData) *string {
	return trimmedStringPtr(data.IdempotencyKey)
}

func modelRevisionIdempotencyKeyFromChange(change ModelRevisionChange) *string {
	return trimmedStringPtr(change.IdempotencyKey)
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
	"name": {}, "description": {}, "thumbnail_image_url": {}, "metadata": {},
}

var modelRevisionFields = map[string]struct{}{
	"name": {}, "description": {}, "thumbnail_image_url": {}, "metadata": {},
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

func (s *Server) writeMutationError(w http.ResponseWriter, err error, action string) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, db.ErrEntryRevisionNotFound), errors.Is(err, db.ErrModelRevisionNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry or model not found")
	case errors.Is(err, errInvalidRequest):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, errConflict), errors.Is(err, db.ErrEntryRevisionConflict), errors.Is(err, db.ErrModelRevisionConflict),
		isUniqueConstraint(err, "entry_revisions_active_pdb_ref_idx"):
		writeError(w, http.StatusConflict, "CONFLICT", "revision conflicts with the current active state")
	default:
		slog.Error(action+" failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to "+action)
	}
	return true
}
