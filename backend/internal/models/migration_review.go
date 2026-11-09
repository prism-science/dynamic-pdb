package models

import "time"

// MigrationReview is one schema_migration_review row: something a migration or a job could not
// settle on its own, for a person to look at. Details is stored as JSON.
type MigrationReview struct {
	ObjectType string
	ObjectID   string
	Issue      string
	Details    any
}

// MigrationReviewResolution names the open schema_migration_review rows a job settled: the rows
// of ObjectType with Issue for each of ObjectIDs. ResolvedBy names the job.
type MigrationReviewResolution struct {
	ObjectType string
	Issue      string
	ObjectIDs  []string
	ResolvedBy string
}

// ReviewFlag is an open schema_migration_review row as a public warning on a record: what it is
// on, the issue, and when it was first flagged. Its details stay internal.
type ReviewFlag struct {
	ObjectType string
	ObjectID   string
	Issue      string
	FlaggedAt  time.Time
}
