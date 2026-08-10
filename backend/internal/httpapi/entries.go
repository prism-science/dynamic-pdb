package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

var errInvalidRequest = errors.New("invalid request")

const (
	defaultEntryListLimit         = 50
	minProteinSequenceQueryLength = 8
	proteinSequenceAlphabet       = "ACDEFGHIKLMNPQRSTVWYX"
)

func (s *Server) ListEntries(w http.ResponseWriter, r *http.Request, params ListEntriesParams) {
	activeState := domainmodels.RevisionStateActive
	filters, err := entryFiltersFromParams(params, activeState)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid entry filters")
		return
	}

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

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "entry name is required")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	err := s.createEntryGraph(r.Context(), req, name, user.ID)
	if errors.Is(err, errInvalidRequest) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if isUniqueConstraint(err, "entry_revisions_active_pdb_ref_idx") {
		writeError(w, http.StatusConflict, "ENTRY_PDB_REF_EXISTS", "entry with this PDB reference already exists")
		return
	}
	if err != nil {
		slog.Error("create entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create entry")
		return
	}

	w.WriteHeader(http.StatusCreated)
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

	proteinSequences, err := s.database.ProteinSequences.List(r.Context(), revision.ID)
	if err != nil {
		slog.Error("list entry protein sequences failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}

	entry, err := entryResponseFromRevision(*revision, proteinSequences)
	if err != nil {
		slog.Error("build entry response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build entry response")
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) DeleteEntry(w http.ResponseWriter, r *http.Request, entryID uuid.UUID) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	err := s.database.Do(r.Context(), func(ctx context.Context) error {
		revision, err := s.activeEntryRevision(ctx, entryID)
		if err != nil {
			return err
		}
		if err := s.database.Entries.Delete(ctx, revision.EntryID, revision.ID, user.ID); err != nil {
			return fmt.Errorf("delete entry revision: %w", err)
		}
		if err := s.database.EntrySearch.DeleteEntry(ctx, revision.EntryID); err != nil {
			return fmt.Errorf("delete entry search rows: %w", err)
		}
		return nil
	})
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	}
	if errors.Is(err, db.ErrEntryRevisionOwnershipMismatch) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the entry creator can delete it")
		return
	}
	if err != nil {
		slog.Error("delete entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete entry")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func entryFiltersFromParams(
	params ListEntriesParams,
	state domainmodels.RevisionState,
) (db.EntryRevisionFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.EntryRevisionFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.EntryRevisionFilters{}, errors.New("offset must be non-negative")
	}
	limit := params.Limit
	if limit == nil {
		defaultLimit := defaultEntryListLimit
		limit = &defaultLimit
	}

	search := ""
	if params.Query != nil {
		search = strings.TrimSpace(*params.Query)
	}

	return db.EntryRevisionFilters{
		State:  &state,
		Limit:  limit,
		Offset: params.Offset,
		Query:  search,
		PDBIDs: stringSliceFromPtr(params.PdbId),
	}, nil
}

func proteinSequenceFromSearchQuery(value string) (string, bool) {
	var normalized strings.Builder
	normalized.Grow(len(value))

	for _, char := range strings.ToUpper(value) {
		if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
			continue
		}
		if !strings.ContainsRune(proteinSequenceAlphabet, char) {
			return "", false
		}
		normalized.WriteRune(char)
	}

	if normalized.Len() < minProteinSequenceQueryLength {
		return "", false
	}
	return normalized.String(), true
}

func (s *Server) createEntryGraph(ctx context.Context, req CreateEntryRequest, name string, createdBy uuid.UUID) error {
	now := time.Now().UTC()
	entryID := uuid.New()
	if req.Id != nil {
		entryID = *req.Id
		if entryID == uuid.Nil {
			return invalidRequest("entry id is required")
		}
	}

	metadata, err := entryMetadataFromRequest(req.Metadata)
	if err != nil {
		return err
	}

	return s.database.Do(ctx, func(ctx context.Context) error {
		revision, err := s.database.Entries.Create(ctx, domainmodels.EntryRevision{
			ID:                uuid.New(),
			EntryID:           entryID,
			RevisionNumber:    ptr(1),
			State:             domainmodels.RevisionStateActive,
			PublishedAt:       &now,
			Name:              name,
			Description:       trimmedStringPtr(req.Description),
			ThumbnailImageURL: trimmedStringPtr(req.ThumbnailImageUrl),
			Metadata:          metadata,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return fmt.Errorf("create entry revision: %w", err)
		}
		if err := s.database.EntrySearch.IndexEntryRevision(ctx, *revision); err != nil {
			return fmt.Errorf("index entry revision search: %w", err)
		}

		if req.Artifacts != nil {
			for _, artifactRequest := range *req.Artifacts {
				artifact, err := s.createArtifact(ctx, artifactRequest, createdBy, now)
				if err != nil {
					return err
				}
				if err := s.database.Artifacts.AttachToEntryRevision(ctx, revision.ID, artifact.ID); err != nil {
					return fmt.Errorf("attach artifact to entry revision: %w", err)
				}
				if err := s.saveProteinSequencesIfFASTA(ctx, revision.ID, *artifact); err != nil {
					return err
				}
			}
		}

		if req.Models != nil {
			for _, modelRequest := range *req.Models {
				if err := s.createModelGraph(ctx, revision.EntryID, revision.ID, modelRequest, now, createdBy); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func (s *Server) createModelGraph(
	ctx context.Context,
	entryID uuid.UUID,
	entryRevisionID uuid.UUID,
	req CreateModelRequest,
	now time.Time,
	createdBy uuid.UUID,
) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return invalidRequest("model name is required")
	}

	modelID := uuid.New()
	if req.Id != nil {
		modelID = *req.Id
		if modelID == uuid.Nil {
			return invalidRequest("model id is required")
		}
	}

	metadata, err := modelMetadataFromRequest(req.Metadata)
	if err != nil {
		return err
	}

	artifacts := make([]domainmodels.Artifact, 0)
	if req.Artifacts != nil {
		artifacts = make([]domainmodels.Artifact, 0, len(*req.Artifacts))
		for _, artifactRequest := range *req.Artifacts {
			artifact, err := s.createArtifact(ctx, artifactRequest, createdBy, now)
			if err != nil {
				return err
			}
			artifacts = append(artifacts, *artifact)
		}
	}

	revision, err := s.database.Models.Create(ctx, entryID, domainmodels.ModelRevision{
		ID:                uuid.New(),
		ModelID:           modelID,
		PrimaryArtifactID: req.PrimaryArtifactId,
		RevisionNumber:    ptr(1),
		State:             domainmodels.RevisionStateActive,
		PublishedAt:       &now,
		Name:              name,
		Description:       trimmedStringPtr(req.Description),
		ThumbnailImageURL: trimmedStringPtr(req.ThumbnailImageUrl),
		Metadata:          metadata,
		CreatedBy:         createdBy,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		return fmt.Errorf("create model revision: %w", err)
	}
	if err := s.database.EntrySearch.IndexModelRevision(ctx, *revision); err != nil {
		return fmt.Errorf("index model revision search: %w", err)
	}

	for _, artifact := range artifacts {
		if err := s.database.Artifacts.AttachToModelRevision(ctx, revision.ID, artifact.ID); err != nil {
			return fmt.Errorf("attach artifact to model revision: %w", err)
		}
		if err := s.saveProteinSequencesIfFASTA(ctx, entryRevisionID, artifact); err != nil {
			return err
		}
	}

	if req.Metrics != nil {
		for _, metricRequest := range *req.Metrics {
			metric, err := s.createMetric(ctx, metricRequest, now)
			if err != nil {
				return err
			}
			if err := s.database.Metrics.AttachToModelRevision(ctx, revision.ID, metric.ID); err != nil {
				return fmt.Errorf("attach metric to model revision: %w", err)
			}
		}
	}

	if req.Runs != nil {
		for _, runRequest := range *req.Runs {
			run, err := s.createRun(ctx, runRequest, createdBy, now)
			if err != nil {
				return err
			}
			if err := s.database.Runs.AttachToModelRevision(ctx, revision.ID, run.ID); err != nil {
				return fmt.Errorf("attach run to model revision: %w", err)
			}
			if runRequest.Artifacts != nil {
				for _, artifactRequest := range *runRequest.Artifacts {
					if err := s.attachArtifactToRun(ctx, run.ID, artifactRequest); err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
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
		ID:        req.Id,
		Name:      name,
		Level:     domainmodels.ArtifactLevel(req.Level),
		URI:       trimmedStringPtr(req.Uri),
		SHA256:    trimmedStringPtr(req.Sha256),
		Format:    trimmedStringPtr(req.Format),
		SizeBytes: req.SizeBytes,
		Metadata:  metadata,
		CreatedBy: createdBy,
		CreatedAt: now,
	})
	if err != nil {
		return nil, fmt.Errorf("create artifact: %w", err)
	}
	return artifact, nil
}

func (s *Server) createMetric(
	ctx context.Context,
	req CreateMetricRequest,
	now time.Time,
) (*domainmodels.Metric, error) {
	if req.Id == uuid.Nil {
		return nil, invalidRequest("metric id is required")
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return nil, invalidRequest("metric key is required")
	}

	metric, err := s.database.Metrics.Create(ctx, domainmodels.Metric{
		ID:        req.Id,
		Key:       domainmodels.MetricKey(key),
		Value:     req.Value,
		CreatedAt: now,
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
		ID:              req.Id,
		Name:            name,
		SoftwareName:    trimmedStringPtr(req.SoftwareName),
		SoftwareVersion: trimmedStringPtr(req.SoftwareVersion),
		Command:         trimmedStringPtr(req.Command),
		Parameters:      parameters,
		Metadata:        metadata,
		StartedAt:       req.StartedAt,
		FinishedAt:      req.FinishedAt,
		CreatedBy:       createdBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

func (s *Server) attachArtifactToRun(
	ctx context.Context,
	runID uuid.UUID,
	req CreateRunArtifactRequest,
) error {
	if req.ArtifactId == uuid.Nil {
		return invalidRequest("run artifact artifact_id is required")
	}
	if req.Direction == "" {
		return invalidRequest("run artifact direction is required")
	}

	if err := s.database.Runs.AttachArtifact(
		ctx,
		runID,
		req.ArtifactId,
		domainmodels.RunArtifactDirection(req.Direction),
	); err != nil {
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

func (s *Server) ListModels(w http.ResponseWriter, r *http.Request, entryID uuid.UUID, params ListModelsParams) {
	activeState := domainmodels.RevisionStateActive
	filters, err := modelFiltersFromParams(entryID, params, activeState)
	if err != nil {
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

	revisions, err := s.database.Models.List(r.Context(), filters)
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
		model, err := modelResponseFromRevision(revision, metrics)
		if err != nil {
			slog.Error("build model response failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build model response")
			return
		}
		items = append(items, model)
	}

	writeJSON(w, http.StatusOK, ModelListResponse{Items: items})
}

func (s *Server) CreateModel(w http.ResponseWriter, r *http.Request, entryID uuid.UUID) {
	var req CreateModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}

	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	err := s.createModelForEntry(r.Context(), entryID, req, user.ID)
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	}
	if errors.Is(err, errInvalidRequest) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err != nil {
		slog.Error("create model failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create model")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) createModelForEntry(
	ctx context.Context,
	entryID uuid.UUID,
	req CreateModelRequest,
	createdBy uuid.UUID,
) error {
	now := time.Now().UTC()

	return s.database.Do(ctx, func(ctx context.Context) error {
		entryRevision, err := s.activeEntryRevision(ctx, entryID)
		if err != nil {
			return err
		}
		return s.createModelGraph(ctx, entryRevision.EntryID, entryRevision.ID, req, now, createdBy)
	})
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
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build model response")
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) DeleteModel(w http.ResponseWriter, r *http.Request, entryID, modelID uuid.UUID) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	err := s.database.Do(r.Context(), func(ctx context.Context) error {
		revision, err := s.activeModelRevision(ctx, entryID, modelID)
		if err != nil {
			return err
		}
		if err := s.database.Models.Delete(ctx, revision.ModelID, revision.ID, user.ID); err != nil {
			return fmt.Errorf("delete model revision: %w", err)
		}
		if err := s.database.EntrySearch.DeleteModelRevision(ctx, *revision); err != nil {
			return fmt.Errorf("delete model revision search: %w", err)
		}
		return nil
	})
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
		return
	}
	if errors.Is(err, db.ErrModelRevisionOwnershipMismatch) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the model creator can delete it")
		return
	}
	if err != nil {
		slog.Error("delete model failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete model")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func modelFiltersFromParams(
	entryID uuid.UUID,
	params ListModelsParams,
	state domainmodels.RevisionState,
) (db.ModelRevisionFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.ModelRevisionFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.ModelRevisionFilters{}, errors.New("offset must be non-negative")
	}

	return db.ModelRevisionFilters{
		EntryID: &entryID,
		State:   &state,
		Limit:   params.Limit,
		Offset:  params.Offset,
	}, nil
}

func (s *Server) ListArtifacts(w http.ResponseWriter, r *http.Request, entryID uuid.UUID, params ListArtifactsParams) {
	entryRevision, err := s.activeEntryRevision(r.Context(), entryID)
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
	filters.EntryRevisionID = &entryRevision.ID

	artifacts, err := s.database.Artifacts.List(r.Context(), filters)
	if err != nil {
		slog.Error("list entry artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list artifacts")
		return
	}

	items, err := artifactResponsesFromModels(artifacts)
	if err != nil {
		slog.Error("build artifact response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build artifact response")
		return
	}
	writeJSON(w, http.StatusOK, ArtifactListResponse{Items: items})
}

func (s *Server) ListModelArtifacts(
	w http.ResponseWriter,
	r *http.Request,
	entryID uuid.UUID,
	modelID uuid.UUID,
	params ListModelArtifactsParams,
) {
	modelRevision, err := s.activeModelRevision(r.Context(), entryID, modelID)
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
	filters.ModelRevisionID = &modelRevision.ID

	artifacts, err := s.database.Artifacts.List(r.Context(), filters)
	if err != nil {
		slog.Error("list model artifacts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	runs, err := s.database.Runs.List(r.Context(), db.RunFilters{ModelRevisionID: &modelRevision.ID})
	if err != nil {
		slog.Error("list model runs failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}
	runArtifactLinks, err := s.database.Runs.ListArtifactLinks(r.Context(), modelRevision.ID)
	if err != nil {
		slog.Error("list model run artifact links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model artifacts")
		return
	}

	artifactItems, err := artifactResponsesFromModels(artifacts)
	if err != nil {
		slog.Error("build artifact response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build artifact response")
		return
	}

	runItems := make([]Run, 0, len(runs))
	for _, run := range runs {
		runItems = append(runItems, runResponseFromModel(run))
	}

	relationItems := make([]RunArtifact, 0, len(runArtifactLinks))
	for _, link := range runArtifactLinks {
		relationItems = append(relationItems, runArtifactResponseFromModel(link))
	}

	writeJSON(w, http.StatusOK, ModelArtifactListResponse{
		Items:     artifactItems,
		Runs:      runItems,
		Relations: relationItems,
	})
}

func artifactFiltersFromParams(params ListArtifactsParams) (db.ArtifactFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.ArtifactFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.ArtifactFilters{}, errors.New("offset must be non-negative")
	}

	filters := db.ArtifactFilters{
		Limit:  params.Limit,
		Offset: params.Offset,
	}
	if params.Levels != nil {
		filters.Levels = make([]domainmodels.ArtifactLevel, 0, len(*params.Levels))
		for _, level := range *params.Levels {
			filters.Levels = append(filters.Levels, domainmodels.ArtifactLevel(level))
		}
	}
	return filters, nil
}

func modelArtifactFiltersFromParams(params ListModelArtifactsParams) (db.ArtifactFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.ArtifactFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.ArtifactFilters{}, errors.New("offset must be non-negative")
	}

	filters := db.ArtifactFilters{
		Limit:  params.Limit,
		Offset: params.Offset,
	}
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
	revision, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID: &entryID,
		State:   &activeState,
	})
	if err != nil {
		return nil, err
	}
	return revision, nil
}

func (s *Server) activeModelRevision(
	ctx context.Context,
	entryID uuid.UUID,
	modelID uuid.UUID,
) (*domainmodels.ModelRevision, error) {
	if _, err := s.activeEntryRevision(ctx, entryID); errors.Is(err, db.ErrEntryRevisionNotFound) {
		return nil, db.ErrModelRevisionNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get active entry for model: %w", err)
	}

	activeState := domainmodels.RevisionStateActive
	revision, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
		EntryID: &entryID,
		ModelID: &modelID,
		State:   &activeState,
	})
	if err != nil {
		return nil, err
	}
	return revision, nil
}

func entryInfoResponseFromRevision(revision domainmodels.EntryRevision) (EntryInfo, error) {
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return EntryInfo{}, fmt.Errorf("build entry metadata response: %w", err)
	}
	return EntryInfo{
		Id:                revision.EntryID,
		CreatedBy:         revision.CreatedBy,
		Name:              revision.Name,
		Description:       revision.Description,
		ThumbnailImageUrl: revision.ThumbnailImageURL,
		Metadata:          &metadata,
		PublishedAt:       revision.PublishedAt,
		CreatedAt:         revision.CreatedAt,
		UpdatedAt:         revision.UpdatedAt,
	}, nil
}

func stringSliceFromPtr(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

func entryResponseFromRevision(
	revision domainmodels.EntryRevision,
	proteinSequences []domainmodels.ProteinSequence,
) (Entry, error) {
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return Entry{}, fmt.Errorf("build entry metadata response: %w", err)
	}

	return Entry{
		Id:                revision.EntryID,
		CreatedBy:         revision.CreatedBy,
		Name:              revision.Name,
		Description:       revision.Description,
		ThumbnailImageUrl: revision.ThumbnailImageURL,
		Metadata:          metadata,
		PublishedAt:       revision.PublishedAt,
		CreatedAt:         revision.CreatedAt,
		UpdatedAt:         revision.UpdatedAt,
		ProteinSequences:  proteinSequenceResponsesFromModels(proteinSequences),
	}, nil
}

func modelResponseFromRevision(
	revision domainmodels.ModelRevision,
	metrics []domainmodels.Metric,
) (Model, error) {
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return Model{}, fmt.Errorf("build model metadata response: %w", err)
	}

	return Model{
		Id:                revision.ModelID,
		EntryId:           revision.EntryID,
		CreatedBy:         revision.CreatedBy,
		Name:              revision.Name,
		Description:       revision.Description,
		ThumbnailImageUrl: revision.ThumbnailImageURL,
		Metadata:          metadata,
		PrimaryArtifactId: revision.PrimaryArtifactID,
		PublishedAt:       revision.PublishedAt,
		CreatedAt:         revision.CreatedAt,
		UpdatedAt:         revision.UpdatedAt,
		Metrics:           metricResponsesFromModels(metrics),
	}, nil
}

func artifactResponsesFromModels(artifacts []domainmodels.Artifact) ([]Artifact, error) {
	items := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		item, err := artifactResponseFromModel(artifact)
		if err != nil {
			return nil, err
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

	return Artifact{
		Id:        artifact.ID,
		Name:      artifact.Name,
		Level:     ArtifactLevel(artifact.Level),
		Uri:       artifact.URI,
		Sha256:    artifact.SHA256,
		Format:    artifact.Format,
		SizeBytes: artifact.SizeBytes,
		Metadata:  metadata,
		CreatedBy: artifact.CreatedBy,
		CreatedAt: artifact.CreatedAt,
	}, nil
}

func metricResponsesFromModels(metrics []domainmodels.Metric) []Metric {
	items := make([]Metric, 0, len(metrics))
	for _, metric := range metrics {
		items = append(items, Metric{
			Id:        metric.ID,
			Key:       string(metric.Key),
			Value:     metric.Value,
			CreatedAt: metric.CreatedAt,
		})
	}
	return items
}

func runResponseFromModel(run domainmodels.Run) Run {
	return Run{
		Id:              run.ID,
		Name:            run.Name,
		SoftwareName:    run.SoftwareName,
		SoftwareVersion: run.SoftwareVersion,
		Command:         run.Command,
		Parameters:      mapFromNil(run.Parameters),
		Metadata:        mapFromNil(run.Metadata),
		StartedAt:       run.StartedAt,
		FinishedAt:      run.FinishedAt,
		CreatedBy:       run.CreatedBy,
		CreatedAt:       run.CreatedAt,
		UpdatedAt:       run.UpdatedAt,
	}
}

func runArtifactResponseFromModel(link db.RunArtifactLink) RunArtifact {
	return RunArtifact{
		RunId:      link.RunID,
		ArtifactId: link.ArtifactID,
		Direction:  RunArtifactDirection(link.Direction),
		Position:   nil,
	}
}

func proteinSequenceResponsesFromModels(sequences []domainmodels.ProteinSequence) []ProteinSequence {
	items := make([]ProteinSequence, 0, len(sequences))
	for _, sequence := range sequences {
		items = append(items, ProteinSequence{
			Id:               sequence.ID,
			SourceArtifactId: sequence.SourceArtifactID,
			RecordIndex:      sequence.RecordIndex,
			Header:           sequence.Header,
			Sequence:         sequence.Sequence,
			CreatedAt:        sequence.CreatedAt,
		})
	}
	return items
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

func ptr[T any](value T) *T {
	return &value
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
