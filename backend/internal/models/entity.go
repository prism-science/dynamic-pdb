package models

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnexpectedEntityType = errors.New("models: unexpected entity type")
	ErrInvalidEntityPayload = errors.New("models: invalid entity payload")
)

type EntityLevel string

const (
	EntityLevelL0 EntityLevel = "L0"
	EntityLevelL1 EntityLevel = "L1"
	EntityLevelL2 EntityLevel = "L2"
	EntityLevelL3 EntityLevel = "L3"
)

type EntityType string

const (
	EntityTypeMetrics EntityType = "metrics"
	EntityTypeModel   EntityType = "model"
)

type Entity struct {
	ID           uuid.UUID
	EntryID      uuid.UUID
	ExperimentID *uuid.UUID
	Type         EntityType
	Level        *EntityLevel
	Name         string
	Payload      any
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (e Entity) Model() (*ModelPayload, error) {
	if e.Type != EntityTypeModel {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrUnexpectedEntityType, EntityTypeModel, e.Type)
	}

	switch payload := e.Payload.(type) {
	case *ModelPayload:
		if payload == nil {
			return nil, fmt.Errorf("%w: model payload is nil", ErrInvalidEntityPayload)
		}
		return payload, nil
	case ModelPayload:
		return &payload, nil
	default:
		return nil, fmt.Errorf("%w: model payload has type %T", ErrInvalidEntityPayload, e.Payload)
	}
}

func (e Entity) Metrics() (*MetricsPayload, error) {
	if e.Type != EntityTypeMetrics {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrUnexpectedEntityType, EntityTypeMetrics, e.Type)
	}

	switch payload := e.Payload.(type) {
	case *MetricsPayload:
		if payload == nil {
			return nil, fmt.Errorf("%w: metrics payload is nil", ErrInvalidEntityPayload)
		}
		return payload, nil
	case MetricsPayload:
		return &payload, nil
	default:
		return nil, fmt.Errorf("%w: metrics payload has type %T", ErrInvalidEntityPayload, e.Payload)
	}
}
