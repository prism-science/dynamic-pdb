package models

import "fmt"

// DownloadAvailability is whether an archive holds a selected file's recorded bytes, and if not,
// how its manifest lists the file (researcher item 8). It is kept apart from verification.
type DownloadAvailability string

const (
	// DownloadAvailabilityIncluded: the zip holds the file, read from Dynamic PDB's storage and
	// checked against its recorded SHA-256.
	DownloadAvailabilityIncluded DownloadAvailability = "included"
	// DownloadAvailabilityLinked: not in the zip; Dynamic PDB holds no copy. The manifest gives the
	// source URL and the recorded SHA-256.
	DownloadAvailabilityLinked DownloadAvailability = "linked"
	// DownloadAvailabilityUnavailable: the file cannot be read in its recorded form.
	DownloadAvailabilityUnavailable DownloadAvailability = "unavailable"
	// DownloadAvailabilityChangedAtSource: the source no longer serves the recorded bytes and no copy
	// was kept.
	DownloadAvailabilityChangedAtSource DownloadAvailability = "changed-at-source"
)

// DownloadReasonCode says why an archive does not include a selected file. Each code has one
// availability and one sentence for people (Text).
type DownloadReasonCode string

const (
	// DownloadReasonOverCopyLimit: over 1 GiB; Dynamic PDB keeps only the link (by design).
	DownloadReasonOverCopyLimit DownloadReasonCode = "over_copy_limit"
	// DownloadReasonCopyFailed: the checksum job could not store a copy.
	DownloadReasonCopyFailed DownloadReasonCode = "copy_failed"
	// DownloadReasonCopyOff: the checksum job checked the file while copying was off.
	DownloadReasonCopyOff DownloadReasonCode = "copy_off"
	// DownloadReasonNotCheckedYet: the checksum job has not read the file yet.
	DownloadReasonNotCheckedYet DownloadReasonCode = "not_checked_yet"
	// DownloadReasonCopiedAfterPlanning: a copy appeared after the archive was planned, so the size
	// limit did not count it.
	DownloadReasonCopiedAfterPlanning DownloadReasonCode = "copied_after_planning"
	// DownloadReasonStoredFileDiffers: the checksum job found the file in our storage differs from
	// its record.
	DownloadReasonStoredFileDiffers DownloadReasonCode = "stored_file_differs"
	// DownloadReasonChangedAtSource: the source serves other bytes and no copy was kept.
	DownloadReasonChangedAtSource DownloadReasonCode = "changed_at_source"
	// DownloadReasonSourceUnreadable: the checksum job could not read the file at its source.
	DownloadReasonSourceUnreadable DownloadReasonCode = "source_unreadable"
	// DownloadReasonCopyNotInStorage: the recorded location of our copy is outside our bucket.
	DownloadReasonCopyNotInStorage DownloadReasonCode = "copy_not_in_storage"
	// DownloadReasonCopyMissing: our copy was not found when the archive was prepared.
	DownloadReasonCopyMissing DownloadReasonCode = "copy_missing"
	// DownloadReasonCopyDiffers: our copy did not match the recorded SHA-256 or size when the
	// archive was prepared.
	DownloadReasonCopyDiffers DownloadReasonCode = "copy_differs"
)

// DownloadReasonCodes lists every reason code, in the order previews and the API list them.
var DownloadReasonCodes = []DownloadReasonCode{
	DownloadReasonOverCopyLimit,
	DownloadReasonCopyFailed,
	DownloadReasonCopyOff,
	DownloadReasonNotCheckedYet,
	DownloadReasonCopiedAfterPlanning,
	DownloadReasonStoredFileDiffers,
	DownloadReasonChangedAtSource,
	DownloadReasonSourceUnreadable,
	DownloadReasonCopyNotInStorage,
	DownloadReasonCopyMissing,
	DownloadReasonCopyDiffers,
}

// Availability is how the manifest lists a file left out for this reason.
func (code DownloadReasonCode) Availability() (DownloadAvailability, error) {
	switch code {
	case DownloadReasonOverCopyLimit, DownloadReasonCopyFailed, DownloadReasonCopyOff,
		DownloadReasonNotCheckedYet, DownloadReasonCopiedAfterPlanning:
		return DownloadAvailabilityLinked, nil
	case DownloadReasonStoredFileDiffers, DownloadReasonSourceUnreadable, DownloadReasonCopyNotInStorage,
		DownloadReasonCopyMissing, DownloadReasonCopyDiffers:
		return DownloadAvailabilityUnavailable, nil
	case DownloadReasonChangedAtSource:
		return DownloadAvailabilityChangedAtSource, nil
	default:
		return "", unknownValueError("download reason code", code, DownloadReasonCodes)
	}
}

// Text is the reason as the manifest and the API say it.
func (code DownloadReasonCode) Text() (string, error) {
	switch code {
	case DownloadReasonOverCopyLimit:
		return "larger than 1 GiB, so Dynamic PDB keeps only its link", nil
	case DownloadReasonCopyFailed:
		return "Dynamic PDB could not keep a copy", nil
	case DownloadReasonCopyOff:
		return "Dynamic PDB keeps no copy (copying was off)", nil
	case DownloadReasonNotCheckedYet:
		return "not checked yet, so Dynamic PDB holds no copy yet", nil
	case DownloadReasonCopiedAfterPlanning:
		return "Dynamic PDB copied this file after the archive was planned; ask again to include it", nil
	case DownloadReasonStoredFileDiffers:
		return "the file in Dynamic PDB's storage differs from its record", nil
	case DownloadReasonChangedAtSource:
		return "the source no longer serves the recorded bytes, and no copy was kept", nil
	case DownloadReasonSourceUnreadable:
		return "the file cannot be read at its source", nil
	case DownloadReasonCopyNotInStorage:
		return "the location of Dynamic PDB's copy is not in its storage", nil
	case DownloadReasonCopyMissing:
		return "Dynamic PDB's copy of the file is missing", nil
	case DownloadReasonCopyDiffers:
		return "Dynamic PDB's copy differs from the recorded SHA-256", nil
	default:
		return "", unknownValueError("download reason code", code, DownloadReasonCodes)
	}
}

// DownloadPreview is what an archive of a selection and file choice holds, as the worker planned
// it before reading any file (L1). Files are distinct: a file listed for several models counts
// once, as in the archive's counts. It holds counts and byte totals only.
type DownloadPreview struct {
	// Entries and Models are the selection's records. ModelsWithoutMatchingFiles have no file of
	// the file choice, and the manifest says why in a row each (original mmCIF and modified
	// coordinates; for all files every model is listed with its files).
	Entries                    int `json:"entries"`
	Models                     int `json:"models"`
	ModelsWithoutMatchingFiles int `json:"models_without_matching_files"`
	// Files is every distinct selected file of the file choice.
	Files        int                         `json:"files"`
	FileTypes    []DownloadPreviewFileType   `json:"file_types"`
	Availability DownloadCounts              `json:"availability"`
	NotIncluded  []DownloadPreviewReason     `json:"not_included"`
	Verification DownloadPreviewVerification `json:"verification"`
	// IncludedBytes is exact: the sizes the checksum job read of the files the zip will hold,
	// before compression. The size limit applies to it.
	IncludedBytes int64 `json:"included_bytes"`
	// ArchiveBytesUpperBound bounds the zip's size: the included bytes, zip headers, README and
	// manifest. Compression only makes the zip smaller.
	ArchiveBytesUpperBound int64 `json:"archive_bytes_upper_bound"`
	// LinkedBytesKnown adds up the linked files whose size is known; LinkedFilesSizeUnknown counts
	// the others.
	LinkedBytesKnown       int64                          `json:"linked_bytes_known"`
	LinkedFilesSizeUnknown int                            `json:"linked_files_size_unknown"`
	RelatedDatasets        DownloadPreviewRelatedDatasets `json:"related_datasets"`
}

// DownloadPreviewFileType counts the selected files of one type.
type DownloadPreviewFileType struct {
	Type  ArtifactType `json:"type"`
	Files int          `json:"files"`
	// BytesKnown adds up the files whose size is known; FilesSizeUnknown counts the others.
	BytesKnown       int64 `json:"bytes_known"`
	FilesSizeUnknown int   `json:"files_size_unknown"`
}

// DownloadPreviewReason counts the selected files the archive leaves out for one reason.
type DownloadPreviewReason struct {
	Code  DownloadReasonCode `json:"code"`
	Files int                `json:"files"`
}

// DownloadPreviewVerification counts the selected files by their checksum result, kept apart from
// availability: verified (the bytes matched the record at the last read), mismatch, not verified.
type DownloadPreviewVerification struct {
	Verified    int `json:"verified"`
	Mismatch    int `json:"mismatch"`
	NotVerified int `json:"not_verified"`
}

// DownloadPreviewRelatedDatasets counts the related dataset rows of the manifest: one per related
// dataset of each selected entry. They are links to other repositories, never files in the zip.
type DownloadPreviewRelatedDatasets struct {
	Total             int `json:"total"`
	WithDOI           int `json:"with_doi"`
	WithAccessionOnly int `json:"with_accession_only"`
	WithoutIdentifier int `json:"without_identifier"`
}

// Validate checks the values a stored preview names: every file type and reason code is known.
func (preview DownloadPreview) Validate() error {
	for _, fileType := range preview.FileTypes {
		switch fileType.Type {
		case ArtifactTypeModel, ArtifactTypeStructureFactors, ArtifactTypeFASTA, ArtifactTypeOther:
		default:
			return fmt.Errorf("download preview: unknown artifact type: %s", fileType.Type)
		}
	}
	for _, reason := range preview.NotIncluded {
		if _, err := reason.Code.Availability(); err != nil {
			return fmt.Errorf("download preview: %w", err)
		}
	}
	return nil
}
