package httpapi

import (
	"context"
	"fmt"
	"log/slog"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

// missingInfoForRevision lists what a stored revision lacks that a write would now be
// rejected for (researcher answer Q5). It only feeds a label, so a failure is logged and
// nil returned: the read goes on without the field and never fails because of it.
func (s *Server) missingInfoForRevision(
	ctx context.Context,
	revision domainmodels.ModelRevision,
	metrics []domainmodels.Metric,
) *[]MissingInfo {
	runs, err := s.database.Runs.List(ctx, db.RunFilters{ModelRevisionID: &revision.ID})
	if err != nil {
		slog.ErrorContext(ctx, "list runs for missing info failed", "err", err, "model_revision_id", revision.ID)
		return nil
	}
	// Run files are not loaded: no Q5 gap depends on them.
	modelRuns := make([]validation.ModelRun, 0, len(runs))
	for _, run := range runs {
		modelRuns = append(modelRuns, validation.ModelRun{Run: run})
	}
	gaps := validation.MissingInfoOf(validation.ModelRevision{ModelType: revision.Metadata.ModelType, Runs: modelRuns}, metrics)
	items, err := missingInfoFromValidation(gaps)
	if err != nil {
		slog.ErrorContext(ctx, "build missing info failed", "err", err, "model_revision_id", revision.ID)
		return nil
	}
	return &items
}

func missingInfoFromValidation(gaps []validation.MissingInfo) ([]MissingInfo, error) {
	items := make([]MissingInfo, 0, len(gaps))
	for _, gap := range gaps {
		var kind MissingInfoKind
		switch gap.Kind {
		case validation.MissingInfoMetricSoftware:
			kind = MissingInfoKindMetricSoftware
		case validation.MissingInfoRunSoftwareVersion:
			kind = MissingInfoKindRunSoftwareVersion
		case validation.MissingInfoModelRun:
			kind = MissingInfoKindModelRun
		default:
			return nil, fmt.Errorf("unknown missing info kind: %s", gap.Kind)
		}
		items = append(items, MissingInfo{Kind: kind, MetricId: gap.MetricID, RunId: gap.RunID})
	}
	return items, nil
}
