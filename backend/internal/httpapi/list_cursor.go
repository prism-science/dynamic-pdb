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
	listCursorKindEntries listCursorKind = "entries"
	listCursorKindModels  listCursorKind = "models"
)

// maxListCursorLength bounds the cursor query value before it is decoded.
// It matches maxLength of the cursor parameter in openapi.yaml.
const maxListCursorLength = 512

var errInvalidListCursor = errors.New("invalid list cursor")

// listCursor is the decoded form of the opaque `cursor` query parameter: the keyset
// position of the last row on the previous page.
type listCursor struct {
	Kind      listCursorKind `json:"k"`
	CreatedAt time.Time      `json:"t"`
	ID        uuid.UUID      `json:"i"`
	// ProteinSequenceSearch records that an entry search fell back to protein-sequence
	// matching, so the following pages keep matching sequences instead of text.
	ProteinSequenceSearch bool `json:"s,omitempty"`
}

func (c listCursor) position() *db.ListPosition {
	return &db.ListPosition{CreatedAt: c.CreatedAt, ID: c.ID}
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

	switch cursor.Kind {
	case listCursorKindEntries, listCursorKindModels:
	default:
		return listCursor{}, fmt.Errorf("%w: unknown kind: %s", errInvalidListCursor, cursor.Kind)
	}
	if cursor.Kind != expectedKind {
		return listCursor{}, fmt.Errorf("%w: issued by the %s list, not %s", errInvalidListCursor, cursor.Kind, expectedKind)
	}
	if cursor.CreatedAt.IsZero() || cursor.ID == uuid.Nil {
		return listCursor{}, fmt.Errorf("%w: missing position", errInvalidListCursor)
	}
	if cursor.ProteinSequenceSearch && cursor.Kind != listCursorKindEntries {
		return listCursor{}, fmt.Errorf("%w: protein sequence search is only valid for entries", errInvalidListCursor)
	}
	return cursor, nil
}
