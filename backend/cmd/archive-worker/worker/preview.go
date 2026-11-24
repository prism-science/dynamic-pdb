package worker

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

const (
	// zipEntryHeaderBound bounds the bytes archive/zip writes for one entry besides its data and
	// its name (written twice): the local header (30), the zip64 data descriptor (24), the central
	// directory record (46), a zip64 extra field in each (20 and 28) and an extended timestamp in
	// each (9 and 9) add up to 166.
	zipEntryHeaderBound = 256
	// zipEndBound bounds the end of central directory records: 22, plus 56 and 20 for zip64.
	zipEndBound = 1024
	// deflateBoundDivisor and deflateBoundExtra bound what deflate adds to data it cannot shrink:
	// about 5 bytes per 16 KiB stored block, plus a few bytes for a short input.
	deflateBoundDivisor = 2000
	deflateBoundExtra   = 64
)

// placeholderTime stands in for the manifest's dates while it is counted: the planning walk does
// not know when the archive will be prepared, and every RFC 3339 date in UTC has the same length.
var placeholderTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// previewFileTypes is the order the preview lists file types in.
var previewFileTypes = []models.ArtifactType{
	models.ArtifactTypeModel, models.ArtifactTypeStructureFactors, models.ArtifactTypeFASTA, models.ArtifactTypeOther,
}

// zipEntryBound bounds the bytes a file of size bytes takes in the zip under name.
func zipEntryBound(size int64, name string) int64 {
	return size + size/deflateBoundDivisor + deflateBoundExtra + zipEntryHeaderBound + 2*int64(len(name))
}

// byteCounter counts what is written to it.
type byteCounter struct {
	bytes int64
}

func (c *byteCounter) Write(data []byte) (int, error) {
	c.bytes += int64(len(data))
	return len(data), nil
}

// previewCollector walks a selection as the archive's writing walk will (modelRows, the same
// layout) and counts what the archive will hold: the files it includes and their bytes, and the
// preview. Its manifest goes to a byte counter, so the zip's size bound counts the real rows.
type previewCollector struct {
	job      models.DownloadJob
	bucket   keyLocator
	walk     *modelWalk
	layout   *archiveLayout
	header   manifestHeader
	counter  *byteCounter
	manifest *manifestWriter
	// include holds the files the zip includes, with their size; paths their path in it.
	include map[uuid.UUID]int64
	paths   map[uuid.UUID]string
	bytes   int64
	// entriesBound bounds the zip entries of the included files.
	entriesBound int64
	// seen holds the files counted in the preview, which counts each file once; entries the
	// entries counted.
	seen      map[uuid.UUID]bool
	entries   map[string]bool
	folders   []string
	fileTypes map[models.ArtifactType]*models.DownloadPreviewFileType
	reasons   map[models.DownloadReasonCode]int
	preview   models.DownloadPreview
}

func newPreviewCollector(job models.DownloadJob, bucket keyLocator, only map[uuid.UUID]bool) (*previewCollector, error) {
	header := manifestHeader{
		SelectionID: job.SelectionID,
		CatalogDate: placeholderTime,
		FileChoice:  job.FileChoice,
		PreparedAt:  placeholderTime,
		RetryOf:     job.RetryOf,
	}
	counter := &byteCounter{}
	manifest, err := newManifestWriter(counter, header)
	if err != nil {
		return nil, err
	}
	return &previewCollector{
		job:       job,
		bucket:    bucket,
		walk:      newModelWalk(job.FileChoice, only),
		layout:    newArchiveLayout(archiveRoot(job)),
		header:    header,
		counter:   counter,
		manifest:  manifest,
		include:   map[uuid.UUID]int64{},
		paths:     map[uuid.UUID]string{},
		seen:      map[uuid.UUID]bool{},
		entries:   map[string]bool{},
		fileTypes: map[models.ArtifactType]*models.DownloadPreviewFileType{},
		reasons:   map[models.DownloadReasonCode]int{},
	}, nil
}

// addModel lists one model as the writing walk will, and counts it.
func (c *previewCollector) addModel(model models.SelectionModel, details models.DownloadSelectionModel) error {
	// The writing walk reserves every model folder before it names a file; this walk learns them as
	// it goes, so an entry file named after a later model may get a slightly shorter path here: the
	// zip's bound keeps far more room per entry than that.
	folder := c.layout.modelFolder(model)
	c.layout.reserveFolder(folder)
	c.folders = append(c.folders, folder)
	if c.walk.only != nil && !listsAny(model, c.walk.only) {
		// A retry counts only the records of the files it asks for again.
		return nil
	}
	c.preview.Models++
	if !c.entries[model.EntryID] {
		c.entries[model.EntryID] = true
		c.preview.Entries++
		if c.walk.only == nil {
			c.countRelatedDatasets(details.RelatedDatasets)
		}
	}
	return modelRows(c.walk, model, details, func(row selectedFile) (manifestRow, error) {
		outcome, err := c.outcomeOf(model, row)
		if err != nil {
			return nil, err
		}
		return fileRow(model, details, row, outcome, c.bucket)
	}, c.write)
}

// write counts a manifest row, and the models it lists as having no file of the file choice.
func (c *previewCollector) write(row manifestRow) error {
	if row["record_type"] == string(recordNoMatchingFile) {
		c.preview.ModelsWithoutMatchingFiles++
	}
	return c.manifest.write(row)
}

// listsAny reports whether the model lists one of the files.
func listsAny(model models.SelectionModel, files map[uuid.UUID]bool) bool {
	for _, file := range model.Files {
		if files[file.ID] {
			return true
		}
	}
	return false
}

// outcomeOf is how the archive will list the file if every planned file reads back as recorded:
// a file it can include is included, the first time at a new path; any other file as the checksum
// job left it. It counts a file in the preview the first time it appears.
func (c *previewCollector) outcomeOf(model models.SelectionModel, row selectedFile) (fileOutcome, error) {
	state, err := stateOf(row.file, c.bucket)
	if err != nil {
		return fileOutcome{}, err
	}
	if !c.seen[row.file.ID] {
		c.seen[row.file.ID] = true
		if err := c.countFile(row.file, state); err != nil {
			return fileOutcome{}, err
		}
	}
	if !state.canInclude {
		return stateOutcome(state), nil
	}
	path, planned := c.paths[row.file.ID]
	if !planned {
		path = c.layout.path(row.file, model, row.entryFile)
		c.paths[row.file.ID] = path
		c.include[row.file.ID] = state.sizeBytes
		c.bytes += state.sizeBytes
		c.entriesBound += zipEntryBound(state.sizeBytes, path)
	}
	return fileOutcome{
		availability: availabilityIncluded,
		verification: verificationVerified,
		verifiedAt:   &placeholderTime,
		path:         path,
	}, nil
}

// previewSize is the size the preview counts for a file: the size the checksum job read of a file
// the zip includes, else the recorded size, else the read size of a verified file; known false
// when there is none.
func previewSize(file models.SelectionFile, state fileState) (size int64, known bool) {
	switch {
	case state.canInclude:
		return state.sizeBytes, true
	case file.SizeBytes != nil:
		return *file.SizeBytes, true
	case state.verification == verificationVerified && file.Checksum.SizeBytes != nil:
		return *file.Checksum.SizeBytes, true
	default:
		return 0, false
	}
}

// countFile counts one distinct file by type, availability, reason and verification.
func (c *previewCollector) countFile(file models.SelectionFile, state fileState) error {
	c.preview.Files++
	fileType := c.fileTypes[file.Type]
	if fileType == nil {
		fileType = &models.DownloadPreviewFileType{Type: file.Type}
		c.fileTypes[file.Type] = fileType
	}
	fileType.Files++
	size, known := previewSize(file, state)
	if known {
		fileType.BytesKnown += size
	} else {
		fileType.FilesSizeUnknown++
	}
	switch state.availability {
	case availabilityIncluded:
		c.preview.Availability.Included++
	case availabilityLinked:
		c.preview.Availability.Linked++
		if known {
			c.preview.LinkedBytesKnown += size
		} else {
			c.preview.LinkedFilesSizeUnknown++
		}
	case availabilityUnavailable:
		c.preview.Availability.Unavailable++
	case availabilityChangedAtSource:
		c.preview.Availability.ChangedAtSource++
	default:
		return fmt.Errorf("file %s: unknown availability: %s", file.ID, state.availability)
	}
	if state.reasonCode != "" {
		c.reasons[state.reasonCode]++
	}
	switch state.verification {
	case verificationVerified:
		c.preview.Verification.Verified++
	case verificationMismatch:
		c.preview.Verification.Mismatch++
	case verificationNotVerified:
		c.preview.Verification.NotVerified++
	default:
		return fmt.Errorf("file %s: unknown verification: %s", file.ID, state.verification)
	}
	return nil
}

// countRelatedDatasets counts an entry's related datasets, as its first model's manifest rows
// list them.
func (c *previewCollector) countRelatedDatasets(datasets []models.RelatedDataset) {
	for _, dataset := range datasets {
		c.preview.RelatedDatasets.Total++
		switch {
		case strings.TrimSpace(dataset.DOI) != "":
			c.preview.RelatedDatasets.WithDOI++
		case dataset.Accession != nil && strings.TrimSpace(*dataset.Accession) != "":
			c.preview.RelatedDatasets.WithAccessionOnly++
		default:
			c.preview.RelatedDatasets.WithoutIdentifier++
		}
	}
}

// plan finishes the walk: the files to include, their bytes, and the preview with the zip's size
// bound.
func (c *previewCollector) plan() (archivePlan, error) {
	if err := c.manifest.flush(); err != nil {
		return archivePlan{}, err
	}
	for fileType := range c.fileTypes {
		switch fileType {
		case models.ArtifactTypeModel, models.ArtifactTypeStructureFactors, models.ArtifactTypeFASTA,
			models.ArtifactTypeOther:
		default:
			return archivePlan{}, fmt.Errorf("unknown artifact type: %s", fileType)
		}
	}
	c.preview.FileTypes = []models.DownloadPreviewFileType{}
	for _, fileType := range previewFileTypes {
		if counted := c.fileTypes[fileType]; counted != nil {
			c.preview.FileTypes = append(c.preview.FileTypes, *counted)
		}
	}
	c.preview.NotIncluded = []models.DownloadPreviewReason{}
	listed := 0
	for _, code := range models.DownloadReasonCodes {
		if files := c.reasons[code]; files > 0 {
			c.preview.NotIncluded = append(c.preview.NotIncluded, models.DownloadPreviewReason{Code: code, Files: files})
			listed++
		}
	}
	if listed != len(c.reasons) {
		return archivePlan{}, fmt.Errorf("the preview counted a reason code that is not listed: %v", c.reasons)
	}
	readme := int64(len(readmeText(c.header)))
	c.preview.IncludedBytes = c.bytes
	c.preview.ArchiveBytesUpperBound = c.entriesBound +
		zipEntryBound(readme, c.layout.root+"/README.txt") +
		zipEntryBound(c.counter.bytes, c.layout.root+"/manifest.tsv") +
		zipEndBound
	return archivePlan{
		include:         c.include,
		bytes:           c.bytes,
		maxArchiveBytes: c.job.MaxArchiveBytes,
		preview:         c.preview,
		modelFolders:    c.folders,
	}, nil
}
