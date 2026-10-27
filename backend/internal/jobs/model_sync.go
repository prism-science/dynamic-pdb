package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
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
)

type modelSyncStrategy struct {
	database       *db.DB
	rcsbClient     *rcsb.RemoteClient
	logger         *slog.Logger
	now            func() time.Time
	validationMode validation.Mode
}

func newModelSyncStrategy(
	database *db.DB,
	rcsbClient *rcsb.RemoteClient,
	logger *slog.Logger,
	now func() time.Time,
	validationMode validation.Mode,
) *modelSyncStrategy {
	return &modelSyncStrategy{
		database:       database,
		rcsbClient:     rcsbClient,
		logger:         logger,
		now:            now,
		validationMode: validationMode,
	}
}

func (*modelSyncStrategy) appliesTo(job models.DataSyncJob) bool {
	return job.ModelID != nil
}

func (s *modelSyncStrategy) sync(ctx context.Context, job models.DataSyncJob) error {
	modelID := strings.TrimSpace(*job.ModelID)
	if modelID == "" {
		return fmt.Errorf("sync model for entry %s: model ID is empty", job.EntryID)
	}
	if err := s.syncModel(ctx, job.EntryID, modelID); err != nil {
		return fmt.Errorf("sync model %s: %w", modelID, err)
	}
	return nil
}

func (s *modelSyncStrategy) syncModel(ctx context.Context, entryID string, modelID string) error {
	currentRevision, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
		EntryID:    &entryID,
		ModelID:    &modelID,
		State:      new(models.RevisionStateActive),
		ModelState: new(models.ModelStateActive),
		EntryState: new(models.EntryStateActive),
	})
	if err != nil {
		return fmt.Errorf("get current model revision: %w", err)
	}
	pdbID := strings.TrimSpace(currentRevision.Metadata.ExternalRefs[models.ModelSourcePDB])
	if pdbID == "" {
		return nil
	}
	if !isDepositedModel(*currentRevision) {
		s.logger.Warn(
			"data sync skipped a model that is not the archive's Deposited model",
			"entry_id", entryID,
			"model_id", modelID,
			"model_type", modelTypeName(currentRevision.Metadata.ModelType),
		)
		return nil
	}
	metricKeys := syncedArchiveMetricKeys(currentRevision.FieldSources)

	currentMetrics, err := s.database.Metrics.List(ctx, db.MetricFilters{
		ModelRevisionID: &currentRevision.ID,
	})
	if err != nil {
		return fmt.Errorf("list current model metrics: %w", err)
	}
	desiredRevision, desiredMetrics, err := s.buildModelUpdate(ctx, pdbID)
	if err != nil {
		return fmt.Errorf("build model update: %w", err)
	}
	keepDepositorModelFields(*currentRevision, &desiredRevision)
	desiredMetrics = metricsWithKeys(desiredMetrics, metricKeys)

	modelRevisionChanged, metricsChanged := compareModelUpdate(
		*currentRevision,
		desiredRevision,
		currentMetrics,
		desiredMetrics,
		metricKeys,
	)
	if !metricsChanged {
		fills := metricSoftwareFills(currentMetrics, desiredMetrics, metricKeys)
		if err := s.fillMetricSoftware(ctx, modelID, fills); err != nil {
			return fmt.Errorf("fill archive metric software: %w", err)
		}
	}
	if !modelRevisionChanged && !metricsChanged {
		return nil
	}
	fieldErrors := modelUpdateErrors(desiredRevision, desiredMetrics)
	if err := s.validationMode.Check(ctx, s.logger, "data sync model "+modelID, fieldErrors); err != nil {
		return fmt.Errorf("validate model update: %w", err)
	}

	if err := s.saveModelUpdate(
		ctx,
		*currentRevision,
		desiredRevision,
		desiredMetrics,
		metricsChanged,
		metricKeys,
		pdbID,
	); err != nil {
		return fmt.Errorf("save model update: %w", err)
	}
	return nil
}

func (s *modelSyncStrategy) buildModelUpdate(
	ctx context.Context,
	pdbID string,
) (models.ModelRevision, []models.Metric, error) {
	details, err := s.rcsbClient.GetEntryDetails(ctx, pdbID)
	if err != nil {
		return models.ModelRevision{}, nil, fmt.Errorf("get RCSB entry %s: %w", pdbID, err)
	}

	desiredRevision := models.ModelRevision{}
	applyRCSBModelDetails(&desiredRevision, details)
	residueData, err := s.buildResidueData(ctx, pdbID, details.Identifiers.PolymerEntityIDs)
	if err != nil {
		return models.ModelRevision{}, nil, fmt.Errorf("build RCSB residue data for %s: %w", pdbID, err)
	}
	desiredRevision.Metadata.ResidueData = residueData
	return desiredRevision, modelMetricsFromRCSB(details), nil
}

func (s *modelSyncStrategy) buildResidueData(
	ctx context.Context,
	pdbID string,
	polymerEntityIDs []string,
) ([]models.ResidueData, error) {
	var residues []models.ResidueData
	for _, entityID := range normalizedUniqueStrings(polymerEntityIDs) {
		entity, err := s.rcsbClient.GetPolymerEntityDetails(ctx, pdbID, entityID)
		if err != nil {
			return nil, fmt.Errorf("get polymer entity %s_%s: %w", pdbID, entityID, err)
		}
		for _, asymID := range normalizedUniqueStrings(entity.Identifiers.AsymIDs) {
			instance, err := s.rcsbClient.GetPolymerEntityInstanceDetails(ctx, pdbID, asymID)
			if err != nil {
				return nil, fmt.Errorf("get polymer entity instance %s.%s: %w", pdbID, asymID, err)
			}
			residues = append(residues, residueDataFromRCSB(entity, instance, asymID)...)
		}
	}
	sort.Slice(residues, func(first int, second int) bool {
		if residues[first].LabelAsymID != residues[second].LabelAsymID {
			return residues[first].LabelAsymID < residues[second].LabelAsymID
		}
		if residues[first].LabelSeqID != residues[second].LabelSeqID {
			return residues[first].LabelSeqID < residues[second].LabelSeqID
		}
		return stringValue(residues[first].LabelAltID) < stringValue(residues[second].LabelAltID)
	})
	return residues, nil
}

func compareModelUpdate(
	currentRevision models.ModelRevision,
	desiredRevision models.ModelRevision,
	currentMetrics []models.Metric,
	desiredMetrics []models.Metric,
	metricKeys []models.MetricKey,
) (bool, bool) {
	modelRevisionChanged := !reflect.DeepEqual(currentRevision.Metadata.Authors, desiredRevision.Metadata.Authors) ||
		!reflect.DeepEqual(currentRevision.Metadata.Details, desiredRevision.Metadata.Details) ||
		!reflect.DeepEqual(currentRevision.Metadata.Affiliation, desiredRevision.Metadata.Affiliation) ||
		!reflect.DeepEqual(currentRevision.Metadata.AtomCount, desiredRevision.Metadata.AtomCount) ||
		!reflect.DeepEqual(currentRevision.Metadata.ModeledResidues, desiredRevision.Metadata.ModeledResidues) ||
		!reflect.DeepEqual(currentRevision.Metadata.UniqueProteinChains, desiredRevision.Metadata.UniqueProteinChains) ||
		!reflect.DeepEqual(currentRevision.Metadata.UnmodeledFraction, desiredRevision.Metadata.UnmodeledFraction) ||
		!reflect.DeepEqual(currentRevision.Metadata.Ligands, desiredRevision.Metadata.Ligands) ||
		!reflect.DeepEqual(currentRevision.Metadata.ResidueData, desiredRevision.Metadata.ResidueData)
	metricsChanged := !modelMetricsHaveSameData(currentMetrics, desiredMetrics, metricKeys)
	return modelRevisionChanged, metricsChanged
}

func (s *modelSyncStrategy) saveModelUpdate(
	ctx context.Context,
	currentRevision models.ModelRevision,
	desiredRevision models.ModelRevision,
	desiredMetrics []models.Metric,
	metricsChanged bool,
	metricKeys []models.MetricKey,
	pdbID string,
) error {
	now := s.now()
	parentRevisionID := currentRevision.ID
	revision := currentRevision
	revision.Metadata.Authors = desiredRevision.Metadata.Authors
	revision.Metadata.Details = desiredRevision.Metadata.Details
	revision.Metadata.Affiliation = desiredRevision.Metadata.Affiliation
	revision.Metadata.AtomCount = desiredRevision.Metadata.AtomCount
	revision.Metadata.ModeledResidues = desiredRevision.Metadata.ModeledResidues
	revision.Metadata.UniqueProteinChains = desiredRevision.Metadata.UniqueProteinChains
	revision.Metadata.UnmodeledFraction = desiredRevision.Metadata.UnmodeledFraction
	revision.Metadata.Ligands = desiredRevision.Metadata.Ligands
	revision.Metadata.ResidueData = desiredRevision.Metadata.ResidueData
	revision.FieldSources = archiveModelFieldSources(currentRevision.FieldSources, pdbID, now, metricKeys)
	revision.ParentRevisionID = new(parentRevisionID)
	revision.ID = uuid.New()
	revision.RevisionNumber = nil
	revision.State = models.RevisionStateInReview
	revision.ChangeSummary = nil
	revision.PublishedAt = nil
	revision.IdempotencyKey = nil
	revision.CreatedBy = dataSyncUserID
	revision.CreatedAt = now
	revision.UpdatedAt = now

	if err := s.database.Do(ctx, func(ctx context.Context) error {
		if err := s.database.Entries.Lock(ctx, currentRevision.EntryID); err != nil {
			return fmt.Errorf("lock entry: %w", err)
		}
		activeRevision, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
			EntryID: &currentRevision.EntryID,
			ModelID: &currentRevision.ModelID,
			State:   new(models.RevisionStateActive),
		})
		if err != nil {
			return fmt.Errorf("get active parent revision: %w", err)
		}
		if activeRevision.ID != parentRevisionID {
			return fmt.Errorf("model revision parent is no longer active: %w", db.ErrModelRevisionConflict)
		}

		createdRevision, err := s.database.Models.Create(ctx, currentRevision.EntryID, revision)
		if err != nil {
			return fmt.Errorf("create model revision: %w", err)
		}
		if err := s.saveModelRevisionData(
			ctx,
			parentRevisionID,
			createdRevision.ID,
			desiredMetrics,
			metricsChanged,
			metricKeys,
			now,
		); err != nil {
			return fmt.Errorf("save model revision data: %w", err)
		}
		activatedRevision, err := s.database.Models.ActivateRevision(ctx, createdRevision.ID)
		if err != nil {
			return fmt.Errorf("activate model revision: %w", err)
		}
		if err := s.database.EntrySearch.DeleteModelRevision(ctx, currentRevision); err != nil {
			return fmt.Errorf("remove previous model revision from search index: %w", err)
		}
		if err := s.database.EntrySearch.IndexModelRevision(ctx, *activatedRevision); err != nil {
			return fmt.Errorf("index active model revision: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("save model revision update: %w", err)
	}

	s.logger.Info(
		"data sync model revision activated",
		"entry_id", currentRevision.EntryID,
		"model_id", currentRevision.ModelID,
		"revision_id", revision.ID,
	)
	return nil
}

func (s *modelSyncStrategy) saveModelRevisionData(
	ctx context.Context,
	fromRevisionID uuid.UUID,
	toRevisionID uuid.UUID,
	desiredMetrics []models.Metric,
	metricsChanged bool,
	metricKeys []models.MetricKey,
	createdAt time.Time,
) error {
	if err := s.database.Artifacts.CopyModelRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
		return fmt.Errorf("copy artifact links: %w", err)
	}
	if err := s.database.Runs.CopyModelRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
		return fmt.Errorf("copy run links: %w", err)
	}
	if !metricsChanged {
		if err := s.database.Metrics.CopyModelRevisionLinks(ctx, fromRevisionID, toRevisionID); err != nil {
			return fmt.Errorf("copy metric links: %w", err)
		}
		return nil
	}

	if err := s.database.Metrics.CopyModelRevisionLinksExcludingKeys(
		ctx, fromRevisionID, toRevisionID, metricKeys,
	); err != nil {
		return fmt.Errorf("copy metric links the sync does not replace: %w", err)
	}
	for _, desiredMetric := range desiredMetrics {
		desiredMetric.ID = uuid.New()
		desiredMetric.CreatedAt = createdAt
		createdMetric, err := s.database.Metrics.Create(ctx, desiredMetric)
		if err != nil {
			return fmt.Errorf("create metric %s: %w", desiredMetric.Key, err)
		}
		if err := s.database.Metrics.AttachToModelRevision(ctx, toRevisionID, createdMetric.ID); err != nil {
			return fmt.Errorf("attach metric %s: %w", desiredMetric.Key, err)
		}
	}
	return nil
}

func modelMetricsHaveSameData(current []models.Metric, desired []models.Metric, keys []models.MetricKey) bool {
	current = metricsWithKeys(current, keys)
	desired = metricsWithKeys(desired, keys)
	if len(current) != len(desired) {
		return false
	}
	for index := range desired {
		if current[index].Key != desired[index].Key || current[index].Value != desired[index].Value {
			return false
		}
	}
	return true
}

// metricsWithKeys returns the metrics whose key is one of keys, sorted by key and value.
func metricsWithKeys(metrics []models.Metric, keys []models.MetricKey) []models.Metric {
	result := make([]models.Metric, 0, len(metrics))
	for _, metric := range metrics {
		if slices.Contains(keys, metric.Key) {
			result = append(result, metric)
		}
	}
	sort.Slice(result, func(first int, second int) bool {
		if result[first].Key != result[second].Key {
			return result[first].Key < result[second].Key
		}
		return result[first].Value < result[second].Value
	})
	return result
}

// modelUpdateErrors checks the values the sync takes from RCSB with the rules that
// apply to API submissions. Pointers address the model revision. Synced metrics are
// archive values, so the accession is their provenance and software stays optional.
func modelUpdateErrors(revision models.ModelRevision, metrics []models.Metric) []validation.FieldError {
	fieldErrors := validation.ModelMetadata(validation.Root.Field("metadata"), revision.Metadata)
	return append(fieldErrors, validation.Metrics(validation.Root.Field("metrics"), metrics)...)
}

// metricSoftwareFill is software the archive names for a stored metric that has none.
type metricSoftwareFill struct {
	metricID        uuid.UUID
	softwareName    string
	softwareVersion *string
}

// metricSoftwareFills returns the archive's software for the stored metrics with keys that name
// none, when stored and archive values are the same; it never replaces named software.
func metricSoftwareFills(current []models.Metric, desired []models.Metric, keys []models.MetricKey) []metricSoftwareFill {
	current = metricsWithKeys(current, keys)
	desired = metricsWithKeys(desired, keys)
	if !modelMetricsHaveSameData(current, desired, keys) {
		return nil
	}
	var fills []metricSoftwareFill
	for index, stored := range current {
		archived := desired[index]
		if stored.SoftwareName != nil || archived.SoftwareName == nil {
			continue
		}
		fills = append(fills, metricSoftwareFill{
			metricID:        stored.ID,
			softwareName:    *archived.SoftwareName,
			softwareVersion: archived.SoftwareVersion,
		})
	}
	return fills
}

func (s *modelSyncStrategy) fillMetricSoftware(ctx context.Context, modelID string, fills []metricSoftwareFill) error {
	filled := 0
	for _, fill := range fills {
		updated, err := s.database.Metrics.FillMissingSoftware(ctx, fill.metricID, fill.softwareName, fill.softwareVersion)
		if err != nil {
			return fmt.Errorf("fill software of metric %s: %w", fill.metricID, err)
		}
		if updated {
			filled++
		}
	}
	if filled > 0 {
		s.logger.Info("data sync filled archive metric software", "model_id", modelID, "metrics", filled)
	}
	return nil
}

func applyRCSBModelDetails(revision *models.ModelRevision, details rcsb.EntryDetails) {
	metadata := archivemodels.ModelMetadata(archive.MapModel(details))
	revision.Metadata.Authors = metadata.Authors
	revision.Metadata.Details = metadata.Details
	revision.Metadata.Affiliation = metadata.Affiliation
	revision.Metadata.AtomCount = metadata.AtomCount
	revision.Metadata.ModeledResidues = metadata.ModeledResidues
	revision.Metadata.UniqueProteinChains = metadata.UniqueProteinChains
	revision.Metadata.UnmodeledFraction = metadata.UnmodeledFraction
	revision.Metadata.Ligands = metadata.Ligands
}

func modelMetricsFromRCSB(details rcsb.EntryDetails) []models.Metric {
	return archivemodels.Metrics(archive.MapModel(details))
}

func modelTypeName(modelType *models.StructureModelType) string {
	if modelType == nil {
		return ""
	}
	return string(*modelType)
}
