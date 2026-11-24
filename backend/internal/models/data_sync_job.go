package models

import "time"

// Stop reasons of data sync jobs whose entry or model has nothing left to sync.
const (
	// DataSyncStopReasonEntryDeleted stops the jobs of a deleted entry.
	DataSyncStopReasonEntryDeleted = "entry deleted"
	// DataSyncStopReasonEntryNeverApproved stops the job of an entry that was never approved and
	// has no revision left in review: nothing can make one of its revisions active.
	DataSyncStopReasonEntryNeverApproved = "entry was never approved and has no revision in review"
	// DataSyncStopReasonModelDeleted stops the job of a deleted model.
	DataSyncStopReasonModelDeleted = "model deleted"
)

type DataSyncJob struct {
	EntryID     string
	ModelID     *string
	ScheduledAt time.Time
	// ArchiveVersion is the archive version the job last applied or recorded as its baseline;
	// nil until the job first runs.
	ArchiveVersion *ArchiveVersion
}

// ArchiveVersion identifies one version of an RCSB entry: its ordinal in the entry's revision
// history, its major.minor number, and the date RCSB released it.
type ArchiveVersion struct {
	Ordinal      int
	Version      string
	RevisionDate *time.Time
}
