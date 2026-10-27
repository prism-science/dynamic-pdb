package models

import "time"

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
