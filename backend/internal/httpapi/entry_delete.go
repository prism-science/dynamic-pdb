package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

// entryDeletedSummary is the change summary of the revision that deletes an entry.
const entryDeletedSummary = "Entry deleted"

// entryDeletedSyncStopReason is the stop reason of the data sync jobs of a deleted entry.
const entryDeletedSyncStopReason = "entry deleted"

// WithEntryDeletion turns DELETE /v1/entries/{entry_id} on or off (default off). It is on only
// on dev, where researchers delete their test entries to retest the deposition forms.
func (s *Server) WithEntryDeletion(enabled bool) *Server {
	s.entryDeletionEnabled = enabled
	return s
}

// DeleteEntry soft-deletes an entry. It adds a revision that marks the entry deleted and
// approves it at once, through the same steps as a reviewer's approval, so the entry leaves
// every read endpoint and the search index. Its open revisions are rejected and its data sync
// jobs stopped. Callers must be able to approve revisions.
func (s *Server) DeleteEntry(w http.ResponseWriter, r *http.Request, entryID string) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}
	if !s.entryDeletionEnabled {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "entry deletion is turned off on this server")
		return
	}
	if !s.requirePermission(w, r, user.ID, domainmodels.PermissionKeyRevisionsApprove) {
		return
	}

	var deletion entryDeletion
	err := s.database.Do(r.Context(), func(ctx context.Context) error {
		var err error
		deletion, err = s.deleteEntry(ctx, entryID, user.ID)
		return err
	})
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	}
	if s.writeMutationError(r.Context(), w, err, "delete entry") {
		return
	}
	slog.InfoContext(r.Context(), "entry deleted",
		"entry_id", entryID,
		"revision_id", deletion.revisionID,
		"user_id", user.ID,
		"rejected_entry_revisions", deletion.rejectedEntryRevisions,
		"rejected_model_revisions", deletion.rejectedModelRevisions,
		"stopped_sync_jobs", deletion.stoppedSyncJobs,
	)
	w.WriteHeader(http.StatusNoContent)
}

// entryDeletion reports what deleteEntry changed, for the log line.
type entryDeletion struct {
	revisionID             uuid.UUID
	rejectedEntryRevisions int64
	rejectedModelRevisions int64
	stoppedSyncJobs        int64
}

// deleteEntry runs inside the caller's transaction. It returns db.ErrEntryRevisionNotFound when
// the entry has no active revision, including when it is already deleted.
func (s *Server) deleteEntry(ctx context.Context, entryID string, deletedBy uuid.UUID) (entryDeletion, error) {
	activeState := domainmodels.RevisionStateActive
	activeEntryState := domainmodels.EntryStateActive
	if err := s.lockEntry(ctx, entryID); err != nil {
		return entryDeletion{}, fmt.Errorf("lock before reading the active revision: %w", err)
	}
	base, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID:    &entryID,
		State:      &activeState,
		EntryState: &activeEntryState,
	})
	if err != nil {
		return entryDeletion{}, fmt.Errorf("get active entry revision: %w", err)
	}

	// Open revisions could never be approved once the entry is deleted, so they would stay in
	// the review queue for good.
	rejectedEntryRevisions, err := s.database.Entries.RejectOpenRevisions(ctx, entryID)
	if err != nil {
		return entryDeletion{}, fmt.Errorf("reject open entry revisions: %w", err)
	}
	rejectedModelRevisions, err := s.database.Models.RejectOpenRevisionsForEntry(ctx, entryID)
	if err != nil {
		return entryDeletion{}, fmt.Errorf("reject open model revisions: %w", err)
	}

	// The sync jobs read only active entries, so a deleted entry's jobs would fail and retry
	// every hour for good.
	now := time.Now().UTC()
	stoppedSyncJobs, err := s.database.DataSyncJobs.StopForEntry(ctx, entryID, entryDeletedSyncStopReason, now)
	if err != nil {
		return entryDeletion{}, fmt.Errorf("stop entry data sync jobs: %w", err)
	}

	created, err := s.database.Entries.Create(ctx, deletionRevision(*base, deletedBy, now))
	if err != nil {
		return entryDeletion{}, fmt.Errorf("create entry deletion revision: %w", err)
	}
	if err := s.database.Artifacts.CopyEntryRevisionLinks(ctx, base.ID, created.ID); err != nil {
		return entryDeletion{}, fmt.Errorf("copy entry revision artifacts: %w", err)
	}
	if err := s.activateEntryRevision(ctx, *created); err != nil {
		return entryDeletion{}, fmt.Errorf("activate entry deletion revision: %w", err)
	}
	return entryDeletion{
		revisionID:             created.ID,
		rejectedEntryRevisions: rejectedEntryRevisions,
		rejectedModelRevisions: rejectedModelRevisions,
		stoppedSyncJobs:        stoppedSyncJobs,
	}, nil
}

// deletionRevision returns the in-review revision that deletes the entry whose active revision
// is base. It keeps base's values except the PDB ID, which it drops so that a new entry can use
// it: the unique index on active PDB IDs ignores entry_state. base keeps the PDB ID for history.
func deletionRevision(base domainmodels.EntryRevision, deletedBy uuid.UUID, now time.Time) domainmodels.EntryRevision {
	metadata := base.Metadata
	metadata.ExternalRefs = maps.Clone(base.Metadata.ExternalRefs)
	delete(metadata.ExternalRefs, domainmodels.EntrySourcePDB)
	return domainmodels.EntryRevision{
		ID:                uuid.New(),
		EntryID:           base.EntryID,
		ParentRevisionID:  &base.ID,
		State:             domainmodels.RevisionStateInReview,
		EntryState:        domainmodels.EntryStateDeleted,
		ChangeSummary:     new(entryDeletedSummary),
		Title:             base.Title,
		ThumbnailImageURL: base.ThumbnailImageURL,
		Metadata:          metadata,
		// The deletion is not a depositor's edit, so the sources stay as they were.
		FieldSources:      base.FieldSources,
		CompoundSources:   base.CompoundSources,
		GlycanDescriptors: base.GlycanDescriptors,
		CreatedBy:         deletedBy,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}
