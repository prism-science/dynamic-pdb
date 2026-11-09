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
// the models from 1, in the order GET /v1/models lists them.
type SelectionModel struct {
	Ordinal             int
	ModelID             string
	EntryID             string
	ModelRevisionID     uuid.UUID
	ModelRevisionNumber int
	EntryRevisionID     uuid.UUID
	EntryRevisionNumber int
}
