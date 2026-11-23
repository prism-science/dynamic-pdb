package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DownloadFileChoice is which files of a selection a bulk-download archive holds (researcher
// item 8).
type DownloadFileChoice string

const (
	// DownloadFileChoiceOriginalMMCIF is the PDB's coordinate file, byte for byte, of each selected
	// model whose own record holds it (usually the Deposited models).
	DownloadFileChoiceOriginalMMCIF DownloadFileChoice = "original_mmcif"
	// DownloadFileChoiceModifiedCoordinates is the coordinate files of the selected Dynamic PDB
	// models (Single Conformer, Multiconformer, Ensemble).
	DownloadFileChoiceModifiedCoordinates DownloadFileChoice = "modified_coordinates"
	// DownloadFileChoiceAllFiles is every file of the selected models and their entries.
	DownloadFileChoiceAllFiles DownloadFileChoice = "all_files"
)

// DownloadFileChoices lists every file choice, in the order the API documents them.
var DownloadFileChoices = []DownloadFileChoice{
	DownloadFileChoiceOriginalMMCIF,
	DownloadFileChoiceModifiedCoordinates,
	DownloadFileChoiceAllFiles,
}

func (choice DownloadFileChoice) Validate() error {
	switch choice {
	case DownloadFileChoiceOriginalMMCIF, DownloadFileChoiceModifiedCoordinates, DownloadFileChoiceAllFiles:
		return nil
	default:
		return unknownValueError("download file choice", choice, DownloadFileChoices)
	}
}

// DownloadStatus is where a bulk-download archive stands. A ready archive past its expiry is
// reported as expired (DownloadJob.ReportedStatus); expired is never stored.
type DownloadStatus string

const (
	// DownloadStatusQueued waits for the archive worker.
	DownloadStatusQueued DownloadStatus = "queued"
	// DownloadStatusPlanning means the worker reads the selection and adds up the bytes.
	DownloadStatusPlanning DownloadStatus = "planning"
	// DownloadStatusPreparing means the worker writes the zip.
	DownloadStatusPreparing DownloadStatus = "preparing"
	// DownloadStatusReady means the zip and its manifest are in the bucket until the expiry.
	DownloadStatusReady DownloadStatus = "ready"
	// DownloadStatusBlocked means the files to include add up to more than the limit; nothing was
	// written.
	DownloadStatusBlocked DownloadStatus = "blocked"
	// DownloadStatusFailed means the worker gave up; the job's error says why.
	DownloadStatusFailed DownloadStatus = "failed"
	// DownloadStatusExpired is a ready archive past its expiry, as the API reports it.
	DownloadStatusExpired DownloadStatus = "expired"
)

// DownloadCounts are an archive manifest's file rows by availability.
type DownloadCounts struct {
	Included        int
	Linked          int
	Unavailable     int
	ChangedAtSource int
}

// DownloadJob is one request to build a bulk-download archive of a frozen selection.
type DownloadJob struct {
	ID              uuid.UUID
	SelectionID     uuid.UUID
	FileChoice      DownloadFileChoice
	Status          DownloadStatus
	MaxArchiveBytes int64
	CreatedAt       time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	ExpiresAt       *time.Time
	LeaseUntil      *time.Time
	// Attempts counts the worker's claims of the job; each write of a worker names its attempt.
	Attempts      int
	PlannedBytes  *int64
	ArchiveKey    *string
	ArchiveURL    *string
	ArchiveBytes  *int64
	ArchiveSHA256 *string
	ManifestKey   *string
	ManifestURL   *string
	// Counts is set on a ready archive.
	Counts        *DownloadCounts
	BlockedReason *string
	Error         *string
}

// ReportedStatus is the status the API shows at now: a ready archive past its expiry is expired.
func (job DownloadJob) ReportedStatus(now time.Time) (DownloadStatus, error) {
	switch job.Status {
	case DownloadStatusReady:
		if job.ExpiresAt == nil {
			return "", fmt.Errorf("download %s is ready without an expiry", job.ID)
		}
		if !now.Before(*job.ExpiresAt) {
			return DownloadStatusExpired, nil
		}
		return DownloadStatusReady, nil
	case DownloadStatusQueued, DownloadStatusPlanning, DownloadStatusPreparing,
		DownloadStatusBlocked, DownloadStatusFailed:
		return job.Status, nil
	case DownloadStatusExpired:
		return "", fmt.Errorf("download %s has the reported-only status %s stored", job.ID, job.Status)
	default:
		return "", fmt.Errorf("unknown download status: %s", job.Status)
	}
}

// DownloadSelectionModel is what an archive's manifest says about one model of a frozen
// selection and its entry, read from the pinned revisions. Values the record lacks are nil.
type DownloadSelectionModel struct {
	Ordinal         int
	ModelType       *string
	Origin          *string
	ModelPDBID      *string
	Authors         []string
	Affiliation     *string
	EntryPDBID      *string
	RelatedDatasets []RelatedDataset
}
