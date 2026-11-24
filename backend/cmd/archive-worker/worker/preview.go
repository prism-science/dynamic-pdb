package worker

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

// placeholderTime stands in for the manifest's dates while the planning walk lists the rows: it
// does not know when the archive will be prepared.
var placeholderTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// previewFileTypes is the order the preview lists file types in.
var previewFileTypes = []models.ArtifactType{
	models.ArtifactTypeModel, models.ArtifactTypeStructureFactors, models.ArtifactTypeFASTA, models.ArtifactTypeOther,
}

// previewCollector walks a selection as the archive's writing walk will (modelRows, the same
// rows) and counts what the archive will hold: the files it includes and their bytes, and the
// preview. Its manifest rows are checked like the real ones, then dropped.
type previewCollector struct {
	job           models.DownloadJob
	bucket        keyLocator
	cc0Depositors map[uuid.UUID]bool
	walk          *modelWalk
	layout        *archiveLayout
	manifest      *manifestWriter
	// include holds the files the zip includes, with their size.
	include map[uuid.UUID]int64
	bytes   int64
	// seen holds the files counted in the preview, which counts each file once; entries the
	// entries counted.
	seen      map[uuid.UUID]bool
	entries   map[string]bool
	folders   []string
	fileTypes map[models.ArtifactType]*models.DownloadPreviewFileType
	reasons   map[models.DownloadReasonCode]int
	preview   models.DownloadPreview
}

func newPreviewCollector(
	job models.DownloadJob,
	bucket keyLocator,
	cc0Depositors map[uuid.UUID]bool,
	only map[uuid.UUID]bool,
) (*previewCollector, error) {
	header := manifestHeader{
		SelectionID: job.SelectionID,
		CatalogDate: placeholderTime,
		FileChoice:  job.FileChoice,
		PreparedAt:  placeholderTime,
		RetryOf:     job.RetryOf,
	}
	manifest, err := newManifestWriter(io.Discard, header)
	if err != nil {
		return nil, err
	}
	return &previewCollector{
		job:           job,
		bucket:        bucket,
		cc0Depositors: cc0Depositors,
		walk:          newModelWalk(job.FileChoice, only),
		layout:        newArchiveLayout(archiveRoot(job)),
		manifest:      manifest,
		include:       map[uuid.UUID]int64{},
		seen:          map[uuid.UUID]bool{},
		entries:       map[string]bool{},
		fileTypes:     map[models.ArtifactType]*models.DownloadPreviewFileType{},
		reasons:       map[models.DownloadReasonCode]int{},
	}, nil
}

// addModel lists one model as the writing walk will, and counts it.
func (c *previewCollector) addModel(model models.SelectionModel, details models.DownloadSelectionModel) error {
	// The writing walk reserves every model folder before it names a file.
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
		outcome, err := c.outcomeOf(row)
		if err != nil {
			return nil, err
		}
		return fileRow(model, details, row, outcome, c.bucket, c.cc0Depositors)
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
// a file it can include is included; any other file as the checksum job left it. It counts a file
// in the preview the first time it appears.
func (c *previewCollector) outcomeOf(row selectedFile) (fileOutcome, error) {
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
	if _, planned := c.include[row.file.ID]; !planned {
		c.include[row.file.ID] = state.sizeBytes
		c.bytes += state.sizeBytes
	}
	return fileOutcome{
		availability: availabilityIncluded,
		verification: verificationVerified,
		verifiedAt:   &placeholderTime,
	}, nil
}

// previewSize is the size the preview counts for a file: the size the checksum job read of a file
// the zip includes, else the file's known size (knownSize); known false when there is none.
func previewSize(file models.SelectionFile, state fileState) (size int64, known bool) {
	if state.canInclude {
		return state.sizeBytes, true
	}
	size, _, known = knownSize(file)
	return size, known
}

// countFile counts one distinct file by type, availability, reason, verification, reuse terms and
// whether it is verified against the bytes at save time.
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
	default:
		return fmt.Errorf("file %s: unknown availability: %s", file.ID, state.availability)
	}
	terms, err := reuseTermsOf(file, sourceOf(file, c.bucket), c.cc0Depositors)
	if err != nil {
		return err
	}
	switch terms {
	case models.DownloadReuseTermsCC0:
		c.preview.ReuseTerms.CC0++
	case models.DownloadReuseTermsNotConfirmed:
		c.preview.ReuseTerms.NotConfirmed++
	case models.DownloadReuseTermsRepository:
		c.preview.ReuseTerms.Repository++
	default:
		return fmt.Errorf("file %s: unknown reuse terms: %s", file.ID, terms)
	}
	atSave, err := verifiedAtSave(file)
	if err != nil {
		return err
	}
	if !atSave {
		c.preview.NotVerifiedAtSave++
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

// plan finishes the walk: the files to include, their bytes, and the preview.
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
	c.preview.IncludedBytes = c.bytes
	return archivePlan{
		include:         c.include,
		bytes:           c.bytes,
		maxArchiveBytes: c.job.MaxArchiveBytes,
		preview:         c.preview,
		modelFolders:    c.folders,
	}, nil
}
