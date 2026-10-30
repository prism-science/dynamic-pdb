package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/models"
)

// fieldSourcesToSQL encodes revision field sources. Nil sources, not recorded, are SQL NULL.
func fieldSourcesToSQL(sources models.FieldSources) (*string, error) {
	if sources == nil {
		return nil, nil
	}
	data, err := json.Marshal(sources)
	if err != nil {
		return nil, fmt.Errorf("marshal field sources: %w", err)
	}
	return new(string(data)), nil
}

// fieldSourcesFromSQL decodes revision field sources. SQL NULL decodes to nil sources.
func fieldSourcesFromSQL(data []byte) (models.FieldSources, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var sources models.FieldSources
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil, fmt.Errorf("unmarshal field sources: %w", err)
	}
	return sources, nil
}

// optionalJSONToSQL encodes a revision value that may not be recorded. Nil is SQL NULL.
func optionalJSONToSQL[T any](value *T) (*string, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return new(string(data)), nil
}

// optionalJSONFromSQL decodes a revision value that may not be recorded. SQL NULL decodes to nil.
func optionalJSONFromSQL[T any](data []byte) (*T, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("unmarshal json: %w", err)
	}
	return &value, nil
}

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
