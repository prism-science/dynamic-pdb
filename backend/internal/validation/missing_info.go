package validation

import (
	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

// MissingInfoKind names information that a record written before the provenance
// rules may lack. Such records stay public and are labelled "information missing"
// (researcher answer Q5).
type MissingInfoKind string

const (
	// MissingInfoMetricSoftware: a metric of a computed model names neither the
	// software nor the run that produced it.
	MissingInfoMetricSoftware MissingInfoKind = "metric_software"
	// MissingInfoRunSoftwareVersion: a run of a computed model has no software version.
	MissingInfoRunSoftwareVersion MissingInfoKind = "run_software_version"
	// MissingInfoModelRun: a computed model has no run.
	MissingInfoModelRun MissingInfoKind = "model_run"
)

// MissingInfo is one gap. MetricID is set for MissingInfoMetricSoftware and RunID
// for MissingInfoRunSoftwareVersion.
type MissingInfo struct {
	Kind     MissingInfoKind
	MetricID *uuid.UUID
	RunID    *uuid.UUID
}

// MissingInfoOf lists what a stored model revision lacks that a write would now be
// rejected for. It runs the write rules themselves, MetricProvenance and ModelRuns,
// so the label and enforce mode cannot disagree. Only the three Q5 gaps are kept; the
// other errors those rules report, such as a run link outside the revision or a run
// without software or files, are not labelled.
func MissingInfoOf(revision ModelRevision, metrics []models.Metric) []MissingInfo {
	var gaps []MissingInfo

	metricRequired := requiredPointers(MetricProvenance(Root, metrics, revision))
	for index, metric := range metrics {
		if metricRequired[Root.Index(index).Field("software_name")] {
			gaps = append(gaps, MissingInfo{Kind: MissingInfoMetricSoftware, MetricID: &metric.ID})
		}
	}

	runRequired := requiredPointers(ModelRuns(Root, revision))
	for index, run := range revision.Runs {
		if runRequired[Root.Index(index).Field("software_version")] {
			gaps = append(gaps, MissingInfo{Kind: MissingInfoRunSoftwareVersion, RunID: &run.Run.ID})
		}
	}
	if runRequired[Root] {
		gaps = append(gaps, MissingInfo{Kind: MissingInfoModelRun})
	}
	return gaps
}

func requiredPointers(fieldErrors []FieldError) map[Pointer]bool {
	required := make(map[Pointer]bool, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		if fieldError.Code == CodeRequired {
			required[fieldError.Pointer] = true
		}
	}
	return required
}
