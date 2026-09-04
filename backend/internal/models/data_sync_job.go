package models

import "time"

type DataSyncJob struct {
	EntryID     string
	ModelID     *string
	ScheduledAt time.Time
}
