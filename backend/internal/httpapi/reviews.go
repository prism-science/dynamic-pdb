package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

// ListReviews returns the review queue: one row per entry that has an entry
// revision in_review or at least one model revision in_review. The configured
// reviewer sees everything; any other caller sees only their own submissions.
func (s *Server) ListReviews(w http.ResponseWriter, r *http.Request, params ListReviewsParams) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	filters := db.QueueFilters{
		Limit:  params.Limit,
		Offset: params.Offset,
	}
	if !s.isReviewer(user) {
		filters.SubmittedBy = &user.ID
	}

	items, err := s.database.Reviews.ListQueue(r.Context(), filters)
	if err != nil {
		slog.Error("list review queue failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list reviews")
		return
	}

	responses := make([]ReviewQueueItem, 0, len(items))
	for _, item := range items {
		submitters := make([]openapi_types.UUID, 0, len(item.SubmittedBy))
		submitters = append(submitters, item.SubmittedBy...)
		responses = append(responses, ReviewQueueItem{
			EntryId:     item.EntryID,
			Name:        item.Name,
			SubmittedAt: item.SubmittedAt,
			SubmittedBy: submitters,
		})
	}
	writeJSON(w, http.StatusOK, ReviewQueueResponse{Items: responses})
}

// GetEntryReview returns the submission waiting on one entry: the entry and
// each of its models under review, each as an active/proposed pair.
func (s *Server) GetEntryReview(w http.ResponseWriter, r *http.Request, entryID openapi_types.UUID) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	review, submitters, err := s.buildEntryReview(r.Context(), entryID)
	if errors.Is(err, db.ErrReviewNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no submission is waiting on this entry")
		return
	}
	if err != nil {
		slog.Error("build entry review failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get review")
		return
	}
	if !s.isReviewer(user) && !containsUser(submitters, user.ID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "not allowed to view this review")
		return
	}
	writeJSON(w, http.StatusOK, *review)
}

// DecideEntryReview applies one decision to a whole submission. The entry
// revision, when it is in review, and every model revision in review under it
// are approved or rejected together.
func (s *Server) DecideEntryReview(w http.ResponseWriter, r *http.Request, entryID openapi_types.UUID) {
	var req ReviewDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	if !s.isReviewer(user) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the reviewer can decide reviews")
		return
	}

	status := strings.TrimSpace(string(req.Status))
	if status != "approved" && status != "rejected" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "status must be approved or rejected")
		return
	}
	comment := trimmedStringPtr(req.Comment)
	if status == "rejected" && comment == nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "a comment is required when rejecting")
		return
	}

	err := s.database.Do(r.Context(), func(ctx context.Context) error {
		return s.decideSubmission(ctx, entryID, user.ID, status, comment)
	})
	if errors.Is(err, db.ErrReviewNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no submission is waiting on this entry")
		return
	}
	if s.writeReviewDecisionError(w, err) {
		return
	}

	review, _, err := s.buildEntryReview(r.Context(), entryID)
	if errors.Is(err, db.ErrReviewNotFound) {
		// Everything under the entry was published, so nothing is waiting any
		// more. Report the entry with no proposal rather than a 404.
		review, err = s.buildDecidedEntryReview(r.Context(), entryID)
	}
	if err != nil {
		slog.Error("reload review after decision failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load review")
		return
	}
	writeJSON(w, http.StatusOK, *review)
}

func (s *Server) decideSubmission(
	ctx context.Context,
	entryID, reviewerID uuid.UUID,
	status string,
	comment *string,
) error {
	proposedEntry, err := s.entryRevisionInState(ctx, entryID, domainmodels.RevisionStateInReview)
	if err != nil {
		return err
	}
	proposedModels, err := s.modelRevisionsInReview(ctx, entryID)
	if err != nil {
		return err
	}
	if proposedEntry == nil && len(proposedModels) == 0 {
		return db.ErrReviewNotFound
	}

	if status == "rejected" {
		if proposedEntry != nil {
			if _, err := s.database.Entries.Reject(ctx, entryID, reviewerID, *comment); err != nil {
				return fmt.Errorf("reject entry revision: %w", err)
			}
		}
		for _, revision := range proposedModels {
			if _, err := s.database.Models.Reject(ctx, revision.ModelID, reviewerID, *comment); err != nil {
				return fmt.Errorf("reject model revision: %w", err)
			}
		}
		return nil
	}

	if proposedEntry != nil {
		if _, err := s.database.Entries.Approve(ctx, entryID, reviewerID, comment); err != nil {
			return fmt.Errorf("approve entry revision: %w", err)
		}
		// Publishing the entry publishes everything staged under it, including
		// models that were still pending because they came in with the entry.
		if err := s.database.Models.PublishForEntry(ctx, entryID, reviewerID); err != nil {
			return fmt.Errorf("publish models for entry: %w", err)
		}
		return nil
	}

	for _, revision := range proposedModels {
		if _, err := s.database.Models.Approve(ctx, revision.ModelID, reviewerID, comment); err != nil {
			return fmt.Errorf("approve model revision: %w", err)
		}
	}
	return nil
}

// buildEntryReview assembles the active/proposed pairs. It returns
// db.ErrReviewNotFound when nothing under the entry is in review.
func (s *Server) buildEntryReview(
	ctx context.Context,
	entryID uuid.UUID,
) (*EntryReview, []uuid.UUID, error) {
	activeEntry, err := s.entryRevisionInState(ctx, entryID, domainmodels.RevisionStateActive)
	if err != nil {
		return nil, nil, err
	}
	proposedEntry, err := s.entryRevisionInState(ctx, entryID, domainmodels.RevisionStateInReview)
	if err != nil {
		return nil, nil, err
	}
	proposedModels, err := s.modelRevisionsInReview(ctx, entryID)
	if err != nil {
		return nil, nil, err
	}
	if proposedEntry == nil && len(proposedModels) == 0 {
		return nil, nil, db.ErrReviewNotFound
	}

	submitters := make([]uuid.UUID, 0, len(proposedModels)+1)
	pair := EntryReviewPair{}
	if activeEntry != nil {
		side, err := s.reviewEntrySide(ctx, *activeEntry)
		if err != nil {
			return nil, nil, err
		}
		pair.Active = side
	}
	if proposedEntry != nil {
		side, err := s.reviewEntrySide(ctx, *proposedEntry)
		if err != nil {
			return nil, nil, err
		}
		pair.Proposed = side
		submitters = append(submitters, proposedEntry.CreatedBy)
	}

	modelPairs := make([]ModelReviewPair, 0, len(proposedModels))
	for _, revision := range proposedModels {
		proposedSide, err := s.reviewModelSide(ctx, revision)
		if err != nil {
			return nil, nil, err
		}
		modelPair := ModelReviewPair{ModelId: revision.ModelID, Proposed: *proposedSide}

		activeModel, err := s.modelRevisionInState(ctx, revision.ModelID, domainmodels.RevisionStateActive)
		if err != nil {
			return nil, nil, err
		}
		if activeModel != nil {
			activeSide, err := s.reviewModelSide(ctx, *activeModel)
			if err != nil {
				return nil, nil, err
			}
			modelPair.Active = activeSide
		}

		modelPairs = append(modelPairs, modelPair)
		submitters = append(submitters, revision.CreatedBy)
	}

	return &EntryReview{EntryId: entryID, Entry: pair, Models: modelPairs}, submitters, nil
}

// buildDecidedEntryReview is the shape returned right after a decision, when
// nothing is in review any more: the entry as published, no proposal.
func (s *Server) buildDecidedEntryReview(
	ctx context.Context,
	entryID uuid.UUID,
) (*EntryReview, error) {
	activeEntry, err := s.entryRevisionInState(ctx, entryID, domainmodels.RevisionStateActive)
	if err != nil {
		return nil, err
	}
	pair := EntryReviewPair{}
	if activeEntry != nil {
		side, err := s.reviewEntrySide(ctx, *activeEntry)
		if err != nil {
			return nil, err
		}
		pair.Active = side
	}
	return &EntryReview{EntryId: entryID, Entry: pair, Models: []ModelReviewPair{}}, nil
}

func (s *Server) reviewEntrySide(
	ctx context.Context,
	revision domainmodels.EntryRevision,
) (*ReviewEntry, error) {
	proteinSequences, err := s.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &revision.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("list entry revision protein sequences: %w", err)
	}
	artifacts, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{EntryRevisionID: &revision.ID})
	if err != nil {
		return nil, fmt.Errorf("list entry revision artifacts: %w", err)
	}
	artifactResponses, err := artifactResponsesFromModels(artifacts)
	if err != nil {
		return nil, err
	}
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return nil, fmt.Errorf("build entry metadata response: %w", err)
	}

	return &ReviewEntry{
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
		Artifacts:         artifactResponses,
	}, nil
}

func (s *Server) reviewModelSide(
	ctx context.Context,
	revision domainmodels.ModelRevision,
) (*ReviewModel, error) {
	metrics, err := s.database.Metrics.List(ctx, db.MetricFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		return nil, fmt.Errorf("list model revision metrics: %w", err)
	}
	artifacts, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		return nil, fmt.Errorf("list model revision artifacts: %w", err)
	}
	artifactResponses, err := artifactResponsesFromModels(artifacts)
	if err != nil {
		return nil, err
	}
	metadata, err := metadataResponseFromValue(revision.Metadata)
	if err != nil {
		return nil, fmt.Errorf("build model metadata response: %w", err)
	}

	return &ReviewModel{
		Id:                revision.ModelID,
		EntryId:           revision.EntryID,
		CreatedBy:         revision.CreatedBy,
		Name:              revision.Name,
		Description:       revision.Description,
		ThumbnailImageUrl: revision.ThumbnailImageURL,
		PrimaryArtifactId: revision.PrimaryArtifactID,
		Metadata:          metadata,
		PublishedAt:       revision.PublishedAt,
		CreatedAt:         revision.CreatedAt,
		UpdatedAt:         revision.UpdatedAt,
		Metrics:           metricResponsesFromModels(metrics),
		Artifacts:         artifactResponses,
	}, nil
}

// entryRevisionInState returns the entry's revision in the given state, or nil
// when there is none.
func (s *Server) entryRevisionInState(
	ctx context.Context,
	entryID uuid.UUID,
	state domainmodels.RevisionState,
) (*domainmodels.EntryRevision, error) {
	revisions, err := s.database.Entries.List(ctx, db.EntryRevisionFilters{
		EntryID: &entryID,
		State:   &state,
	})
	if err != nil {
		return nil, fmt.Errorf("list entry revisions in state %s: %w", state, err)
	}
	if len(revisions) == 0 {
		return nil, nil
	}
	return &revisions[0], nil
}

func (s *Server) modelRevisionInState(
	ctx context.Context,
	modelID uuid.UUID,
	state domainmodels.RevisionState,
) (*domainmodels.ModelRevision, error) {
	revisions, err := s.database.Models.List(ctx, db.ModelRevisionFilters{
		ModelID: &modelID,
		State:   &state,
	})
	if err != nil {
		return nil, fmt.Errorf("list model revisions in state %s: %w", state, err)
	}
	if len(revisions) == 0 {
		return nil, nil
	}
	return &revisions[0], nil
}

func (s *Server) modelRevisionsInReview(
	ctx context.Context,
	entryID uuid.UUID,
) ([]domainmodels.ModelRevision, error) {
	state := domainmodels.RevisionStateInReview
	revisions, err := s.database.Models.List(ctx, db.ModelRevisionFilters{
		EntryID: &entryID,
		State:   &state,
	})
	if err != nil {
		return nil, fmt.Errorf("list model revisions in review: %w", err)
	}
	return revisions, nil
}

func (s *Server) writeReviewDecisionError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, db.ErrReviewNotFound),
		errors.Is(err, db.ErrEntryRevisionNotFound),
		errors.Is(err, db.ErrModelRevisionNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no submission is waiting on this entry")
	case errors.Is(err, db.ErrEntryRevisionConflict), errors.Is(err, db.ErrModelRevisionConflict):
		writeError(w, http.StatusConflict, "CONFLICT", "the submission changed since it was loaded")
	default:
		slog.Error("decide review failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to decide review")
	}
	return true
}

func containsUser(users []uuid.UUID, target uuid.UUID) bool {
	for _, user := range users {
		if user == target {
			return true
		}
	}
	return false
}
