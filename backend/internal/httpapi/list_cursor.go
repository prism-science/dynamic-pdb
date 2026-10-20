package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
)

// listCursorKind names the list a cursor was issued by. A cursor is only accepted by
// lists of the same kind, so a cursor copied between lists fails instead of paging
// from an unrelated position.
type listCursorKind string

const (
	listCursorKindEntries        listCursorKind = "entries"
	listCursorKindModels         listCursorKind = "models"
	listCursorKindArtifacts      listCursorKind = "artifacts"
	listCursorKindEntryRevisions listCursorKind = "entry_revisions"
	listCursorKindModelRevisions listCursorKind = "model_revisions"
	listCursorKindRevisionGroups listCursorKind = "revision_groups"
	listCursorKindSimilarEntries listCursorKind = "similar_entries"
)

// maxListCursorLength bounds the cursor query value before it is decoded.
// It matches maxLength of the cursor parameter in openapi.yaml.
const maxListCursorLength = 512

var errInvalidListCursor = errors.New("invalid list cursor")

// listCursor is the decoded form of the opaque `cursor` query parameter: the keyset
// position of the last row on the previous page. Which fields make up the position
// depends on the kind's sort order:
//   - row lists (entries, models, artifacts, revisions): CreatedAt and ID
//   - revision groups: CreatedAt (of the group's newest revision) and EntryID
//   - similar entries: Score and EntryID
type listCursor struct {
	Kind      listCursorKind `json:"k"`
	CreatedAt time.Time      `json:"t,omitzero"`
	ID        uuid.UUID      `json:"i,omitzero"`
	EntryID   string         `json:"e,omitempty"`
	Score     *float64       `json:"c,omitempty"`
	// ProteinSequenceSearch records that an entry search fell back to protein-sequence
	// matching, so the following pages keep matching sequences instead of text.
	ProteinSequenceSearch bool `json:"s,omitempty"`
}

func (c listCursor) position() *db.ListPosition {
	return &db.ListPosition{CreatedAt: c.CreatedAt, ID: c.ID}
}

func (c listCursor) revisionGroupPosition() *db.RevisionGroupPosition {
	return &db.RevisionGroupPosition{LatestCreatedAt: c.CreatedAt, EntryID: c.EntryID}
}

func (c listCursor) similarEntryPosition() *db.SimilarEntryPosition {
	return &db.SimilarEntryPosition{Score: *c.Score, EntryID: c.EntryID}
}

func encodeListCursor(cursor listCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal list cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeListCursor(value string, expectedKind listCursorKind) (listCursor, error) {
	if len(value) > maxListCursorLength {
		return listCursor{}, fmt.Errorf("%w: longer than %d characters", errInvalidListCursor, maxListCursorLength)
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return listCursor{}, fmt.Errorf("%w: decode base64: %w", errInvalidListCursor, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cursor listCursor
	if err := decoder.Decode(&cursor); err != nil {
		return listCursor{}, fmt.Errorf("%w: decode json: %w", errInvalidListCursor, err)
	}

	if err := cursor.validatePosition(); err != nil {
		return listCursor{}, fmt.Errorf("%w: %w", errInvalidListCursor, err)
	}
	if cursor.Kind != expectedKind {
		return listCursor{}, fmt.Errorf("%w: issued by the %s list, not %s", errInvalidListCursor, cursor.Kind, expectedKind)
	}
	if cursor.ProteinSequenceSearch && cursor.Kind != listCursorKindEntries {
		return listCursor{}, fmt.Errorf("%w: protein sequence search is only valid for entries", errInvalidListCursor)
	}
	return cursor, nil
}

// validatePosition checks that the cursor carries exactly the position fields its
// kind sorts by, so a hand-edited cursor cannot page by a half-set position.
func (c listCursor) validatePosition() error {
	hasCreatedAt := !c.CreatedAt.IsZero()
	hasID := c.ID != uuid.Nil
	hasEntryID := c.EntryID != ""
	hasScore := c.Score != nil

	switch c.Kind {
	case listCursorKindEntries,
		listCursorKindModels,
		listCursorKindArtifacts,
		listCursorKindEntryRevisions,
		listCursorKindModelRevisions:
		if !hasCreatedAt || !hasID || hasEntryID || hasScore {
			return errors.New("row position needs created_at and id only")
		}
	case listCursorKindRevisionGroups:
		if !hasCreatedAt || !hasEntryID || hasID || hasScore {
			return errors.New("revision group position needs created_at and entry_id only")
		}
	case listCursorKindSimilarEntries:
		if !hasScore || !hasEntryID || hasCreatedAt || hasID {
			return errors.New("similar entry position needs score and entry_id only")
		}
	default:
		return fmt.Errorf("unknown kind: %s", c.Kind)
	}
	return nil
}
