package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
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

func (s *entrySyncStrategy) sync(ctx context.Context, job models.DataSyncJob) error {
	if err := s.syncEntry(ctx, job.EntryID); err != nil {
		return fmt.Errorf("sync entry %s: %w", job.EntryID, err)
	}
	return nil
}

func (s *entrySyncStrategy) syncEntry(ctx context.Context, entryID string) error {
	currentRevision, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID:    &entryID,
		State:      new(models.RevisionStateActive),
		EntryState: new(models.EntryStateActive),
	})
	if err != nil {
		return fmt.Errorf("get current entry revision: %w", err)
	}
	pdbID := strings.TrimSpace(currentRevision.Metadata.ExternalRefs[models.EntrySourcePDB])
	if pdbID == "" {
		return nil
	}

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

	desiredRevision, desiredPolymerEntities, err := s.buildEntryUpdate(ctx, pdbID, proteinSequences)
	if err != nil {
		return fmt.Errorf("build entry update: %w", err)
	}
	desiredPolymerEntities = applyDepositorPolymerEntities(
		desiredPolymerEntities,
		currentRevision.Metadata.DepositorPolymerEntities,
	)
	keepDepositorEntryFields(*currentRevision, &desiredRevision)

	entryRevisionChanged, polymerEntitiesChanged := compareEntryUpdate(
		*currentRevision,
		desiredRevision,
		currentPolymerEntities,
		desiredPolymerEntities,
	)
	if !entryRevisionChanged && !polymerEntitiesChanged {
		return nil
	}
	if err := s.validationMode.Check(
		ctx, s.logger, "data sync entry "+entryID, entryUpdateErrors(desiredRevision, desiredPolymerEntities),
	); err != nil {
		return fmt.Errorf("validate entry update: %w", err)
	}

	if err := s.saveEntryUpdate(
		ctx,
		*currentRevision,
		desiredRevision,
		desiredPolymerEntities,
		polymerEntitiesChanged,
	); err != nil {
		return fmt.Errorf("save entry update: %w", err)
	}
	return nil
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
