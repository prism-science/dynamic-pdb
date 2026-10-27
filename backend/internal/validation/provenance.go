package validation

import (
	"fmt"
	"slices"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

// ModelRevision is what the provenance rules need to know about the model revision
// that metrics and runs belong to.
type ModelRevision struct {
	// ModelType is nil when the write does not state it.
	ModelType *models.StructureModelType
	Runs      []ModelRun
}

type ModelRun struct {
	Run       models.Run
	Artifacts []RunArtifactLink
}

type RunArtifactLink struct {
	ArtifactID uuid.UUID
	Direction  models.RunArtifactDirection
}

// IsComputed reports whether the computed-model rules apply. Only a stated Deposited
// type exempts a revision; an unknown type is reported by ModelMetadata.
func (revision ModelRevision) IsComputed() bool {
	if revision.ModelType == nil {
		return true
	}
	computed, err := revision.ModelType.IsComputed()
	return err != nil || computed
}

func (revision ModelRevision) hasRun(runID uuid.UUID) bool {
	return slices.ContainsFunc(revision.Runs, func(run ModelRun) bool { return run.Run.ID == runID })
}

// MetricProvenance checks that each metric of a computed model names the software
// that produced it, or links to the run that did. A run link must point to a run of
// the same model revision, whatever the model type.
func MetricProvenance(at Pointer, metrics []models.Metric, revision ModelRevision) []FieldError {
	var fieldErrors []FieldError
	for index, metric := range metrics {
		metricAt := at.Index(index)
		if metric.RunID != nil {
			if !revision.hasRun(*metric.RunID) {
				fieldErrors = append(fieldErrors, FieldError{
					Pointer: metricAt.Field("run_id"),
					Code:    CodeInvalidValue,
					Detail:  "must be a run of the same model revision",
				})
			}
			continue
		}
		if revision.IsComputed() && len(RequiredText(metricAt.Field("software_name"), metric.SoftwareName)) > 0 {
			fieldErrors = append(fieldErrors, FieldError{
				Pointer: metricAt.Field("software_name"),
				Code:    CodeRequired,
				Detail:  "is required unless run_id names the run that produced the metric",
			})
		}
	}
	return fieldErrors
}

// ModelRuns checks that a computed model records how it was produced: at least one
// run, each naming its software and version, with input and output artifacts.
func ModelRuns(at Pointer, revision ModelRevision) []FieldError {
	if !revision.IsComputed() {
		return nil
	}
	if len(revision.Runs) == 0 {
		return []FieldError{{
			Pointer: at,
			Code:    CodeRequired,
			Detail:  "a computed model needs at least one run that records its software, version, input and output",
		}}
	}
	var fieldErrors []FieldError
	for index, run := range revision.Runs {
		fieldErrors = append(fieldErrors, computedRunErrors(at.Index(index), run)...)
	}
	return fieldErrors
}

// RunArtifactScope lists the artifacts a model revision's runs may link. Inputs may
// come from the model revision or from its entry, such as structure factors deposited
// with the entry; outputs only from the model revision.
type RunArtifactScope struct {
	ModelArtifactIDs []uuid.UUID
	EntryArtifactIDs []uuid.UUID
}

// RunArtifactLinks checks that every run, whatever the model type, links only
// artifacts in its scope. Unknown directions are reported by RunArtifact.
func RunArtifactLinks(at Pointer, runs []ModelRun, scope RunArtifactScope) []FieldError {
	var fieldErrors []FieldError
	for index, run := range runs {
		for linkIndex, link := range run.Artifacts {
			if link.ArtifactID == uuid.Nil {
				continue
			}
			inModel := slices.Contains(scope.ModelArtifactIDs, link.ArtifactID)
			inEntry := slices.Contains(scope.EntryArtifactIDs, link.ArtifactID)
			var detail string
			switch link.Direction {
			case models.RunArtifactDirectionInput:
				if !inModel && !inEntry {
					detail = "must be an artifact of the same model revision or of its entry"
				}
			case models.RunArtifactDirectionOutput:
				if !inModel {
					detail = "must be an artifact of the same model revision"
				}
			default:
				fieldErrors = append(fieldErrors, RunArtifact(at.Index(index).Field("artifacts").Index(linkIndex), link.Direction)...)
				continue
			}
			if detail != "" {
				fieldErrors = append(fieldErrors, FieldError{
					Pointer: at.Index(index).Field("artifacts").Index(linkIndex).Field("artifact_id"),
					Code:    CodeInvalidValue,
					Detail:  detail,
				})
			}
		}
	}
	return fieldErrors
}

// KeptMetricRuns checks a revision change that replaces the runs and keeps the
// metrics: a kept metric's run_id must still name one of the new runs.
func KeptMetricRuns(at Pointer, keptMetrics []models.Metric, revision ModelRevision) []FieldError {
	var fieldErrors []FieldError
	for _, metric := range keptMetrics {
		if metric.RunID == nil || revision.hasRun(*metric.RunID) {
			continue
		}
		fieldErrors = append(fieldErrors, FieldError{
			Pointer: at,
			Code:    CodeInvalidValue,
			Detail: fmt.Sprintf("leaves out run %s, which the kept %s metric names as its producer; "+
				"send the metrics with the runs", metric.RunID, metric.Key),
		})
	}
	return fieldErrors
}

func computedRunErrors(at Pointer, run ModelRun) []FieldError {
	fieldErrors := RequiredText(at.Field("software_name"), run.Run.SoftwareName)
	fieldErrors = append(fieldErrors, RequiredText(at.Field("software_version"), run.Run.SoftwareVersion)...)
	for _, direction := range models.RunArtifactDirections {
		hasDirection := slices.ContainsFunc(run.Artifacts, func(link RunArtifactLink) bool {
			return link.Direction == direction
		})
		if !hasDirection {
			fieldErrors = append(fieldErrors, FieldError{
				Pointer: at.Field("artifacts"),
				Code:    CodeRequired,
				Detail:  "needs at least one " + string(direction) + " artifact",
			})
		}
	}
	return fieldErrors
}
