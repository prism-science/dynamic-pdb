package upload

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"dynamic-pdb/cli/internal/dynamicpdbapi"
	"dynamic-pdb/cli/internal/upload/manifest"
)

// The checks below mirror the API's enforce-mode provenance rules
// (backend/internal/validation/provenance.go and NewEntryMethod), so an upload that
// would get a 422 stops before any file is uploaded.

const (
	runDirectionInput  = "input"
	runDirectionOutput = "output"
)

// runDraft is what the manifest's run block and the coordinates file header say
// about the program that produced a model.
type runDraft struct {
	SoftwareName    string
	SoftwareVersion string
	Artifacts       []dynamicpdbapi.CreateRunArtifactRequest
	// InputsListed and OutputsListed report whether the run block set its lists.
	InputsListed  bool
	OutputsListed bool
}

// newRunDraft takes the program from the coordinates file header and lets each set
// field of the manifest's run block override it. Artifact directions follow the
// run block's lists where set, and the id and format rule of runArtifactDirection
// otherwise.
func newRunDraft(manifestRun *manifest.Run, artifacts []artifactRef, program *modelProgramInfo) runDraft {
	var draft runDraft
	if program != nil {
		draft.SoftwareName = strings.TrimSpace(program.Name)
		draft.SoftwareVersion = strings.TrimSpace(program.Version)
	}
	if manifestRun != nil {
		draft.InputsListed = len(manifestRun.Inputs) > 0
		draft.OutputsListed = len(manifestRun.Outputs) > 0
		if name := strings.TrimSpace(manifestRun.SoftwareName); name != "" {
			draft.SoftwareName = name
		}
		if version := strings.TrimSpace(manifestRun.SoftwareVersion); version != "" {
			draft.SoftwareVersion = version
		}
	}
	for _, artifact := range artifacts {
		direction, ok := runArtifactDirectionFor(manifestRun, artifact)
		if !ok {
			continue
		}
		draft.Artifacts = append(draft.Artifacts, dynamicpdbapi.CreateRunArtifactRequest{
			ArtifactID: artifact.ArtifactID,
			Direction:  direction,
		})
	}
	return draft
}

// runArtifactDirectionFor gives an artifact the direction the run block lists it
// under. A list that is set replaces the default rule for its direction, so the
// default rule only assigns the directions whose list is empty.
func runArtifactDirectionFor(manifestRun *manifest.Run, artifact artifactRef) (string, bool) {
	if manifestRun == nil {
		return runArtifactDirection(artifact)
	}
	manifestID := strings.TrimSpace(artifact.ManifestID)
	if listsArtifact(manifestRun.Inputs, manifestID) {
		return runDirectionInput, true
	}
	if listsArtifact(manifestRun.Outputs, manifestID) {
		return runDirectionOutput, true
	}
	direction, ok := runArtifactDirection(artifact)
	if !ok {
		return "", false
	}
	if direction == runDirectionInput && len(manifestRun.Inputs) > 0 {
		return "", false
	}
	if direction == runDirectionOutput && len(manifestRun.Outputs) > 0 {
		return "", false
	}
	return direction, true
}

func listsArtifact(ids []string, manifestID string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return strings.TrimSpace(id) == manifestID })
}

func (d runDraft) hasDirection(direction string) bool {
	return slices.ContainsFunc(d.Artifacts, func(artifact dynamicpdbapi.CreateRunArtifactRequest) bool {
		return artifact.Direction == direction
	})
}

// runs returns the model's one run when the draft names the program and links an
// input and an output, and no run otherwise. The version may be missing: a
// Deposited model's run does not need one.
func (d runDraft) runs() []dynamicpdbapi.CreateRunRequest {
	if d.SoftwareName == "" || !d.hasDirection(runDirectionInput) || !d.hasDirection(runDirectionOutput) {
		return nil
	}
	softwareName := d.SoftwareName
	return []dynamicpdbapi.CreateRunRequest{
		{
			ID:              uuid.NewString(),
			Name:            softwareName,
			SoftwareName:    &softwareName,
			SoftwareVersion: stringPtr(d.SoftwareVersion),
			Parameters:      map[string]any{},
			Metadata:        map[string]any{},
			Artifacts:       d.Artifacts,
		},
	}
}

// problems lists what keeps the draft from being the run a computed model needs:
// software name and version, at least one input and at least one output.
func (d runDraft) problems() []string {
	var problems []string
	if d.SoftwareName == "" {
		problems = append(problems, "a computed model needs a run, but neither the coordinates file header "+
			"nor run.software_name names the program that produced it; set run.software_name and run.software_version")
	} else if d.SoftwareVersion == "" {
		problems = append(problems, fmt.Sprintf("run %q has no software version, and the coordinates file header "+
			"gives none; set run.software_version", d.SoftwareName))
	}
	if !d.hasDirection(runDirectionInput) {
		if d.InputsListed {
			problems = append(problems, "the run has no input artifact: no artifact in run.inputs has a file for this entry")
		} else {
			problems = append(problems, "the run has no input artifact: no artifact id starts with mtz_ or "+
				"structure_factors_ and no format is mtz, structure_factors_cif, map, ccp4 or mrc; "+
				"list the input artifact ids in run.inputs")
		}
	}
	if !d.hasDirection(runDirectionOutput) {
		if d.OutputsListed {
			problems = append(problems, "the run has no output artifact: no artifact in run.outputs has a file for this entry")
		} else {
			problems = append(problems, "the run has no output artifact; list the output artifact ids in run.outputs")
		}
	}
	return problems
}

// modelMetric is a metric request and whether its value was read from the
// coordinates file, whose run produced it.
type modelMetric struct {
	Request         dynamicpdbapi.CreateMetricRequest
	FromCoordinates bool
}

func metricRequests(metrics []modelMetric) []dynamicpdbapi.CreateMetricRequest {
	requests := make([]dynamicpdbapi.CreateMetricRequest, 0, len(metrics))
	for _, metric := range metrics {
		requests = append(requests, metric.Request)
	}
	return requests
}

// computedModelProblems lists what a computed model lacks: a complete run, and for
// each metric the software that produced it or a link to the run. Only a stated
// Deposited type or archive origin exempts a model, as on the server. A metric read from the
// coordinates file is not reported while the run itself has problems: it links to
// the run once the run is complete.
func computedModelProblems(model manifest.ModelPattern, draft runDraft, metrics []modelMetric) []string {
	if strings.TrimSpace(model.ModelType) == manifest.ModelTypeDeposited ||
		strings.TrimSpace(model.Origin) == manifest.ModelOriginArchive {
		return nil
	}
	problems := draft.problems()
	runComplete := len(problems) == 0
	for _, metric := range metrics {
		if metric.Request.RunID != nil || metric.Request.SoftwareName != nil {
			continue
		}
		if metric.FromCoordinates && !runComplete {
			continue
		}
		problems = append(problems, fmt.Sprintf("metric %q names no software and links to no run; "+
			"set software.name and software.version on its extraction", metric.Request.Key))
	}
	return problems
}

// newEntryProblems lists what a new entry lacks: the API needs its experimental
// method.
func newEntryProblems(properties dynamicpdbapi.EntryProperties) []string {
	if properties.Method == nil || strings.TrimSpace(*properties.Method) == "" {
		return []string{"the entry has no experimental method: no metadata.method extraction of the manifest " +
			"finds one; add one, such as RCSB entry field exptl[0].method"}
	}
	return nil
}
