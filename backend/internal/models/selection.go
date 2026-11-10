package models

import (
	"time"

	"github.com/google/uuid"
)

// SelectionRequest is what a client asked a frozen selection to hold: models by ID, every model
// matching a search, minus excluded models. Each search is a GET /v1/models query string, "" for
// every model.
type SelectionRequest struct {
	ModelIDs        []string `json:"model_ids"`
	Searches        []string `json:"searches"`
	ExcludeModelIDs []string `json:"exclude_model_ids"`
}

// Selection is a frozen selection: the models a request matched when it was saved, each with
// the model revision and entry revision active then.
type Selection struct {
	ID         uuid.UUID
	ModelCount int
	EntryCount int
	Request    SelectionRequest
	CreatedAt  time.Time
}

// SelectionModel is one model of a frozen selection with its frozen revisions. Ordinal numbers
// the models from 1, in the order GET /v1/models lists them. Files are the files of the two
// revisions; a published revision's files never change, so they are the files the selection
// pinned when it was saved.
type SelectionModel struct {
	Ordinal             int
	ModelID             string
	EntryID             string
	ModelRevisionID     uuid.UUID
	ModelRevisionNumber int
	EntryRevisionID     uuid.UUID
	EntryRevisionNumber int
	Files               []SelectionFile
}

// SelectionFileRole is how a file belongs to a selection model's revisions.
type SelectionFileRole string

const (
	// SelectionFileRolePrimary is the model revision's primary file.
	SelectionFileRolePrimary SelectionFileRole = "primary"
	// SelectionFileRoleModel is another file of the model revision.
	SelectionFileRoleModel SelectionFileRole = "model"
	// SelectionFileRoleRunInput is an input of a run of the model revision.
	SelectionFileRoleRunInput SelectionFileRole = "run_input"
	// SelectionFileRoleRunOutput is an output of a run of the model revision.
	SelectionFileRoleRunOutput SelectionFileRole = "run_output"
	// SelectionFileRoleEntry is a file of the entry revision.
	SelectionFileRoleEntry SelectionFileRole = "entry"
)

// SelectionSHA256Source says where a selection file's recorded SHA-256 comes from.
type SelectionSHA256Source string

const (
	// SelectionSHA256SourceDeclared is the SHA-256 recorded with the file when it was added.
	SelectionSHA256SourceDeclared SelectionSHA256Source = "declared"
	// SelectionSHA256SourceComputed is the SHA-256 the checksum job computed from the file's
	// bytes, for a file added without one.
	SelectionSHA256SourceComputed SelectionSHA256Source = "computed"
	// SelectionSHA256SourceNotRecorded means the file has no SHA-256 yet: none was declared and
	// the checksum job has not read its bytes.
	SelectionSHA256SourceNotRecorded SelectionSHA256Source = "not_recorded"
)

// SelectionFile is one file a frozen selection names through its pinned revisions: the file's
// identity as recorded (ID, location, declared size), the SHA-256 the selection names, and the
// checksum job's last check of the bytes. Roles lists every way the file belongs to the model,
// in the order of the SelectionFileRole constants.
type SelectionFile struct {
	ID                   uuid.UUID
	Roles                []SelectionFileRole
	Name                 string
	Level                ArtifactLevel
	Type                 ArtifactType
	Format               *string
	URI                  *string
	SizeBytes            *int64
	RecordedSHA256       *string
	RecordedSHA256Source SelectionSHA256Source
	// RecordedAfterSave is true when the recorded SHA-256 was computed after the selection was
	// saved: it describes the bytes the checksum job read then, not bytes known at the save.
	RecordedAfterSave bool
	Checksum          ArtifactChecksum
}
