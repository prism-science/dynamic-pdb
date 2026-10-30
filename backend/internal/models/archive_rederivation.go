package models

import "github.com/google/uuid"

// ArchiveRederivationOutcome is what the archive re-derivation job did to one revision. The
// archive_rederivations migration describes each outcome.
type ArchiveRederivationOutcome string

const (
	ArchiveRederivationDerived          ArchiveRederivationOutcome = "derived"
	ArchiveRederivationUnchanged        ArchiveRederivationOutcome = "unchanged"
	ArchiveRederivationSkippedPersonSet ArchiveRederivationOutcome = "skipped_person_set"
	ArchiveRederivationSkippedInReview  ArchiveRederivationOutcome = "skipped_in_review"
	ArchiveRederivationSkippedNotActive ArchiveRederivationOutcome = "skipped_not_active"
	// ArchiveRederivationSkippedIncompleteFile means the head of the file could not be parsed or
	// ends before the coordinates, so nothing was derived.
	ArchiveRederivationSkippedIncompleteFile ArchiveRederivationOutcome = "skipped_incomplete_file"
	ArchiveRederivationFailed                ArchiveRederivationOutcome = "failed"
)

// ArchiveRederivationCandidate is an active archive entry revision the job has not finished.
type ArchiveRederivationCandidate struct {
	EntryRevisionID uuid.UUID
	EntryID         string
	PDBID           string
}

// ArchiveRederivation is one row of the job's log. Before and After hold what the job changed;
// both are nil when it changed nothing. Error says why the job failed or found the file
// incomplete.
type ArchiveRederivation struct {
	ArchiveRederivationCandidate
	Outcome          ArchiveRederivationOutcome
	DryRun           bool
	ReferenceVersion *string
	Before           *ArchiveRederivationValues
	After            *ArchiveRederivationValues
	Error            *string
}

// ArchiveRederivationValues are the values the job changed on one revision, before or after
// the change. Compounds is nil when the compounds did not change; PolymerEntities lists only the
// entities whose mutations changed.
type ArchiveRederivationValues struct {
	Compounds       *EntryComposition               `json:"compounds,omitempty"`
	PolymerEntities []ArchivePolymerEntityMutations `json:"polymer_entities,omitempty"`
}

// ArchivePolymerEntityMutations are the mutation fields of one polymer entity.
type ArchivePolymerEntityMutations struct {
	ID              uuid.UUID        `json:"id"`
	Mutations       []string         `json:"mutations"`
	MutationsRaw    *string          `json:"mutations_raw"`
	MutationsStatus *MutationsStatus `json:"mutations_status"`
}

// MigrationReviewResolution names the open schema_migration_review rows a job settled: the rows
// of ObjectType with Issue for each of ObjectIDs. ResolvedBy names the job.
type MigrationReviewResolution struct {
	ObjectType string
	Issue      string
	ObjectIDs  []string
	ResolvedBy string
}
