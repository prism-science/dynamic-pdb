package validation

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_report_no_missing_info_when_model_is_deposited(t *testing.T) {
	// given
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeDeposited),
		Runs:      []ModelRun{{Run: models.Run{ID: uuid.New(), SoftwareName: new("REFMAC")}}},
	}
	metrics := []models.Metric{{ID: uuid.New(), Key: models.MetricKeyRFree, Value: 0.21}}

	// when
	gaps := MissingInfoOf(revision, metrics)

	// then
	assert.Empty(t, gaps)
}

func Test_should_report_no_missing_info_when_deposited_model_has_no_runs(t *testing.T) {
	// given
	revision := ModelRevision{ModelType: new(models.StructureModelTypeDeposited)}

	// when
	gaps := MissingInfoOf(revision, nil)

	// then
	assert.Empty(t, gaps)
}

func Test_should_report_metric_software_when_metric_of_computed_model_names_no_software_or_run(t *testing.T) {
	// given
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeMulticonformer),
		Runs:      []ModelRun{completeRun()},
	}
	unnamed := models.Metric{ID: uuid.New(), Key: models.MetricKeyRFree, Value: 0.21}
	blank := models.Metric{ID: uuid.New(), Key: models.MetricKeyRWork, Value: 0.18, SoftwareName: new("  ")}
	named := models.Metric{ID: uuid.New(), Key: models.MetricKeyClashscore, Value: 3.1, SoftwareName: new("MolProbity")}

	// when
	gaps := MissingInfoOf(revision, []models.Metric{unnamed, blank, named})

	// then
	assert.Equal(t, []MissingInfo{
		{Kind: MissingInfoMetricSoftware, MetricID: &unnamed.ID},
		{Kind: MissingInfoMetricSoftware, MetricID: &blank.ID},
	}, gaps)
}

func Test_should_report_no_metric_software_when_metric_links_a_run_of_the_revision(t *testing.T) {
	// given
	run := completeRun()
	revision := ModelRevision{ModelType: new(models.StructureModelTypeEnsemble), Runs: []ModelRun{run}}
	metrics := []models.Metric{{ID: uuid.New(), Key: models.MetricKeyRFree, Value: 0.21, RunID: &run.Run.ID}}

	// when
	gaps := MissingInfoOf(revision, metrics)

	// then
	assert.Empty(t, gaps)
}

func Test_should_report_no_metric_software_when_metric_links_a_run_outside_the_revision(t *testing.T) {
	// given
	// A wrong run link is an invalid value, not missing information.
	revision := ModelRevision{ModelType: new(models.StructureModelTypeEnsemble), Runs: []ModelRun{completeRun()}}
	metrics := []models.Metric{{ID: uuid.New(), Key: models.MetricKeyRFree, Value: 0.21, RunID: new(uuid.New())}}

	// when
	gaps := MissingInfoOf(revision, metrics)

	// then
	assert.Empty(t, gaps)
}

func Test_should_report_run_software_version_when_run_of_computed_model_has_none(t *testing.T) {
	// given
	missing, blank, complete := completeRun(), completeRun(), completeRun()
	missing.Run.SoftwareVersion = nil
	blank.Run.SoftwareVersion = new(" ")
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeSingleConformer),
		Runs:      []ModelRun{missing, blank, complete},
	}

	// when
	gaps := MissingInfoOf(revision, nil)

	// then
	assert.Equal(t, []MissingInfo{
		{Kind: MissingInfoRunSoftwareVersion, RunID: &missing.Run.ID},
		{Kind: MissingInfoRunSoftwareVersion, RunID: &blank.Run.ID},
	}, gaps)
}

func Test_should_report_no_run_software_version_when_deposited_model_run_has_none(t *testing.T) {
	// given
	run := completeRun()
	run.Run.SoftwareVersion = nil
	revision := ModelRevision{ModelType: new(models.StructureModelTypeDeposited), Runs: []ModelRun{run}}

	// when
	gaps := MissingInfoOf(revision, nil)

	// then
	assert.Empty(t, gaps)
}

func Test_should_report_only_q5_gaps_when_run_also_lacks_software_and_files(t *testing.T) {
	// given
	// The write rules also require a run's software name and its input and output
	// files; those are not part of the label.
	run := ModelRun{Run: models.Run{ID: uuid.New()}}
	revision := ModelRevision{ModelType: new(models.StructureModelTypeMulticonformer), Runs: []ModelRun{run}}

	// when
	gaps := MissingInfoOf(revision, nil)

	// then
	assert.Equal(t, []MissingInfo{{Kind: MissingInfoRunSoftwareVersion, RunID: &run.Run.ID}}, gaps)
}

func Test_should_report_model_run_when_computed_model_has_no_run(t *testing.T) {
	// given
	revision := ModelRevision{ModelType: new(models.StructureModelTypeEnsemble)}

	// when
	gaps := MissingInfoOf(revision, nil)

	// then
	assert.Equal(t, []MissingInfo{{Kind: MissingInfoModelRun}}, gaps)
}

func Test_should_report_no_missing_info_when_computed_model_is_fully_described(t *testing.T) {
	// given
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeMulticonformer),
		Runs:      []ModelRun{completeRun()},
	}
	metrics := []models.Metric{{ID: uuid.New(), Key: models.MetricKeyRFree, Value: 0.21, SoftwareName: new("PHENIX")}}

	// when
	gaps := MissingInfoOf(revision, metrics)

	// then
	assert.Empty(t, gaps)
}

func Test_should_treat_model_as_computed_when_model_type_is_missing_or_unknown(t *testing.T) {
	// given
	metric := models.Metric{ID: uuid.New(), Key: models.MetricKeyRFree, Value: 0.21}
	untyped := ModelRevision{}
	unknown := ModelRevision{ModelType: new(models.StructureModelType("Docked"))}

	// when
	untypedGaps := MissingInfoOf(untyped, []models.Metric{metric})
	unknownGaps := MissingInfoOf(unknown, []models.Metric{metric})

	// then
	expected := []MissingInfo{
		{Kind: MissingInfoMetricSoftware, MetricID: &metric.ID},
		{Kind: MissingInfoModelRun},
	}
	assert.Equal(t, expected, untypedGaps)
	assert.Equal(t, expected, unknownGaps)
}

// completeRun is a run that meets every write rule for a computed model.
func completeRun() ModelRun {
	return ModelRun{
		Run: models.Run{ID: uuid.New(), SoftwareName: new("qFit"), SoftwareVersion: new("2024.3")},
		Artifacts: []RunArtifactLink{
			{ArtifactID: uuid.New(), Direction: models.RunArtifactDirectionInput},
			{ArtifactID: uuid.New(), Direction: models.RunArtifactDirectionOutput},
		},
	}
}
