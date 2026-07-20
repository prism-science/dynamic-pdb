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
	EntityTypeData    EntityType = "data"
	EntityTypeMetrics EntityType = "metrics"
	EntityTypeModel   EntityType = "model"
	EntityTypeProgram EntityType = "program"
)

type RelationType string

const (
	RelationInputTo    RelationType = "input_to"
	RelationOutputOf   RelationType = "output_of"
	RelationMetricsFor RelationType = "metrics_for"
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

type EntityRelation struct {
	ID             uuid.UUID
	SourceEntityID uuid.UUID
	TargetEntityID uuid.UUID
	RelationType   RelationType
	CreatedAt      time.Time
	UpdatedAt      time.Time
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

func (e Entity) Data() (*DataPayload, error) {
	if e.Type != EntityTypeData {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrUnexpectedEntityType, EntityTypeData, e.Type)
	}

	switch payload := e.Payload.(type) {
	case *DataPayload:
		if payload == nil {
			return nil, fmt.Errorf("%w: data payload is nil", ErrInvalidEntityPayload)
		}
		return payload, nil
	case DataPayload:
		return &payload, nil
	default:
		return nil, fmt.Errorf("%w: data payload has type %T", ErrInvalidEntityPayload, e.Payload)
	}
}

func (e Entity) Program() (*ProgramPayload, error) {
	if e.Type != EntityTypeProgram {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrUnexpectedEntityType, EntityTypeProgram, e.Type)
	}

	switch payload := e.Payload.(type) {
	case *ProgramPayload:
		if payload == nil {
			return nil, fmt.Errorf("%w: program payload is nil", ErrInvalidEntityPayload)
		}
		return payload, nil
	case ProgramPayload:
		return &payload, nil
	default:
		return nil, fmt.Errorf("%w: program payload has type %T", ErrInvalidEntityPayload, e.Payload)
	}
}
