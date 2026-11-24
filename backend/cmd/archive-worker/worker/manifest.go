package worker

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

const (
	// cc0Name and cc0URL are the reuse terms of Dynamic PDB and PDB archive files (item 10).
	cc0Name = "CC0 1.0"
	cc0URL  = "https://creativecommons.org/publicdomain/zero/1.0/"
	// cc0Statement is the researchers' text for the manifest and the download page, word for word.
	// It asks for acknowledgement; it is not a condition of use.
	cc0Statement = "These data are dedicated to the public domain under CC0 1.0. We ask that you " +
		"acknowledge Dynamic PDB and cite the depositors of the entries you use."
	// externalTerms replaces CC0 for anything held by another repository.
	externalTerms = "See the repository's terms"
	// rcsbEntryPage is where the PDB shows an entry's primary citation.
	rcsbEntryPage = "https://www.rcsb.org/structure/"
)

// recordType is what a manifest row describes.
type recordType string

const (
	recordFile           recordType = "file"
	recordRelatedDataset recordType = "related_dataset"
	recordNoMatchingFile recordType = "no_matching_file"
)

// manifestColumns are manifest.tsv's columns, in order.
var manifestColumns = []string{
	"record_type", "entry_id", "entry_revision", "model_id", "model_revision", "model_type", "pdb_id",
	"file_id", "file_name", "path_in_archive", "roles", "type", "level", "format", "size_bytes",
	"recorded_sha256", "sha256_source", "recorded_after_save", "status", "reason", "verification",
	"verified_at", "source", "source_url", "license", "license_url", "authors", "affiliation",
	"citation_link", "dataset_id",
}

// manifestRow is one row of manifest.tsv; empty values are written as empty cells.
type manifestRow map[string]string

// manifestHeader identifies the archive in the manifest's comment lines.
type manifestHeader struct {
	SelectionID uuid.UUID
	CatalogDate time.Time
	FileChoice  models.DownloadFileChoice
	PreparedAt  time.Time
}

// manifestWriter writes manifest.tsv: three comment lines with the terms, a header row, then one
// row per file, related dataset or model without a file of the choice.
type manifestWriter struct {
	out  *bufio.Writer
	rows int
}

func newManifestWriter(out io.Writer, header manifestHeader) (*manifestWriter, error) {
	writer := &manifestWriter{out: bufio.NewWriter(out)}
	lines := []string{
		fmt.Sprintf("# Dynamic PDB archive manifest. Selection %s, catalog date %s, file choice %s, prepared %s.",
			header.SelectionID, header.CatalogDate.UTC().Format(time.RFC3339), header.FileChoice,
			header.PreparedAt.UTC().Format(time.RFC3339)),
		"# " + cc0Statement,
		"# License of Dynamic PDB and PDB archive files: " + cc0Name + " " + cc0URL +
			". Related datasets and files held elsewhere: " + externalTerms + ".",
		strings.Join(manifestColumns, "\t"),
	}
	for _, line := range lines {
		if _, err := writer.out.WriteString(line + "\n"); err != nil {
			return nil, fmt.Errorf("write manifest header: %w", err)
		}
	}
	return writer, nil
}

// write adds one row. A key that is not a manifest column is an error, so a misspelled column
// never silently drops a value.
func (m *manifestWriter) write(row manifestRow) error {
	for key := range row {
		if !isManifestColumn(key) {
			return fmt.Errorf("manifest row has unknown column %q", key)
		}
	}
	cells := make([]string, len(manifestColumns))
	for index, column := range manifestColumns {
		cells[index] = escapeManifestValue(row[column])
	}
	if _, err := m.out.WriteString(strings.Join(cells, "\t") + "\n"); err != nil {
		return fmt.Errorf("write manifest row: %w", err)
	}
	m.rows++
	return nil
}

func (m *manifestWriter) flush() error {
	if err := m.out.Flush(); err != nil {
		return fmt.Errorf("flush manifest: %w", err)
	}
	return nil
}

func isManifestColumn(key string) bool {
	for _, column := range manifestColumns {
		if column == key {
			return true
		}
	}
	return false
}

// escapeManifestValue keeps one value in one cell: backslash, tab, newline and carriage return
// become \\, \t, \n and \r.
func escapeManifestValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`).Replace(value)
}

// fileRowValues are a file row's values that do not depend on the model it is listed for.
func fileRowValues(file models.SelectionFile, source fileSource) (manifestRow, error) {
	row := manifestRow{
		"record_type":         string(recordFile),
		"file_id":             file.ID.String(),
		"file_name":           file.Name,
		"roles":               joinRoles(file.Roles),
		"type":                string(file.Type),
		"level":               string(file.Level),
		"format":              stringValue(file.Format),
		"size_bytes":          sizeValue(file),
		"recorded_sha256":     stringValue(file.RecordedSHA256),
		"sha256_source":       string(file.RecordedSHA256Source),
		"recorded_after_save": strconv.FormatBool(file.RecordedAfterSave),
		"source":              string(source),
		"source_url":          stringValue(file.URI),
	}
	switch source {
	case sourceDynamicPDBUpload, sourcePDBArchive:
		row["license"] = cc0Name
		row["license_url"] = cc0URL
	case sourceExternal:
		row["license"] = externalTerms
		row["license_url"] = stringValue(file.URI)
	default:
		return nil, fmt.Errorf("file %s: unknown file source: %s", file.ID, source)
	}
	return row, nil
}

// sizeValue is the file's size as the checksum job read it, or as declared when it was not read.
func sizeValue(file models.SelectionFile) string {
	if file.Checksum.SizeBytes != nil {
		return strconv.FormatInt(*file.Checksum.SizeBytes, 10)
	}
	if file.SizeBytes != nil {
		return strconv.FormatInt(*file.SizeBytes, 10)
	}
	return ""
}

func joinRoles(roles []models.SelectionFileRole) string {
	values := make([]string, 0, len(roles))
	for _, role := range roles {
		values = append(values, string(role))
	}
	return strings.Join(values, ",")
}

// recordValues are the entry and model columns of a row; model values are empty for an entry row.
func recordValues(model models.SelectionModel, details models.DownloadSelectionModel, withModel bool) manifestRow {
	row := manifestRow{
		"entry_id":       model.EntryID,
		"entry_revision": strconv.Itoa(model.EntryRevisionNumber),
	}
	pdbID := stringValue(details.EntryPDBID)
	if withModel {
		row["model_id"] = model.ModelID
		row["model_revision"] = strconv.Itoa(model.ModelRevisionNumber)
		row["model_type"] = stringValue(details.ModelType)
		row["authors"] = strings.Join(details.Authors, "; ")
		row["affiliation"] = stringValue(details.Affiliation)
		if details.ModelPDBID != nil {
			pdbID = *details.ModelPDBID
		}
	}
	if pdbID != "" {
		row["pdb_id"] = pdbID
		row["citation_link"] = rcsbEntryPage + strings.ToUpper(pdbID)
	}
	return row
}

// relatedDatasetRow lists a dataset another repository holds; the archive holds none of it.
func relatedDatasetRow(dataset models.RelatedDataset) manifestRow {
	id := dataset.DOI
	link := ""
	if dataset.DOI != "" {
		link = "https://doi.org/" + dataset.DOI
	}
	if dataset.Accession != nil && id == "" {
		id = *dataset.Accession
	}
	if dataset.LandingURL != nil {
		link = *dataset.LandingURL
	}
	return manifestRow{
		"record_type": string(recordRelatedDataset),
		"type":        dataset.DataType,
		"source":      stringValue(dataset.Repository),
		"source_url":  link,
		"dataset_id":  id,
		"reason":      "held by its repository, not part of this archive",
		"license":     externalTerms,
		"license_url": link,
	}
}

// merge adds extra's values to row and returns row.
func merge(row manifestRow, extra manifestRow) manifestRow {
	for key, value := range extra {
		row[key] = value
	}
	return row
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// archiveLayout gives each included file its one path in the zip: entry files in the entry's
// folder, model files in the folder of the first model that lists them.
type archiveLayout struct {
	root  string
	paths map[uuid.UUID]string
	used  map[string]uuid.UUID
	// folders are the model folders: no file may take one's path, or the zip could not be
	// unpacked (a file and a folder of the same name).
	folders map[string]bool
}

func newArchiveLayout(selectionID uuid.UUID) *archiveLayout {
	return &archiveLayout{
		root:    "dynamic-pdb-" + selectionID.String(),
		paths:   map[uuid.UUID]string{},
		used:    map[string]uuid.UUID{},
		folders: map[string]bool{},
	}
}

// modelFolder is the folder of a model's files.
func (l *archiveLayout) modelFolder(model models.SelectionModel) string {
	return path.Join(l.root, model.EntryID, model.ModelID)
}

// reserveFolder keeps a model folder's path from every file.
func (l *archiveLayout) reserveFolder(folder string) {
	l.folders[folder] = true
}

// path returns the file's path, assigning one the first time: <root>/<entry>/<name> for an entry
// file, <root>/<entry>/<model>/<name> otherwise. When another file or a model folder holds that
// path, the file's name gets the first 8 characters of its ID as a prefix, then its whole ID, then
// its whole ID and a number, until the path is free: two files never share a path, and no file
// takes a model folder's.
func (l *archiveLayout) path(file models.SelectionFile, model models.SelectionModel, entryFile bool) string {
	if assigned, found := l.paths[file.ID]; found {
		return assigned
	}
	folder := path.Join(l.root, model.EntryID)
	if !entryFile {
		folder = l.modelFolder(model)
	}
	name := safeFileName(file.Name, file.ID)
	id := file.ID.String()
	for _, prefix := range []string{"", id[:8] + "_", id + "_"} {
		if l.claim(path.Join(folder, boundedFileName(prefix, name)), file.ID) {
			return l.paths[file.ID]
		}
	}
	for number := 2; ; number++ {
		if l.claim(path.Join(folder, boundedFileName(fmt.Sprintf("%s-%d_", id, number), name)), file.ID) {
			return l.paths[file.ID]
		}
	}
}

const (
	// maxFileNameBytes is the longest file name common filesystems take (ext4, APFS, NTFS: 255
	// bytes); a longer name in the zip could not be unpacked.
	maxFileNameBytes = 255
	// maxKeptExtensionBytes is the longest extension a shortened name keeps.
	maxKeptExtensionBytes = 32
)

// boundedFileName is prefix and name within maxFileNameBytes: a longer name loses the end of its
// stem, cut at a UTF-8 character boundary, and keeps its extension.
func boundedFileName(prefix, name string) string {
	room := maxFileNameBytes - len(prefix)
	if len(name) <= room {
		return prefix + name
	}
	extension := path.Ext(name)
	if len(extension) > maxKeptExtensionBytes || len(extension) == len(name) {
		extension = ""
	}
	stem := cutUTF8(strings.TrimSuffix(name, extension), room-len(extension))
	return prefix + stem + extension
}

// cutUTF8 returns at most limit bytes of value, ending at a character boundary.
func cutUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

// claim gives the file candidate as its path unless another file holds it or it is a model folder.
func (l *archiveLayout) claim(candidate string, fileID uuid.UUID) bool {
	if owner, taken := l.used[candidate]; (taken && owner != fileID) || l.folders[candidate] {
		return false
	}
	l.paths[fileID] = candidate
	l.used[candidate] = fileID
	return true
}

// safeFileName keeps a file name inside its folder: its last path element, without control
// characters; the file ID when nothing usable is left.
func safeFileName(name string, id uuid.UUID) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.ReplaceAll(name, `\`, "/"))
	cleaned = strings.TrimSpace(path.Base(strings.TrimSpace(cleaned)))
	if cleaned == "" || cleaned == "." || cleaned == ".." || cleaned == "/" {
		return id.String()
	}
	return cleaned
}

// readmeText is README.txt: how the archive is laid out, what the statuses mean, how to check the
// files, and the reuse terms.
func readmeText(header manifestHeader) string {
	return strings.Join([]string{
		"Dynamic PDB archive",
		"",
		fmt.Sprintf("Selection %s, catalog date %s, file choice %s.", header.SelectionID,
			header.CatalogDate.UTC().Format(time.RFC3339), header.FileChoice),
		"",
		cc0Statement,
		"License of Dynamic PDB and PDB archive files: " + cc0Name + " " + cc0URL + ".",
		"Related datasets and files held elsewhere: " + externalTerms + " (the manifest links each one).",
		"",
		"Layout",
		"  <entry id>/<file>             files of the entry (for example its sequence)",
		"  <entry id>/<model id>/<file>  files of the model, its runs' inputs and outputs",
		"  A file that several selected models share is stored once; the manifest row of each model",
		"  points to it (path_in_archive).",
		"",
		"File choices",
		"  original_mmcif: the PDB's coordinate file, byte for byte, of each selected model whose own",
		"  record holds it, usually the Deposited models. Computed models (for example qFit or",
		"  ensembles) do not hold it. To get it, add the Deposited model of the entry to your selection.",
		"  modified_coordinates: the coordinate files of the selected Dynamic PDB models.",
		"  all_files: every file of the selected models and their entries.",
		"",
		"manifest.tsv",
		"  One row per file of each selected model (status included, linked, unavailable or",
		"  changed-at-source), per related dataset, and per model with no file of the file choice.",
		"  included: in this archive, read from Dynamic PDB's storage and checked against",
		"    recorded_sha256 when the archive was prepared.",
		"  linked: not in this archive; Dynamic PDB holds no copy (for example over 1 GiB). Fetch it",
		"    from source_url and check it against recorded_sha256.",
		"  changed-at-source: the source no longer serves the recorded bytes, and no copy was kept.",
		"  unavailable: the file cannot be read in its recorded form.",
		"  verification is kept apart from status: verified, mismatch or not_verified, with verified_at.",
		"  sha256_source: declared (recorded when the file was added) or computed (Dynamic PDB's first",
		"    read of a file added without one); recorded_after_save is true when that read came after",
		"    the selection was saved.",
		"",
		"Checking the files",
		"  sha256sum <path_in_archive> must print the row's recorded_sha256.",
		"",
	}, "\n")
}
