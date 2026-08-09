package artifact

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

const structureReadLimit = 4 * 1024 * 1024

type FieldExtractor struct {
	artifacts map[string]extractors.Artifact
}

var _ extractors.FieldExtractor = FieldExtractor{}

func NewFieldExtractor(artifacts map[string]extractors.Artifact) FieldExtractor {
	return FieldExtractor{artifacts: artifacts}
}

func (e FieldExtractor) Extract(
	_ context.Context,
	_ string,
	source manifest.Source,
	extract manifest.Extract,
) (any, bool, error) {
	artifactID := strings.TrimSpace(source.Artifact)
	if artifactID == "" {
		return nil, false, nil
	}
	artifact, ok := e.artifacts[artifactID]
	if !ok {
		return nil, false, nil
	}
	text, format, ok, err := artifactText(artifact)
	if err != nil || !ok {
		return nil, false, err
	}
	switch format {
	case "pdb":
		if extract.PDB == nil {
			return nil, false, nil
		}
		return extractPDB(text, extract.PDB.Field)
	case "mmcif":
		if extract.MMCIF == nil {
			return nil, false, nil
		}
		return extractMMCIF(text, extract.MMCIF.Field)
	default:
		return nil, false, nil
	}
}

func artifactText(artifact extractors.Artifact) (string, string, bool, error) {
	format := structureFormat(artifact)
	if format == "" {
		return "", "", false, nil
	}
	if len(artifact.Contents) > 0 {
		limit := min(len(artifact.Contents), structureReadLimit)
		return string(artifact.Contents[:limit]), format, true, nil
	}
	path := strings.TrimSpace(artifact.LocalPath)
	if path == "" {
		// TODO: support extracting fields from remote-only artifacts that have a URI but no downloaded contents.
		return "", "", false, nil
	}
	text, err := localText(path)
	if err != nil {
		return "", "", false, err
	}
	return text, format, true, nil
}

func localText(path string) (string, error) {
	if archivePath, entryName, ok := splitZipSource(path); ok {
		contents, err := readZipEntry(archivePath, entryName)
		if err != nil {
			return "", err
		}
		limit := min(len(contents), structureReadLimit)
		return string(contents[:limit]), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact for field extraction: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, structureReadLimit))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", fmt.Errorf("read artifact for field extraction: %w", err)
	}
	return string(contents), nil
}

func structureFormat(artifact extractors.Artifact) string {
	source := artifact.LocalPath
	if _, entryName, ok := splitZipSource(source); ok {
		source = entryName
	}
	if strings.TrimSpace(source) == "" {
		source = artifact.Filename
	}
	switch strings.ToLower(filepath.Ext(source)) {
	case ".pdb", ".ent":
		return "pdb"
	case ".cif", ".mmcif":
		return "mmcif"
	default:
		if strings.EqualFold(artifact.Format, "cif") {
			return "mmcif"
		}
		return strings.ToLower(strings.TrimSpace(artifact.Format))
	}
}

func splitZipSource(source string) (string, string, bool) {
	archiveSource, entryName, ok := strings.Cut(source, "#")
	if !ok || strings.TrimSpace(archiveSource) == "" || strings.TrimSpace(entryName) == "" {
		return "", "", false
	}
	return archiveSource, zipEntryName(entryName), true
}

func zipEntryName(name string) string {
	return strings.TrimLeft(strings.ReplaceAll(name, "\\", "/"), "/")
}

func readZipEntry(archivePath string, entryName string) ([]byte, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open zip artifact for field extraction: %w", err)
	}
	for _, file := range reader.File {
		if zipEntryName(file.Name) != entryName || file.FileInfo().IsDir() {
			continue
		}
		entryReader, err := file.Open()
		if err != nil {
			closeErr := reader.Close()
			if err := errors.Join(err, closeErr); err != nil {
				return nil, fmt.Errorf("open zip entry for field extraction: %w", err)
			}
		}
		contents, readErr := io.ReadAll(io.LimitReader(entryReader, structureReadLimit))
		closeEntryErr := entryReader.Close()
		closeArchiveErr := reader.Close()
		if err := errors.Join(readErr, closeEntryErr, closeArchiveErr); err != nil {
			return nil, fmt.Errorf("read zip entry for field extraction: %w", err)
		}
		return contents, nil
	}
	if err := reader.Close(); err != nil {
		return nil, fmt.Errorf("close zip artifact for field extraction: %w", err)
	}
	return nil, fmt.Errorf("zip entry %s not found", entryName)
}

var (
	pdbRWorkPattern = regexp.MustCompile(`(?i)^\s*R VALUE\s+\(WORKING SET\)\s*:\s*([0-9.]+)`)
	pdbRFreePattern = regexp.MustCompile(`(?i)^\s*FREE R VALUE\s*:\s*([0-9.]+)`)
)

func extractPDB(text string, field string) (any, bool, error) {
	field = strings.TrimSpace(field)
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "REMARK") {
			continue
		}
		body := ""
		if len(line) > 11 {
			body = line[11:]
		}
		switch field {
		case "REMARK 3 FREE R VALUE":
			if value, ok := readMetricNumber(body, pdbRFreePattern); ok {
				return value, true, nil
			}
		case "REMARK 3 R VALUE WORKING SET":
			if value, ok := readMetricNumber(body, pdbRWorkPattern); ok {
				return value, true, nil
			}
		}
	}
	return nil, false, nil
}

func extractMMCIF(text string, field string) (any, bool, error) {
	raw, ok := cifValue(text, strings.TrimSpace(field))
	if !ok {
		return nil, false, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err == nil {
		return value, true, nil
	}
	return raw, true, nil
}

func readMetricNumber(line string, pattern *regexp.Regexp) (float64, bool) {
	match := pattern.FindStringSubmatch(line)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	return value, err == nil
}

func cifValue(text string, tag string) (string, bool) {
	lines := strings.Split(text, "\n")
	needle := strings.ToLower(tag)
	for index, line := range lines {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "_") {
			continue
		}
		tokens := cifTokens(line)
		if len(tokens) == 0 || strings.ToLower(tokens[0]) != needle {
			continue
		}
		if len(tokens) > 1 {
			return cifCleanValue(strings.Join(tokens[1:], " "))
		}
		if index+1 >= len(lines) {
			return "", false
		}
		next := strings.TrimRight(lines[index+1], "\r")
		return cifCleanValue(strings.Join(cifTokens(next), " "))
	}
	return "", false
}

func cifTokens(line string) []string {
	tokens := make([]string, 0)
	for index := 0; index < len(line); {
		if line[index] == ' ' || line[index] == '\t' {
			index++
			continue
		}
		if line[index] == '\'' || line[index] == '"' {
			quote := line[index]
			end := strings.IndexByte(line[index+1:], quote)
			if end == -1 {
				tokens = append(tokens, line[index+1:])
				break
			}
			tokens = append(tokens, line[index+1:index+1+end])
			index = index + end + 2
			continue
		}
		end := index
		for end < len(line) && line[end] != ' ' && line[end] != '\t' {
			end++
		}
		tokens = append(tokens, line[index:end])
		index = end
	}
	return tokens
}

func cifCleanValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == "?" {
		return "", false
	}
	return value, true
}
