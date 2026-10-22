package file

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
	"dynamic-pdb/lib/cif"
)

type FieldExtractor struct {
	dataRoot string
}

var _ extractors.FieldExtractor = FieldExtractor{}

func NewFieldExtractor(dataRoot string) FieldExtractor {
	return FieldExtractor{dataRoot: dataRoot}
}

func (e FieldExtractor) Extract(
	_ context.Context,
	pdbID string,
	source manifest.Source,
	extract manifest.Extract,
) (any, bool, error) {
	for _, fileSource := range source.Files {
		for _, fileSource := range pdbIDFileSources(fileSource, pdbID) {
			if streamsStructure(extract) {
				value, found, exists, err := e.streamStructureField(fileSource, extract)
				if err != nil {
					return nil, false, err
				}
				if !exists {
					continue
				}
				return value, found, nil
			}
			contents, ok, err := e.localContents(fileSource)
			if err != nil {
				return nil, false, err
			}
			if !ok {
				continue
			}
			return extractFromContents(contents, pdbID, extract)
		}
	}
	return nil, false, nil
}

// streamsStructure reports whether the field is a single value of a PDB or mmCIF
// file. Those files can be tens of megabytes, so they are scanned as a stream
// and never held in memory.
func streamsStructure(extract manifest.Extract) bool {
	return extract.JSON == nil && extract.CSV == nil && extract.TSV == nil &&
		(extract.PDB != nil || extract.MMCIF != nil)
}

// streamStructureField reads one value without loading the file. exists is
// false when the source file is not there.
func (e FieldExtractor) streamStructureField(source string, extract manifest.Extract) (value any, found bool, exists bool, err error) {
	exists, err = e.readLocal(source, func(reader io.Reader) error {
		var readErr error
		switch {
		case extract.PDB != nil:
			value, found, readErr = scanPDBField(reader, extract.PDB.Field)
		case extract.MMCIF != nil:
			value, found, readErr = scanMMCIFField(reader, extract.MMCIF.Field)
		default:
			return fmt.Errorf("extract has neither a PDB nor an mmCIF rule")
		}
		return readErr
	})
	if err != nil {
		return nil, false, exists, err
	}
	return value, found, exists, nil
}

// readLocal opens a local file, or an entry of a local zip archive, and passes it
// to read as a stream. It reports false when the file does not exist.
func (e FieldExtractor) readLocal(source string, read func(io.Reader) error) (bool, error) {
	if archiveSource, entryName, ok := splitZipSource(source); ok {
		return readLocalZipEntry(resolvePath(e.dataRoot, archiveSource), entryName, source, read)
	}
	file, err := os.Open(resolvePath(e.dataRoot, source))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open field source %s: %w", source, err)
	}
	readErr := read(file)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return true, fmt.Errorf("read field source %s: %w", source, err)
	}
	return true, nil
}

func readLocalZipEntry(archivePath string, entryName string, source string, read func(io.Reader) error) (bool, error) {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open zip source %s: %w", source, err)
	}
	for _, entry := range archive.File {
		if zipEntryName(entry.Name) != entryName || entry.FileInfo().IsDir() {
			continue
		}
		entryReader, openErr := entry.Open()
		if openErr != nil {
			closeErr := archive.Close()
			return true, fmt.Errorf("open zip source %s: %w", source, errors.Join(openErr, closeErr))
		}
		readErr := read(entryReader)
		closeEntryErr := entryReader.Close()
		closeArchiveErr := archive.Close()
		if err := errors.Join(readErr, closeEntryErr, closeArchiveErr); err != nil {
			return true, fmt.Errorf("read zip source %s: %w", source, err)
		}
		return true, nil
	}
	if err := archive.Close(); err != nil {
		return false, fmt.Errorf("close zip source %s: %w", source, err)
	}
	return false, nil
}

func (e FieldExtractor) localContents(source string) ([]byte, bool, error) {
	if archiveSource, entryName, ok := splitZipSource(source); ok {
		return zipContents(resolvePath(e.dataRoot, archiveSource), entryName, source)
	}
	path := resolvePath(e.dataRoot, source)
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read field source %s: %w", source, err)
	}
	return contents, true, nil
}

func extractFromContents(contents []byte, pdbID string, extract manifest.Extract) (any, bool, error) {
	switch {
	case extract.JSON != nil:
		return extractJSON(contents, extract.JSON.Field)
	case extract.CSV != nil:
		return extractDelimited(contents, *extract.CSV, ',', pdbID, "CSV")
	case extract.TSV != nil:
		return extractDelimited(contents, *extract.TSV, '\t', pdbID, "TSV")
	default:
		return nil, false, nil
	}
}

func extractJSON(contents []byte, field string) (any, bool, error) {
	var payload any
	if err := json.Unmarshal(contents, &payload); err != nil {
		return nil, false, fmt.Errorf("decode JSON field source: %w", err)
	}
	return valueAtPath(payload, field)
}

func extractDelimited(contents []byte, rule manifest.ExtractRule, delimiter rune, pdbID string, format string) (any, bool, error) {
	reader := csv.NewReader(bytes.NewReader(contents))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, false, fmt.Errorf("read %s field source: %w", format, err)
	}
	if len(records) < 2 {
		return nil, false, nil
	}
	columns := map[string]int{}
	for index, column := range records[0] {
		columns[strings.TrimSpace(column)] = index
	}
	columnIndex, ok := columns[strings.TrimSpace(rule.Column)]
	if !ok {
		return nil, false, nil
	}
	for _, record := range records[1:] {
		if !delimitedRowMatches(record, columns, rule.Where, pdbID) {
			continue
		}
		if columnIndex >= len(record) {
			return nil, false, nil
		}
		value := record[columnIndex]
		if missingDelimitedValue(value) {
			return nil, false, nil
		}
		return value, true, nil
	}
	return nil, false, nil
}

func missingDelimitedValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", ".", "?", "na", "n/a", "nan", "null", "none":
		return true
	default:
		return false
	}
}

func delimitedRowMatches(record []string, columns map[string]int, where *manifest.ExtractRule, pdbID string) bool {
	if where == nil {
		return true
	}
	columnIndex, ok := columns[strings.TrimSpace(where.Column)]
	if !ok || columnIndex >= len(record) {
		return false
	}
	expected := strings.ReplaceAll(where.Equals, templatePDBID, strings.ToLower(strings.TrimSpace(pdbID)))
	return strings.EqualFold(strings.TrimSpace(record[columnIndex]), strings.TrimSpace(expected))
}

var (
	pdbRWorkPattern = regexp.MustCompile(`(?i)^\s*R VALUE\s+\(WORKING SET\)\s*:\s*([0-9.]+)`)
	pdbRFreePattern = regexp.MustCompile(`(?i)^\s*FREE R VALUE\s*:\s*([0-9.]+)`)
)

// maxPDBLineBytes bounds one line of a PDB file; the format has 80 columns.
const maxPDBLineBytes = 1 << 20

func scanPDBField(source io.Reader, field string) (any, bool, error) {
	field = strings.TrimSpace(field)
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 0, 64*1024), maxPDBLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
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
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scan PDB field %s: %w", field, err)
	}
	return nil, false, nil
}

func scanMMCIFField(source io.Reader, field string) (any, bool, error) {
	raw, ok, err := cif.ScalarValueFromReader(source, strings.TrimSpace(field))
	if err != nil {
		return nil, false, fmt.Errorf("scan mmCIF field %s: %w", field, err)
	}
	if !ok {
		return nil, false, nil
	}
	value, parseErr := strconv.ParseFloat(raw, 64)
	if parseErr == nil {
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

func valueAtPath(value any, path string) (any, bool, error) {
	current := value
	for _, segment := range strings.Split(strings.TrimSpace(path), ".") {
		name, indexes, err := parseSegment(segment)
		if err != nil {
			return nil, false, err
		}
		if name != "" {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, false, nil
			}
			current, ok = object[name]
			if !ok {
				return nil, false, nil
			}
		}
		for _, index := range indexes {
			items, ok := current.([]any)
			if !ok || index < 0 || index >= len(items) {
				return nil, false, nil
			}
			current = items[index]
		}
	}
	return current, true, nil
}

func parseSegment(segment string) (string, []int, error) {
	name := segment
	indexes := []int{}
	for {
		open := strings.Index(name, "[")
		if open < 0 {
			break
		}
		closeIndex := strings.Index(name[open:], "]")
		if closeIndex < 0 {
			return "", nil, fmt.Errorf("invalid JSON field segment: %s", segment)
		}
		closeIndex += open
		rawIndex := name[open+1 : closeIndex]
		index, err := strconv.Atoi(rawIndex)
		if err != nil {
			return "", nil, fmt.Errorf("invalid JSON field index %s: %w", rawIndex, err)
		}
		indexes = append(indexes, index)
		name = name[:open] + name[closeIndex+1:]
	}
	return name, indexes, nil
}
