package worker

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/models"
)

// availability is a manifest file row's status: whether the archive holds the file's recorded
// bytes, and if not, how it is listed (researcher item 8). It is kept apart from verification.
type availability = models.DownloadAvailability

const (
	availabilityIncluded        = models.DownloadAvailabilityIncluded
	availabilityLinked          = models.DownloadAvailabilityLinked
	availabilityUnavailable     = models.DownloadAvailabilityUnavailable
	availabilityChangedAtSource = models.DownloadAvailabilityChangedAtSource
)

// verification says whether bytes were checked against the recorded SHA-256, and with what result.
type verification string

const (
	// verificationVerified: the bytes matched the recorded SHA-256 and size, when the archive was
	// prepared (included files) or at the checksum job's last read (linked files).
	verificationVerified verification = "verified"
	// verificationMismatch: the bytes differed from the record.
	verificationMismatch verification = "mismatch"
	// verificationNotVerified: no bytes were checked.
	verificationNotVerified verification = "not_verified"
)

// fileSource is where a file's recorded location points.
type fileSource string

const (
	sourceDynamicPDBUpload fileSource = "dynamic_pdb_upload"
	sourcePDBArchive       fileSource = "pdb_archive"
	sourceExternal         fileSource = "external"
)

// pdbArchiveHosts serve the PDB archive's files: coordinates and structure factors from
// files.rcsb.org, sequences from www.rcsb.org.
var pdbArchiveHosts = map[string]bool{"files.rcsb.org": true, "www.rcsb.org": true}

// fileState is what the checksum job's last check says about a pinned file: its availability
// before staging, and where our bytes are when the archive can include it.
type fileState struct {
	availability availability
	verification verification
	// verifiedAt is the checksum job's last read, for a file checked there.
	verifiedAt *time.Time
	// reasonCode says why the archive leaves the file out; empty when it can include it.
	reasonCode models.DownloadReasonCode
	// key is the bucket key of the bytes to include; set only when canInclude.
	key        string
	canInclude bool
	// sizeBytes is the size the checksum job read; set only when canInclude.
	sizeBytes int64
}

// keyLocator finds the bucket key of a URL that points into our data bucket.
type keyLocator interface {
	StoredKey(uri string) (string, bool)
}

// stateOf classifies a pinned file from its checksum result: whether Dynamic PDB holds bytes that
// match its record (canInclude), and otherwise how the manifest lists it. An impossible
// combination of checksum values is an error.
func stateOf(file models.SelectionFile, bucket keyLocator) (fileState, error) {
	checksum := file.Checksum
	switch checksum.Status {
	case models.ChecksumStatusVerified:
		return verifiedState(file, bucket)
	case models.ChecksumStatusMismatch:
		if checksum.CopyStatus == nil {
			return fileState{}, fmt.Errorf("file %s: a mismatch result has no copy status", file.ID)
		}
		switch *checksum.CopyStatus {
		case models.CopyStatusOwnStorage:
			return fileState{
				availability: availabilityUnavailable,
				verification: verificationMismatch,
				verifiedAt:   checksum.CheckedAt,
				reasonCode:   models.DownloadReasonStoredFileDiffers,
			}, nil
		case models.CopyStatusMismatch:
			return fileState{
				availability: availabilityChangedAtSource,
				verification: verificationMismatch,
				verifiedAt:   checksum.CheckedAt,
				reasonCode:   models.DownloadReasonChangedAtSource,
			}, nil
		case models.CopyStatusCopied, models.CopyStatusOverLimit, models.CopyStatusCopyFailed, models.CopyStatusCopyOff:
			return fileState{}, fmt.Errorf("file %s: a mismatch result with copy status %s", file.ID, *checksum.CopyStatus)
		default:
			return fileState{}, fmt.Errorf("file %s: unknown copy status: %s", file.ID, *checksum.CopyStatus)
		}
	case models.ChecksumStatusPending:
		return fileState{
			availability: availabilityLinked,
			verification: verificationNotVerified,
			reasonCode:   models.DownloadReasonNotCheckedYet,
		}, nil
	case models.ChecksumStatusFailed:
		return fileState{
			availability: availabilityUnavailable,
			verification: verificationNotVerified,
			verifiedAt:   checksum.CheckedAt,
			reasonCode:   models.DownloadReasonSourceUnreadable,
		}, nil
	default:
		return fileState{}, fmt.Errorf("file %s: unknown checksum status: %s", file.ID, checksum.Status)
	}
}

// verifiedState classifies a file whose bytes matched its record at the checksum job's last read.
func verifiedState(file models.SelectionFile, bucket keyLocator) (fileState, error) {
	checksum := file.Checksum
	if checksum.CopyStatus == nil {
		return fileState{}, fmt.Errorf("file %s: a verified result has no copy status", file.ID)
	}
	linked := func(code models.DownloadReasonCode) fileState {
		return fileState{
			availability: availabilityLinked,
			verification: verificationVerified,
			verifiedAt:   checksum.CheckedAt,
			reasonCode:   code,
		}
	}
	var location string
	switch *checksum.CopyStatus {
	case models.CopyStatusOwnStorage:
		if file.URI == nil {
			return fileState{}, fmt.Errorf("file %s: stored in our bucket without a URI", file.ID)
		}
		location = *file.URI
	case models.CopyStatusCopied:
		if checksum.StoredURI == nil {
			return fileState{}, fmt.Errorf("file %s: copied without the copy's URI", file.ID)
		}
		location = *checksum.StoredURI
	case models.CopyStatusOverLimit:
		return linked(models.DownloadReasonOverCopyLimit), nil
	case models.CopyStatusCopyFailed:
		return linked(models.DownloadReasonCopyFailed), nil
	case models.CopyStatusCopyOff:
		return linked(models.DownloadReasonCopyOff), nil
	case models.CopyStatusMismatch:
		return fileState{}, fmt.Errorf("file %s: a verified result with copy status mismatch", file.ID)
	default:
		return fileState{}, fmt.Errorf("file %s: unknown copy status: %s", file.ID, *checksum.CopyStatus)
	}
	if file.RecordedSHA256 == nil || checksum.SizeBytes == nil {
		return fileState{}, fmt.Errorf("file %s: verified without a recorded SHA-256 or size", file.ID)
	}
	key, inBucket := bucket.StoredKey(location)
	if !inBucket {
		return fileState{
			availability: availabilityUnavailable,
			verification: verificationNotVerified,
			reasonCode:   models.DownloadReasonCopyNotInStorage,
		}, nil
	}
	return fileState{
		availability: availabilityIncluded,
		verification: verificationVerified,
		key:          key,
		canInclude:   true,
		sizeBytes:    *checksum.SizeBytes,
	}, nil
}

// sourceOf says where a file's recorded location points.
func sourceOf(file models.SelectionFile, bucket keyLocator) fileSource {
	if file.URI == nil {
		return sourceExternal
	}
	if _, inBucket := bucket.StoredKey(*file.URI); inBucket {
		return sourceDynamicPDBUpload
	}
	parsed, err := url.Parse(*file.URI)
	if err == nil && pdbArchiveHosts[strings.ToLower(parsed.Host)] {
		return sourcePDBArchive
	}
	return sourceExternal
}

// isPDBCoordinateFile reports whether a file is the PDB's own coordinate file: a model file at
// files.rcsb.org/download/<PDB ID>.cif, as archive models link it.
func isPDBCoordinateFile(file models.SelectionFile) bool {
	if file.Type != models.ArtifactTypeModel || file.URI == nil {
		return false
	}
	parsed, err := url.Parse(*file.URI)
	if err != nil || !strings.EqualFold(parsed.Host, "files.rcsb.org") {
		return false
	}
	name, found := strings.CutPrefix(parsed.Path, "/download/")
	return found && !strings.Contains(name, "/") && strings.HasSuffix(strings.ToLower(name), ".cif") &&
		!strings.HasSuffix(strings.ToLower(name), "-sf.cif")
}

// isEntryFile reports whether a file belongs to the model only through its entry revision.
func isEntryFile(file models.SelectionFile) bool {
	for _, role := range file.Roles {
		if role != models.SelectionFileRoleEntry {
			return false
		}
	}
	return len(file.Roles) > 0
}

// isComputedModel reports whether a model is a Dynamic PDB model, not a PDB archive deposition:
// Single Conformer, Multiconformer or Ensemble, without origin archive.
func isComputedModel(details models.DownloadSelectionModel) (bool, error) {
	if details.Origin != nil && *details.Origin == string(models.ModelOriginArchive) {
		return false, nil
	}
	if details.ModelType == nil {
		return false, nil
	}
	computed, err := models.StructureModelType(*details.ModelType).IsComputed()
	if err != nil {
		return false, fmt.Errorf("model %d: %w", details.Ordinal, err)
	}
	return computed, nil
}

// selectsFile reports whether an archive of the file choice holds the file of the model.
func selectsFile(
	choice models.DownloadFileChoice,
	file models.SelectionFile,
	details models.DownloadSelectionModel,
) (bool, error) {
	switch choice {
	case models.DownloadFileChoiceAllFiles:
		return true, nil
	case models.DownloadFileChoiceOriginalMMCIF:
		return !isEntryFile(file) && isPDBCoordinateFile(file), nil
	case models.DownloadFileChoiceModifiedCoordinates:
		computed, err := isComputedModel(details)
		if err != nil {
			return false, err
		}
		return computed && !isEntryFile(file) && file.Type == models.ArtifactTypeModel && !isPDBCoordinateFile(file), nil
	default:
		return false, fmt.Errorf("unknown download file choice: %s", choice)
	}
}

// noMatchingFileReason says why a model has no file of the file choice; "" for all_files, where
// every model is listed with its files.
func noMatchingFileReason(choice models.DownloadFileChoice) (string, error) {
	switch choice {
	case models.DownloadFileChoiceAllFiles:
		return "", nil
	case models.DownloadFileChoiceOriginalMMCIF:
		return "this model's record holds no original mmCIF: the PDB's coordinate file belongs to the " +
			"entry's Deposited model, so select that model to include it", nil
	case models.DownloadFileChoiceModifiedCoordinates:
		return "this model's record holds no Dynamic PDB coordinate file: it is a PDB archive model", nil
	default:
		return "", fmt.Errorf("unknown download file choice: %s", choice)
	}
}

// reasonText is a reason code as the manifest's reason column says it; empty for no code.
func reasonText(code models.DownloadReasonCode) (string, error) {
	if code == "" {
		return "", nil
	}
	text, err := code.Text()
	if err != nil {
		return "", fmt.Errorf("manifest reason: %w", err)
	}
	return text, nil
}

// selectedFile is one manifest file row of a model: a file of the file choice, with whether it
// belongs to the model only through its entry.
type selectedFile struct {
	file      models.SelectionFile
	entryFile bool
}

// modelWalk is what a walk over a selection's models has listed so far, so an entry's files and
// related datasets are listed once per entry, as the manifest lists them.
type modelWalk struct {
	choice     models.DownloadFileChoice
	entryFiles map[string]bool
	entries    map[string]bool
}

func newModelWalk(choice models.DownloadFileChoice) *modelWalk {
	return &modelWalk{choice: choice, entryFiles: map[string]bool{}, entries: map[string]bool{}}
}

// filesOf returns the model's manifest file rows, in the model's file order: its files of the file
// choice, an entry file only the first time the entry lists it. matched reports whether the model
// has any file of the choice, listed before or not.
func (walk *modelWalk) filesOf(
	model models.SelectionModel,
	details models.DownloadSelectionModel,
) (rows []selectedFile, matched bool, err error) {
	for _, file := range model.Files {
		selected, err := selectsFile(walk.choice, file, details)
		if err != nil {
			return nil, false, err
		}
		if !selected {
			continue
		}
		matched = true
		entryFile := isEntryFile(file)
		if entryFile {
			key := model.EntryID + "/" + file.ID.String()
			if walk.entryFiles[key] {
				continue
			}
			walk.entryFiles[key] = true
		}
		rows = append(rows, selectedFile{file: file, entryFile: entryFile})
	}
	return rows, matched, nil
}

// firstOfEntry reports whether the model is the first of its entry in the walk, whose related
// datasets the manifest lists.
func (walk *modelWalk) firstOfEntry(model models.SelectionModel) bool {
	if walk.entries[model.EntryID] {
		return false
	}
	walk.entries[model.EntryID] = true
	return true
}
