package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func marshalJSON(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	if string(data) == "null" {
		return "{}", nil
	}
	return string(data), nil
}

func unmarshalJSON(data []byte, dest any) error {
	if len(data) == 0 || string(data) == "null" {
		data = []byte("{}")
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	return nil
}

func stringPtrFromSQL(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func uuidPtrFromSQL(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := value.UUID
	return &id
}

func intPtrFromSQL(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	number := int(value.Int64)
	return &number
}

func int64PtrFromSQL(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func timePtrFromSQL(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

type deleteResult struct {
	MatchedCount int64 `db:"matched_count"`
	DeletedCount int64 `db:"deleted_count"`
}
