package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

var errInvalidCreateEntryRequest = errors.New("invalid create entry request")

func (s *Server) ListEntries(w http.ResponseWriter, r *http.Request, params ListEntriesParams) {
	filters, err := entryFiltersFromParams(params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid entry filters")
		return
	}

	entries, err := s.database.Entries.List(r.Context(), filters)
	if err != nil {
		slog.Error("list entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entries")
		return
	}

	items := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, entryResponseFromModel(entry))
	}

	writeJSON(w, http.StatusOK, EntryListResponse{Items: items})
}

func (s *Server) CreateEntry(w http.ResponseWriter, r *http.Request) {
	var req CreateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "entry name is required")
		return
	}

	err := s.createEntryGraph(r.Context(), req, name)
	if errors.Is(err, errInvalidCreateEntryRequest) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err != nil {
		slog.Error("create entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create entry")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) GetEntry(w http.ResponseWriter, r *http.Request, entryID uuid.UUID) {
	entry, err := s.database.Entries.Get(r.Context(), entryID)
	if errors.Is(err, db.ErrEntryNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	}
	if err != nil {
		slog.Error("get entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get entry")
		return
	}

	writeJSON(w, http.StatusOK, entryResponseFromModel(*entry))
}

func entryFiltersFromParams(params ListEntriesParams) (db.EntryFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.EntryFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.EntryFilters{}, errors.New("offset must be non-negative")
	}

	search := ""
	if params.Query != nil {
		search = strings.TrimSpace(*params.Query)
	}

	return db.EntryFilters{
		Limit:  params.Limit,
		Offset: params.Offset,
		Query:  search,
	}, nil
}

func entryResponseFromModel(entry domainmodels.Entry) Entry {
	return Entry{
		Id:                entry.ID,
		Name:              entry.Name,
		Description:       entry.Description,
		ThumbnailImageUrl: entry.ThumbnailImageURL,
		CreatedAt:         entry.CreatedAt,
		UpdatedAt:         entry.UpdatedAt,
	}
}

func (s *Server) createEntryGraph(ctx context.Context, req CreateEntryRequest, name string) error {
	now := time.Now().UTC()
	entryID := uuid.New()
	if req.Id != nil {
		entryID = uuid.UUID(*req.Id)
		if entryID == uuid.Nil {
			return invalidCreateEntryRequest("entry id is required")
		}
	}

	return s.database.Do(ctx, func(ctx context.Context) error {
		entry, err := s.database.Entries.Create(ctx, domainmodels.Entry{
			ID:                entryID,
			Name:              name,
			Description:       req.Description,
			ThumbnailImageURL: req.ThumbnailImageUrl,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return fmt.Errorf("create entry: %w", err)
		}
		if err := s.database.EntrySearch.IndexEntry(ctx, *entry); err != nil {
			return fmt.Errorf("index entry search: %w", err)
		}

		entryEntityIDs := make(map[uuid.UUID]struct{})
		createdEntityIDs := make(map[uuid.UUID]struct{})
		if req.Entities != nil {
			for _, entityRequest := range *req.Entities {
				entityID := uuid.UUID(entityRequest.Id)
				if _, exists := createdEntityIDs[entityID]; exists {
					return invalidCreateEntryRequest("duplicate entity id: %s", entityID)
				}
				entityID, err := s.createEntity(ctx, *entry, nil, entityRequest, now)
				if err != nil {
					return err
				}
				createdEntityIDs[entityID] = struct{}{}
				entryEntityIDs[entityID] = struct{}{}
			}
		}

		if req.Experiments != nil {
			for _, experimentRequest := range *req.Experiments {
				if err := s.createExperimentGraph(ctx, *entry, entryEntityIDs, createdEntityIDs, experimentRequest, now); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func (s *Server) createExperimentGraph(
	ctx context.Context,
	entry domainmodels.Entry,
	entryEntityIDs map[uuid.UUID]struct{},
	createdEntityIDs map[uuid.UUID]struct{},
	req CreateExperimentRequest,
	now time.Time,
) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return invalidCreateEntryRequest("experiment name is required")
	}

	experimentID := uuid.New()
	if req.Id != nil {
		experimentID = uuid.UUID(*req.Id)
		if experimentID == uuid.Nil {
			return invalidCreateEntryRequest("experiment id is required")
		}
	}

	experiment, err := s.database.Experiments.Create(ctx, domainmodels.Experiment{
		ID:                experimentID,
		EntryID:           entry.ID,
		Name:              name,
		Description:       req.Description,
		ThumbnailImageURL: req.ThumbnailImageUrl,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		return fmt.Errorf("create experiment: %w", err)
	}
	if err := s.database.EntrySearch.IndexExperiment(ctx, *experiment); err != nil {
		return fmt.Errorf("index experiment search: %w", err)
	}

	experimentEntityIDs := make(map[uuid.UUID]struct{})
	if req.Entities != nil {
		for _, entityRequest := range *req.Entities {
			entityID := uuid.UUID(entityRequest.Id)
			if _, exists := createdEntityIDs[entityID]; exists {
				return invalidCreateEntryRequest("duplicate entity id: %s", entityID)
			}
			entityID, err := s.createEntity(ctx, entry, &experiment.ID, entityRequest, now)
			if err != nil {
				return err
			}
			createdEntityIDs[entityID] = struct{}{}
			experimentEntityIDs[entityID] = struct{}{}
		}
	}

	if req.Relations != nil {
		for _, relationRequest := range *req.Relations {
			if err := s.createEntityRelation(ctx, entryEntityIDs, experimentEntityIDs, relationRequest, now); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Server) createEntity(
	ctx context.Context,
	entry domainmodels.Entry,
	experimentID *uuid.UUID,
	req CreateEntityRequest,
	now time.Time,
) (uuid.UUID, error) {
	entityID := uuid.UUID(req.Id)
	if entityID == uuid.Nil {
		return uuid.Nil, invalidCreateEntryRequest("entity id is required")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return uuid.Nil, invalidCreateEntryRequest("entity name is required")
	}

	entityType := domainmodels.EntityType(req.Type)
	payload, err := entityPayloadFromCreateRequest(req)
	if err != nil {
		return uuid.Nil, err
	}

	entity, err := s.database.Entities.Create(ctx, domainmodels.Entity{
		ID:           entityID,
		EntryID:      entry.ID,
		ExperimentID: experimentID,
		Type:         entityType,
		Level:        entityLevelFromRequest(req.Level),
		Name:         name,
		Payload:      payload,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("create entity: %w", err)
	}
	if err := s.database.EntrySearch.IndexEntity(ctx, *entity); err != nil {
		return uuid.Nil, fmt.Errorf("index entity search: %w", err)
	}

	return entity.ID, nil
}

func (s *Server) createEntityRelation(
	ctx context.Context,
	entryEntityIDs map[uuid.UUID]struct{},
	experimentEntityIDs map[uuid.UUID]struct{},
	req CreateEntityRelationRequest,
	now time.Time,
) error {
	sourceEntityID := uuid.UUID(req.SourceEntityId)
	targetEntityID := uuid.UUID(req.TargetEntityId)
	if sourceEntityID == uuid.Nil {
		return invalidCreateEntryRequest("relation source_entity_id is required")
	}
	if targetEntityID == uuid.Nil {
		return invalidCreateEntryRequest("relation target_entity_id is required")
	}
	if sourceEntityID == targetEntityID {
		return invalidCreateEntryRequest("relation source_entity_id and target_entity_id must be different")
	}
	if !entityIDBelongsToExperimentGraph(sourceEntityID, entryEntityIDs, experimentEntityIDs) {
		return invalidCreateEntryRequest("relation source entity was not created in this entry request: %s", sourceEntityID)
	}
	if !entityIDBelongsToExperimentGraph(targetEntityID, entryEntityIDs, experimentEntityIDs) {
		return invalidCreateEntryRequest("relation target entity was not created in this entry request: %s", targetEntityID)
	}

	if _, err := s.database.EntityRelations.Create(ctx, domainmodels.EntityRelation{
		ID:             uuid.New(),
		SourceEntityID: sourceEntityID,
		TargetEntityID: targetEntityID,
		RelationType:   domainmodels.RelationType(req.RelationType),
		CreatedAt:      now,
		UpdatedAt:      now,
	}); err != nil {
		return fmt.Errorf("create entity relation: %w", err)
	}

	return nil
}

func entityPayloadFromCreateRequest(req CreateEntityRequest) (any, error) {
	switch domainmodels.EntityType(req.Type) {
	case domainmodels.EntityTypeData:
		payload, err := req.Payload.AsDataPayload()
		if err != nil {
			return nil, invalidCreateEntryPayloadRequest("decode data payload", err)
		}
		return &domainmodels.DataPayload{
			FileURL:     payload.FileUrl,
			Type:        stringFromPtr(payload.Type),
			Authors:     authorsFromRequest(payload.Authors),
			Affiliation: trimmedStringPtr(payload.Affiliation),
			Size:        payload.Size,
			Metadata:    metadataFromRequest(payload.Metadata),
		}, nil
	case domainmodels.EntityTypeModel:
		payload, err := req.Payload.AsModelPayload()
		if err != nil {
			return nil, invalidCreateEntryPayloadRequest("decode model payload", err)
		}
		return &domainmodels.ModelPayload{
			FileURL:     payload.FileUrl,
			Authors:     authorsFromRequest(payload.Authors),
			Affiliation: trimmedStringPtr(payload.Affiliation),
			Size:        payload.Size,
			Metadata:    metadataFromRequest(payload.Metadata),
		}, nil
	case domainmodels.EntityTypeMetrics:
		payload, err := req.Payload.AsMetricsPayload()
		if err != nil {
			return nil, invalidCreateEntryPayloadRequest("decode metrics payload", err)
		}
		return &domainmodels.MetricsPayload{
			RFree: payload.RFree,
			RWork: payload.RWork,
			RSCC:  payload.Rscc,
			CC:    payload.Cc,
		}, nil
	case domainmodels.EntityTypeProgram:
		payload, err := req.Payload.AsProgramPayload()
		if err != nil {
			return nil, invalidCreateEntryPayloadRequest("decode program payload", err)
		}
		return &domainmodels.ProgramPayload{
			Name:        payload.Name,
			Version:     payload.Version,
			Description: payload.Description,
		}, nil
	default:
		return nil, invalidCreateEntryRequest("unexpected entity type: %s", req.Type)
	}
}

func entityLevelFromRequest(level *EntityLevel) *domainmodels.EntityLevel {
	if level == nil {
		return nil
	}
	domainLevel := domainmodels.EntityLevel(*level)
	return &domainLevel
}

func entityIDBelongsToExperimentGraph(
	entityID uuid.UUID,
	entryEntityIDs map[uuid.UUID]struct{},
	experimentEntityIDs map[uuid.UUID]struct{},
) bool {
	if _, exists := entryEntityIDs[entityID]; exists {
		return true
	}
	if _, exists := experimentEntityIDs[entityID]; exists {
		return true
	}
	return false
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func authorsFromRequest(authors *[]string) []string {
	if authors == nil {
		return nil
	}

	normalized := make([]string, 0, len(*authors))
	for _, author := range *authors {
		trimmed := strings.TrimSpace(author)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func trimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func metadataFromRequest(metadata *map[string]interface{}) map[string]any {
	if metadata == nil {
		return nil
	}
	return *metadata
}

func invalidCreateEntryRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidCreateEntryRequest, fmt.Sprintf(format, args...))
}

func invalidCreateEntryPayloadRequest(description string, err error) error {
	return fmt.Errorf("%s: %w", description, invalidCreateEntryRequest("%v", err))
}

func (s *Server) ListExperiments(w http.ResponseWriter, r *http.Request, entryID uuid.UUID, params ListExperimentsParams) {
	filters, err := experimentFiltersFromParams(entryID, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid experiment filters")
		return
	}

	experiments, err := s.database.Experiments.List(r.Context(), filters)
	if err != nil {
		slog.Error("list experiments failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list experiments")
		return
	}

	items := make([]Experiment, 0, len(experiments))
	for _, experiment := range experiments {
		items = append(items, experimentResponseFromModel(experiment))
	}

	writeJSON(w, http.StatusOK, ExperimentListResponse{Items: items})
}

func (s *Server) GetExperiment(w http.ResponseWriter, r *http.Request, entryID, experimentID uuid.UUID) {
	experiment, err := s.database.Experiments.Get(r.Context(), entryID, experimentID)
	if errors.Is(err, db.ErrExperimentNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "experiment not found")
		return
	}
	if err != nil {
		slog.Error("get experiment failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get experiment")
		return
	}

	writeJSON(w, http.StatusOK, experimentResponseFromModel(*experiment))
}

func experimentFiltersFromParams(entryID uuid.UUID, params ListExperimentsParams) (db.ExperimentFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.ExperimentFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.ExperimentFilters{}, errors.New("offset must be non-negative")
	}

	return db.ExperimentFilters{
		EntryID: &entryID,
		Limit:   params.Limit,
		Offset:  params.Offset,
	}, nil
}

func experimentResponseFromModel(experiment domainmodels.Experiment) Experiment {
	return Experiment{
		Id:                experiment.ID,
		EntryId:           experiment.EntryID,
		Name:              experiment.Name,
		Description:       experiment.Description,
		ThumbnailImageUrl: experiment.ThumbnailImageURL,
		CreatedAt:         experiment.CreatedAt,
		UpdatedAt:         experiment.UpdatedAt,
	}
}

func (s *Server) ListEntities(w http.ResponseWriter, r *http.Request, entryID uuid.UUID, params ListEntitiesParams) {
	filters, err := entityFiltersFromParams(entryID, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid entity filters")
		return
	}

	entities, err := s.database.Entities.List(r.Context(), filters)
	if err != nil {
		slog.Error("list entities failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entities")
		return
	}

	items := make([]Entity, 0, len(entities))
	for _, entity := range entities {
		item, err := entityResponseFromModel(entity)
		if err != nil {
			slog.Error("build entity response failed", "err", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build entity response")
			return
		}
		items = append(items, item)
	}

	relations, err := s.database.EntityRelations.List(r.Context(), entryID)
	if err != nil {
		slog.Error("list entity relations failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entity relations")
		return
	}

	relationItems := make([]EntityRelation, 0, len(relations))
	for _, relation := range relations {
		relationItems = append(relationItems, entityRelationResponseFromModel(relation))
	}

	writeJSON(w, http.StatusOK, EntityListResponse{
		Items:     items,
		Relations: relationItems,
	})
}

func entityFiltersFromParams(entryID uuid.UUID, params ListEntitiesParams) (db.EntityFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.EntityFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.EntityFilters{}, errors.New("offset must be non-negative")
	}

	filters := db.EntityFilters{
		EntryID: &entryID,
		Limit:   params.Limit,
		Offset:  params.Offset,
	}
	if params.ExperimentId != nil {
		experimentID := uuid.UUID(*params.ExperimentId)
		filters.ExperimentID = &experimentID
	}
	if params.Types != nil {
		filters.Types = make([]domainmodels.EntityType, 0, len(*params.Types))
		for _, entityType := range *params.Types {
			filters.Types = append(filters.Types, domainmodels.EntityType(entityType))
		}
	}
	if params.Levels != nil {
		filters.Levels = make([]domainmodels.EntityLevel, 0, len(*params.Levels))
		for _, level := range *params.Levels {
			filters.Levels = append(filters.Levels, domainmodels.EntityLevel(level))
		}
	}

	return filters, nil
}

func entityResponseFromModel(entity domainmodels.Entity) (Entity, error) {
	payload, err := entityPayloadResponseFromModel(entity)
	if err != nil {
		return Entity{}, err
	}

	return Entity{
		Id:           entity.ID,
		EntryId:      entity.EntryID,
		ExperimentId: entity.ExperimentID,
		Type:         EntityType(entity.Type),
		Level:        entityLevelResponseFromModel(entity.Level),
		Name:         entity.Name,
		Payload:      payload,
		CreatedAt:    entity.CreatedAt,
		UpdatedAt:    entity.UpdatedAt,
	}, nil
}

func entityPayloadResponseFromModel(entity domainmodels.Entity) (Entity_Payload, error) {
	var payload Entity_Payload
	switch entity.Type {
	case domainmodels.EntityTypeData:
		dataPayload, err := entity.Data()
		if err != nil {
			return Entity_Payload{}, fmt.Errorf("get data payload: %w", err)
		}
		if err := payload.FromDataPayload(DataPayload{
			FileUrl:     dataPayload.FileURL,
			Authors:     stringSlicePtrFromNonEmpty(dataPayload.Authors),
			Affiliation: dataPayload.Affiliation,
			Metadata:    entityPayloadMetadataResponseFromModel(dataPayload.Metadata),
			Size:        dataPayload.Size,
			Type:        stringPtrFromNonEmpty(dataPayload.Type),
		}); err != nil {
			return Entity_Payload{}, fmt.Errorf("build data payload response: %w", err)
		}
	case domainmodels.EntityTypeModel:
		modelPayload, err := entity.Model()
		if err != nil {
			return Entity_Payload{}, fmt.Errorf("get model payload: %w", err)
		}
		if err := payload.FromModelPayload(ModelPayload{
			FileUrl:     modelPayload.FileURL,
			Authors:     stringSlicePtrFromNonEmpty(modelPayload.Authors),
			Affiliation: modelPayload.Affiliation,
			Metadata:    entityPayloadMetadataResponseFromModel(modelPayload.Metadata),
			Size:        modelPayload.Size,
		}); err != nil {
			return Entity_Payload{}, fmt.Errorf("build model payload response: %w", err)
		}
	case domainmodels.EntityTypeMetrics:
		metricsPayload, err := entity.Metrics()
		if err != nil {
			return Entity_Payload{}, fmt.Errorf("get metrics payload: %w", err)
		}
		if err := payload.FromMetricsPayload(MetricsPayload{
			Cc:    metricsPayload.CC,
			RFree: metricsPayload.RFree,
			RWork: metricsPayload.RWork,
			Rscc:  metricsPayload.RSCC,
		}); err != nil {
			return Entity_Payload{}, fmt.Errorf("build metrics payload response: %w", err)
		}
	case domainmodels.EntityTypeProgram:
		programPayload, err := entity.Program()
		if err != nil {
			return Entity_Payload{}, fmt.Errorf("get program payload: %w", err)
		}
		if err := payload.FromProgramPayload(ProgramPayload{
			Description: programPayload.Description,
			Name:        programPayload.Name,
			Version:     programPayload.Version,
		}); err != nil {
			return Entity_Payload{}, fmt.Errorf("build program payload response: %w", err)
		}
	default:
		return Entity_Payload{}, fmt.Errorf("%w: %s", domainmodels.ErrUnexpectedEntityType, entity.Type)
	}

	return payload, nil
}

func entityPayloadMetadataResponseFromModel(metadata map[string]any) *map[string]interface{} {
	if metadata == nil {
		return nil
	}
	responseMetadata := map[string]interface{}(metadata)
	return &responseMetadata
}

func stringPtrFromNonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringSlicePtrFromNonEmpty(value []string) *[]string {
	if len(value) == 0 {
		return nil
	}
	return &value
}

func entityLevelResponseFromModel(level *domainmodels.EntityLevel) *EntityLevel {
	if level == nil {
		return nil
	}
	responseLevel := EntityLevel(*level)
	return &responseLevel
}

func entityRelationResponseFromModel(relation domainmodels.EntityRelation) EntityRelation {
	return EntityRelation{
		Id:             relation.ID,
		SourceEntityId: relation.SourceEntityID,
		TargetEntityId: relation.TargetEntityID,
		RelationType:   EntityRelationType(relation.RelationType),
		CreatedAt:      relation.CreatedAt,
		UpdatedAt:      relation.UpdatedAt,
	}
}
