package manifest

import (
	"archive/zip"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	pdbIDInFileNamePattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])([0-9][a-z0-9]{3})(?:[^a-z0-9]|$)`)
	coordinateExtensions   = map[string]struct{}{
		".pdb":   {},
		".cif":   {},
		".mmcif": {},
	}
)

const templatePDBID = "{{ pdb_id }}"
const artifactCoordinatesSourceValue = "coordinates"

type localFile struct {
	sourceTemplate   string
	baseStemTemplate string
	fullStemTemplate string
	extension        string
}

type fileIndex struct {
	pdbIDs      map[string]struct{}
	coordinates []localFile
	logs        []localFile
	mtzs        []localFile
}

type coordinateGroup struct {
	baseStemTemplate string
	sources          []string
	extensions       map[string]struct{}
	fullStems        map[string]struct{}
	logs             []string
	mtzs             []string
}

type Stats struct {
	PDBIDs     int
	LocalFiles int
}

func Build(dataRoot string) (Manifest, Stats, error) {
	dataRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return Manifest{}, Stats{}, fmt.Errorf("resolve data folder path: %w", err)
	}
	info, err := os.Stat(dataRoot)
	if err != nil {
		return Manifest{}, Stats{}, fmt.Errorf("stat data folder: %w", err)
	}
	if !info.IsDir() {
		return Manifest{}, Stats{}, fmt.Errorf("%s is not a directory", dataRoot)
	}

	sources, err := listLocalSources(dataRoot)
	if err != nil {
		return Manifest{}, Stats{}, err
	}

	index := indexFiles(sources)
	groups := index.coordinateGroups()
	models := buildModels(groups)

	return Manifest{
		Version:  1,
		DataRoot: filepath.ToSlash(dataRoot),
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
				Artifacts:    entryArtifacts(),
				Models:       models,
			},
		},
	}, Stats{PDBIDs: len(index.pdbIDs), LocalFiles: len(sources)}, nil
}

func listLocalSources(root string) ([]string, error) {
	sources := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", path, walkErr)
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		source, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("build source path for %s: %w", path, err)
		}
		source = filepath.ToSlash(source)
		if strings.EqualFold(filepath.Ext(source), ".zip") {
			zipSources, err := listZipSources(path, source)
			if err != nil {
				return err
			}
			sources = append(sources, zipSources...)
			return nil
		}
		if !isSupportedFileExtension(strings.ToLower(filepath.Ext(source))) {
			return nil
		}
		sources = append(sources, source)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list local sources: %w", err)
	}
	sort.Strings(sources)
	return sources, nil
}

func listZipSources(archivePath string, archiveSource string) ([]string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open zip %s: %w", archiveSource, err)
	}

	sources := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		name := strings.TrimLeft(strings.ReplaceAll(file.Name, "\\", "/"), "/")
		if name == "" || file.FileInfo().IsDir() || strings.HasPrefix(pathpkg.Base(name), ".") {
			continue
		}
		if !isSupportedFileExtension(strings.ToLower(filepath.Ext(name))) {
			continue
		}
		sources = append(sources, archiveSource+"#"+name)
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("close zip %s: %w", archiveSource, err)
	}
	return sources, nil
}

func indexFiles(sources []string) fileIndex {
	index := fileIndex{pdbIDs: make(map[string]struct{})}
	for _, source := range sources {
		file, ok := parseLocalFile(source)
		if !ok {
			continue
		}
		pdbID, ok := pdbIDFromFileName(filepath.Base(source))
		if !ok {
			continue
		}
		index.pdbIDs[pdbID] = struct{}{}

		switch file.extension {
		case ".log":
			index.logs = append(index.logs, file)
		case ".mtz":
			index.mtzs = append(index.mtzs, file)
		default:
			if _, ok := coordinateExtensions[file.extension]; ok {
				index.coordinates = append(index.coordinates, file)
			}
		}
	}
	return index
}

func parseLocalFile(source string) (localFile, bool) {
	extension := strings.ToLower(filepath.Ext(source))
	if !isSupportedFileExtension(extension) {
		return localFile{}, false
	}
	pdbID, ok := pdbIDFromFileName(filepath.Base(source))
	if !ok {
		return localFile{}, false
	}
	sourceTemplate := generateTemplateFromSource(source, pdbID)
	return localFile{
		sourceTemplate:   sourceTemplate,
		baseStemTemplate: strings.TrimSuffix(filepath.Base(sourceTemplate), extension),
		fullStemTemplate: strings.TrimSuffix(sourceTemplate, extension),
		extension:        extension,
	}, true
}

func isSupportedFileExtension(extension string) bool {
	if _, ok := coordinateExtensions[extension]; ok {
		return true
	}
	return extension == ".log" || extension == ".mtz"
}

func pdbIDFromFileName(name string) (string, bool) {
	pdbIDs := make(map[string]struct{})
	for _, match := range pdbIDInFileNamePattern.FindAllStringSubmatch(name, -1) {
		if len(match) < 2 {
			continue
		}
		if pdbID, ok := normalizePDBID(match[1]); ok {
			pdbIDs[pdbID] = struct{}{}
		}
	}
	if len(pdbIDs) != 1 {
		return "", false
	}
	for pdbID := range pdbIDs {
		return pdbID, true
	}
	return "", false
}

func generateTemplateFromSource(source, pdbID string) string {
	pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(pdbID))
	location := pattern.FindStringIndex(source)
	if location == nil {
		return source
	}
	return source[:location[0]] + templatePDBID + source[location[1]:]
}

func normalizePDBID(value string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if len(trimmed) != 4 {
		return "", false
	}
	if trimmed[0] < '0' || trimmed[0] > '9' {
		return "", false
	}
	for _, char := range trimmed {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') {
			continue
		}
		return "", false
	}
	return trimmed, true
}

func buildModels(groups []coordinateGroup) []ModelPattern {
	models := make([]ModelPattern, 0, len(groups)+1)
	models = append(models, depositedModelPattern("model_1"))
	for index, group := range groups {
		models = append(models, modelPatternFromGroup(fmt.Sprintf("model_%d", index+2), group))
	}
	return models
}

func (index fileIndex) coordinateGroups() []coordinateGroup {
	groupsByStem := make(map[string]*coordinateGroup)
	for _, source := range index.coordinates {
		group, ok := groupsByStem[source.baseStemTemplate]
		if !ok {
			group = &coordinateGroup{
				baseStemTemplate: source.baseStemTemplate,
				extensions:       make(map[string]struct{}),
				fullStems:        make(map[string]struct{}),
			}
			groupsByStem[source.baseStemTemplate] = group
		}
		group.sources = appendUniqueString(group.sources, source.sourceTemplate)
		group.extensions[source.extension] = struct{}{}
		group.fullStems[source.fullStemTemplate] = struct{}{}
	}

	for _, source := range index.logs {
		for _, group := range groupsByStem {
			if _, ok := group.fullStems[source.fullStemTemplate]; ok {
				group.logs = appendUniqueString(group.logs, source.sourceTemplate)
			}
		}
	}

	for _, source := range index.mtzs {
		for _, group := range groupsByStem {
			if _, ok := group.fullStems[source.fullStemTemplate]; ok {
				group.mtzs = appendUniqueString(group.mtzs, source.sourceTemplate)
			}
		}
	}

	groups := make([]coordinateGroup, 0, len(groupsByStem))
	for _, group := range groupsByStem {
		sortCoordinateSources(group.sources)
		sort.Strings(group.logs)
		sort.Strings(group.mtzs)
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].baseStemTemplate < groups[j].baseStemTemplate
	})
	return groups
}

func sortCoordinateSources(sources []string) {
	sort.SliceStable(sources, func(i, j int) bool {
		leftPriority := coordinateSourcePriority(sources[i])
		rightPriority := coordinateSourcePriority(sources[j])
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return sources[i] < sources[j]
	})
}

func coordinateSourcePriority(source string) int {
	switch strings.ToLower(filepath.Ext(source)) {
	case ".cif", ".mmcif":
		return 0
	case ".pdb":
		return 1
	default:
		return 2
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func entryMetadata() EntryMetadata {
	return EntryMetadata{
		"title":       rcsbJSONField("struct.title"),
		"method":      rcsbJSONField("exptl[0].method"),
		"resolution":  rcsbJSONField("rcsb_entry_info.resolution_combined[0]"),
		"organism":    rcsbResourceJSONField("polymer_entity", "rcsb_entity_source_organism.ncbi_scientific_name"),
		"space_group": rcsbJSONField("symmetry.space_group_name_H_M"),
	}
}

func entryArtifacts() []Artifact {
	return []Artifact{
		{
			ID:     "fasta",
			Source: rcsbResourceSource("fasta"),
			Level:  "L0",
		},
	}
}

func entryPreviewImage() *EntryPreviewImage {
	return &EntryPreviewImage{Source: rcsbFileSource("{{ pdb_id }}_assembly-1.jpeg")}
}

func depositedModelPattern(id string) ModelPattern {
	return ModelPattern{
		ID:       id,
		Name:     "Deposited model",
		Metadata: depositedModelMetadata(),
		Artifacts: []Artifact{
			{
				ID:     "coordinates",
				Source: rcsbFileSource("{{ pdb_id }}.cif"),
				Level:  "L2",
			},
			{
				ID:     "structure_factors_1",
				Source: rcsbFileSource("{{ pdb_id }}-sf.cif"),
				Level:  "L1",
			},
		},
		Metrics: jsonRefinementMetrics(),
	}
}

func modelPatternFromGroup(id string, group coordinateGroup) ModelPattern {
	artifacts := []Artifact{
		{
			ID:     "coordinates",
			Source: artifactSource(group.sources),
			Level:  "L2",
		},
	}
	if len(group.logs) > 0 {
		artifacts = appendIndexedArtifacts(artifacts, "log", group.logs, "L2")
	}
	if len(group.mtzs) > 0 {
		artifacts = appendIndexedArtifacts(artifacts, "mtz", group.mtzs, "L1")
	} else {
		artifacts = append(artifacts, Artifact{
			ID:     "structure_factors_1",
			Source: rcsbFileSource("{{ pdb_id }}-sf.cif"),
			Level:  "L1",
		})
	}
	return ModelPattern{
		ID:        id,
		Name:      "",
		Metadata:  coordinateModelMetadata(group.extensions),
		Artifacts: artifacts,
		Metrics:   coordinateRefinementMetrics(group.extensions),
	}
}

func depositedModelMetadata() ModelMetadata {
	return ModelMetadata{
		"atom_count":            rcsbJSONField("rcsb_entry_info.deposited_atom_count"),
		"modeled_residues":      rcsbJSONField("rcsb_entry_info.deposited_modeled_polymer_monomer_count"),
		"unique_protein_chains": rcsbJSONField("rcsb_entry_info.deposited_polymer_entity_instance_count"),
		"ligands":               rcsbJSONField("rcsb_entry_info.nonpolymer_bound_components"),
		"authors":               rcsbJSONField("audit_author.name"),
		"affiliation":           rcsbJSONField("pubmed.rcsb_pubmed_affiliation_info"),
	}
}

func coordinateModelMetadata(extensions map[string]struct{}) ModelMetadata {
	return ModelMetadata{
		"atom_count":            coordinateField("atom_count", extensions),
		"modeled_residues":      coordinateField("modeled_residues", extensions),
		"unique_protein_chains": coordinateField("unique_protein_chains", extensions),
		"ligands":               coordinateField("ligands", extensions),
	}
}

func coordinateField(field string, extensions map[string]struct{}) FieldExtraction {
	extract := Extract{}
	if hasPDBExtension(extensions) {
		extract.PDB = &ExtractRule{Field: field}
	}
	if hasMMCIFExtension(extensions) {
		extract.MMCIF = &ExtractRule{Field: field}
	}
	return FieldExtraction{
		Source:  artifactCoordinatesSource(),
		Extract: extract,
	}
}

func jsonRefinementMetrics() Metrics {
	return Metrics{
		"r_free": rcsbJSONField("refine[0].ls_R_factor_R_free"),
		"r_work": rcsbJSONField("refine[0].ls_R_factor_R_work"),
	}
}

func coordinateRefinementMetrics(extensions map[string]struct{}) Metrics {
	rFreeExtract := Extract{}
	rWorkExtract := Extract{}
	if hasPDBExtension(extensions) {
		rFreeExtract.PDB = &ExtractRule{Field: "REMARK 3 FREE R VALUE"}
		rWorkExtract.PDB = &ExtractRule{Field: "REMARK 3 R VALUE WORKING SET"}
	}
	if hasMMCIFExtension(extensions) {
		rFreeExtract.MMCIF = &ExtractRule{Field: "_refine.ls_R_factor_R_free"}
		rWorkExtract.MMCIF = &ExtractRule{Field: "_refine.ls_R_factor_R_work"}
	}
	return Metrics{
		"r_free": {
			Source:  artifactCoordinatesSource(),
			Extract: rFreeExtract,
		},
		"r_work": {
			Source:  artifactCoordinatesSource(),
			Extract: rWorkExtract,
		},
	}
}

func hasPDBExtension(extensions map[string]struct{}) bool {
	_, hasPDB := extensions[".pdb"]
	_, hasENT := extensions[".ent"]
	return hasPDB || hasENT
}

func hasMMCIFExtension(extensions map[string]struct{}) bool {
	_, hasCIF := extensions[".cif"]
	_, hasMMCIF := extensions[".mmcif"]
	return hasCIF || hasMMCIF
}

func rcsbJSONField(field string) FieldExtraction {
	return rcsbResourceJSONField("entry", field)
}

func rcsbResourceJSONField(resource string, field string) FieldExtraction {
	return FieldExtraction{
		Source: rcsbResourceSource(resource),
		Extract: Extract{
			JSON: &ExtractRule{Field: field},
		},
	}
}

func appendIndexedArtifacts(artifacts []Artifact, prefix string, sources []string, level string) []Artifact {
	for index, source := range sources {
		artifacts = append(artifacts, Artifact{
			ID:     fmt.Sprintf("%s_%d", prefix, index+1),
			Source: Source{Files: []string{source}},
			Level:  level,
		})
	}
	return artifacts
}

func artifactSource(sources []string) Source {
	return Source{Files: sources}
}

func rcsbResourceSource(resource string) Source {
	return Source{RCSB: &RCSBSource{PDBID: templatePDBID, Resource: resource}}
}

func rcsbFileSource(file string) Source {
	return Source{RCSB: &RCSBSource{PDBID: templatePDBID, File: file}}
}

func artifactCoordinatesSource() Source {
	return Source{Artifact: artifactCoordinatesSourceValue}
}
