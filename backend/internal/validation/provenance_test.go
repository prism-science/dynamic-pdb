package validation

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"dynamic-pdb/backend/internal/models"
)

func Test_should_require_software_when_metric_of_computed_model_names_no_run(t *testing.T) {
	// given
	revision := ModelRevision{ModelType: new(models.StructureModelTypeMulticonformer)}
	metrics := []models.Metric{
		{Key: models.MetricKeyRFree, Value: 0.21},
		{Key: models.MetricKeyRWork, Value: 0.18, SoftwareName: new("  ")},
		{Key: models.MetricKeyClashscore, Value: 3.1, SoftwareName: new("MolProbity")},
	}

	// when
	fieldErrors := MetricProvenance(Root.Field("metrics"), metrics, revision)

	// then
	detail := "is required unless run_id names the run that produced the metric"
	assert.Equal(t, []FieldError{
		{Pointer: "/metrics/0/software_name", Code: CodeRequired, Detail: detail},
		{Pointer: "/metrics/1/software_name", Code: CodeRequired, Detail: detail},
	}, fieldErrors)
}

func Test_should_accept_metric_without_software_when_run_id_names_a_run_of_the_revision(t *testing.T) {
	// given
	run := models.Run{ID: uuid.New()}
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeEnsemble),
		Runs:      []ModelRun{{Run: run}},
	}
	metrics := []models.Metric{{Key: models.MetricKeyRFree, Value: 0.21, RunID: &run.ID}}

	// when
	fieldErrors := MetricProvenance(Root.Field("metrics"), metrics, revision)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_metric_when_run_id_names_a_run_outside_the_revision(t *testing.T) {
	// given
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeEnsemble),
		Runs:      []ModelRun{{Run: models.Run{ID: uuid.New()}}},
	}
	metrics := []models.Metric{{
		Key:          models.MetricKeyRFree,
		Value:        0.21,
		SoftwareName: new("PHENIX"),
		RunID:        new(uuid.New()),
	}}

	// when
	fieldErrors := MetricProvenance(Root.Field("metrics"), metrics, revision)

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/metrics/0/run_id",
		Code:    CodeInvalidValue,
		Detail:  "must be a run of the same model revision",
	}}, fieldErrors)
}

func Test_should_accept_metric_without_software_when_model_is_deposited(t *testing.T) {
	// given
	revision := ModelRevision{ModelType: new(models.StructureModelTypeDeposited)}
	metrics := []models.Metric{{Key: models.MetricKeyRFree, Value: 0.21}}

	// when
	fieldErrors := MetricProvenance(Root.Field("metrics"), metrics, revision)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_require_metric_software_when_model_type_is_missing_or_unknown(t *testing.T) {
	// given
	metrics := []models.Metric{{Key: models.MetricKeyRFree, Value: 0.21}}

	// when
	missingTypeErrors := MetricProvenance(Root.Field("metrics"), metrics, ModelRevision{})
	unknownTypeErrors := MetricProvenance(Root.Field("metrics"), metrics,
		ModelRevision{ModelType: new(models.StructureModelType("Docked"))})

	// then
	assert.Equal(t, []Pointer{"/metrics/0/software_name"}, pointersOf(missingTypeErrors))
	assert.Equal(t, []Pointer{"/metrics/0/software_name"}, pointersOf(unknownTypeErrors))
}

func Test_should_require_a_run_when_model_is_computed(t *testing.T) {
	// given
	revision := ModelRevision{ModelType: new(models.StructureModelTypeSingleConformer)}

	// when
	fieldErrors := ModelRuns(Root.Field("model").Field("runs"), revision)

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/model/runs",
		Code:    CodeRequired,
		Detail:  "a computed model needs at least one run that records its software, version, input and output",
	}}, fieldErrors)
}

func Test_should_accept_model_without_runs_when_type_deposited(t *testing.T) {
	// given
	revision := ModelRevision{ModelType: new(models.StructureModelTypeDeposited)}

	// when
	fieldErrors := ModelRuns(Root.Field("model").Field("runs"), revision)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_accept_run_when_computed_model_records_software_version_input_and_output(t *testing.T) {
	// given
	input, output := uuid.New(), uuid.New()
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeMulticonformer),
		Runs: []ModelRun{{
			Run: models.Run{ID: uuid.New(), SoftwareName: new("qFit"), SoftwareVersion: new("2024.3")},
			Artifacts: []RunArtifactLink{
				{ArtifactID: input, Direction: models.RunArtifactDirectionInput},
				{ArtifactID: output, Direction: models.RunArtifactDirectionOutput},
			},
		}},
	}

	// when
	fieldErrors := ModelRuns(Root.Field("runs"), revision)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_require_software_version_input_and_output_when_run_belongs_to_computed_model(t *testing.T) {
	// given
	output := uuid.New()
	revision := ModelRevision{
		ModelType: new(models.StructureModelTypeMulticonformer),
		Runs: []ModelRun{
			{Run: models.Run{ID: uuid.New(), SoftwareVersion: new(" ")}},
			{
				Run:       models.Run{ID: uuid.New(), SoftwareName: new("qFit"), SoftwareVersion: new("2024.3")},
				Artifacts: []RunArtifactLink{{ArtifactID: output, Direction: models.RunArtifactDirectionOutput}},
			},
		},
	}

	// when
	fieldErrors := ModelRuns(Root.Field("runs"), revision)

	// then
	assert.Equal(t, []FieldError{
		{Pointer: "/runs/0/software_name", Code: CodeRequired, Detail: "is required"},
		{Pointer: "/runs/0/software_version", Code: CodeRequired, Detail: "must not be blank"},
		{Pointer: "/runs/0/artifacts", Code: CodeRequired, Detail: "needs at least one input artifact"},
		{Pointer: "/runs/0/artifacts", Code: CodeRequired, Detail: "needs at least one output artifact"},
		{Pointer: "/runs/1/artifacts", Code: CodeRequired, Detail: "needs at least one input artifact"},
	}, fieldErrors)
}

func Test_should_accept_run_input_when_artifact_belongs_to_the_entry(t *testing.T) {
	// given
	entryFile, modelFile := uuid.New(), uuid.New()
	scope := RunArtifactScope{ModelArtifactIDs: []uuid.UUID{modelFile}, EntryArtifactIDs: []uuid.UUID{entryFile}}
	runs := []ModelRun{{
		Run: models.Run{ID: uuid.New()},
		Artifacts: []RunArtifactLink{
			{ArtifactID: entryFile, Direction: models.RunArtifactDirectionInput},
			{ArtifactID: modelFile, Direction: models.RunArtifactDirectionInput},
			{ArtifactID: modelFile, Direction: models.RunArtifactDirectionOutput},
		},
	}}

	// when
	fieldErrors := RunArtifactLinks(Root.Field("runs"), runs, scope)

	// then
	assert.Empty(t, fieldErrors)
}

func Test_should_reject_run_artifact_when_artifact_is_outside_its_scope(t *testing.T) {
	// given
	entryFile := uuid.New()
	scope := RunArtifactScope{EntryArtifactIDs: []uuid.UUID{entryFile}}
	runs := []ModelRun{{
		Run: models.Run{ID: uuid.New()},
		Artifacts: []RunArtifactLink{
			{ArtifactID: uuid.New(), Direction: models.RunArtifactDirectionInput},
			{ArtifactID: entryFile, Direction: models.RunArtifactDirectionOutput},
			{ArtifactID: uuid.New(), Direction: "sideways"},
			{ArtifactID: uuid.Nil, Direction: models.RunArtifactDirectionOutput},
		},
	}}

	// when
	fieldErrors := RunArtifactLinks(Root.Field("runs"), runs, scope)

	// then
	assert.Equal(t, []FieldError{
		{
			Pointer: "/runs/0/artifacts/0/artifact_id",
			Code:    CodeInvalidValue,
			Detail:  "must be an artifact of the same model revision or of its entry",
		},
		{
			Pointer: "/runs/0/artifacts/1/artifact_id",
			Code:    CodeInvalidValue,
			Detail:  "must be an artifact of the same model revision",
		},
		{
			Pointer: "/runs/0/artifacts/2/direction",
			Code:    CodeInvalidValue,
			Detail:  `run artifact direction "sideways" is not one of "input", "output"`,
		},
	}, fieldErrors)
}

func Test_should_report_kept_metric_when_revision_change_drops_the_run_it_names(t *testing.T) {
	// given
	keptRun, droppedRun := uuid.New(), uuid.New()
	revision := ModelRevision{Runs: []ModelRun{{Run: models.Run{ID: keptRun}}}}
	keptMetrics := []models.Metric{
		{Key: models.MetricKeyRFree, Value: 0.21, RunID: &droppedRun},
		{Key: models.MetricKeyRWork, Value: 0.18, RunID: &keptRun},
		{Key: models.MetricKeyClashscore, Value: 3.1},
	}

	// when
	fieldErrors := KeptMetricRuns(Root.Field("model").Field("runs"), keptMetrics, revision)

	// then
	assert.Equal(t, []FieldError{{
		Pointer: "/model/runs",
		Code:    CodeInvalidValue,
		Detail: "leaves out run " + droppedRun.String() +
			", which the kept r_free metric names as its producer; send the metrics with the runs",
	}}, fieldErrors)
}

func Test_should_tell_archive_import_from_computed_model_when_revision_type_is_checked(t *testing.T) {
	// given
	deposited := ModelRevision{ModelType: new(models.StructureModelTypeDeposited)}
	ensemble := ModelRevision{ModelType: new(models.StructureModelTypeEnsemble)}
	untyped := ModelRevision{}

	// when
	depositedComputed, ensembleComputed, untypedComputed := deposited.IsComputed(), ensemble.IsComputed(), untyped.IsComputed()

	// then
	assert.False(t, depositedComputed)
	assert.True(t, ensembleComputed)
	assert.True(t, untypedComputed)
}
