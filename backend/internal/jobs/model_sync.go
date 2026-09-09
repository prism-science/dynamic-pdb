package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
)

type modelSyncStrategy struct {
	database   *db.DB
	rcsbClient *rcsb.RemoteClient
	logger     *slog.Logger
	now        func() time.Time
}

func newModelSyncStrategy(
	database *db.DB,
	rcsbClient *rcsb.RemoteClient,
	logger *slog.Logger,
	now func() time.Time,
) *modelSyncStrategy {
	return &modelSyncStrategy{
		database:   database,
		rcsbClient: rcsbClient,
		logger:     logger,
		now:        now,
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

	modelRevisionChanged, metricsChanged := compareModelUpdate(
		*currentRevision,
		desiredRevision,
		currentMetrics,
		desiredMetrics,
	)
	if !modelRevisionChanged && !metricsChanged {
		return nil
	}

	if err := s.saveModelUpdate(
		ctx,
		*currentRevision,
		desiredRevision,
		currentMetrics,
		desiredMetrics,
		metricsChanged,
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
	return desiredRevision, modelMetricsFromRCSB(details), nil
}

func compareModelUpdate(
	currentRevision models.ModelRevision,
	desiredRevision models.ModelRevision,
	currentMetrics []models.Metric,
	desiredMetrics []models.Metric,
) (bool, bool) {
	modelRevisionChanged := !reflect.DeepEqual(currentRevision.Metadata.Authors, desiredRevision.Metadata.Authors) ||
		!reflect.DeepEqual(currentRevision.Metadata.Details, desiredRevision.Metadata.Details) ||
		!reflect.DeepEqual(currentRevision.Metadata.Affiliation, desiredRevision.Metadata.Affiliation) ||
		!reflect.DeepEqual(currentRevision.Metadata.AtomCount, desiredRevision.Metadata.AtomCount) ||
		!reflect.DeepEqual(currentRevision.Metadata.ModeledResidues, desiredRevision.Metadata.ModeledResidues) ||
		!reflect.DeepEqual(currentRevision.Metadata.UniqueProteinChains, desiredRevision.Metadata.UniqueProteinChains) ||
		!reflect.DeepEqual(currentRevision.Metadata.UnmodeledFraction, desiredRevision.Metadata.UnmodeledFraction) ||
		!reflect.DeepEqual(currentRevision.Metadata.Ligands, desiredRevision.Metadata.Ligands)
	metricsChanged := !modelMetricsHaveSameData(currentMetrics, desiredMetrics)
	return modelRevisionChanged, metricsChanged
}

func (s *modelSyncStrategy) saveModelUpdate(
	ctx context.Context,
	currentRevision models.ModelRevision,
	desiredRevision models.ModelRevision,
	currentMetrics []models.Metric,
	desiredMetrics []models.Metric,
	metricsChanged bool,
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
			currentMetrics,
			desiredMetrics,
			metricsChanged,
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
	currentMetrics []models.Metric,
	desiredMetrics []models.Metric,
	metricsChanged bool,
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

	for _, metric := range currentMetrics {
		if isRCSBModelMetric(metric.Key) {
			continue
		}
		if err := s.database.Metrics.AttachToModelRevision(ctx, toRevisionID, metric.ID); err != nil {
			return fmt.Errorf("copy metric %s: %w", metric.Key, err)
		}
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

func applyRCSBModelDetails(revision *models.ModelRevision, details rcsb.EntryDetails) {
	var authors []string
	for _, sourceAuthor := range details.Authors {
		authors = appendUniqueModelMetadataValue(authors, sourceAuthor.Name)
	}

	var affiliations []string
	for _, sourceAffiliation := range details.Publication.Affiliations {
		affiliations = appendUniqueModelMetadataValue(affiliations, sourceAffiliation)
	}

	var ligands []string
	for _, sourceLigand := range details.Info.NonpolymerBoundComponents {
		for _, value := range strings.FieldsFunc(sourceLigand, func(character rune) bool {
			return character == ',' || character == ';'
		}) {
			ligand := strings.ToUpper(strings.TrimSpace(value))
			if ligand == "HOH" || ligand == "DOD" || ligand == "WAT" || ligand == "H2O" {
				continue
			}
			ligands = appendUniqueModelMetadataValue(ligands, ligand)
		}
	}

	revision.Metadata.Authors = authors
	revision.Metadata.Details = optionalString(details.Structure.ModelDetails)
	revision.Metadata.Affiliation = optionalString(strings.Join(affiliations, "; "))
	revision.Metadata.AtomCount = details.Info.DepositedAtomCount
	revision.Metadata.ModeledResidues = details.Info.DepositedModeledPolymerMonomerCount
	revision.Metadata.UniqueProteinChains = details.Info.DepositedPolymerEntityInstanceCount
	revision.Metadata.UnmodeledFraction = modelUnmodeledFraction(details.Info)
	revision.Metadata.Ligands = ligands
}

func modelUnmodeledFraction(info rcsb.EntryInfo) *float64 {
	if info.DepositedPolymerMonomerCount == nil || *info.DepositedPolymerMonomerCount <= 0 ||
		info.DepositedUnmodeledPolymerMonomerCount == nil {
		return nil
	}
	fraction := float64(*info.DepositedUnmodeledPolymerMonomerCount) /
		float64(*info.DepositedPolymerMonomerCount)
	return &fraction
}

func appendUniqueModelMetadataValue(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func modelMetricsFromRCSB(details rcsb.EntryDetails) []models.Metric {
	metrics := make([]models.Metric, 0, 4)
	if len(details.Refinements) > 0 {
		appendModelMetric(&metrics, models.MetricKeyRFree, details.Refinements[0].RFree)
		appendModelMetric(&metrics, models.MetricKeyRWork, details.Refinements[0].RWork)
	}
	if len(details.ValidationGeometry) > 0 {
		appendModelMetric(&metrics, models.MetricKeyClashscore, details.ValidationGeometry[0].Clashscore)
		appendModelMetric(
			&metrics,
			models.MetricKeyRamachandranOutliers,
			details.ValidationGeometry[0].RamachandranOutliersPercent,
		)
	}
	sort.Slice(metrics, func(first int, second int) bool {
		return metrics[first].Key < metrics[second].Key
	})
	return metrics
}

func appendModelMetric(metrics *[]models.Metric, key models.MetricKey, value *float64) {
	if value == nil {
		return
	}
	*metrics = append(*metrics, models.Metric{Key: key, Value: *value})
}

func modelMetricsHaveSameData(current []models.Metric, desired []models.Metric) bool {
	current = rcsbModelMetrics(current)
	desired = rcsbModelMetrics(desired)
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

func rcsbModelMetrics(metrics []models.Metric) []models.Metric {
	result := make([]models.Metric, 0, len(metrics))
	for _, metric := range metrics {
		if isRCSBModelMetric(metric.Key) {
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

func isRCSBModelMetric(key models.MetricKey) bool {
	switch key {
	case models.MetricKeyRFree,
		models.MetricKeyRWork,
		models.MetricKeyRamachandranOutliers,
		models.MetricKeyClashscore:
		return true
	default:
		return false
	}
}
