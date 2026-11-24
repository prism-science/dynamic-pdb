package httpapi

import (
	"fmt"

	domainmodels "dynamic-pdb/backend/internal/models"
)

const (
	includedSizeMeasures = "the files the zip holds, before compression; max_archive_bytes applies to this"
	linkedSizeMeasures   = "the linked files, which the manifest points to at their source; not in the zip"
)

// downloadPreviewResponse is a job's preview as the API shows it; nil until the job is planned.
func downloadPreviewResponse(job domainmodels.DownloadJob) (*DownloadPreview, error) {
	if job.Preview == nil {
		return nil, nil
	}
	preview := job.Preview
	byType := make([]DownloadPreviewFileType, 0, len(preview.FileTypes))
	for _, counted := range preview.FileTypes {
		fileType, err := artifactTypeResponse(counted.Type)
		if err != nil {
			return nil, fmt.Errorf("download %s preview: %w", job.ID, err)
		}
		byType = append(byType, DownloadPreviewFileType{
			Type:  fileType,
			Files: counted.Files,
			Size: partialSize(counted.Files, counted.FilesSizeUnknown, counted.BytesKnown,
				fmt.Sprintf("the %s files of the file choice, in the zip or not", counted.Type)),
		})
	}
	notIncluded := make([]DownloadPreviewReason, 0, len(preview.NotIncluded))
	for _, reason := range preview.NotIncluded {
		listed, err := downloadPreviewReasonResponse(reason)
		if err != nil {
			return nil, fmt.Errorf("download %s preview: %w", job.ID, err)
		}
		notIncluded = append(notIncluded, listed)
	}
	included := preview.IncludedBytes
	return &DownloadPreview{
		Records: DownloadPreviewRecords{
			Entries:                    preview.Entries,
			Models:                     preview.Models,
			ModelsWithoutMatchingFiles: preview.ModelsWithoutMatchingFiles,
		},
		Files: DownloadPreviewFiles{
			Total:  preview.Files,
			ByType: byType,
			Availability: DownloadCounts{
				Included:    preview.Availability.Included,
				Linked:      preview.Availability.Linked,
				Unavailable: preview.Availability.Unavailable,
			},
			NotVerifiedAtSave: preview.NotVerifiedAtSave,
			NotIncluded:       notIncluded,
			Verification: DownloadPreviewVerification{
				Verified:    preview.Verification.Verified,
				Mismatch:    preview.Verification.Mismatch,
				NotVerified: preview.Verification.NotVerified,
			},
		},
		Sizes: DownloadPreviewSizes{
			Included: DownloadSize{Bytes: &included, Kind: DownloadSizeKindExact, Measures: includedSizeMeasures},
			Linked: partialSize(preview.Availability.Linked, preview.LinkedFilesSizeUnknown, preview.LinkedBytesKnown,
				linkedSizeMeasures),
		},
		RelatedDatasets: DownloadPreviewRelatedDatasets{
			Total:             preview.RelatedDatasets.Total,
			WithDoi:           preview.RelatedDatasets.WithDOI,
			WithAccessionOnly: preview.RelatedDatasets.WithAccessionOnly,
			WithoutIdentifier: preview.RelatedDatasets.WithoutIdentifier,
		},
		ReuseTerms: DownloadPreviewReuseTerms{
			Cc0:          preview.ReuseTerms.CC0,
			NotConfirmed: preview.ReuseTerms.NotConfirmed,
			Repository:   preview.ReuseTerms.Repository,
		},
		OverLimit: preview.IncludedBytes > job.MaxArchiveBytes,
	}, nil
}

// partialSize is the size of files some of which may have no known size, never estimated
// (researchers' Q9): exact when every size is known, unknown when none is, and otherwise partial:
// the known sizes added up, with the number of files left out.
func partialSize(files, sizeUnknown int, bytesKnown int64, measures string) DownloadSize {
	switch {
	case sizeUnknown == 0:
		return DownloadSize{Bytes: &bytesKnown, Kind: DownloadSizeKindExact, Measures: measures}
	case sizeUnknown >= files:
		return DownloadSize{Kind: DownloadSizeKindUnknown, Measures: measures, FilesSizeUnknown: &sizeUnknown}
	default:
		return DownloadSize{
			Bytes: &bytesKnown, Kind: DownloadSizeKindPartial, Measures: measures, FilesSizeUnknown: &sizeUnknown,
		}
	}
}

func downloadPreviewReasonResponse(reason domainmodels.DownloadPreviewReason) (DownloadPreviewReason, error) {
	code, err := downloadReasonCodeResponse(reason.Code)
	if err != nil {
		return DownloadPreviewReason{}, err
	}
	availability, err := reason.Code.Availability()
	if err != nil {
		return DownloadPreviewReason{}, err
	}
	status, err := downloadAvailabilityResponse(availability)
	if err != nil {
		return DownloadPreviewReason{}, err
	}
	text, err := reason.Code.Text()
	if err != nil {
		return DownloadPreviewReason{}, err
	}
	return DownloadPreviewReason{Code: code, Status: status, Reason: text, Files: reason.Files}, nil
}

func artifactTypeResponse(fileType domainmodels.ArtifactType) (ArtifactType, error) {
	switch fileType {
	case domainmodels.ArtifactTypeModel:
		return ArtifactTypeModel, nil
	case domainmodels.ArtifactTypeStructureFactors:
		return ArtifactTypeStructureFactors, nil
	case domainmodels.ArtifactTypeFASTA:
		return ArtifactTypeFASTA, nil
	case domainmodels.ArtifactTypeOther:
		return ArtifactTypeOther, nil
	default:
		return "", fmt.Errorf("unknown artifact type: %s", fileType)
	}
}

func downloadAvailabilityResponse(availability domainmodels.DownloadAvailability) (DownloadFileAvailability, error) {
	switch availability {
	case domainmodels.DownloadAvailabilityIncluded:
		return DownloadFileAvailabilityIncluded, nil
	case domainmodels.DownloadAvailabilityLinked:
		return DownloadFileAvailabilityLinked, nil
	case domainmodels.DownloadAvailabilityUnavailable:
		return DownloadFileAvailabilityUnavailable, nil
	default:
		return "", fmt.Errorf("unknown download availability: %s", availability)
	}
}

func downloadReasonCodeResponse(code domainmodels.DownloadReasonCode) (DownloadReasonCode, error) {
	switch code {
	case domainmodels.DownloadReasonOverCopyLimit:
		return DownloadReasonCodeOverCopyLimit, nil
	case domainmodels.DownloadReasonCopyFailed:
		return DownloadReasonCodeCopyFailed, nil
	case domainmodels.DownloadReasonCopyOff:
		return DownloadReasonCodeCopyOff, nil
	case domainmodels.DownloadReasonNotCheckedYet:
		return DownloadReasonCodeNotCheckedYet, nil
	case domainmodels.DownloadReasonCopiedAfterPlanning:
		return DownloadReasonCodeCopiedAfterPlanning, nil
	case domainmodels.DownloadReasonStoredFileDiffers:
		return DownloadReasonCodeStoredFileDiffers, nil
	case domainmodels.DownloadReasonOriginalBytesUnavailable:
		return DownloadReasonCodeOriginalBytesUnavailable, nil
	case domainmodels.DownloadReasonSourceUnreadable:
		return DownloadReasonCodeSourceUnreadable, nil
	case domainmodels.DownloadReasonCopyNotInStorage:
		return DownloadReasonCodeCopyNotInStorage, nil
	case domainmodels.DownloadReasonCopyMissing:
		return DownloadReasonCodeCopyMissing, nil
	case domainmodels.DownloadReasonCopyDiffers:
		return DownloadReasonCodeCopyDiffers, nil
	case domainmodels.DownloadReasonReadFailed:
		return DownloadReasonCodeReadFailed, nil
	default:
		return "", fmt.Errorf("unknown download reason code: %s", code)
	}
}
