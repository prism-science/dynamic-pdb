package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

const (
	// syncValidationFailedIssue is the schema_migration_review issue of an object whose latest
	// RCSB version breaks the schema rules, so the sync keeps its current revision.
	syncValidationFailedIssue = "sync_validation_failed"
	// dataSyncResolver names the data sync in schema_migration_review.resolved_by.
	dataSyncResolver = "data-sync"
)

// syncValidationFailure is the details of a sync_validation_failed review row.
type syncValidationFailure struct {
	PDBID               string                `json:"pdb_id"`
	ArchiveVersion      string                `json:"archive_version"`
	ArchiveRevisionDate *time.Time            `json:"archive_revision_date,omitempty"`
	Errors              []syncValidationError `json:"errors"`
}

// syncValidationError is one schema violation of a rejected archive version.
type syncValidationError struct {
	Pointer string `json:"pointer"`
	Code    string `json:"code"`
	Detail  string `json:"detail"`
}

// skipRejectedVersion keeps the job's object as it is when RCSB's latest version breaks the schema
// rules. It opens a review row for a person, logs the version, and tries again at the normal
// cadence instead of within the hour: RCSB data rarely changes sooner, and each try downloads the
// entry's file.
func (j *DataSyncJob) skipRejectedVersion(ctx context.Context, job models.DataSyncJob, rejected rejectedArchiveVersion) error {
	objectType, objectID := syncReviewObject(job)
	if err := j.database.MigrationReviews.Open(ctx, models.MigrationReview{
		ObjectType: objectType,
		ObjectID:   objectID,
		Issue:      syncValidationFailedIssue,
		Details:    syncValidationFailureOf(rejected),
	}); err != nil {
		return fmt.Errorf("report rejected archive version: %w", err)
	}
	job.ScheduledAt = j.nextScheduledAt(j.now())
	if err := j.database.DataSyncJobs.Schedule(ctx, job); err != nil {
		return fmt.Errorf("reschedule data sync job: %w", err)
	}
	j.logger.Warn(
		"data sync skipped archive version",
		"entry_id", job.EntryID,
		"model_id", stringValue(job.ModelID),
		"pdb_id", rejected.pdbID,
		"archive_version", rejected.version.Version,
		"next_run_at", job.ScheduledAt,
		"errors", describeFieldErrors(rejected.fieldErrors),
	)
	return nil
}

// resolveRejectedVersion closes the job's open sync_validation_failed row, if any, once the job
// has applied or recorded an archive version.
func (j *DataSyncJob) resolveRejectedVersion(ctx context.Context, job models.DataSyncJob) error {
	objectType, objectID := syncReviewObject(job)
	if err := j.database.MigrationReviews.Resolve(ctx, models.MigrationReviewResolution{
		ObjectType: objectType,
		Issue:      syncValidationFailedIssue,
		ObjectIDs:  []string{objectID},
		ResolvedBy: dataSyncResolver,
	}); err != nil {
		return fmt.Errorf("resolve rejected archive version: %w", err)
	}
	return nil
}

// syncReviewObject names the object a job's review row is about: the model of a model job, or the
// entry of an entry job.
func syncReviewObject(job models.DataSyncJob) (objectType string, objectID string) {
	if job.ModelID != nil {
		return "model", *job.ModelID
	}
	return "entry", job.EntryID
}

func syncValidationFailureOf(rejected rejectedArchiveVersion) syncValidationFailure {
	failure := syncValidationFailure{
		PDBID:               rejected.pdbID,
		ArchiveVersion:      rejected.version.Version,
		ArchiveRevisionDate: rejected.version.RevisionDate,
		Errors:              make([]syncValidationError, 0, len(rejected.fieldErrors)),
	}
	for _, fieldError := range rejected.fieldErrors {
		failure.Errors = append(failure.Errors, syncValidationError{
			Pointer: string(fieldError.Pointer),
			Code:    string(fieldError.Code),
			Detail:  fieldError.Detail,
		})
	}
	return failure
}

func describeFieldErrors(fieldErrors []validation.FieldError) string {
	descriptions := make([]string, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		descriptions = append(descriptions, fieldError.String())
	}
	return strings.Join(descriptions, "; ")
}
