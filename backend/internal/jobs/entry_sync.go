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
	validationMode validation.Mode
	now            func() time.Time
}

func newEntrySyncStrategy(
	database *db.DB,
	rcsbClient *rcsb.RemoteClient,
	siftsClient *sifts.RemoteClient,
	logger *slog.Logger,
	validationMode validation.Mode,
	now func() time.Time,
) *entrySyncStrategy {
	return &entrySyncStrategy{
		database:       database,
		rcsbClient:     rcsbClient,
		siftsClient:    siftsClient,
		logger:         logger,
		validationMode: validationMode,
		now:            now,
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

	entryRevisionChanged, polymerEntitiesChanged := compareEntryUpdate(
		*currentRevision,
		desiredRevision,
		currentPolymerEntities,
		desiredPolymerEntities,
	)
	if !entryRevisionChanged && !polymerEntitiesChanged {
		return nil
	}
	if err := s.validationMode.Apply(
		ctx,
		s.logger,
		validateEntryUpdate(desiredRevision, desiredPolymerEntities),
		"entry_id", entryID,
		"pdb_id", pdbID,
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

// validateEntryUpdate checks the RCSB values the sync writes; the sync always overwrites every one of them.
func validateEntryUpdate(
	desiredRevision models.EntryRevision,
	desiredPolymerEntities []models.PolymerEntity,
) []validation.FieldError {
	var method *string
	if desiredRevision.Metadata.Method != nil {
		method = new(string(*desiredRevision.Metadata.Method))
	}
	fieldErrors := validation.ValidateEntryRevisionChange(validation.EntryChange{
		Title:      validation.Present(desiredRevision.Title),
		Method:     validation.Present(method),
		Resolution: validation.Present(desiredRevision.Metadata.Resolution),
	})
	for entityIndex, entity := range desiredPolymerEntities {
		for mappingIndex, mapping := range entity.Metadata.UniProtMappings {
			fieldErrors = append(fieldErrors, validation.ValidateUniProtMapping(
				fmt.Sprintf("/polymer_entities/%d/uniprot_mappings/%d", entityIndex, mappingIndex),
				mapping,
			)...)
		}
	}
	return fieldErrors
}
