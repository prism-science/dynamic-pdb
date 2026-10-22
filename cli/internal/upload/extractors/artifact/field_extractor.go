package artifact

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/cif"
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
	text, truncated, format, ok, err := artifactText(artifact)
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
		return extractMMCIF(text, truncated, extract.MMCIF.Field)
	default:
		return nil, false, nil
	}
}

// artifactText returns at most structureReadLimit bytes of the structure file
// and whether the file was longer than that.
func artifactText(artifact extractors.Artifact) (string, bool, string, bool, error) {
	format := structureFormat(artifact)
	if format == "" {
		return "", false, "", false, nil
	}
	if len(artifact.Contents) > 0 {
		limit := min(len(artifact.Contents), structureReadLimit)
		return string(artifact.Contents[:limit]), len(artifact.Contents) > structureReadLimit, format, true, nil
	}
	path := strings.TrimSpace(artifact.LocalPath)
	if path == "" {
		// TODO: support extracting fields from remote-only artifacts that have a URI but no downloaded contents.
		return "", false, "", false, nil
	}
	text, truncated, err := localText(path)
	if err != nil {
		return "", false, "", false, err
	}
	return text, truncated, format, true, nil
}

func localText(path string) (string, bool, error) {
	if archivePath, entryName, ok := splitZipSource(path); ok {
		contents, err := readZipEntry(archivePath, entryName)
		if err != nil {
			return "", false, err
		}
		limit := min(len(contents), structureReadLimit)
		return string(contents[:limit]), len(contents) > structureReadLimit, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("open artifact for field extraction: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, structureReadLimit+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", false, fmt.Errorf("read artifact for field extraction: %w", err)
	}
	limit := min(len(contents), structureReadLimit)
	return string(contents[:limit]), len(contents) > structureReadLimit, nil
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
		contents, readErr := io.ReadAll(io.LimitReader(entryReader, structureReadLimit+1))
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

func extractPDB(text string, field string) (any, bool, error) {
	fields, err := cif.PDBFields(text)
	if err != nil {
		return nil, false, fmt.Errorf("read PDB fields: %w", err)
	}
	value, ok := cif.FieldValue(fields, field)
	return value, ok, nil
}

func extractMMCIF(text string, truncated bool, field string) (any, bool, error) {
	parseFields := cif.MMCIFFields
	if truncated {
		parseFields = cif.MMCIFFieldsTruncated
	}
	fields, err := parseFields(text)
	if err != nil {
		return nil, false, fmt.Errorf("read mmCIF fields: %w", err)
	}
	value, ok := cif.FieldValue(fields, field)
	return value, ok, nil
}
