package manifest

import (
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const SampleWorksDefaultFilename = DefaultFilename

type SampleWorksManifestBuilder struct{}

func InitSampleWorks(dataRoot string, outputPath string) (string, Manifest, Stats, error) {
	if strings.TrimSpace(outputPath) == "" {
		outputPath = filepath.Join(dataRoot, SampleWorksDefaultFilename)
	}
	outputPath, err := filepath.Abs(outputPath)
	if err != nil {
		return "", Manifest{}, Stats{}, fmt.Errorf("resolve output path: %w", err)
	}
	uploadManifest, stats, err := SampleWorksManifestBuilder{}.Build(dataRoot)
	if err != nil {
		return "", Manifest{}, Stats{}, err
	}
	encoded, err := yaml.Marshal(uploadManifest)
	if err != nil {
		return "", Manifest{}, Stats{}, fmt.Errorf("encode Sampleworks manifest YAML: %w", err)
	}
	if err := os.WriteFile(outputPath, encoded, 0o600); err != nil {
		return "", Manifest{}, Stats{}, fmt.Errorf("write Sampleworks manifest: %w", err)
	}
	return outputPath, uploadManifest, stats, nil
}

func (SampleWorksManifestBuilder) Build(dataRoot string) (Manifest, Stats, error) {
	patterns, runs, fileCount, ok, err := detectSampleWorks(dataRoot)
	if err != nil {
		return Manifest{}, Stats{}, err
	}
	if !ok {
		return Manifest{}, Stats{}, errors.New("folder does not look like Sampleworks: no result folders matched")
	}
	pdbIDs := sampleWorksPDBIDs(runs)
	return Manifest{
		Version:  1,
		DataRoot: filepath.ToSlash(mustAbsPath(dataRoot)),
		Filter: Filter{
			Include: []string{},
			Skip:    []string{},
		},
		Entries: []Entry{
			{
				PDBID:        templatePDBID,
				Name:         templatePDBID,
				Metadata:     entryMetadata(),
				PreviewImage: entryPreviewImage(),
				Artifacts:    sampleWorksEntryArtifacts(),
				Models:       sampleWorksModels(dataRoot, patterns),
			},
		},
	}, Stats{PDBIDs: len(pdbIDs), LocalFiles: fileCount}, nil
}

func sampleWorksPDBIDs(runs []sampleWorksDetectedRun) map[string]struct{} {
	pdbIDs := map[string]struct{}{}
	for _, run := range runs {
		pdbIDs[run.PDBID] = struct{}{}
	}
	return pdbIDs
}

func sampleWorksModels(dataRoot string, patterns []SampleWorksPattern) []ModelPattern {
	manifestPatterns := make([]SampleWorksPattern, 0, len(patterns))
	for _, pattern := range patterns {
		manifestPatterns = append(manifestPatterns, sampleWorksManifestPattern(dataRoot, pattern))
	}
	labels := sampleWorksModelLabels(manifestPatterns)
	models := make([]ModelPattern, 0, len(manifestPatterns))
	for _, pattern := range manifestPatterns {
		models = append(models, sampleWorksModel(pattern, labels[pattern.RefinedModelPattern]))
	}
	return models
}

func sampleWorksManifestPattern(dataRoot string, pattern SampleWorksPattern) SampleWorksPattern {
	pattern.RefinedModelPattern = sampleWorksRelativePattern(dataRoot, pattern.RefinedModelPattern)
	pattern.RunLogPattern = sampleWorksRelativePattern(dataRoot, pattern.RunLogPattern)
	return pattern
}

func sampleWorksRelativePattern(dataRoot string, source string) string {
	root := strings.TrimSuffix(filepath.ToSlash(mustAbsPath(dataRoot)), "/")
	source = filepath.ToSlash(source)
	if strings.HasPrefix(source, root+"/") {
		return strings.TrimPrefix(source, root+"/")
	}
	return source
}

func sampleWorksModel(pattern SampleWorksPattern, label string) ModelPattern {
	coordinateSource := pattern.RefinedModelPattern
	coordinateExtensions := sampleWorksCoordinateExtensions([]string{coordinateSource})
	return ModelPattern{
		ID:        sampleWorksModelID(label),
		Name:      "Sampleworks " + label,
		ModelType: "Single Conformer",
		Purpose:   "Refinement",
		Metadata:  coordinateModelMetadata(coordinateExtensions),
		Artifacts: []Artifact{
			{
				ID:     "coordinates",
				Source: Source{Files: []string{coordinateSource}},
				Level:  "L2",
			},
			{
				ID:     "starting_structure",
				Source: Source{Files: []string{pattern.DensityMapPattern}},
				Level:  "L1",
				Format: "structure_factors_cif",
			},
			{
				ID:     "log_1",
				Source: Source{Files: []string{pattern.RunLogPattern}},
				Level:  "L2",
			},
		},
		Metrics: coordinateRefinementMetrics(coordinateExtensions),
	}
}

func sampleWorksModelLabels(patterns []SampleWorksPattern) map[string]string {
	segmentsByPattern := map[string][]string{}
	for _, pattern := range patterns {
		segmentsByPattern[pattern.RefinedModelPattern] = sampleWorksModelLabelSegments(pattern.RefinedModelPattern)
	}
	labels := map[string]string{}
	for _, pattern := range patterns {
		segments := segmentsByPattern[pattern.RefinedModelPattern]
		for length := 1; length <= len(segments); length++ {
			suffix := sampleWorksModelLabelSuffix(segments, length)
			if sampleWorksModelLabelSuffixIsUnique(suffix, pattern.RefinedModelPattern, segmentsByPattern, length) {
				labels[pattern.RefinedModelPattern] = sampleWorksModelDisplayLabel(suffix)
				break
			}
		}
		if labels[pattern.RefinedModelPattern] == "" {
			labels[pattern.RefinedModelPattern] = sampleWorksModelDisplayLabel(segments)
		}
	}
	return labels
}

func sampleWorksModelLabelSegments(refinedModelPattern string) []string {
	runDir := pathpkg.Dir(refinedModelPattern)
	parts := strings.Split(strings.Trim(runDir, "/"), "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		part = sampleWorksModelLabelSegment(part)
		if strings.TrimSpace(part) == "" || part == "." {
			continue
		}
		segments = append(segments, part)
	}
	return segments
}

func sampleWorksModelLabelSegment(segment string) string {
	prefix := templatePDBID + "_"
	if strings.Contains(segment, prefix) {
		return strings.TrimPrefix(segment, prefix)
	}
	return segment
}

func sampleWorksModelLabelSuffix(segments []string, length int) []string {
	if length >= len(segments) {
		return append([]string(nil), segments...)
	}
	return append([]string(nil), segments[len(segments)-length:]...)
}

func sampleWorksModelLabelSuffixIsUnique(suffix []string, currentPattern string, segmentsByPattern map[string][]string, length int) bool {
	current := strings.Join(suffix, "/")
	for pattern, segments := range segmentsByPattern {
		if pattern == currentPattern {
			continue
		}
		if strings.Join(sampleWorksModelLabelSuffix(segments, length), "/") == current {
			return false
		}
	}
	return true
}

func sampleWorksModelDisplayLabel(segments []string) string {
	display := make([]string, 0, len(segments))
	for _, segment := range segments {
		if strings.Contains(segment, "occ") {
			segment = strings.ReplaceAll(segment, "_", " ")
		}
		display = append(display, segment)
	}
	return strings.Join(display, " ")
}

func sampleWorksCoordinateExtensions(sources []string) map[string]struct{} {
	extensions := map[string]struct{}{}
	for _, source := range sources {
		extensions[strings.ToLower(filepath.Ext(source))] = struct{}{}
	}
	return extensions
}

func sampleWorksModelID(variant string) string {
	replacer := strings.NewReplacer(".", "", "_", "_", "-", "_")
	id := "sampleworks_" + replacer.Replace(strings.ToLower(variant))
	return regexp.MustCompile(`[^a-z0-9_]+`).ReplaceAllString(id, "_")
}

func sampleWorksEntryArtifacts() []Artifact {
	return []Artifact{
		{
			ID:     "fasta",
			Source: rcsbResourceSource("fasta"),
			Level:  "L0",
		},
		{
			ID:     "structure_factors",
			Source: rcsbFileSource("{{ pdb_id }}-sf.cif"),
			Level:  "L1",
		},
	}
}

func mustAbsPath(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absPath
}
