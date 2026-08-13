package file

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"dynamic-pdb/cli/internal/upload/extractors"
	"dynamic-pdb/cli/internal/upload/manifest"
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
		fileSource = strings.ReplaceAll(fileSource, templatePDBID, strings.ToLower(strings.TrimSpace(pdbID)))
		contents, ok, err := e.localContents(fileSource)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		return extractFromContents(contents, pdbID, extract)
	}
	return nil, false, nil
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
	case extract.PDB != nil:
		return extractPDB(string(contents), extract.PDB.Field)
	case extract.MMCIF != nil:
		return extractMMCIF(string(contents), extract.MMCIF.Field)
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
