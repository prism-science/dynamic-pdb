package artifact

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/cif"
)

const structureReadLimit = 4 * 1024 * 1024

// maxCachedArtifacts bounds the parsed structures a cache keeps; when it is
// full the cache is emptied, so memory stays bounded across a long upload.
const maxCachedArtifacts = 32

// FieldExtractor reads manifest fields out of structure artifacts. Every field
// of an artifact is served from one parse of that artifact, kept in the cache.
type FieldExtractor struct {
	artifacts map[string]extractors.Artifact
	cache     *FieldCache
}

var _ extractors.FieldExtractor = FieldExtractor{}

// NewFieldExtractor reads from artifacts with a cache private to this extractor.
func NewFieldExtractor(artifacts map[string]extractors.Artifact) FieldExtractor {
	return NewCachedFieldExtractor(artifacts, NewFieldCache())
}

// NewCachedFieldExtractor shares cache with every other extractor given it, so
// an artifact is parsed once however many extractors read it.
func NewCachedFieldExtractor(artifacts map[string]extractors.Artifact, cache *FieldCache) FieldExtractor {
	if cache == nil {
		cache = NewFieldCache()
	}
	return FieldExtractor{artifacts: artifacts, cache: cache}
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
	format := structureFormat(artifact)
	var rule *manifest.ExtractRule
	switch format {
	case formatPDB:
		rule = extract.PDB
	case formatMMCIF:
		rule = extract.MMCIF
	case "":
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("artifact %s has format %q: only %s and %s artifacts can be read for structure fields", artifactID, format, formatPDB, formatMMCIF)
	}
	if rule == nil {
		return nil, false, nil
	}
	extraction, ok, err := e.cache.extraction(artifact, format)
	if err != nil || !ok {
		return nil, false, err
	}
	value, found := cif.FieldValue(extraction.Fields, rule.Field)
	return value, found, nil
}

const (
	formatPDB   = "pdb"
	formatMMCIF = "mmcif"
)

// FieldCache keeps the structure fields read from each artifact. It is safe for
// concurrent use; concurrent readers of one artifact share a single parse.
type FieldCache struct {
	mutex   sync.Mutex
	entries map[string]*cacheEntry
	seed    maphash.Seed
	// draftWarning makes the draft-list warning appear once per cache, which
	// lives as long as one upload run, instead of once per artifact.
	draftWarning sync.Once
}

type cacheEntry struct {
	once       sync.Once
	extraction cif.Extraction
	ok         bool
	err        error
}

func NewFieldCache() *FieldCache {
	return &FieldCache{entries: map[string]*cacheEntry{}, seed: maphash.MakeSeed()}
}

func (c *FieldCache) entry(artifact extractors.Artifact, format string) *cacheEntry {
	key := c.key(artifact, format)
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if existing, ok := c.entries[key]; ok {
		return existing
	}
	if len(c.entries) >= maxCachedArtifacts {
		c.entries = map[string]*cacheEntry{}
	}
	created := &cacheEntry{}
	c.entries[key] = created
	return created
}

// key identifies an artifact by what it is and a hash of any in-memory
// contents, so two artifacts that share a file name are never confused.
func (c *FieldCache) key(artifact extractors.Artifact, format string) string {
	return strings.Join([]string{
		format,
		artifact.Filename,
		artifact.LocalPath,
		artifact.URI,
		artifact.SHA256,
		strconv.FormatInt(artifact.Size, 10),
		strconv.Itoa(len(artifact.Contents)),
		strconv.FormatUint(maphash.Bytes(c.seed, artifact.Contents[:min(len(artifact.Contents), structureReadLimit)]), 16),
	}, "\x00")
}

func (c *FieldCache) extraction(artifact extractors.Artifact, format string) (cif.Extraction, bool, error) {
	entry := c.entry(artifact, format)
	entry.once.Do(func() {
		entry.extraction, entry.ok, entry.err = c.readStructure(artifact, format)
	})
	return entry.extraction, entry.ok, entry.err
}

// readStructure parses an artifact once and logs what a user must know about
// the result: draft reference lists, cut-off coordinates and compounds that
// need review.
func (c *FieldCache) readStructure(artifact extractors.Artifact, format string) (cif.Extraction, bool, error) {
	text, truncated, _, ok, err := artifactText(artifact)
	if err != nil || !ok {
		return cif.Extraction{}, false, err
	}
	var extraction cif.Extraction
	switch format {
	case formatPDB:
		extraction, err = cif.PDBExtraction(text, truncated)
	case formatMMCIF:
		extraction, err = cif.MMCIFExtraction(text, truncated)
	default:
		return cif.Extraction{}, false, fmt.Errorf("unsupported structure format %q", format)
	}
	if err != nil {
		return cif.Extraction{}, false, fmt.Errorf("read %s fields: %w", format, err)
	}
	c.logExtraction(artifact, extraction)
	return extraction, true, nil
}

func (c *FieldCache) logExtraction(artifact extractors.Artifact, extraction cif.Extraction) {
	name := artifact.Filename
	if name == "" {
		name = artifact.LocalPath
	}
	for _, warning := range extraction.Warnings {
		if warning == cif.DraftWarning {
			c.draftWarning.Do(func() { slog.Warn("structure extraction warning", "warning", warning) })
			continue
		}
		slog.Warn("structure extraction warning", "artifact", name, "warning", warning)
	}
	for _, item := range extraction.Review {
		slog.Warn("compound needs review", "artifact", name, "compound", item.ID, "list", item.List, "reason", item.Reason)
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
		return formatPDB
	case ".cif", ".mmcif":
		return formatMMCIF
	default:
		if strings.EqualFold(artifact.Format, "cif") {
			return formatMMCIF
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
