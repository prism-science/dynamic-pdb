package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

type entrySyncStrategy struct {
	database       *db.DB
	rcsbClient     *rcsb.RemoteClient
	siftsClient    *sifts.RemoteClient
	logger         *slog.Logger
	now            func() time.Time
	validationMode validation.Mode
}

func newEntrySyncStrategy(
	database *db.DB,
	rcsbClient *rcsb.RemoteClient,
	siftsClient *sifts.RemoteClient,
	logger *slog.Logger,
	now func() time.Time,
	validationMode validation.Mode,
) *entrySyncStrategy {
	return &entrySyncStrategy{
		database:       database,
		rcsbClient:     rcsbClient,
		siftsClient:    siftsClient,
		logger:         logger,
		now:            now,
		validationMode: validationMode,
	}
}

func (*entrySyncStrategy) appliesTo(job models.DataSyncJob) bool {
	return job.ModelID == nil
}

func (s *entrySyncStrategy) sync(ctx context.Context, job models.DataSyncJob) (dataSyncOutcome, error) {
	outcome, err := s.syncEntry(ctx, job)
	if err != nil {
		return dataSyncOutcome{}, fmt.Errorf("sync entry %s: %w", job.EntryID, err)
	}
	return outcome, nil
}

// syncEntry applies the archive's latest version of the entry when it is newer than the version
// the job last applied. The job's first run only records that version (decision D2).
func (s *entrySyncStrategy) syncEntry(ctx context.Context, job models.DataSyncJob) (dataSyncOutcome, error) {
	currentRevision, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID:    &job.EntryID,
		State:      new(models.RevisionStateActive),
		EntryState: new(models.EntryStateActive),
	})
	if err != nil {
		return dataSyncOutcome{}, fmt.Errorf("get current entry revision: %w", err)
	}
	pdbID := strings.TrimSpace(currentRevision.Metadata.ExternalRefs[models.EntrySourcePDB])
	if pdbID == "" {
		return dataSyncOutcome{}, nil
	}

	details, err := s.rcsbClient.GetEntryDetails(ctx, pdbID)
	if errors.Is(err, rcsb.ErrNotFound) {
		return s.handleMissingEntry(ctx, *currentRevision, pdbID)
	}
	if err != nil {
		return dataSyncOutcome{}, fmt.Errorf("get RCSB entry %s: %w", pdbID, err)
	}
	mapping := archive.MapEntry(details)
	latest, err := archiveVersionOf(mapping)
	if err != nil {
		return dataSyncOutcome{}, fmt.Errorf("read version of RCSB entry %s: %w", pdbID, err)
	}
	if job.ArchiveVersion == nil {
		return dataSyncOutcome{archiveVersion: &latest}, nil
	}
	if !isNewerArchiveVersion(job.ArchiveVersion, latest) {
		return dataSyncOutcome{}, nil
	}
	source := archiveRevisionSource{
		pdbID:   pdbID,
		version: latest,
		summary: archiveChangeSummary(*job.ArchiveVersion, latest, mapping.RevisionGroups),
	}
	if err := s.applyArchiveVersion(ctx, *currentRevision, details, source); err != nil {
		return dataSyncOutcome{}, err
	}
	return dataSyncOutcome{archiveVersion: &latest}, nil
}

// applyArchiveVersion activates a sync revision with the archive's values when they differ from
// the active revision's. Values a depositor typed stay as they are.
func (s *entrySyncStrategy) applyArchiveVersion(
	ctx context.Context,
	currentRevision models.EntryRevision,
	details rcsb.EntryDetails,
	source archiveRevisionSource,
) error {
	currentPolymerEntities, err := s.database.PolymerEntities.List(ctx, db.PolymerEntityFilters{
		EntryRevisionID: &currentRevision.ID,
	})
	if err != nil {
		return fmt.Errorf("list current polymer entities: %w", err)
	}
	proteinSequences, err := s.database.ProteinSequences.List(ctx, db.ProteinSequenceFilters{
		EntryRevisionID: &currentRevision.ID,
	})
	if err != nil {
		return fmt.Errorf("list current protein sequences: %w", err)
	}

	desiredRevision, desiredPolymerEntities, err := s.buildEntryUpdate(ctx, source.pdbID, details, proteinSequences)
	if err != nil {
		return fmt.Errorf("build entry update: %w", err)
	}
	desiredPolymerEntities = applyDepositorPolymerEntities(
		desiredPolymerEntities,
		currentRevision.Metadata.DepositorPolymerEntities,
	)
	keepDepositorEntryFields(currentRevision, &desiredRevision)

	entryRevisionChanged, polymerEntitiesChanged := compareEntryUpdate(
		currentRevision,
		desiredRevision,
		currentPolymerEntities,
		desiredPolymerEntities,
	)
	if !entryRevisionChanged && !polymerEntitiesChanged {
		return nil
	}
	if err := s.validationMode.Check(
		ctx, s.logger, "data sync entry "+currentRevision.EntryID, entryUpdateErrors(desiredRevision, desiredPolymerEntities),
	); err != nil {
		return fmt.Errorf("validate entry update: %w", err)
	}

	if err := s.saveEntryUpdate(
		ctx,
		currentRevision,
		desiredRevision,
		desiredPolymerEntities,
		polymerEntitiesChanged,
		source,
	); err != nil {
		return fmt.Errorf("save entry update: %w", err)
	}
	return nil
}

// handleMissingEntry decides what happens when RCSB serves no entry for pdbID. An entry RCSB
// obsoleted is marked obsolete (researcher answer Q4b); then, as for an entry RCSB withdrew or
// has not released, the job stops for good.
func (s *entrySyncStrategy) handleMissingEntry(
	ctx context.Context,
	currentRevision models.EntryRevision,
	pdbID string,
) (dataSyncOutcome, error) {
	status, err := s.rcsbClient.GetHoldingsStatus(ctx, pdbID)
	if errors.Is(err, rcsb.ErrNotFound) {
		return dataSyncOutcome{stopReason: "RCSB does not know PDB ID " + pdbID}, nil
	}
	if err != nil {
		return dataSyncOutcome{}, fmt.Errorf("get RCSB holdings status of %s: %w", pdbID, err)
	}
	switch status.Status {
	case rcsb.HoldingsRemoved:
		return s.handleRemovedEntry(ctx, currentRevision, pdbID, status.StatusCode)
	case rcsb.HoldingsUnreleased:
		return dataSyncOutcome{
			stopReason: fmt.Sprintf("RCSB has not released %s (status code %s)", pdbID, status.StatusCode),
		}, nil
	case rcsb.HoldingsCurrent:
		return dataSyncOutcome{}, fmt.Errorf("RCSB lists %s as current but serves no entry for it", pdbID)
	default:
		return dataSyncOutcome{}, fmt.Errorf("unknown RCSB holdings status of %s: %s", pdbID, status.Status)
	}
}

func (s *entrySyncStrategy) handleRemovedEntry(
	ctx context.Context,
	currentRevision models.EntryRevision,
	pdbID string,
	statusCode string,
) (dataSyncOutcome, error) {
	switch statusCode {
	case rcsb.StatusCodeObsolete:
		removed, err := s.rcsbClient.GetRemovedEntry(ctx, pdbID)
		if err != nil {
			return dataSyncOutcome{}, fmt.Errorf("get RCSB removal of %s: %w", pdbID, err)
		}
		status, err := obsoleteArchiveStatus(removed)
		if err != nil {
			return dataSyncOutcome{}, fmt.Errorf("read RCSB removal of %s: %w", pdbID, err)
		}
		if err := s.saveObsoleteEntry(ctx, currentRevision, status, obsoleteChangeSummary(pdbID, status)); err != nil {
			return dataSyncOutcome{}, fmt.Errorf("mark entry obsolete: %w", err)
		}
		return dataSyncOutcome{stopReason: "RCSB obsoleted " + pdbID}, nil
	case rcsb.StatusCodeWithdrawn:
		return dataSyncOutcome{stopReason: "RCSB withdrew " + pdbID}, nil
	default:
		return dataSyncOutcome{}, fmt.Errorf("unknown status code of removed RCSB entry %s: %s", pdbID, statusCode)
	}
}

// entryUpdateErrors checks the values the sync takes from RCSB with the rules that
// apply to API submissions. Pointers address the entry revision and its polymer entities.
func entryUpdateErrors(revision models.EntryRevision, polymerEntities []models.PolymerEntity) []validation.FieldError {
	fieldErrors := validation.RequiredText(validation.Root.Field("title"), revision.Title)
	fieldErrors = append(fieldErrors, validation.EntryMetadata(validation.Root, revision.Metadata)...)
	for index, polymerEntity := range polymerEntities {
		at := validation.Root.Field("polymer_entities").Index(index)
		fieldErrors = append(fieldErrors, validation.PolymerEntity(at, polymerEntity.Metadata)...)
	}
	return fieldErrors
}
