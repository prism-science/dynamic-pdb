package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
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
	sync(context.Context, models.DataSyncJob) error
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
	if logger == nil {
		logger = slog.Default()
	}

	now := func() time.Time { return time.Now().UTC() }
	return &DataSyncJob{
		database: database,
		logger:   logger,
		strategies: []dataSyncStrategy{
			newEntrySyncStrategy(database, rcsbClient, siftsClient, logger, now),
			newModelSyncStrategy(database, rcsbClient, logger, now),
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

	if executionErr := j.execute(ctx, *job); executionErr != nil {
		if errors.Is(executionErr, rcsb.ErrNotFound) {
			if err := j.database.DataSyncJobs.Delete(ctx, *job); err != nil {
				return false, errors.Join(
					fmt.Errorf("execute data sync job: %w", executionErr),
					fmt.Errorf("delete data sync job after RCSB 404: %w", err),
				)
			}
			j.logger.Warn(
				"data sync job removed after RCSB resource was not found",
				"entry_id", job.EntryID,
				"model_id", stringValue(job.ModelID),
				"err", executionErr,
			)
			return true, nil
		}
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
	job.ScheduledAt = j.nextScheduledAt(j.now())
	if err := j.database.DataSyncJobs.Schedule(ctx, *job); err != nil {
		return false, fmt.Errorf("reschedule data sync job: %w", err)
	}
	return true, nil
}

func (j *DataSyncJob) execute(ctx context.Context, job models.DataSyncJob) error {
	for _, strategy := range j.strategies {
		if strategy.appliesTo(job) {
			return strategy.sync(ctx, job)
		}
	}
	return nil
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

func (j *entrySyncStrategy) saveEntryUpdate(
	ctx context.Context,
	currentRevision models.EntryRevision,
	desiredRevision models.EntryRevision,
	desiredPolymerEntities []models.PolymerEntity,
	polymerEntitiesChanged bool,
) error {
	now := j.now()
	entryID := currentRevision.EntryID
	parentRevisionID := currentRevision.ID
	revision := currentRevision
	revision.Title = desiredRevision.Title
	revision.Metadata.Details = desiredRevision.Metadata.Details
	revision.Metadata.Resolution = desiredRevision.Metadata.Resolution
	revision.Metadata.Method = desiredRevision.Metadata.Method
	revision.Metadata.SpaceGroup = desiredRevision.Metadata.SpaceGroup
	revision.Metadata.Crystallography = desiredRevision.Metadata.Crystallography
	revision.ParentRevisionID = new(parentRevisionID)
	revision.ID = uuid.New()
	revision.RevisionNumber = nil
	revision.State = models.RevisionStateInReview
	revision.ChangeSummary = nil
	revision.PublishedAt = nil
	revision.CreatedBy = dataSyncUserID
	revision.CreatedAt = now
	revision.UpdatedAt = now

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
			now,
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
	proteinSequences []models.ProteinSequence,
) (models.EntryRevision, []models.PolymerEntity, error) {
	details, err := j.rcsbClient.GetEntryDetails(ctx, pdbID)
	if err != nil {
		return models.EntryRevision{}, nil, fmt.Errorf("get RCSB entry %s: %w", pdbID, err)
	}
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
		if !uniProtReleaseLoaded && hasSIFTSMapping(details) {
			release, err := j.siftsClient.GetUniProtRelease(ctx, pdbID)
			if err != nil {
				return nil, fmt.Errorf("get UniProt release for %s: %w", pdbID, err)
			}
			uniProtRelease = normalizeUniProtRelease(release)
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

func residueDataFromRCSB(
	entity rcsb.PolymerEntityDetails,
	instance rcsb.PolymerEntityInstanceDetails,
	fallbackAsymID string,
) []models.ResidueData {
	labelAsymID := strings.TrimSpace(instance.Identifiers.AsymID)
	if labelAsymID == "" {
		labelAsymID = strings.TrimSpace(fallbackAsymID)
	}
	authAsymID := optionalString(instance.Identifiers.AuthAsymID)

	metrics := make(map[int]*rcsbResidueMetrics)
	componentIDs := make(map[int]string)
	sequenceComponentIDs := polymerComponentIDs(entity.Polymer)
	for _, feature := range instance.Features {
		metricType := strings.ToUpper(strings.TrimSpace(feature.Type))
		if metricType != "RSCC" && metricType != "OWAB" && metricType != "AVERAGE_OCCUPANCY" {
			continue
		}
		for _, position := range feature.Positions {
			if position.BeginSequenceID < 1 {
				continue
			}
			if componentID := strings.TrimSpace(position.BeginComponentID); componentID != "" {
				componentIDs[position.BeginSequenceID] = componentID
			}
			for offset, value := range position.Values {
				if value == nil {
					continue
				}
				sequenceID := position.BeginSequenceID + offset
				residueMetrics := metrics[sequenceID]
				if residueMetrics == nil {
					residueMetrics = &rcsbResidueMetrics{}
					metrics[sequenceID] = residueMetrics
				}
				switch metricType {
				case "RSCC":
					residueMetrics.RSCC = value
				case "OWAB":
					residueMetrics.BIso = value
				case "AVERAGE_OCCUPANCY":
					residueMetrics.Occupancy = value
				}
			}
		}
	}

	identityBySequenceID := make(map[int]models.ResidueData)
	for _, scheme := range instance.SequenceScheme {
		if scheme.SequenceID < 1 {
			continue
		}
		if asymID := strings.TrimSpace(scheme.AsymID); asymID != "" && asymID != labelAsymID {
			continue
		}
		if _, exists := identityBySequenceID[scheme.SequenceID]; exists {
			continue
		}
		residueAuthAsymID := optionalString(firstNonEmpty(scheme.PDBStrandID, stringValue(authAsymID)))
		identityBySequenceID[scheme.SequenceID] = models.ResidueData{
			LabelAsymID:     labelAsymID,
			LabelSeqID:      scheme.SequenceID,
			LabelCompID:     strings.TrimSpace(scheme.MonomerID),
			AuthAsymID:      residueAuthAsymID,
			AuthSeqID:       scheme.AuthSeqNum,
			PDBxPDBInsCode:  optionalCIFString(scheme.PDBInsCode),
			UniProtPosition: uniProtPosition(entity.Alignments, scheme.SequenceID),
		}
	}
	residuesBySequenceID := make(map[int]models.ResidueData, len(metrics))
	for sequenceID, residueMetrics := range metrics {
		residue, exists := identityBySequenceID[sequenceID]
		if !exists {
			authSeqID, insertionCode := authSequencePosition(
				instance.Identifiers.AuthToEntityPolySeqMapping,
				sequenceID,
			)
			residue = models.ResidueData{
				LabelAsymID:     labelAsymID,
				LabelSeqID:      sequenceID,
				LabelCompID:     firstNonEmpty(componentIDs[sequenceID], sequenceComponentIDs[sequenceID]),
				AuthAsymID:      authAsymID,
				AuthSeqID:       authSeqID,
				PDBxPDBInsCode:  insertionCode,
				UniProtPosition: uniProtPosition(entity.Alignments, sequenceID),
			}
		}
		residue.RSCC = residueMetrics.RSCC
		residue.BIso = residueMetrics.BIso
		residue.Occupancy = residueMetrics.Occupancy
		residuesBySequenceID[sequenceID] = residue
	}

	sequenceIDs := make([]int, 0, len(residuesBySequenceID))
	for sequenceID := range residuesBySequenceID {
		sequenceIDs = append(sequenceIDs, sequenceID)
	}
	sort.Ints(sequenceIDs)
	residues := make([]models.ResidueData, 0, len(sequenceIDs))
	for _, sequenceID := range sequenceIDs {
		residues = append(residues, residuesBySequenceID[sequenceID])
	}
	return residues
}

func authSequencePosition(mapping []string, labelSequenceID int) (*int, *string) {
	if labelSequenceID < 1 || labelSequenceID > len(mapping) {
		return nil, nil
	}
	value := strings.TrimSpace(mapping[labelSequenceID-1])
	if value == "" || value == "." || value == "?" {
		return nil, nil
	}

	digitEnd := 0
	if value[0] == '+' || value[0] == '-' {
		digitEnd++
	}
	digitStart := digitEnd
	for digitEnd < len(value) && value[digitEnd] >= '0' && value[digitEnd] <= '9' {
		digitEnd++
	}
	if digitEnd == digitStart {
		return nil, nil
	}
	position, err := strconv.Atoi(value[:digitEnd])
	if err != nil {
		return nil, nil
	}
	return &position, optionalCIFString(value[digitEnd:])
}

func polymerComponentIDs(polymer rcsb.PolymerData) map[int]string {
	sequence := polymer.Sequence
	if strings.TrimSpace(sequence) == "" {
		sequence = polymer.CanonicalSequence
	}
	components := make(map[int]string)
	sequenceID := 0
	for index := 0; index < len(sequence); {
		character := sequence[index]
		if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			index++
			continue
		}
		if character == '(' {
			closingOffset := strings.IndexByte(sequence[index+1:], ')')
			if closingOffset < 0 {
				break
			}
			componentID := strings.ToUpper(strings.TrimSpace(sequence[index+1 : index+1+closingOffset]))
			if componentID != "" {
				sequenceID++
				components[sequenceID] = componentID
			}
			index += closingOffset + 2
			continue
		}
		sequenceID++
		components[sequenceID] = componentIDForOneLetter(character, polymer.Type)
		index++
	}
	return components
}

func componentIDForOneLetter(character byte, polymerType string) string {
	if character >= 'a' && character <= 'z' {
		character -= 'a' - 'A'
	}
	polymerType = strings.ToLower(strings.TrimSpace(polymerType))
	if strings.Contains(polymerType, "polypeptide") {
		switch character {
		case 'A':
			return "ALA"
		case 'R':
			return "ARG"
		case 'N':
			return "ASN"
		case 'D':
			return "ASP"
		case 'C':
			return "CYS"
		case 'Q':
			return "GLN"
		case 'E':
			return "GLU"
		case 'G':
			return "GLY"
		case 'H':
			return "HIS"
		case 'I':
			return "ILE"
		case 'L':
			return "LEU"
		case 'K':
			return "LYS"
		case 'M':
			return "MET"
		case 'F':
			return "PHE"
		case 'P':
			return "PRO"
		case 'S':
			return "SER"
		case 'T':
			return "THR"
		case 'W':
			return "TRP"
		case 'Y':
			return "TYR"
		case 'V':
			return "VAL"
		case 'U':
			return "SEC"
		case 'O':
			return "PYL"
		case 'B':
			return "ASX"
		case 'Z':
			return "GLX"
		case 'J':
			return "XLE"
		default:
			return "UNK"
		}
	}
	if strings.Contains(polymerType, "polydeoxyribonucleotide") &&
		!strings.Contains(polymerType, "hybrid") {
		return "D" + string(character)
	}
	return string(character)
}

type rcsbResidueMetrics struct {
	RSCC      *float64
	BIso      *float64
	Occupancy *float64
}

func uniProtPosition(alignments []rcsb.PolymerEntityAlignment, labelSequenceID int) *string {
	for _, requireSIFTS := range []bool{true, false} {
		for _, alignment := range alignments {
			if !strings.EqualFold(strings.TrimSpace(alignment.ReferenceDatabaseName), "UniProt") {
				continue
			}
			isSIFTS := strings.EqualFold(strings.TrimSpace(alignment.ProvenanceSource), "SIFTS")
			if requireSIFTS != isSIFTS {
				continue
			}
			accession := strings.TrimSpace(alignment.ReferenceDatabaseAccession)
			if accession == "" {
				continue
			}
			for _, region := range alignment.AlignedRegions {
				if region.Length < 1 || labelSequenceID < region.EntityBeginSequenceID ||
					labelSequenceID >= region.EntityBeginSequenceID+region.Length {
					continue
				}
				position := region.ReferenceBeginSequenceID + labelSequenceID - region.EntityBeginSequenceID
				value := fmt.Sprintf("%s:%d", accession, position)
				return &value
			}
		}
	}
	return nil
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

func hasSIFTSMapping(details rcsb.PolymerEntityDetails) bool {
	for _, reference := range details.Identifiers.ReferenceSequenceIdentifiers {
		if strings.EqualFold(strings.TrimSpace(reference.DatabaseName), "UniProt") &&
			strings.EqualFold(strings.TrimSpace(reference.ProvenanceSource), "SIFTS") {
			return true
		}
	}
	return false
}

func polymerEntityMetadataFromRCSB(
	details rcsb.PolymerEntityDetails,
	fallbackEntityID string,
	uniProtRelease *string,
) models.PolymerEntityMetadata {
	entityID := strings.TrimSpace(details.Identifiers.EntityID)
	if entityID == "" {
		entityID = strings.TrimSpace(fallbackEntityID)
	}

	var sourceOrganisms []models.PolymerEntityOrganism
	for _, sourceOrganism := range details.SourceOrganisms {
		name := strings.TrimSpace(sourceOrganism.ScientificName)
		if name == "" && sourceOrganism.NCBITaxonomyID == nil {
			continue
		}
		sourceOrganisms = append(sourceOrganisms, models.PolymerEntityOrganism{
			ScientificName: name,
			NCBITaxonomyID: sourceOrganism.NCBITaxonomyID,
		})
	}
	sort.Slice(sourceOrganisms, func(first int, second int) bool {
		if sourceOrganisms[first].ScientificName != sourceOrganisms[second].ScientificName {
			return sourceOrganisms[first].ScientificName < sourceOrganisms[second].ScientificName
		}
		return intValue(sourceOrganisms[first].NCBITaxonomyID) < intValue(sourceOrganisms[second].NCBITaxonomyID)
	})

	var uniProtMappings []models.PolymerEntityUniProtReference
	for _, reference := range details.Identifiers.ReferenceSequenceIdentifiers {
		if !strings.EqualFold(strings.TrimSpace(reference.DatabaseName), "UniProt") {
			continue
		}
		accession := strings.TrimSpace(reference.DatabaseAccession)
		if accession == "" {
			continue
		}
		source, ok := uniProtReferenceSource(reference.ProvenanceSource)
		if !ok {
			continue
		}
		mapping := models.PolymerEntityUniProtReference{
			Accession: accession,
			Source:    source,
		}
		if source == models.UniProtReferenceSourceSIFTS {
			mapping.UniProtRelease = uniProtRelease
		}
		uniProtMappings = append(uniProtMappings, mapping)
	}
	sort.Slice(uniProtMappings, func(first int, second int) bool {
		if uniProtMappings[first].Accession != uniProtMappings[second].Accession {
			return uniProtMappings[first].Accession < uniProtMappings[second].Accession
		}
		return uniProtMappings[first].Source < uniProtMappings[second].Source
	})

	return models.PolymerEntityMetadata{
		LabelEntityID:   optionalString(entityID),
		Description:     optionalString(details.Entity.Description),
		SourceOrganisms: sourceOrganisms,
		Construct:       optionalString(details.Entity.Fragment),
		Mutations:       optionalString(details.Entity.Mutation),
		UniProtMappings: uniProtMappings,
	}
}

func uniProtReferenceSource(value string) (models.UniProtReferenceSource, bool) {
	switch {
	case strings.EqualFold(strings.TrimSpace(value), "SIFTS"):
		return models.UniProtReferenceSourceSIFTS, true
	case strings.EqualFold(strings.TrimSpace(value), "PDB"):
		return models.UniProtReferenceSourceStructRef, true
	default:
		return "", false
	}
}

func normalizeUniProtRelease(release *string) *string {
	if release == nil {
		return nil
	}
	value := strings.ReplaceAll(strings.TrimSpace(*release), ".", "_")
	return optionalString(value)
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
		if stored[index].ProteinSequenceID != desired[index].ProteinSequenceID ||
			!reflect.DeepEqual(stored[index].Metadata, desired[index].Metadata) {
			return false
		}
	}
	return true
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

func applyRCSBEntryDetails(
	revision *models.EntryRevision,
	details rcsb.EntryDetails,
) {
	revision.Title = optionalString(details.Structure.Title)
	revision.Metadata.Details = optionalString(details.Structure.Details)
	revision.Metadata.Resolution = firstResolution(details.Info.CombinedResolution)
	revision.Metadata.Method = structureMethod(details.Experiments)
	revision.Metadata.SpaceGroup = optionalString(details.Symmetry.SpaceGroup)
	revision.Metadata.Crystallography = crystallographyFromRCSB(details)
}

func crystallographyFromRCSB(details rcsb.EntryDetails) *models.EntryCrystallography {
	crystalsByID := make(map[string]models.EntryCrystal)
	ensureCrystal := func(rawID string) (models.EntryCrystal, string, bool) {
		crystalID := strings.TrimSpace(rawID)
		if crystalID == "" {
			return models.EntryCrystal{}, "", false
		}
		crystal, ok := crystalsByID[crystalID]
		if !ok {
			crystal = models.EntryCrystal{ID: crystalID}
		}
		return crystal, crystalID, true
	}

	for _, sourceCrystal := range details.Crystals {
		crystal, crystalID, ok := ensureCrystal(sourceCrystal.ID)
		if ok {
			crystalsByID[crystalID] = crystal
		}
	}
	for _, sourceGrowth := range details.CrystalGrowth {
		crystal, crystalID, ok := ensureCrystal(sourceGrowth.CrystalID)
		if !ok {
			continue
		}
		if sourceGrowth.PH != nil || sourceGrowth.TemperatureKelvin != nil {
			crystal.Growth = &models.EntryCrystalGrowth{
				PH:                sourceGrowth.PH,
				TemperatureKelvin: sourceGrowth.TemperatureKelvin,
			}
		}
		crystalsByID[crystalID] = crystal
	}
	for _, sourceDiffraction := range details.Diffractions {
		crystal, crystalID, ok := ensureCrystal(sourceDiffraction.CrystalID)
		if !ok {
			continue
		}
		diffractionID := strings.TrimSpace(sourceDiffraction.ID)
		if diffractionID == "" {
			continue
		}
		crystal.Diffractions = append(crystal.Diffractions, models.EntryDiffraction{
			ID:                diffractionID,
			TemperatureKelvin: sourceDiffraction.TemperatureKelvin,
		})
		crystalsByID[crystalID] = crystal
	}
	if len(crystalsByID) == 0 {
		return nil
	}

	crystalIDs := make([]string, 0, len(crystalsByID))
	for crystalID := range crystalsByID {
		crystalIDs = append(crystalIDs, crystalID)
	}
	sort.Strings(crystalIDs)
	crystals := make([]models.EntryCrystal, 0, len(crystalIDs))
	for _, crystalID := range crystalIDs {
		crystal := crystalsByID[crystalID]
		sort.Slice(crystal.Diffractions, func(first int, second int) bool {
			return crystal.Diffractions[first].ID < crystal.Diffractions[second].ID
		})
		crystals = append(crystals, crystal)
	}
	return &models.EntryCrystallography{Crystals: crystals}
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
