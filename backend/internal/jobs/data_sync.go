package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
)

const (
	dataSyncLockName        = "data-sync"
	dataSyncInterval        = time.Hour
	dataSyncBatchTimeout    = 5 * time.Minute
	dataSyncMinimumInterval = 7 * 24 * time.Hour
	dataSyncScheduleJitter  = 7 * 24 * time.Hour
)

type DataSyncJob struct {
	database        *db.DB
	rcsbClient      *rcsb.RemoteClient
	logger          *slog.Logger
	now             func() time.Time
	nextScheduledAt func(time.Time) time.Time
}

func NewDataSyncJob(
	database *db.DB,
	rcsbClient *rcsb.RemoteClient,
	logger *slog.Logger,
) (*DataSyncJob, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	if rcsbClient == nil {
		return nil, errors.New("RCSB client is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &DataSyncJob{
		database:        database,
		rcsbClient:      rcsbClient,
		logger:          logger,
		now:             func() time.Time { return time.Now().UTC() },
		nextScheduledAt: nextDataSyncScheduledAt,
	}, nil
}

func (j *DataSyncJob) Run(ctx context.Context) {
	for ctx.Err() == nil {
		ran, err := j.database.RunLocked(ctx, dataSyncLockName, j.runBatch)
		if err != nil && ctx.Err() == nil {
			j.logger.Error("data sync failed", "err", err)
		} else if !ran && ctx.Err() == nil {
			j.logger.Info("data sync iteration skipped", "reason", "lock is already held")
		}
		if !waitForDataSync(ctx, dataSyncInterval) {
			return
		}
	}
}

func (j *DataSyncJob) runBatch(ctx context.Context) error {
	batchCtx, cancel := context.WithTimeout(ctx, dataSyncBatchTimeout)
	defer cancel()
	startedAt := time.Now()
	processedJobs := 0
	j.logger.Info("data sync iteration started")
	defer func() {
		j.logger.Info(
			"data sync iteration finished",
			"processed_jobs", processedJobs,
			"duration", time.Since(startedAt),
		)
	}()

	for batchCtx.Err() == nil {
		executed, err := j.executeNext(batchCtx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && batchCtx.Err() != nil {
				return nil
			}
			return err
		}
		if !executed {
			return nil
		}
		processedJobs++
	}
	return nil
}

func (j *DataSyncJob) executeNext(ctx context.Context) (bool, error) {
	job, err := j.database.DataSyncJobs.GetNextScheduled(ctx, j.now())
	if err != nil {
		return false, fmt.Errorf("get next scheduled data sync job: %w", err)
	}
	if job == nil {
		return false, nil
	}

	if err := j.execute(ctx, *job); err != nil {
		return false, fmt.Errorf("execute data sync job: %w", err)
	}
	job.ScheduledAt = j.nextScheduledAt(j.now())
	if err := j.database.DataSyncJobs.Schedule(ctx, *job); err != nil {
		return false, fmt.Errorf("reschedule data sync job: %w", err)
	}
	return true, nil
}

func (j *DataSyncJob) execute(ctx context.Context, job models.DataSyncJob) error {
	if job.ModelID != nil {
		return nil
	}
	if err := j.syncEntry(ctx, job.EntryID); err != nil {
		return fmt.Errorf("sync entry %s: %w", job.EntryID, err)
	}
	return nil
}

func (j *DataSyncJob) syncEntry(ctx context.Context, entryID string) error {
	revision, err := j.prepareEntryRevisionUpdate(ctx, entryID)
	if err != nil {
		return fmt.Errorf("prepare entry revision update: %w", err)
	}
	if revision == nil {
		return nil
	}

	now := j.now()
	parentRevisionID := revision.ID
	revision.ParentRevisionID = new(parentRevisionID)
	revision.ID = uuid.New()
	revision.RevisionNumber = nil
	revision.State = models.RevisionStateInReview
	revision.ChangeSummary = nil
	revision.PublishedAt = nil
	revision.CreatedAt = now
	revision.UpdatedAt = now

	if err := j.database.Do(ctx, func(ctx context.Context) error {
		createdRevision, err := j.database.Entries.Create(ctx, *revision)
		if err != nil {
			return fmt.Errorf("create entry revision: %w", err)
		}
		if err := j.moveEntryRevisionData(ctx, parentRevisionID, createdRevision.ID); err != nil {
			return fmt.Errorf("move entry revision data: %w", err)
		}
		activatedRevision, err := j.database.Entries.ActivateRevision(ctx, entryID, createdRevision.ID)
		if err != nil {
			return fmt.Errorf("activate entry revision: %w", err)
		}
		if err := j.reindexEntry(ctx, *activatedRevision); err != nil {
			return fmt.Errorf("reindex entry: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("save entry revision update: %w", err)
	}
	j.logger.Info(
		"data sync entry revision activated",
		"entry_id", entryID,
		"revision_id", revision.ID,
	)
	return nil
}

func (j *DataSyncJob) moveEntryRevisionData(
	ctx context.Context,
	fromRevisionID uuid.UUID,
	toRevisionID uuid.UUID,
) error {
	if err := j.database.Artifacts.CopyEntryRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
		return fmt.Errorf("copy artifact links: %w", err)
	}

	if err := j.database.ProteinSequences.MoveEntryRevision(ctx, fromRevisionID, toRevisionID); err != nil {
		return fmt.Errorf("move protein sequences: %w", err)
	}
	return nil
}

func (j *DataSyncJob) reindexEntry(ctx context.Context, revision models.EntryRevision) error {
	if err := j.database.EntrySearch.DeleteEntry(ctx, revision.EntryID); err != nil {
		return fmt.Errorf("clear entry search index: %w", err)
	}
	if err := j.database.EntrySearch.IndexEntryRevision(ctx, revision); err != nil {
		return fmt.Errorf("index entry revision: %w", err)
	}

	activeModels, err := j.database.Models.List(ctx, db.ModelRevisionFilters{
		EntryID:    new(revision.EntryID),
		State:      new(models.RevisionStateActive),
		ModelState: new(models.ModelStateActive),
	})
	if err != nil {
		return fmt.Errorf("list active models: %w", err)
	}
	for _, model := range activeModels {
		if err := j.database.EntrySearch.IndexModelRevision(ctx, model); err != nil {
			return fmt.Errorf("index active model revision %s: %w", model.ID, err)
		}
	}
	return nil
}

func (j *DataSyncJob) prepareEntryRevisionUpdate(
	ctx context.Context,
	entryID string,
) (*models.EntryRevision, error) {
	activeRevision, err := j.database.Entries.Get(ctx, db.EntryRevisionFilters{
		EntryID:    &entryID,
		State:      new(models.RevisionStateActive),
		EntryState: new(models.EntryStateActive),
	})
	if err != nil {
		return nil, fmt.Errorf("get active entry revision %s: %w", entryID, err)
	}

	pdbID := strings.TrimSpace(activeRevision.Metadata.ExternalRefs[models.EntrySourcePDB])
	if pdbID == "" {
		return nil, nil
	}

	details, err := j.rcsbClient.GetEntryDetails(ctx, pdbID)
	if err != nil {
		return nil, fmt.Errorf("get RCSB details for entry %s: %w", entryID, err)
	}
	organism, err := j.getEntryOrganism(ctx, pdbID, details.Identifiers.PolymerEntityIDs)
	if err != nil {
		return nil, fmt.Errorf("get RCSB organism for entry %s: %w", entryID, err)
	}

	updatedRevision := *activeRevision
	applyRCSBEntryDetails(&updatedRevision, details, organism)
	if activeRevision.HasSameData(updatedRevision) {
		return nil, nil
	}
	return &updatedRevision, nil
}

func (j *DataSyncJob) getEntryOrganism(
	ctx context.Context,
	pdbID string,
	polymerEntityIDs []string,
) (*string, error) {
	organisms := make([]string, 0)
	seen := make(map[string]struct{})
	for _, entityID := range polymerEntityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		entity, err := j.rcsbClient.GetPolymerEntityDetails(ctx, pdbID, entityID)
		if errors.Is(err, rcsb.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get polymer entity %s_%s: %w", pdbID, entityID, err)
		}
		for _, sourceOrganism := range entity.SourceOrganisms {
			name := strings.TrimSpace(sourceOrganism.ScientificName)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			organisms = append(organisms, name)
		}
	}
	return optionalString(strings.Join(organisms, "; ")), nil
}

func applyRCSBEntryDetails(
	revision *models.EntryRevision,
	details rcsb.EntryDetails,
	organism *string,
) {
	revision.Description = optionalString(details.Structure.Title)
	revision.Metadata.Resolution = firstResolution(details.Info.CombinedResolution)
	revision.Metadata.Organism = organism
	revision.Metadata.Method = structureMethod(details.Experiments)
	revision.Metadata.SpaceGroup = optionalString(details.Symmetry.SpaceGroup)
}

func firstResolution(resolutions []float64) *float64 {
	if len(resolutions) == 0 {
		return nil
	}
	resolution := resolutions[0]
	return &resolution
}

func structureMethod(experiments []rcsb.EntryExperiment) *models.StructureMethod {
	if len(experiments) == 0 {
		return nil
	}
	rawMethod := strings.TrimSpace(experiments[0].Method)
	if rawMethod == "" {
		return nil
	}
	normalizedMethod := strings.ToLower(rawMethod)
	method := models.StructureMethod(rawMethod)
	switch {
	case strings.Contains(normalizedMethod, "x-ray"), strings.Contains(normalizedMethod, "xray"):
		method = models.StructureMethodXRayCrystallography
	case strings.Contains(normalizedMethod, "electron microscopy"),
		strings.Contains(normalizedMethod, "cryo-em"),
		strings.Contains(normalizedMethod, "cryoem"):
		method = models.StructureMethodCryoEM
	}
	return &method
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func nextDataSyncScheduledAt(now time.Time) time.Time {
	// Scheduling jitter does not require cryptographic randomness.
	//nolint:gosec
	jitter := time.Duration(rand.Int64N(int64(dataSyncScheduleJitter)))
	return now.Add(dataSyncMinimumInterval + jitter)
}

func waitForDataSync(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
