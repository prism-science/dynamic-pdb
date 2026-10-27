package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

func (s *Server) CreateEntryRevision(w http.ResponseWriter, r *http.Request, entryID string) {
	payload, err := decodeCreateEntryRevisionPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	schemaErr := s.checkSchema(r, entryRevisionChangeErrors(payload))
	if s.writeMutationError(r.Context(), w, schemaErr, "create entry revision") {
		return
	}
	result, err := s.createEntryRevisionGraph(r.Context(), entryID, payload, user.ID)
	if s.writeMutationError(r.Context(), w, err, "create entry revision") {
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

func (s *Server) createEntryRevisionGraph(
	ctx context.Context,
	entryID string,
	payload createEntryRevisionPayload,
	createdBy uuid.UUID,
) (CreateEntryRevisionAttributes, error) {
	req := payload.Request
	if len(payload.EntryFields) == 0 {
		return CreateEntryRevisionAttributes{}, invalidRequest("entry change must contain at least one field")
	}
	now := time.Now().UTC()
	attributes := CreateEntryRevisionAttributes{
		EntryId:      entryID,
		State:        CreateEntryRevisionAttributesStateInReview,
		ModelResults: make([]ModelOperationResult, 0),
	}

	err := s.database.Do(ctx, func(ctx context.Context) error {
		activeState := domainmodels.RevisionStateActive
		activeEntryState := domainmodels.EntryStateActive
		base, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
			EntryID:    &entryID,
			State:      &activeState,
			EntryState: &activeEntryState,
		})
		if err != nil {
			return fmt.Errorf("get active entry revision: %w", err)
		}
		attributes.BaseRevisionId = &base.ID

		revision := domainmodels.EntryRevision{
			ID:                uuid.New(),
			EntryID:           entryID,
			ParentRevisionID:  &base.ID,
			State:             domainmodels.RevisionStateInReview,
			EntryState:        base.EntryState,
			Title:             base.Title,
			ThumbnailImageURL: base.ThumbnailImageURL,
			Metadata:          base.Metadata,
			CreatedBy:         createdBy,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		if err := applyEntryRevisionChange(&revision, req.Entry, payload.EntryFields); err != nil {
			return fmt.Errorf("apply entry changes: %w", err)
		}
		revision.FieldSources = domainmodels.EntryRevisionFieldSources(base, revision)
		if err := s.ensurePDBReferenceAvailable(ctx, revision.Metadata, &entryID); err != nil {
			return fmt.Errorf("validate entry PDB reference: %w", err)
		}

		created, err := s.database.Entries.Create(ctx, revision)
		if err != nil {
			return fmt.Errorf("create entry revision: %w", err)
		}
		attributes.RevisionId = created.ID
		if err := s.database.Artifacts.CopyEntryRevisionLinks(ctx, base.ID, created.ID); err != nil {
			return fmt.Errorf("copy entry revision artifacts: %w", err)
		}
		return nil
	})
	if err != nil {
		return CreateEntryRevisionAttributes{}, fmt.Errorf("create entry revision graph: %w", err)
	}
	return attributes, nil
}

func (s *Server) ListEntryRevisionGroups(
	w http.ResponseWriter,
	r *http.Request,
	params ListEntryRevisionGroupsParams,
) {
	if !s.requireReviewer(w, r) {
		return
	}
	state := domainmodels.RevisionState(params.State)
	if err := validateRevisionState(state); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindRevisionGroups)
	if err != nil {
		writeListParamsError(w, err, "invalid pagination")
		return
	}
	s.listEntryRevisionGroups(w, r, state, nil, page)
}

func (s *Server) ListEntryRevisionsForEntry(
	w http.ResponseWriter,
	r *http.Request,
	entryID string,
	params ListEntryRevisionsForEntryParams,
) {
	if !s.requireReviewer(w, r) {
		return
	}
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindEntryRevisions)
	if err != nil {
		writeListParamsError(w, err, "invalid pagination")
		return
	}
	s.listEntryRevisionSummaries(w, r, db.EntryRevisionFilters{EntryID: &entryID}, page)
}

func (s *Server) GetEntryRevision(
	w http.ResponseWriter,
	r *http.Request,
	entryID string,
	revisionID uuid.UUID,
) {
	if !s.requireReviewer(w, r) {
		return
	}
	s.writeEntryRevision(w, r, entryID, revisionID, nil)
}

func (s *Server) UpdateEntryRevisionState(
	w http.ResponseWriter,
	r *http.Request,
	entryID string,
	revisionID uuid.UUID,
) {
	var req UpdateRevisionStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	state := domainmodels.RevisionState(req.State)
	if state != domainmodels.RevisionStateActive && state != domainmodels.RevisionStateRejected {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "state must be active or rejected")
		return
	}
	permission := domainmodels.PermissionKeyRevisionsApprove
	if state == domainmodels.RevisionStateRejected {
		permission = domainmodels.PermissionKeyRevisionsReject
	}
	if !s.requirePermission(w, r, user.ID, permission) {
		return
	}

	target, err := s.database.Entries.Get(r.Context(), db.EntryRevisionFilters{
		ID:      &revisionID,
		EntryID: &entryID,
	})
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry revision not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get entry revision for decision failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update entry revision")
		return
	}
	if target.State != domainmodels.RevisionStateInReview {
		writeError(w, http.StatusConflict, "CONFLICT", "entry revision is not in the required state")
		return
	}

	err = s.database.Do(r.Context(), func(ctx context.Context) error {
		if state == domainmodels.RevisionStateRejected {
			if _, err := s.database.Entries.RejectRevision(ctx, entryID, revisionID); err != nil {
				return fmt.Errorf("reject entry revision: %w", err)
			}
			return nil
		}

		if err := s.database.Entries.Lock(ctx, entryID); err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		if err := s.ensureRevisionParentIsActive(ctx, *target); err != nil {
			return fmt.Errorf("validate entry revision parent: %w", err)
		}
		if err := s.reconcileProteinSequencesForActivation(ctx, *target); err != nil {
			return fmt.Errorf("reconcile activated protein sequences: %w", err)
		}
		activated, err := s.database.Entries.ActivateRevision(ctx, entryID, revisionID)
		if err != nil {
			return fmt.Errorf("activate entry revision: %w", err)
		}
		if err := s.reindexActiveEntry(ctx, *activated); err != nil {
			return fmt.Errorf("reindex activated entry: %w", err)
		}
		return nil
	})
	if s.writeMutationError(r.Context(), w, err, "update entry revision") {
		return
	}
	s.writeEntryRevision(w, r, entryID, revisionID, nil)
}

func (s *Server) reconcileProteinSequencesForActivation(
	ctx context.Context,
	target domainmodels.EntryRevision,
) error {
	artifacts, err := s.finalFASTAArtifacts(ctx, target)
	if err != nil {
		return fmt.Errorf("list final FASTA artifacts: %w", err)
	}
	targetArtifactIDs := make(map[uuid.UUID]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		targetArtifactIDs[artifact.ID] = struct{}{}
	}

	targetSequences, err := s.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &target.ID,
	})
	if err != nil {
		return fmt.Errorf("list pre-activation protein sequences: %w", err)
	}

	previousSequences := make([]domainmodels.ProteinSequence, 0)
	if target.ParentRevisionID != nil {
		previousSequences, err = s.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
			EntryRevisionID: target.ParentRevisionID,
		})
		if err != nil {
			return fmt.Errorf("list active protein sequences: %w", err)
		}
	}

	retainedArtifactIDs := make(map[uuid.UUID]struct{})
	sequenceIDsToDelete := make([]uuid.UUID, 0, len(targetSequences)+len(previousSequences))
	for _, sequence := range targetSequences {
		sequenceIDsToDelete = append(sequenceIDsToDelete, sequence.ID)
	}
	for _, sequence := range previousSequences {
		if _, retained := targetArtifactIDs[sequence.SourceArtifactID]; retained {
			retainedArtifactIDs[sequence.SourceArtifactID] = struct{}{}
			continue
		}
		sequenceIDsToDelete = append(sequenceIDsToDelete, sequence.ID)
	}

	if len(sequenceIDsToDelete) > 0 {
		if err := s.database.ProteinSequenceSimilarities.DeleteForProteinSequences(ctx, sequenceIDsToDelete); err != nil {
			return fmt.Errorf("delete invalidated protein sequence similarities: %w", err)
		}
		if err := s.database.ProteinSequences.Delete(ctx, sequenceIDsToDelete); err != nil {
			return fmt.Errorf("delete invalidated protein sequences: %w", err)
		}
	}

	retainedIDs := make([]uuid.UUID, 0, len(retainedArtifactIDs))
	for artifactID := range retainedArtifactIDs {
		retainedIDs = append(retainedIDs, artifactID)
	}
	if target.ParentRevisionID != nil {
		if err := s.database.ProteinSequences.MoveEntryRevisionArtifacts(
			ctx,
			*target.ParentRevisionID,
			target.ID,
			retainedIDs,
		); err != nil {
			return fmt.Errorf("move retained protein sequences: %w", err)
		}
	}

	for _, artifact := range artifacts {
		if _, retained := retainedArtifactIDs[artifact.ID]; retained {
			continue
		}
		if err := s.saveProteinSequencesIfFASTA(ctx, target.ID, artifact); err != nil {
			return fmt.Errorf("create activated protein sequences: %w", err)
		}
	}
	return nil
}

func (s *Server) reconcileProteinSequencesForActiveModels(
	ctx context.Context,
	activeEntry domainmodels.EntryRevision,
) error {
	artifacts, err := s.finalFASTAArtifacts(ctx, activeEntry)
	if err != nil {
		return fmt.Errorf("list final FASTA artifacts: %w", err)
	}
	artifactIDs := make(map[uuid.UUID]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		artifactIDs[artifact.ID] = struct{}{}
	}
	sequences, err := s.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &activeEntry.ID,
	})
	if err != nil {
		return fmt.Errorf("list active protein sequences: %w", err)
	}
	retainedArtifactIDs := make(map[uuid.UUID]struct{})
	sequenceIDsToDelete := make([]uuid.UUID, 0)
	for _, sequence := range sequences {
		if _, retained := artifactIDs[sequence.SourceArtifactID]; retained {
			retainedArtifactIDs[sequence.SourceArtifactID] = struct{}{}
			continue
		}
		sequenceIDsToDelete = append(sequenceIDsToDelete, sequence.ID)
	}
	if len(sequenceIDsToDelete) > 0 {
		if err := s.database.ProteinSequenceSimilarities.DeleteForProteinSequences(ctx, sequenceIDsToDelete); err != nil {
			return fmt.Errorf("delete invalidated protein sequence similarities: %w", err)
		}
		if err := s.database.ProteinSequences.Delete(ctx, sequenceIDsToDelete); err != nil {
			return fmt.Errorf("delete invalidated protein sequences: %w", err)
		}
	}
	for _, artifact := range artifacts {
		if _, retained := retainedArtifactIDs[artifact.ID]; retained {
			continue
		}
		if err := s.saveProteinSequencesIfFASTA(ctx, activeEntry.ID, artifact); err != nil {
			return fmt.Errorf("create active model protein sequences: %w", err)
		}
	}
	return nil
}

func (s *Server) finalFASTAArtifacts(
	ctx context.Context,
	target domainmodels.EntryRevision,
) ([]domainmodels.Artifact, error) {
	if target.EntryState == domainmodels.EntryStateDeleted {
		return []domainmodels.Artifact{}, nil
	}

	formats := []string{"fasta"}
	artifacts, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{
		EntryRevisionID: &target.ID,
		Formats:         formats,
	})
	if err != nil {
		return nil, fmt.Errorf("list entry FASTA artifacts: %w", err)
	}
	seenArtifactIDs := make(map[uuid.UUID]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		seenArtifactIDs[artifact.ID] = struct{}{}
	}

	activeRevisionState := domainmodels.RevisionStateActive
	activeModelState := domainmodels.ModelStateActive
	activeModels, err := s.database.Models.List(ctx, db.ModelRevisionFilters{
		EntryID:    &target.EntryID,
		State:      &activeRevisionState,
		ModelState: &activeModelState,
	})
	if err != nil {
		return nil, fmt.Errorf("list final active models: %w", err)
	}
	for _, model := range activeModels {
		modelArtifacts, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{
			ModelRevisionID: &model.ID,
			Formats:         formats,
		})
		if err != nil {
			return nil, fmt.Errorf("list model %s FASTA artifacts: %w", model.ID, err)
		}
		for _, artifact := range modelArtifacts {
			if _, seen := seenArtifactIDs[artifact.ID]; seen {
				continue
			}
			seenArtifactIDs[artifact.ID] = struct{}{}
			artifacts = append(artifacts, artifact)
		}
	}
	return artifacts, nil
}

func (s *Server) ensureRevisionParentIsActive(
	ctx context.Context,
	target domainmodels.EntryRevision,
) error {
	activeState := domainmodels.RevisionStateActive
	active, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID: &target.EntryID,
		State:   &activeState,
	})
	switch {
	case err == nil:
		if target.ParentRevisionID == nil || *target.ParentRevisionID != active.ID {
			return fmt.Errorf("revision parent is no longer active: %w", errConflict)
		}
		return nil
	case errors.Is(err, db.ErrEntryRevisionNotFound):
		if target.ParentRevisionID != nil {
			return fmt.Errorf("revision parent is missing: %w", errConflict)
		}
		return nil
	default:
		return fmt.Errorf("get active parent revision: %w", err)
	}
}

func (s *Server) ensureModelRevisionParentIsActive(
	ctx context.Context,
	target domainmodels.ModelRevision,
) error {
	activeState := domainmodels.RevisionStateActive
	active, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
		EntryID: &target.EntryID,
		ModelID: &target.ModelID,
		State:   &activeState,
	})
	switch {
	case err == nil:
		if target.ParentRevisionID == nil || *target.ParentRevisionID != active.ID {
			return fmt.Errorf("revision parent is no longer active: %w", errConflict)
		}
		return nil
	case errors.Is(err, db.ErrModelRevisionNotFound):
		if target.ParentRevisionID != nil {
			return fmt.Errorf("revision parent is missing: %w", errConflict)
		}
		return nil
	default:
		return fmt.Errorf("get active parent model revision: %w", err)
	}
}

func (s *Server) reindexActiveEntry(ctx context.Context, revision domainmodels.EntryRevision) error {
	// TODO: Reconsider the full search-index rebuild: changing an entry revision should not require reindexing all of its models.
	if err := s.database.EntrySearch.DeleteEntry(ctx, revision.EntryID); err != nil {
		return fmt.Errorf("clear entry search index: %w", err)
	}
	if revision.EntryState != domainmodels.EntryStateActive {
		return nil
	}
	if err := s.database.EntrySearch.IndexEntryRevision(ctx, revision); err != nil {
		return fmt.Errorf("index active entry revision: %w", err)
	}
	activeState := domainmodels.RevisionStateActive
	activeModelState := domainmodels.ModelStateActive
	models, err := s.database.Models.List(ctx, db.ModelRevisionFilters{
		EntryID:    &revision.EntryID,
		State:      &activeState,
		ModelState: &activeModelState,
	})
	if err != nil {
		return fmt.Errorf("list active models for search index: %w", err)
	}
	for _, model := range models {
		if err := s.database.EntrySearch.IndexModelRevision(ctx, model); err != nil {
			return fmt.Errorf("index active model revision: %w", err)
		}
	}
	return nil
}

func (s *Server) ListModelRevisionsForModel(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID string,
	params ListModelRevisionsForModelParams,
) {
	if !s.requireReviewer(w, r) {
		return
	}
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindModelRevisions)
	if err != nil {
		writeListParamsError(w, err, "invalid pagination")
		return
	}
	s.listModelRevisionSummaries(w, r, db.ModelRevisionFilters{EntryID: &entryID, ModelID: &modelID}, page)
}

func (s *Server) GetModelRevision(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID string,
	revisionID uuid.UUID,
) {
	if !s.requireReviewer(w, r) {
		return
	}
	s.writeModelRevision(w, r, entryID, modelID, revisionID, nil)
}

func (s *Server) UpdateModelRevisionState(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID string,
	revisionID uuid.UUID,
) {
	var request UpdateRevisionStateRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	state := domainmodels.RevisionState(request.State)
	if state != domainmodels.RevisionStateActive && state != domainmodels.RevisionStateRejected {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "state must be active or rejected")
		return
	}
	permission := domainmodels.PermissionKeyRevisionsApprove
	if state == domainmodels.RevisionStateRejected {
		permission = domainmodels.PermissionKeyRevisionsReject
	}
	if !s.requirePermission(w, r, user.ID, permission) {
		return
	}

	target, err := s.database.Models.Get(r.Context(), db.ModelRevisionFilters{
		ID: &revisionID, EntryID: &entryID, ModelID: &modelID,
	})
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model revision not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get model revision for decision failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to update model revision")
		return
	}
	if target.State != domainmodels.RevisionStateInReview {
		writeError(w, http.StatusConflict, "CONFLICT", "model revision is not in the required state")
		return
	}

	err = s.database.Do(r.Context(), func(ctx context.Context) error {
		if state == domainmodels.RevisionStateRejected {
			if _, err := s.database.Models.RejectRevision(ctx, revisionID); err != nil {
				return fmt.Errorf("reject model revision: %w", err)
			}
			return nil
		}
		if err := s.ensureModelRevisionParentIsActive(ctx, *target); err != nil {
			return fmt.Errorf("validate model revision parent: %w", err)
		}
		if _, err := s.database.Models.ActivateRevision(ctx, revisionID); err != nil {
			return fmt.Errorf("activate model revision: %w", err)
		}
		activeState := domainmodels.RevisionStateActive
		activeEntry, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
			EntryID: &entryID,
			State:   &activeState,
		})
		if err != nil {
			return fmt.Errorf("get active entry revision: %w", err)
		}
		if activeEntry.EntryState != domainmodels.EntryStateActive {
			return fmt.Errorf("entry is not active: %w", errConflict)
		}
		if err := s.reconcileProteinSequencesForActiveModels(ctx, *activeEntry); err != nil {
			return fmt.Errorf("reconcile model protein sequences: %w", err)
		}
		if err := s.reindexActiveEntry(ctx, *activeEntry); err != nil {
			return fmt.Errorf("reindex entry after model activation: %w", err)
		}
		return nil
	})
	if s.writeMutationError(r.Context(), w, err, "update model revision") {
		return
	}
	s.writeModelRevision(w, r, entryID, modelID, revisionID, nil)
}

func (s *Server) ListUserEntryRevisionGroups(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
	params ListUserEntryRevisionGroupsParams,
) {
	if !s.requirePathUser(w, r, userID) {
		return
	}
	state := domainmodels.RevisionState(params.State)
	if err := validateRevisionState(state); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindRevisionGroups)
	if err != nil {
		writeListParamsError(w, err, "invalid pagination")
		return
	}
	s.listEntryRevisionGroups(w, r, state, &userID, page)
}

func (s *Server) ListUserEntryRevisionsForEntry(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
	entryID string,
	params ListUserEntryRevisionsForEntryParams,
) {
	if !s.requirePathUser(w, r, userID) {
		return
	}
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindEntryRevisions)
	if err != nil {
		writeListParamsError(w, err, "invalid pagination")
		return
	}
	s.listEntryRevisionSummaries(w, r, db.EntryRevisionFilters{EntryID: &entryID, CreatedBy: &userID}, page)
}

func (s *Server) GetUserEntryRevision(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
	entryID string,
	revisionID uuid.UUID,
) {
	if !s.requirePathUser(w, r, userID) {
		return
	}
	s.writeEntryRevision(w, r, entryID, revisionID, &userID)
}

func (s *Server) SubmitUserModelRevision(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
	entryID, modelID string,
	revisionID uuid.UUID,
) {
	var request SubmitRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	if !s.requirePathUser(w, r, userID) {
		return
	}
	if request.State != SubmitRevisionRequestStateInReview {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "state must be in_review")
		return
	}
	target, err := s.database.Models.Get(r.Context(), db.ModelRevisionFilters{
		ID:        &revisionID,
		EntryID:   &entryID,
		ModelID:   &modelID,
		CreatedBy: &userID,
	})
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model revision not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get model revision for submit failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to submit model revision")
		return
	}
	if target.State != domainmodels.RevisionStatePending && target.State != domainmodels.RevisionStateRejected {
		writeError(w, http.StatusConflict, "CONFLICT", "model revision cannot be submitted")
		return
	}
	if _, err := s.database.Models.SetRevisionState(
		r.Context(),
		revisionID,
		[]domainmodels.RevisionState{domainmodels.RevisionStatePending, domainmodels.RevisionStateRejected},
		domainmodels.RevisionStateInReview,
	); s.writeMutationError(r.Context(), w, err, "submit model revision") {
		return
	}
	s.writeModelRevision(w, r, entryID, modelID, revisionID, &userID)
}

func (s *Server) ListUserModelRevisionsForModel(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
	entryID, modelID string,
	params ListUserModelRevisionsForModelParams,
) {
	if !s.requirePathUser(w, r, userID) {
		return
	}
	page, err := listPageFromParams(params.Limit, params.Offset, params.Cursor, listCursorKindModelRevisions)
	if err != nil {
		writeListParamsError(w, err, "invalid pagination")
		return
	}
	s.listModelRevisionSummaries(w, r, db.ModelRevisionFilters{
		EntryID:   &entryID,
		ModelID:   &modelID,
		CreatedBy: &userID,
	}, page)
}

func (s *Server) GetUserModelRevision(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
	entryID, modelID string,
	revisionID uuid.UUID,
) {
	if !s.requirePathUser(w, r, userID) {
		return
	}
	s.writeModelRevision(w, r, entryID, modelID, revisionID, &userID)
}

func (s *Server) requireReviewer(w http.ResponseWriter, r *http.Request) bool {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return false
	}

	allowed, err := s.authorizer.HasRole(r.Context(), user.ID, domainmodels.RoleKeyReviewer)
	if err != nil {
		slog.ErrorContext(r.Context(), "reviewer authorization failed", "err", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to authorize request")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "reviewer access is required")
		return false
	}
	return true
}

func (s *Server) requirePermission(
	w http.ResponseWriter,
	r *http.Request,
	userID uuid.UUID,
	permission domainmodels.PermissionKey,
) bool {
	allowed, err := s.authorizer.Can(r.Context(), userID, permission)
	if err != nil {
		slog.ErrorContext(r.Context(),
			"permission authorization failed",
			"err", err,
			"user_id", userID,
			"permission", permission,
		)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to authorize request")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "required permission is missing")
		return false
	}
	return true
}

func (s *Server) requirePathUser(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return false
	}
	if user.ID != userID {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "authenticated user does not match user_id")
		return false
	}
	return true
}

func validateRevisionState(state domainmodels.RevisionState) error {
	switch state {
	case domainmodels.RevisionStatePending,
		domainmodels.RevisionStateInReview,
		domainmodels.RevisionStateActive,
		domainmodels.RevisionStateRejected,
		domainmodels.RevisionStateArchived:
		return nil
	default:
		return invalidRequest("invalid revision state")
	}
}

func (s *Server) listEntryRevisionGroups(
	w http.ResponseWriter,
	r *http.Request,
	state domainmodels.RevisionState,
	createdBy *uuid.UUID,
	page listPage,
) {
	groups, err := s.database.Entries.ListRevisionGroups(r.Context(), db.RevisionGroupFilters{
		State:     state,
		CreatedBy: createdBy,
		Limit:     page.fetchLimit(),
		Offset:    page.Offset,
		After:     page.afterRevisionGroup(),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "list revision groups failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list grouped revisions")
		return
	}
	groups, hasNextPage := trimToPage(groups, page)
	items, err := s.entryRevisionGroupItems(r.Context(), state, createdBy, groups)
	if err != nil {
		slog.ErrorContext(r.Context(), "build revision groups failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list grouped revisions")
		return
	}
	// The cursor follows the last group of the SQL page even when that group was skipped
	// for having no matching revisions, so the next page still starts right after it.
	links, meta, err := nextPageLinksAfter(s, r, groups, hasNextPage, func(last db.RevisionGroup) listCursor {
		return listCursor{Kind: listCursorKindRevisionGroups, CreatedAt: last.LatestCreatedAt, EntryID: last.EntryID}
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "build revision group list links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list grouped revisions")
		return
	}
	writeJSON(w, http.StatusOK, EntryRevisionGroupCollectionDocument{Data: items, Links: links, Meta: meta})
}

// entryRevisionGroupItems builds the page's groups in order. It loads the matching
// entry revisions, matching model revisions and active entry revisions of all the
// page's entries with one query each.
func (s *Server) entryRevisionGroupItems(
	ctx context.Context,
	state domainmodels.RevisionState,
	createdBy *uuid.UUID,
	groups []db.RevisionGroup,
) ([]EntryRevisionGroupData, error) {
	// An empty EntryIDs filter matches every entry, so an empty page stops here.
	if len(groups) == 0 {
		return []EntryRevisionGroupData{}, nil
	}
	entryIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		entryIDs = append(entryIDs, group.EntryID)
	}

	entryRevisions, err := s.database.Entries.List(ctx, db.EntryRevisionFilters{
		EntryIDs:  entryIDs,
		State:     &state,
		CreatedBy: createdBy,
	})
	if err != nil {
		return nil, fmt.Errorf("list grouped entry revisions: %w", err)
	}
	modelRevisions, err := s.database.Models.List(ctx, db.ModelRevisionFilters{
		EntryIDs:  entryIDs,
		State:     &state,
		CreatedBy: createdBy,
		// Summaries never return metadata, so the large per-residue data is not worth loading.
		ExcludeResidueData: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list grouped model revisions: %w", err)
	}
	activeState := domainmodels.RevisionStateActive
	activeRevisions, err := s.database.Entries.List(ctx, db.EntryRevisionFilters{
		EntryIDs: entryIDs,
		State:    &activeState,
	})
	if err != nil {
		return nil, fmt.Errorf("list active entry revisions of groups: %w", err)
	}
	return revisionGroupItemsFromRevisions(ctx, groups, entryRevisions, modelRevisions, activeRevisions)
}

// revisionGroupItemsFromRevisions builds the page's groups in order from the revisions
// loaded for them. The groups and the revisions come from separate queries, so a review
// decision in between can leave a group with no matching revisions; such a group is
// skipped, which only makes the page shorter.
func revisionGroupItemsFromRevisions(
	ctx context.Context,
	groups []db.RevisionGroup,
	entryRevisions []domainmodels.EntryRevision,
	modelRevisions []domainmodels.ModelRevision,
	activeRevisions []domainmodels.EntryRevision,
) ([]EntryRevisionGroupData, error) {
	entryRevisionsByEntryID := revisionsByEntryID(entryRevisions, func(revision domainmodels.EntryRevision) string {
		return revision.EntryID
	})
	modelRevisionsByEntryID := revisionsByEntryID(modelRevisions, func(revision domainmodels.ModelRevision) string {
		return revision.EntryID
	})
	activeRevisionByEntryID := make(map[string]domainmodels.EntryRevision, len(activeRevisions))
	for _, revision := range activeRevisions {
		activeRevisionByEntryID[revision.EntryID] = revision
	}

	items := make([]EntryRevisionGroupData, 0, len(groups))
	for _, group := range groups {
		groupEntryRevisions := entryRevisionsByEntryID[group.EntryID]
		groupModelRevisions := modelRevisionsByEntryID[group.EntryID]
		if len(groupEntryRevisions) == 0 && len(groupModelRevisions) == 0 {
			slog.WarnContext(ctx, "skip revision group without matching revisions", "entry_id", group.EntryID)
			continue
		}
		displayRevision, hasActiveRevision := activeRevisionByEntryID[group.EntryID]
		if !hasActiveRevision {
			if len(groupEntryRevisions) == 0 {
				slog.WarnContext(ctx, "skip revision group without an entry revision", "entry_id", group.EntryID)
				continue
			}
			// Entries.List orders newest first, so this is the newest matching revision.
			displayRevision = groupEntryRevisions[0]
		}
		entry, err := entryInfoAttributesFromRevision(displayRevision)
		if err != nil {
			return nil, fmt.Errorf("build entry info of revision group %s: %w", group.EntryID, err)
		}
		entrySummaries := make([]EntryRevisionSummaryAttributes, 0, len(groupEntryRevisions))
		for _, revision := range groupEntryRevisions {
			entrySummaries = append(entrySummaries, entryRevisionSummaryAttributesFromModel(revision))
		}
		modelSummaries := make([]ModelRevisionSummaryAttributes, 0, len(groupModelRevisions))
		for _, revision := range groupModelRevisions {
			modelSummaries = append(modelSummaries, modelRevisionSummaryAttributesFromModel(revision))
		}
		items = append(items, EntryRevisionGroupData{
			Type: jsonAPITypeEntryRevisionGroups,
			Id:   entry.Id,
			Attributes: EntryRevisionGroupAttributes{
				Entry:          entry,
				EntryRevisions: entrySummaries,
				ModelRevisions: modelSummaries,
			},
		})
	}
	return items, nil
}

// revisionsByEntryID groups revisions by entry, keeping their order within each entry.
func revisionsByEntryID[T any](revisions []T, entryID func(T) string) map[string][]T {
	grouped := make(map[string][]T)
	for _, revision := range revisions {
		key := entryID(revision)
		grouped[key] = append(grouped[key], revision)
	}
	return grouped
}

func (s *Server) listEntryRevisionSummaries(
	w http.ResponseWriter,
	r *http.Request,
	filters db.EntryRevisionFilters,
	page listPage,
) {
	filters.Limit = page.fetchLimit()
	filters.Offset = page.Offset
	filters.After = page.after()
	revisions, err := s.database.Entries.List(r.Context(), filters)
	if err != nil {
		slog.ErrorContext(r.Context(), "list entry revisions failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entry revisions")
		return
	}
	revisions, hasNextPage := trimToPage(revisions, page)
	items := make([]EntryRevisionSummaryData, 0, len(revisions))
	for _, revision := range revisions {
		summary := entryRevisionSummaryAttributesFromModel(revision)
		items = append(items, EntryRevisionSummaryData{
			Type:       jsonAPITypeEntryRevisionSummaries,
			Id:         summary.Id,
			Attributes: summary,
		})
	}
	links, meta, err := nextPageLinksAfter(s, r, revisions, hasNextPage, func(last domainmodels.EntryRevision) listCursor {
		return listCursor{Kind: listCursorKindEntryRevisions, CreatedAt: last.CreatedAt, ID: last.ID}
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "build entry revision list links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entry revisions")
		return
	}
	writeJSON(w, http.StatusOK, EntryRevisionSummaryCollectionDocument{Data: items, Links: links, Meta: meta})
}

func (s *Server) listModelRevisionSummaries(
	w http.ResponseWriter,
	r *http.Request,
	filters db.ModelRevisionFilters,
	page listPage,
) {
	filters.Limit = page.fetchLimit()
	filters.Offset = page.Offset
	filters.After = page.after()
	// Summaries never return metadata, so the large per-residue data is not worth loading.
	filters.ExcludeResidueData = true
	revisions, err := s.database.Models.List(r.Context(), filters)
	if err != nil {
		slog.ErrorContext(r.Context(), "list model revisions failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model revisions")
		return
	}
	revisions, hasNextPage := trimToPage(revisions, page)
	items := make([]ModelRevisionSummaryData, 0, len(revisions))
	for _, revision := range revisions {
		summary := modelRevisionSummaryAttributesFromModel(revision)
		items = append(items, ModelRevisionSummaryData{
			Type:       jsonAPITypeModelRevisionSummaries,
			Id:         summary.Id,
			Attributes: summary,
		})
	}
	links, meta, err := nextPageLinksAfter(s, r, revisions, hasNextPage, func(last domainmodels.ModelRevision) listCursor {
		return listCursor{Kind: listCursorKindModelRevisions, CreatedAt: last.CreatedAt, ID: last.ID}
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "build model revision list links failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list model revisions")
		return
	}
	writeJSON(w, http.StatusOK, ModelRevisionSummaryCollectionDocument{Data: items, Links: links, Meta: meta})
}

func (s *Server) writeEntryRevision(
	w http.ResponseWriter,
	r *http.Request,
	entryID string,
	revisionID uuid.UUID,
	createdBy *uuid.UUID,
) {
	filters := db.EntryRevisionFilters{ID: &revisionID, EntryID: &entryID, CreatedBy: createdBy}
	revision, err := s.database.Entries.Get(r.Context(), filters)
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry revision not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get entry revision failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry revision")
		return
	}
	attributes, err := s.entryRevisionAttributesFromModel(r.Context(), *revision)
	if err != nil {
		slog.ErrorContext(r.Context(), "build entry revision response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry revision")
		return
	}
	writeJSON(w, http.StatusOK, EntryRevisionDocument{
		Data: EntryRevisionData{
			Type:       jsonAPITypeEntryRevisions,
			Id:         attributes.Id,
			Attributes: attributes,
		},
	})
}

func (s *Server) writeModelRevision(
	w http.ResponseWriter,
	r *http.Request,
	entryID, modelID string,
	revisionID uuid.UUID,
	createdBy *uuid.UUID,
) {
	revision, err := s.database.Models.Get(r.Context(), db.ModelRevisionFilters{
		ID: &revisionID, EntryID: &entryID, ModelID: &modelID, CreatedBy: createdBy,
	})
	if errors.Is(err, db.ErrModelRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model revision not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "get model revision failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model revision")
		return
	}
	attributes, err := s.modelRevisionAttributesFromModel(r.Context(), *revision)
	if err != nil {
		slog.ErrorContext(r.Context(), "build model revision response failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model revision")
		return
	}
	writeJSON(w, http.StatusOK, ModelRevisionDocument{
		Data: ModelRevisionData{
			Type:       jsonAPITypeModelRevisions,
			Id:         attributes.Id,
			Attributes: attributes,
		},
	})
}

func (s *Server) entryRevisionAttributesFromModel(
	ctx context.Context,
	revision domainmodels.EntryRevision,
) (EntryRevisionAttributes, error) {
	properties, err := entryPropertiesFromModel(revision.Metadata)
	if err != nil {
		return EntryRevisionAttributes{}, fmt.Errorf("build entry revision fields: %w", err)
	}
	artifacts, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{EntryRevisionID: &revision.ID})
	if err != nil {
		return EntryRevisionAttributes{}, fmt.Errorf("list entry revision artifacts: %w", err)
	}
	artifactAttributes, err := artifactAttributesFromModels(artifacts)
	if err != nil {
		return EntryRevisionAttributes{}, fmt.Errorf("build entry revision artifacts: %w", err)
	}
	sequences, err := s.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{EntryRevisionID: &revision.ID})
	if err != nil {
		return EntryRevisionAttributes{}, fmt.Errorf("list entry revision protein sequences: %w", err)
	}
	fieldSources, err := fieldSourcesFromModel(revision.FieldSources)
	if err != nil {
		return EntryRevisionAttributes{}, fmt.Errorf("build entry revision field sources: %w", err)
	}
	listItem := entryRevisionSummaryAttributesFromModel(revision)
	return EntryRevisionAttributes{
		Id:                listItem.Id,
		EntryId:           listItem.EntryId,
		ParentRevisionId:  listItem.ParentRevisionId,
		RevisionNumber:    listItem.RevisionNumber,
		State:             listItem.State,
		EntryState:        listItem.EntryState,
		CreatedBy:         listItem.CreatedBy,
		Title:             listItem.Title,
		PublishedAt:       listItem.PublishedAt,
		CreatedAt:         listItem.CreatedAt,
		UpdatedAt:         listItem.UpdatedAt,
		ThumbnailImageUrl: revision.ThumbnailImageURL,
		ExternalRefs:      properties.ExternalRefs,
		Details:           properties.Details,
		Resolution:        properties.Resolution,
		Method:            properties.Method,
		SpaceGroup:        properties.SpaceGroup,
		Crystallography:   properties.Crystallography,
		Ligands:           properties.Ligands,
		Cofactors:         properties.Cofactors,
		ProteinSequences:  proteinSequencesFromModels(sequences),
		Artifacts:         artifactAttributes,
		FieldSources:      fieldSources,
	}, nil
}

func (s *Server) modelRevisionAttributesFromModel(
	ctx context.Context,
	revision domainmodels.ModelRevision,
) (ModelRevisionAttributes, error) {
	metadata, err := modelMetadataForResponse(revision.Metadata)
	if err != nil {
		return ModelRevisionAttributes{}, fmt.Errorf("build model revision metadata: %w", err)
	}
	artifacts, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		return ModelRevisionAttributes{}, fmt.Errorf("list model revision artifacts: %w", err)
	}
	artifactAttributes, err := artifactAttributesFromModels(artifacts)
	if err != nil {
		return ModelRevisionAttributes{}, fmt.Errorf("build model revision artifacts: %w", err)
	}
	metrics, err := s.database.Metrics.List(ctx, db.MetricFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		return ModelRevisionAttributes{}, fmt.Errorf("list model revision metrics: %w", err)
	}
	fieldSources, err := fieldSourcesFromModel(revision.FieldSources)
	if err != nil {
		return ModelRevisionAttributes{}, fmt.Errorf("build model revision field sources: %w", err)
	}
	listItem := modelRevisionSummaryAttributesFromModel(revision)
	return ModelRevisionAttributes{
		Id:                listItem.Id,
		EntryId:           listItem.EntryId,
		ModelId:           listItem.ModelId,
		IdempotencyKey:    listItem.IdempotencyKey,
		ParentRevisionId:  listItem.ParentRevisionId,
		RevisionNumber:    listItem.RevisionNumber,
		State:             listItem.State,
		ModelState:        listItem.ModelState,
		CreatedBy:         listItem.CreatedBy,
		Title:             listItem.Title,
		CreatedAt:         listItem.CreatedAt,
		UpdatedAt:         listItem.UpdatedAt,
		ThumbnailImageUrl: revision.ThumbnailImageURL,
		PrimaryArtifactId: revision.PrimaryArtifactID,
		Metadata:          metadata,
		ResidueData:       residueDataFromModels(revision.Metadata.ResidueData),
		PublishedAt:       listItem.PublishedAt,
		Metrics:           metricsFromModels(metrics),
		Artifacts:         artifactAttributes,
		FieldSources:      fieldSources,
	}, nil
}

// fieldSourcesFromModel converts revision field sources for a response. Sources that were never
// recorded stay null.
func fieldSourcesFromModel(sources domainmodels.FieldSources) (*FieldSources, error) {
	if sources == nil {
		return nil, nil
	}
	result := make(FieldSources, len(sources))
	for pointer, source := range sources {
		kind, err := fieldSourceKindFromModel(source.Source)
		if err != nil {
			return nil, fmt.Errorf("field source of %s: %w", pointer, err)
		}
		result[string(pointer)] = FieldSource{
			Source:      kind,
			Accession:   source.Accession,
			Release:     source.Release,
			RetrievedAt: source.RetrievedAt,
		}
	}
	return &result, nil
}

func fieldSourceKindFromModel(kind domainmodels.FieldSourceKind) (FieldSourceSource, error) {
	switch kind {
	case domainmodels.FieldSourceRCSB:
		return FieldSourceSourceRcsb, nil
	case domainmodels.FieldSourceDepositor:
		return FieldSourceSourceDepositor, nil
	default:
		return "", fmt.Errorf("unknown field source: %s", kind)
	}
}

func entryRevisionSummaryAttributesFromModel(revision domainmodels.EntryRevision) EntryRevisionSummaryAttributes {
	return EntryRevisionSummaryAttributes{
		Id:               revision.ID,
		EntryId:          revision.EntryID,
		ParentRevisionId: revision.ParentRevisionID,
		RevisionNumber:   revision.RevisionNumber,
		State:            RevisionState(revision.State),
		EntryState:       EntryState(revision.EntryState),
		CreatedBy:        revision.CreatedBy,
		Title:            revision.Title,
		PublishedAt:      revision.PublishedAt,
		CreatedAt:        revision.CreatedAt,
		UpdatedAt:        revision.UpdatedAt,
	}
}

func modelRevisionSummaryAttributesFromModel(revision domainmodels.ModelRevision) ModelRevisionSummaryAttributes {
	return ModelRevisionSummaryAttributes{
		Id:               revision.ID,
		EntryId:          revision.EntryID,
		ModelId:          revision.ModelID,
		IdempotencyKey:   revision.IdempotencyKey,
		ParentRevisionId: revision.ParentRevisionID,
		RevisionNumber:   revision.RevisionNumber,
		State:            RevisionState(revision.State),
		ModelState:       ModelState(revision.ModelState),
		CreatedBy:        revision.CreatedBy,
		Title:            revision.Title,
		PublishedAt:      revision.PublishedAt,
		CreatedAt:        revision.CreatedAt,
		UpdatedAt:        revision.UpdatedAt,
	}
}
