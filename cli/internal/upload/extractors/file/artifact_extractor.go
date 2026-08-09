package file

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
)

const templatePDBID = "{{ pdb_id }}"

type ArtifactExtractor struct {
	dataRoot string
}

var _ extractors.ArtifactExtractor = ArtifactExtractor{}

func NewArtifactExtractor(dataRoot string) ArtifactExtractor {
	return ArtifactExtractor{dataRoot: dataRoot}
}

func (e ArtifactExtractor) Extract(
	_ context.Context,
	pdbID string,
	artifact manifest.Artifact,
) (extractors.Artifact, bool, error) {
	for _, fileSource := range artifact.Source.Files {
		fileSource = strings.ReplaceAll(fileSource, templatePDBID, strings.ToLower(strings.TrimSpace(pdbID)))
		payload, ok, err := e.localArtifact(fileSource)
		if err != nil {
			return extractors.Artifact{}, false, err
		}
		if ok {
			return payload, true, nil
		}
	}
	return extractors.Artifact{}, false, nil
}

func (e ArtifactExtractor) localArtifact(source string) (extractors.Artifact, bool, error) {
	if archiveSource, entryName, ok := splitZipSource(source); ok {
		return e.zipArtifact(archiveSource, entryName, source)
	}

	path := resolvePath(e.dataRoot, source)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return extractors.Artifact{}, false, nil
		}
		return extractors.Artifact{}, false, fmt.Errorf("stat artifact source %s: %w", source, err)
	}
	hash, err := fileSHA256(path)
	if err != nil {
		return extractors.Artifact{}, false, err
	}
	return extractors.Artifact{
		Filename:  filepath.Base(path),
		Size:      info.Size(),
		Format:    formatFromSource(source),
		SHA256:    hash,
		LocalPath: path,
	}, true, nil
}

func (e ArtifactExtractor) zipArtifact(archiveSource string, entryName string, source string) (extractors.Artifact, bool, error) {
	archivePath := resolvePath(e.dataRoot, archiveSource)
	contents, ok, err := zipContents(archivePath, entryName, source)
	if err != nil || !ok {
		return extractors.Artifact{}, ok, err
	}
	hash := sha256.Sum256(contents)
	return extractors.Artifact{
		Filename:  filepath.Base(entryName),
		Size:      int64(len(contents)),
		Format:    formatFromSource(entryName),
		SHA256:    hex.EncodeToString(hash[:]),
		LocalPath: archivePath + "#" + entryName,
		Contents:  contents,
	}, true, nil
}

func zipContents(archivePath string, entryName string, source string) ([]byte, bool, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open zip source %s: %w", source, err)
	}
	for _, file := range reader.File {
		if zipEntryName(file.Name) != entryName || file.FileInfo().IsDir() {
			continue
		}
		contents, err := readZipEntry(file)
		closeErr := reader.Close()
		if err := errors.Join(err, closeErr); err != nil {
			return nil, false, fmt.Errorf("read zip source %s: %w", source, err)
		}
		return contents, true, nil
	}
	if err := reader.Close(); err != nil {
		return nil, false, fmt.Errorf("close zip source %s: %w", source, err)
	}
	return nil, false, nil
}

func readZipEntry(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open zip entry %s: %w", file.Name, err)
	}
	contents, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read zip entry %s: %w", file.Name, err)
	}
	return contents, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for sha256: %w", err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func resolvePath(dataRoot string, source string) string {
	if filepath.IsAbs(source) {
		return source
	}
	return filepath.Join(dataRoot, filepath.FromSlash(source))
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

func formatFromSource(source string) string {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(source)), ".")
	if extension == "fa" {
		return "fasta"
	}
	return extension
}
