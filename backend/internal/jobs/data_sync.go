package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/archivemodels"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
	"dynamic-pdb/lib/sifts"
)

const (
	dataSyncLockName        = "data-sync"
	dataSyncPollingInterval = 5 * time.Second
	dataSyncRetryInterval   = time.Hour
	dataSyncBatchTimeout    = 5 * time.Minute
	dataSyncMinimumInterval = 7 * 24 * time.Hour
	dataSyncScheduleJitter  = 7 * 24 * time.Hour
)

var dataSyncUserID = uuid.MustParse(models.SystemUserID)

type polymerEntitySnapshot = models.PolymerEntity

type dataSyncStrategy interface {
	appliesTo(models.DataSyncJob) bool
	sync(context.Context, models.DataSyncJob) (dataSyncOutcome, error)
}

type DataSyncJob struct {
	database        *db.DB
	logger          *slog.Logger
	strategies      []dataSyncStrategy
	now             func() time.Time
	nextScheduledAt func(time.Time) time.Time
}

func NewDataSyncJob(
	database *db.DB,
	rcsbClient *rcsb.RemoteClient,
	siftsClient *sifts.RemoteClient,
	logger *slog.Logger,
	validationMode validation.Mode,
) (*DataSyncJob, error) {
	if database == nil {
		return nil, errors.New("database is nil")
	}
	if rcsbClient == nil {
		return nil, errors.New("RCSB client is nil")
	}
	if siftsClient == nil {
		return nil, errors.New("SIFTS client is nil")
	}
	if err := validationMode.Validate(); err != nil {
		return nil, fmt.Errorf("validation mode: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}

	now := func() time.Time { return time.Now().UTC() }
	return &DataSyncJob{
		database: database,
		logger:   logger,
		strategies: []dataSyncStrategy{
			newEntrySyncStrategy(database, rcsbClient, siftsClient, logger, now, validationMode),
			newModelSyncStrategy(database, rcsbClient, logger, now, validationMode),
		},
		now:             now,
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
		if !waitForDataSync(ctx, dataSyncPollingInterval) {
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

	outcome, executionErr := j.execute(ctx, *job)
	if executionErr != nil {
		job.ScheduledAt = j.now().Add(dataSyncRetryInterval)
		if err := j.database.DataSyncJobs.Schedule(ctx, *job); err != nil {
			return false, errors.Join(
				fmt.Errorf("execute data sync job: %w", executionErr),
				fmt.Errorf("reschedule failed data sync job: %w", err),
			)
		}
		j.logger.Error(
			"data sync job failed; retry scheduled",
			"entry_id", job.EntryID,
			"retry_at", job.ScheduledAt,
			"err", executionErr,
		)
		return true, nil
	}
	if outcome.stopReason != "" {
		if err := j.database.DataSyncJobs.Stop(ctx, *job, outcome.stopReason, j.now()); err != nil {
			return false, fmt.Errorf("stop data sync job: %w", err)
		}
		j.logger.Warn(
			"data sync job stopped",
			"entry_id", job.EntryID,
			"model_id", stringValue(job.ModelID),
			"reason", outcome.stopReason,
		)
		return true, nil
	}
	if outcome.archiveVersion != nil {
		if err := j.database.DataSyncJobs.RecordArchiveVersion(ctx, *job, *outcome.archiveVersion); err != nil {
			return false, fmt.Errorf("record data sync job archive version: %w", err)
		}
	}
	job.ScheduledAt = j.nextScheduledAt(j.now())
	if err := j.database.DataSyncJobs.Schedule(ctx, *job); err != nil {
		return false, fmt.Errorf("reschedule data sync job: %w", err)
	}
	return true, nil
}

func (j *DataSyncJob) execute(ctx context.Context, job models.DataSyncJob) (dataSyncOutcome, error) {
	for _, strategy := range j.strategies {
		if strategy.appliesTo(job) {
			return strategy.sync(ctx, job)
		}
	}
	return dataSyncOutcome{}, nil
}

func compareEntryUpdate(
	currentRevision models.EntryRevision,
	desiredRevision models.EntryRevision,
	currentPolymerEntities []models.PolymerEntity,
	desiredPolymerEntities []models.PolymerEntity,
) (bool, bool) {
	entryRevisionChanged := !reflect.DeepEqual(currentRevision.Title, desiredRevision.Title) ||
		!reflect.DeepEqual(currentRevision.Metadata.Details, desiredRevision.Metadata.Details) ||
		!reflect.DeepEqual(currentRevision.Metadata.Resolution, desiredRevision.Metadata.Resolution) ||
		!reflect.DeepEqual(currentRevision.Metadata.Method, desiredRevision.Metadata.Method) ||
		!reflect.DeepEqual(currentRevision.Metadata.SpaceGroup, desiredRevision.Metadata.SpaceGroup) ||
		!reflect.DeepEqual(currentRevision.Metadata.Crystallography, desiredRevision.Metadata.Crystallography)
	polymerEntitiesChanged := !polymerEntitiesHaveSameData(
		currentPolymerEntities,
		desiredPolymerEntities,
	)
	return entryRevisionChanged, polymerEntitiesChanged
}

// archiveRevisionSource names the archive version a sync revision applies and how its
// change_summary describes it.
type archiveRevisionSource struct {
	pdbID   string
	version models.ArchiveVersion
	summary string
}

func (j *entrySyncStrategy) saveEntryUpdate(
	ctx context.Context,
	currentRevision models.EntryRevision,
	desiredRevision models.EntryRevision,
	desiredPolymerEntities []models.PolymerEntity,
	polymerEntitiesChanged bool,
	source archiveRevisionSource,
) error {
	now := j.now()
	revision := newSyncEntryRevision(currentRevision, source.summary, now)
	revision.Title = desiredRevision.Title
	revision.Metadata.Details = desiredRevision.Metadata.Details
	revision.Metadata.Resolution = desiredRevision.Metadata.Resolution
	revision.Metadata.Method = desiredRevision.Metadata.Method
	revision.Metadata.SpaceGroup = desiredRevision.Metadata.SpaceGroup
	revision.Metadata.Crystallography = desiredRevision.Metadata.Crystallography
	revision.FieldSources = archiveEntryFieldSources(currentRevision.FieldSources, source, now)
	return j.saveSyncEntryRevision(ctx, currentRevision.ID, revision, desiredPolymerEntities, polymerEntitiesChanged)
}

// saveObsoleteEntry activates a sync revision that marks the entry obsolete and, when the entry
// has no title, gives it the title of RCSB's removal record. It changes nothing else.
func (j *entrySyncStrategy) saveObsoleteEntry(
	ctx context.Context,
	currentRevision models.EntryRevision,
	pdbID string,
	update obsoleteEntryUpdate,
) error {
	now := j.now()
	revision := newSyncEntryRevision(currentRevision, update.summary, now)
	revision.Metadata.ArchiveStatus = &update.status
	if update.title != nil {
		revision.Title = update.title
		revision.FieldSources = removalTitleFieldSources(currentRevision.FieldSources, pdbID, now)
	}
	return j.saveSyncEntryRevision(ctx, currentRevision.ID, revision, nil, false)
}

// newSyncEntryRevision starts a sync revision as a copy of the active one.
func newSyncEntryRevision(currentRevision models.EntryRevision, summary string, now time.Time) models.EntryRevision {
	revision := currentRevision
	revision.ParentRevisionID = new(currentRevision.ID)
	revision.ID = uuid.New()
	revision.RevisionNumber = nil
	revision.State = models.RevisionStateInReview
	revision.ChangeSummary = new(summary)
	revision.PublishedAt = nil
	revision.CreatedBy = dataSyncUserID
	revision.CreatedAt = now
	revision.UpdatedAt = now
	return revision
}

// saveSyncEntryRevision creates revision, gives it the parent's data, and activates it, unless the
// parent stopped being the active revision meanwhile.
func (j *entrySyncStrategy) saveSyncEntryRevision(
	ctx context.Context,
	parentRevisionID uuid.UUID,
	revision models.EntryRevision,
	desiredPolymerEntities []models.PolymerEntity,
	polymerEntitiesChanged bool,
) error {
	entryID := revision.EntryID
	if err := j.database.Do(ctx, func(ctx context.Context) error {
		if err := j.database.Entries.Lock(ctx, entryID); err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		activeState := models.RevisionStateActive
		activeRevision, err := j.database.Entries.Get(ctx, db.EntryRevisionFilters{
			EntryID: &entryID,
			State:   &activeState,
		})
		if err != nil {
			return fmt.Errorf("get active parent revision: %w", err)
		}
		if activeRevision.ID != parentRevisionID {
			return fmt.Errorf("entry revision parent is no longer active: %w", db.ErrEntryRevisionConflict)
		}

		createdRevision, err := j.database.Entries.Create(ctx, revision)
		if err != nil {
			return fmt.Errorf("create entry revision: %w", err)
		}
		if err := j.saveEntryRevisionData(
			ctx,
			parentRevisionID,
			createdRevision.ID,
			desiredPolymerEntities,
			polymerEntitiesChanged,
			revision.CreatedAt,
		); err != nil {
			return fmt.Errorf("save entry revision data: %w", err)
		}
		activatedRevision, err := j.database.Entries.ActivateRevision(
			ctx,
			entryID,
			createdRevision.ID,
		)
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
		"change_summary", stringValue(revision.ChangeSummary),
	)
	return nil
}

func (j *entrySyncStrategy) saveEntryRevisionData(
	ctx context.Context,
	fromRevisionID uuid.UUID,
	toRevisionID uuid.UUID,
	polymerEntities []models.PolymerEntity,
	polymerEntitiesChanged bool,
	createdAt time.Time,
) error {
	if err := j.database.Artifacts.CopyEntryRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
		return fmt.Errorf("copy artifact links: %w", err)
	}

	if err := j.database.ProteinSequences.MoveEntryRevision(ctx, fromRevisionID, toRevisionID); err != nil {
		return fmt.Errorf("move protein sequences: %w", err)
	}

	if !polymerEntitiesChanged {
		if err := j.database.PolymerEntities.CopyEntryRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
			return fmt.Errorf("copy polymer entity links: %w", err)
		}
		return nil
	}

	for _, polymerEntity := range polymerEntities {
		entity, err := j.database.PolymerEntities.Create(ctx, models.PolymerEntity{
			ID:                uuid.New(),
			ProteinSequenceID: polymerEntity.ProteinSequenceID,
			Metadata:          polymerEntity.Metadata,
			CreatedAt:         createdAt,
		})
		if err != nil {
			return fmt.Errorf("create polymer entity %s: %w", stringValue(polymerEntity.Metadata.LabelEntityID), err)
		}
		if err := j.database.PolymerEntities.AttachToEntryRevision(ctx, toRevisionID, entity.ID); err != nil {
			return fmt.Errorf("attach polymer entity %s: %w", stringValue(polymerEntity.Metadata.LabelEntityID), err)
		}
	}
	return nil
}

func (j *entrySyncStrategy) reindexEntry(ctx context.Context, revision models.EntryRevision) error {
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

func (j *entrySyncStrategy) buildEntryUpdate(
	ctx context.Context,
	pdbID string,
	details rcsb.EntryDetails,
	proteinSequences []models.ProteinSequence,
) (models.EntryRevision, []models.PolymerEntity, error) {
	desiredPolymerEntities, err := j.buildPolymerEntities(
		ctx,
		pdbID,
		details.Identifiers.PolymerEntityIDs,
		proteinSequences,
	)
	if err != nil {
		return models.EntryRevision{}, nil, fmt.Errorf("build RCSB polymer entities for %s: %w", pdbID, err)
	}

	desiredRevision := models.EntryRevision{}
	applyRCSBEntryDetails(&desiredRevision, details)
	return desiredRevision, desiredPolymerEntities, nil
}

func (j *entrySyncStrategy) buildPolymerEntities(
	ctx context.Context,
	pdbID string,
	polymerEntityIDs []string,
	proteinSequences []models.ProteinSequence,
) ([]models.PolymerEntity, error) {
	entities := make([]models.PolymerEntity, 0, len(polymerEntityIDs))
	var uniProtRelease *string
	uniProtReleaseLoaded := false

	for _, entityID := range polymerEntityIDs {
		entityID = strings.TrimSpace(entityID)
		if entityID == "" {
			continue
		}
		details, err := j.rcsbClient.GetPolymerEntityDetails(ctx, pdbID, entityID)
		if err != nil {
			return nil, fmt.Errorf("get polymer entity %s_%s: %w", pdbID, entityID, err)
		}
		if !uniProtReleaseLoaded && archive.HasSIFTSMapping(details) {
			release, err := j.siftsClient.GetUniProtRelease(ctx, pdbID)
			if err != nil {
				return nil, fmt.Errorf("get UniProt release for %s: %w", pdbID, err)
			}
			uniProtRelease = archive.NormalizeUniProtRelease(release)
			uniProtReleaseLoaded = true
		}

		metadata := polymerEntityMetadataFromRCSB(details, entityID, uniProtRelease)
		proteinSequence, err := proteinSequenceForPolymerEntity(
			pdbID,
			stringValue(metadata.LabelEntityID),
			details.Polymer.CanonicalSequence,
			proteinSequences,
		)
		if err != nil {
			return nil, err
		}
		entities = append(entities, models.PolymerEntity{
			ProteinSequenceID: proteinSequence.ID,
			Metadata:          metadata,
		})
	}
	sort.Slice(entities, func(first int, second int) bool {
		return stringValue(entities[first].Metadata.LabelEntityID) < stringValue(entities[second].Metadata.LabelEntityID)
	})
	return entities, nil
}

func normalizedUniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		known := false
		for _, existing := range result {
			if existing == value {
				known = true
				break
			}
		}
		if !known {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func optionalCIFString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == "?" {
		return nil
	}
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func proteinSequenceForPolymerEntity(
	pdbID string,
	entityID string,
	canonicalSequence string,
	proteinSequences []models.ProteinSequence,
) (*models.ProteinSequence, error) {
	expectedHeaderID := strings.TrimSpace(pdbID) + "_" + strings.TrimSpace(entityID)
	canonicalSequence = normalizeProteinSequence(canonicalSequence)
	for index := range proteinSequences {
		sequence := &proteinSequences[index]
		headerID := strings.TrimSpace(strings.SplitN(sequence.Header, "|", 2)[0])
		if !strings.EqualFold(headerID, expectedHeaderID) {
			continue
		}
		if canonicalSequence != "" && normalizeProteinSequence(sequence.Sequence) != canonicalSequence {
			return nil, fmt.Errorf("protein sequence for polymer entity %s does not match RCSB", expectedHeaderID)
		}
		return sequence, nil
	}
	if canonicalSequence != "" {
		for index := range proteinSequences {
			sequence := &proteinSequences[index]
			if normalizeProteinSequence(sequence.Sequence) == canonicalSequence {
				return sequence, nil
			}
		}
	}
	return nil, fmt.Errorf("protein sequence for polymer entity %s was not found", expectedHeaderID)
}

func normalizeProteinSequence(sequence string) string {
	return strings.ToUpper(strings.Join(strings.Fields(sequence), ""))
}

// applyDepositorPolymerEntities overlays what depositors supplied onto the entities
// extracted from RCSB, matching on label_entity_id. A depositor's construct and
// mutations replace extracted ones; their organisms fill in only where extraction
// found none.
func applyDepositorPolymerEntities(
	extracted []models.PolymerEntity,
	depositor []models.DepositorPolymerEntity,
) []models.PolymerEntity {
	merged := append([]models.PolymerEntity(nil), extracted...)
	for index := range merged {
		label := stringValue(merged[index].Metadata.LabelEntityID)
		for _, supplied := range depositor {
			if supplied.LabelEntityID != label {
				continue
			}
			if supplied.Construct != nil {
				merged[index].Metadata.Construct = supplied.Construct
			}
			if len(supplied.Mutations) > 0 {
				applyDepositorMutations(&merged[index].Metadata, supplied.Mutations)
			}
			if len(merged[index].Metadata.SourceOrganisms) == 0 {
				merged[index].Metadata.SourceOrganisms = supplied.SourceOrganisms
			}
		}
	}
	return merged
}

// applyDepositorMutations replaces the extracted mutations with the depositor's. The API only
// accepts plain substitutions; a token that is not one is kept out of Mutations all the same,
// in the raw text, flagged for review.
func applyDepositorMutations(metadata *models.PolymerEntityMetadata, supplied []string) {
	accepted, status := models.ParseMutations(strings.Join(supplied, ", "))
	if status == models.MutationsStatusParsed {
		metadata.Mutations = supplied
		return
	}
	metadata.Mutations = accepted
	metadata.MutationsRaw = new(strings.Join(supplied, ", "))
	metadata.MutationsStatus = &status
}

func polymerEntitiesHaveSameData(
	stored []models.PolymerEntity,
	desired []models.PolymerEntity,
) bool {
	if len(stored) != len(desired) {
		return false
	}
	stored = append([]models.PolymerEntity(nil), stored...)
	desired = append([]models.PolymerEntity(nil), desired...)
	sort.Slice(stored, func(first int, second int) bool {
		return stringValue(stored[first].Metadata.LabelEntityID) < stringValue(stored[second].Metadata.LabelEntityID)
	})
	sort.Slice(desired, func(first int, second int) bool {
		return stringValue(desired[first].Metadata.LabelEntityID) < stringValue(desired[second].Metadata.LabelEntityID)
	})
	for index := range desired {
		storedMetadata := withoutBlankMutations(stored[index].Metadata)
		desiredMetadata := comparableToStored(storedMetadata, withoutBlankMutations(desired[index].Metadata))
		if stored[index].ProteinSequenceID != desired[index].ProteinSequenceID ||
			!reflect.DeepEqual(storedMetadata, desiredMetadata) {
			return false
		}
	}
	return true
}

// comparableToStored drops from the desired metadata what a stored entity cannot hold:
// entities whose mutations were already a list when the schema migration structured them
// may have no original text, and mutation text that only differs from the stored text in
// separators or whitespace is the same text.
func comparableToStored(stored models.PolymerEntityMetadata, desired models.PolymerEntityMetadata) models.PolymerEntityMetadata {
	switch {
	case stored.MutationsRaw == nil:
		desired.MutationsRaw = nil
	case desired.MutationsRaw != nil && models.SameMutationText(*stored.MutationsRaw, *desired.MutationsRaw):
		desired.MutationsRaw = stored.MutationsRaw
	}
	return desired
}

// withoutBlankMutations treats blank mutation text as no text: RCSB blank
// text is never stored, but the schema migration records such text as status none.
func withoutBlankMutations(metadata models.PolymerEntityMetadata) models.PolymerEntityMetadata {
	if len(metadata.Mutations) == 0 && strings.TrimSpace(stringValue(metadata.MutationsRaw)) == "" {
		metadata.Mutations = nil
		metadata.MutationsRaw = nil
		metadata.MutationsStatus = nil
	}
	return metadata
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return -1
	}
	return *value
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

func applyRCSBEntryDetails(
	revision *models.EntryRevision,
	details rcsb.EntryDetails,
) {
	mapping := archive.MapEntry(details)
	metadata := archivemodels.EntryMetadata(mapping)
	revision.Title = mapping.Title
	revision.Metadata.Details = metadata.Details
	revision.Metadata.Resolution = metadata.Resolution
	revision.Metadata.Method = metadata.Method
	revision.Metadata.SpaceGroup = metadata.SpaceGroup
	revision.Metadata.Crystallography = metadata.Crystallography
}

func polymerEntityMetadataFromRCSB(
	details rcsb.PolymerEntityDetails,
	fallbackEntityID string,
	uniProtRelease *string,
) models.PolymerEntityMetadata {
	return archivemodels.PolymerEntityMetadata(archive.MapPolymerEntity(details, fallbackEntityID, uniProtRelease), false)
}

func residueDataFromRCSB(
	entity rcsb.PolymerEntityDetails,
	instance rcsb.PolymerEntityInstanceDetails,
	fallbackAsymID string,
) []models.ResidueData {
	return archivemodels.ResidueData(archive.MapResidueData(entity, instance, fallbackAsymID))
}
