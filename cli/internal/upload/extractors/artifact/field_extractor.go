package artifact

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

func extractPDB(text string, field string) (any, bool, error) {
	value, ok := fieldValue(pdbFields(text), field)
	return value, ok, nil
}

func extractMMCIF(text string, field string) (any, bool, error) {
	value, ok := fieldValue(mmcifFields(text), field)
	return value, ok, nil
}

func fieldValue(fields map[string]any, field string) (any, bool) {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil, false
	}
	value, ok := fields[field]
	if ok {
		return value, true
	}
	value, ok = fields[normalizedFieldKey(field)]
	return value, ok
}

func addField(fields map[string]any, key string, value any) {
	key = strings.TrimSpace(key)
	if key == "" || value == nil {
		return
	}
	fields[key] = value
	normalized := normalizedFieldKey(key)
	if normalized != "" {
		fields[normalized] = value
	}
}

func normalizedFieldKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("(", " ", ")", " ", ":", " ").Replace(key)
	return strings.Join(strings.Fields(key), " ")
}

func pdbFields(text string) map[string]any {
	fields := map[string]any{}
	residues := map[string]struct{}{}
	chains := map[string]struct{}{}
	ligands := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		record := recordName(line)
		if record == "REMARK" {
			addPDBRemarkFields(fields, line)
			continue
		}
		if record != "ATOM" && record != "HETATM" {
			continue
		}
		compound := upperSlice(line, 17, 20)
		if record == "HETATM" && incidentalCompounds[compound] {
			continue
		}
		if element := upperSlice(line, 76, 78); element == "H" || element == "D" {
			continue
		}
		chain := slice(line, 21, 22)
		if record == "ATOM" {
			residues[chain+"|"+slice(line, 22, 27)] = struct{}{}
			if chain != "" {
				chains[chain] = struct{}{}
			}
			continue
		}
		if compound != "" {
			ligands[compound] = struct{}{}
		}
	}
	addPositiveInt(fields, "atom_count", pdbAtomCount(text))
	addPositiveInt(fields, "modeled_residues", len(residues))
	addPositiveInt(fields, "unique_protein_chains", len(chains))
	if len(ligands) > 0 {
		addField(fields, "ligands", sortedKeys(ligands))
	}
	return fields
}

func addPDBRemarkFields(fields map[string]any, line string) {
	if len(line) < 6 {
		return
	}
	body := strings.TrimSpace(line[6:])
	label, value, ok := strings.Cut(body, ":")
	if !ok {
		return
	}
	label = strings.Join(strings.Fields(label), " ")
	value = strings.TrimSpace(value)
	if label == "" || value == "" {
		return
	}
	addField(fields, "REMARK "+label, value)
	if strings.HasPrefix(label, "3 ") {
		addField(fields, "REMARK 3 "+strings.TrimSpace(strings.TrimPrefix(label, "3 ")), value)
	}
	if normalizedFieldKey(label) == normalizedFieldKey("3 PROGRAM") {
		program := splitProgram(value)
		addField(fields, "program.name", program.name)
		addField(fields, "program.version", program.version)
	}
}

func pdbAtomCount(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		record := recordName(line)
		if record != "ATOM" && record != "HETATM" {
			continue
		}
		compound := upperSlice(line, 17, 20)
		if record == "HETATM" && incidentalCompounds[compound] {
			continue
		}
		if element := upperSlice(line, 76, 78); element == "H" || element == "D" {
			continue
		}
		count++
	}
	return count
}

func mmcifFields(text string) map[string]any {
	fields := map[string]any{}
	addMMCIFScalarFields(fields, text)
	addMMCIFProgramFields(fields, text)

	loop := cifLoop(text, "_atom_site")
	if loop == nil {
		return fields
	}
	residues := map[string]struct{}{}
	chains := map[string]struct{}{}
	ligands := map[string]struct{}{}
	atomCount := 0
	for _, row := range loop.rows {
		group := loop.value(row, "group_pdb")
		if group == "" {
			group = "ATOM"
		}
		group = strings.ToUpper(group)
		compound := strings.ToUpper(loop.value(row, "label_comp_id"))
		if group == "HETATM" && incidentalCompounds[compound] {
			continue
		}
		element := strings.ToUpper(loop.value(row, "type_symbol"))
		if element == "H" || element == "D" {
			continue
		}
		atomCount++
		chain := firstNonEmpty(loop.value(row, "auth_asym_id"), loop.value(row, "label_asym_id"))
		if group == "HETATM" {
			if compound != "" {
				ligands[compound] = struct{}{}
			}
			continue
		}
		seq := firstNonEmpty(loop.value(row, "auth_seq_id"), loop.value(row, "label_seq_id"))
		residues[chain+"|"+seq] = struct{}{}
		if chain != "" {
			chains[chain] = struct{}{}
		}
	}
	addPositiveInt(fields, "atom_count", atomCount)
	addPositiveInt(fields, "modeled_residues", len(residues))
	addPositiveInt(fields, "unique_protein_chains", len(chains))
	if len(ligands) > 0 {
		addField(fields, "ligands", sortedKeys(ligands))
	}
	return fields
}

func addPositiveInt(fields map[string]any, key string, value int) {
	if value > 0 {
		addField(fields, key, value)
	}
}

func addMMCIFScalarFields(fields map[string]any, text string) {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(strings.TrimSpace(line), "_") {
			continue
		}
		tokens := cifTokens(line)
		if len(tokens) == 0 {
			continue
		}
		if len(tokens) > 1 {
			if value := cleanCIFValue(strings.Join(tokens[1:], " ")); value != "" {
				addField(fields, tokens[0], value)
			}
			continue
		}
		if index+1 >= len(lines) {
			continue
		}
		if value := cleanCIFValue(strings.Join(cifTokens(lines[index+1]), " ")); value != "" {
			addField(fields, tokens[0], value)
		}
	}
}

func addMMCIFProgramFields(fields map[string]any, text string) {
	program := mmcifProgram(text)
	addField(fields, "program.name", program.name)
	addField(fields, "program.version", program.version)
}

type programInfo struct {
	name    string
	version string
}

func pdbProgram(line string) (programInfo, bool) {
	if recordName(line) != "REMARK" || len(line) < 11 {
		return programInfo{}, false
	}
	body := strings.TrimSpace(line[10:])
	label, value, ok := strings.Cut(body, ":")
	if !ok {
		return programInfo{}, false
	}
	label = strings.TrimSpace(label)
	if !strings.EqualFold(label, "3   PROGRAM") && !strings.EqualFold(label, "PROGRAM") {
		return programInfo{}, false
	}
	program := splitProgram(value)
	return program, program.name != ""
}

func mmcifProgram(text string) programInfo {
	software := cifLoop(text, "_software")
	if software != nil {
		var chosen []string
		for _, row := range software.rows {
			if strings.Contains(strings.ToLower(software.value(row, "classification")), "refinement") {
				chosen = row
				break
			}
		}
		if chosen == nil && len(software.rows) > 0 {
			chosen = software.rows[len(software.rows)-1]
		}
		if chosen != nil {
			name := software.value(chosen, "name")
			if name != "" {
				return programInfo{name: name, version: cleanVersion(software.value(chosen, "version"))}
			}
		}
	}
	name, ok := cifValue(text, "_software.name")
	if ok {
		version, _ := cifValue(text, "_software.version")
		return programInfo{name: name, version: cleanVersion(version)}
	}
	computing, ok := cifValue(text, "_computing.structure_refinement")
	if ok {
		return splitProgram(computing)
	}
	return programInfo{}
}

func splitProgram(raw string) programInfo {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return programInfo{}
	}
	if open := strings.LastIndex(raw, "("); open > 0 && strings.HasSuffix(raw, ")") {
		return programInfo{
			name:    strings.TrimSpace(raw[:open]),
			version: cleanVersion(strings.TrimSuffix(raw[open+1:], ")")),
		}
	}
	fields := strings.Fields(raw)
	if len(fields) > 1 && startsWithDigit(strings.TrimPrefix(fields[len(fields)-1], "v")) {
		return programInfo{
			name:    strings.Join(fields[:len(fields)-1], " "),
			version: cleanVersion(strings.TrimPrefix(fields[len(fields)-1], "v")),
		}
	}
	return programInfo{name: raw}
}

func cleanVersion(raw string) string {
	version := strings.TrimSpace(strings.Split(raw, ":")[0])
	if version == "" || strings.Trim(version, "?") == "" {
		return ""
	}
	return version
}

func startsWithDigit(value string) bool {
	return value != "" && value[0] >= '0' && value[0] <= '9'
}

type cifTable struct {
	columns []string
	rows    [][]string
}

func (t cifTable) value(row []string, column string) string {
	for index, candidate := range t.columns {
		if strings.EqualFold(candidate, column) && index < len(row) {
			return cleanCIFValue(row[index])
		}
	}
	return ""
}

func cifLoop(text string, prefix string) *cifTable {
	lines := strings.Split(text, "\n")
	for index := 0; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) != "loop_" {
			continue
		}
		cursor := index + 1
		columns := []string{}
		for cursor < len(lines) {
			line := strings.TrimSpace(lines[cursor])
			if !strings.HasPrefix(line, prefix+".") {
				break
			}
			columns = append(columns, strings.TrimPrefix(line, prefix+"."))
			cursor++
		}
		if len(columns) == 0 {
			continue
		}
		rows := [][]string{}
		for cursor < len(lines) {
			line := strings.TrimSpace(lines[cursor])
			if line == "" || line == "#" || strings.HasPrefix(line, "_") || line == "loop_" {
				break
			}
			tokens := cifTokens(line)
			if len(tokens) > 0 {
				rows = append(rows, tokens)
			}
			cursor++
		}
		return &cifTable{columns: columns, rows: rows}
	}
	return nil
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

func cleanCIFValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "." || value == "?" {
		return ""
	}
	return value
}

func recordName(line string) string {
	if len(line) < 6 {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(line[:6])
}

func slice(line string, start int, end int) string {
	if start >= len(line) {
		return ""
	}
	if end > len(line) {
		end = len(line)
	}
	return strings.TrimSpace(line[start:end])
}

func upperSlice(line string, start int, end int) string {
	return strings.ToUpper(slice(line, start, end))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

var incidentalCompounds = map[string]bool{
	"HOH": true,
	"DOD": true,
	"WAT": true,
	"H2O": true,
}
