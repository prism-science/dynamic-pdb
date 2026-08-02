package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"dynamic-pdb/backend/internal/models"
)

type EntitiesRepository struct {
	db       *sqlx.DB
	queriers *QuerierProvider
}

type EntityFilters struct {
	StructureID        *uuid.UUID
	ModelID            *uuid.UUID
	BelongsToStructure bool
	Types              []models.EntityType
	Levels             []models.EntityLevel
	Limit              *int
	Offset             *int
}

func NewEntitiesRepository(database *sqlx.DB, queriers *QuerierProvider) *EntitiesRepository {
	return &EntitiesRepository{
		db:       database,
		queriers: queriers,
	}
}

func (r *EntitiesRepository) Create(ctx context.Context, entity models.Entity) (*models.Entity, error) {
	payload, err := marshalEntityPayload(entity.Payload)
	if err != nil {
		return nil, fmt.Errorf("prepare entity payload: %w", err)
	}

	query := `insert into entities(id, structure_id, model_id, type, level, name, payload, created_at, updated_at)
			  values (:id, :structure_id, :model_id, :type, :level, :name, cast(:payload as jsonb), :created_at, :updated_at)
			  returning id, structure_id, model_id, type, level, name, payload, created_at, updated_at`

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var row entityRow
	if err := stmt.GetContext(ctx, &row, map[string]any{
		"id":           entity.ID,
		"structure_id": entity.StructureID,
		"model_id":     nullableUUID(entity.ModelID),
		"type":         string(entity.Type),
		"level":        nullableEntityLevel(entity.Level),
		"name":         entity.Name,
		"payload":      payload,
		"created_at":   entity.CreatedAt,
		"updated_at":   entity.UpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert entity: %w", err)
	}

	created, err := entityFromRow(&row)
	if err != nil {
		return nil, fmt.Errorf("decode inserted entity: %w", err)
	}
	return created, nil
}

func (r *EntitiesRepository) List(ctx context.Context, filters EntityFilters) ([]models.Entity, error) {
	query, args, err := entityListQuery(filters)
	if err != nil {
		return nil, fmt.Errorf("build entity list query: %w", err)
	}

	stmt, err := r.queriers.Querier(ctx, r.db).PrepareNamedContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryxContext(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to list entities: %w", err)
	}
	defer rows.Close()

	entities := make([]models.Entity, 0)
	for rows.Next() {
		var row entityRow
		if err := rows.StructScan(&row); err != nil {
			return nil, fmt.Errorf("scan entity row: %w", err)
		}

		entity, err := entityFromRow(&row)
		if err != nil {
			return nil, fmt.Errorf("decode entity: %w", err)
		}
		entities = append(entities, *entity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity rows: %w", err)
	}

	return entities, nil
}

func entityListQuery(filters EntityFilters) (string, map[string]any, error) {
	if filters.Limit != nil && *filters.Limit < 0 {
		return "", nil, errors.New("limit must be non-negative")
	}
	if filters.Offset != nil && *filters.Offset < 0 {
		return "", nil, errors.New("offset must be non-negative")
	}

	conditions := make([]string, 0)
	args := map[string]any{}

	if filters.StructureID != nil {
		conditions = append(conditions, "structure_id = :structure_id")
		args["structure_id"] = *filters.StructureID
	}
	if filters.ModelID != nil {
		conditions = append(conditions, "model_id = :model_id")
		args["model_id"] = *filters.ModelID
	}
	if filters.BelongsToStructure {
		conditions = append(conditions, "model_id is null")
	}
	if len(filters.Types) > 0 {
		conditions = append(conditions, "type = any(cast(:types as text[]))")
		args["types"] = pq.Array(entityTypeStrings(filters.Types))
	}
	if len(filters.Levels) > 0 {
		conditions = append(conditions, "level = any(cast(:levels as text[]))")
		args["levels"] = pq.Array(entityLevelStrings(filters.Levels))
	}

	query := `select id, structure_id, model_id, type, level, name, payload, created_at, updated_at
			  from entities`
	if len(conditions) > 0 {
		query += "\nwhere " + strings.Join(conditions, "\n  and ")
	}
	query += "\norder by created_at asc, id asc"

	if filters.Limit != nil {
		query += "\nlimit :limit"
		args["limit"] = *filters.Limit
	}
	if filters.Offset != nil {
		query += "\noffset :offset"
		args["offset"] = *filters.Offset
	}

	return query, args, nil
}

func marshalEntityPayload(payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	return string(data), nil
}

func entityFromRow(row *entityRow) (*models.Entity, error) {
	entityType := models.EntityType(row.Type)
	payload, err := entityPayloadFromStorage(entityType, row.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	return &models.Entity{
		ID:          row.ID,
		StructureID: row.StructureID,
		ModelID:     uuidPtrFromNull(row.ModelID),
		Type:        entityType,
		Level:       entityLevelPtrFromNull(row.Level),
		Name:        row.Name,
		Payload:     payload,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func entityPayloadFromStorage(entityType models.EntityType, data []byte) (any, error) {
	if len(data) == 0 || string(data) == "null" {
		data = []byte("{}")
	}

	switch entityType {
	case models.EntityTypeData:
		var payload models.DataPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("unmarshal data payload: %w", err)
		}
		return &payload, nil
	case models.EntityTypeModel:
		var payload models.ModelPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("unmarshal model payload: %w", err)
		}
		return &payload, nil
	case models.EntityTypeMetrics:
		var payload models.MetricsPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("unmarshal metrics payload: %w", err)
		}
		return &payload, nil
	case models.EntityTypeProgram:
		var payload models.ProgramPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("unmarshal program payload: %w", err)
		}
		return &payload, nil
	default:
		return nil, fmt.Errorf("%w: %s", models.ErrUnexpectedEntityType, entityType)
	}
}

func nullableUUID(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableEntityLevel(value *models.EntityLevel) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func uuidPtrFromNull(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	id := value.UUID
	return &id
}

func entityLevelPtrFromNull(value sql.NullString) *models.EntityLevel {
	if !value.Valid {
		return nil
	}
	level := models.EntityLevel(value.String)
	return &level
}

func entityTypeStrings(values []models.EntityType) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func entityLevelStrings(values []models.EntityLevel) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

type entityRow struct {
	ID          uuid.UUID      `db:"id"`
	StructureID uuid.UUID      `db:"structure_id"`
	ModelID     uuid.NullUUID  `db:"model_id"`
	Type        string         `db:"type"`
	Level       sql.NullString `db:"level"`
	Name        string         `db:"name"`
	Payload     []byte         `db:"payload"`
	CreatedAt   time.Time      `db:"created_at"`
	UpdatedAt   time.Time      `db:"updated_at"`
}
