package manifest

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ValidationError lists every problem Validate found, each starting with the YAML
// path of the manifest value it is about.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	noun := "problems"
	if len(e.Problems) == 1 {
		noun = "problem"
	}
	return fmt.Sprintf("manifest has %d %s; nothing was uploaded:\n  %s", len(e.Problems), noun, strings.Join(e.Problems, "\n  "))
}

// Validate checks what can be checked without reading any data file: the closed
// vocabularies the API enforces (metric keys, artifact levels and types, model types
// and purposes) and the shape of the software and run fields. It returns a
// *ValidationError listing every problem, or nil.
func Validate(manifest Manifest) error {
	var problems []string
	for entryIndex, entry := range manifest.Entries {
		entryPath := fmt.Sprintf("entries[%d]", entryIndex)
		problems = append(problems, softwareOutsideMetrics(entryPath+".metadata", entry.Metadata)...)
		for artifactIndex, artifact := range entry.Artifacts {
			problems = append(problems, artifactProblems(fmt.Sprintf("%s.artifacts[%d]", entryPath, artifactIndex), artifact)...)
		}
		for modelIndex, model := range entry.Models {
			problems = append(problems, modelProblems(fmt.Sprintf("%s.models[%d]", entryPath, modelIndex), model)...)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: problems}
}

func modelProblems(path string, model ModelPattern) []string {
	var problems []string
	problems = append(problems, vocabularyProblem(path+".model_type", "model type", model.ModelType, ModelTypes)...)
	problems = append(problems, vocabularyProblem(path+".purpose", "purpose", model.Purpose, ModelPurposes)...)
	problems = append(problems, vocabularyProblem(path+".origin", "model origin", model.Origin, ModelOrigins)...)
	problems = append(problems, softwareOutsideMetrics(path+".metadata", model.Metadata)...)
	for artifactIndex, artifact := range model.Artifacts {
		problems = append(problems, artifactProblems(fmt.Sprintf("%s.artifacts[%d]", path, artifactIndex), artifact)...)
	}
	for _, key := range sortedKeys(model.Metrics) {
		metricPath := path + ".metrics." + key
		if !slices.Contains(MetricKeys, key) {
			problems = append(problems, fmt.Sprintf("%s: unknown metric key %q; use one of %s", metricPath, key, strings.Join(MetricKeys, ", ")))
		}
		for extractionIndex, extraction := range model.Metrics[key] {
			if extraction.Software != nil && strings.TrimSpace(extraction.Software.Name) == "" {
				problems = append(problems, fmt.Sprintf("%s[%d].software.name: is required when software is set", metricPath, extractionIndex))
			}
		}
	}
	if model.Run != nil {
		problems = append(problems, runProblems(path+".run", *model.Run, model.Artifacts)...)
	}
	return problems
}

// runProblems checks that the run lists only artifacts of its model, each in one
// direction.
func runProblems(path string, run Run, artifacts []Artifact) []string {
	artifactIDs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifactIDs = append(artifactIDs, strings.TrimSpace(artifact.ID))
	}
	var problems []string
	lists := []struct {
		name string
		ids  []string
	}{
		{"inputs", run.Inputs},
		{"outputs", run.Outputs},
	}
	for _, list := range lists {
		for index, id := range list.ids {
			if !slices.Contains(artifactIDs, strings.TrimSpace(id)) {
				problems = append(problems, fmt.Sprintf("%s.%s[%d]: the model has no artifact with id %q", path, list.name, index, id))
			}
		}
	}
	for _, id := range run.Inputs {
		if slices.Contains(run.Outputs, id) {
			problems = append(problems, fmt.Sprintf("%s: artifact %q is listed in both inputs and outputs", path, id))
		}
	}
	return problems
}

func artifactProblems(path string, artifact Artifact) []string {
	problems := vocabularyProblem(path+".level", "artifact level", artifact.Level, ArtifactLevels)
	return append(problems, vocabularyProblem(path+".type", "artifact type", artifact.Type, ArtifactTypes)...)
}

// vocabularyProblem reports a set value outside allowed. An empty value is left to
// the CLI's defaults.
func vocabularyProblem(path string, name string, value string, allowed []string) []string {
	value = strings.TrimSpace(value)
	if value == "" || slices.Contains(allowed, value) {
		return nil
	}
	return []string{fmt.Sprintf("%s: unknown %s %q; use one of %s", path, name, value, strings.Join(allowed, ", "))}
}

// softwareOutsideMetrics reports software set on a metadata extraction: only a
// metric value has a producing program.
func softwareOutsideMetrics(path string, fields map[string][]FieldExtraction) []string {
	var problems []string
	for _, key := range sortedKeys(fields) {
		for index, extraction := range fields[key] {
			if extraction.Software != nil {
				problems = append(problems, fmt.Sprintf("%s.%s[%d].software: only metric extractions may name software", path, key, index))
			}
		}
	}
	return problems
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
