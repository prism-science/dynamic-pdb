package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

var errInvalidRequest = errors.New("invalid request")

const (
	minProteinSequenceQueryLength = 8
	proteinSequenceAlphabet       = "ACDEFGHIKLMNPQRSTVWYX"
)

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

	if len(entries) == 0 {
		if proteinSequence, ok := proteinSequenceFromSearchQuery(filters.Query); ok {
			filters.Query = ""
			filters.ProteinSequence = proteinSequence
			entries, err = s.database.Entries.List(r.Context(), filters)
			if err != nil {
				slog.Error("list entries by protein sequence failed", "err", err)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list entries")
				return
			}
		}
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
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	err := s.createEntryGraph(r.Context(), req, name, user.ID)
	if errors.Is(err, errInvalidRequest) {
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

func (s *Server) DeleteEntry(w http.ResponseWriter, r *http.Request, entryID uuid.UUID) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	if err := s.database.Entries.Delete(r.Context(), entryID, user.ID); errors.Is(err, db.ErrEntryNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	} else if errors.Is(err, db.ErrEntryOwnershipMismatch) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the entry creator can delete it")
		return
	} else if err != nil {
		slog.Error("delete entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete entry")
		return
	}

	w.WriteHeader(http.StatusNoContent)
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

func proteinSequenceFromSearchQuery(value string) (string, bool) {
	var normalized strings.Builder
	normalized.Grow(len(value))

	for _, char := range strings.ToUpper(value) {
		if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
			continue
		}
		if !strings.ContainsRune(proteinSequenceAlphabet, char) {
			return "", false
		}
		normalized.WriteRune(char)
	}

	if normalized.Len() < minProteinSequenceQueryLength {
		return "", false
	}
	return normalized.String(), true
}

func entryResponseFromModel(entry domainmodels.Entry) Entry {
	return Entry{
		Id:                entry.ID,
		CreatedBy:         entry.CreatedBy,
		Name:              entry.Name,
		Description:       entry.Description,
		ThumbnailImageUrl: entry.ThumbnailImageURL,
		CreatedAt:         entry.CreatedAt,
		UpdatedAt:         entry.UpdatedAt,
	}
}

func (s *Server) createEntryGraph(ctx context.Context, req CreateEntryRequest, name string, createdBy uuid.UUID) error {
	now := time.Now().UTC()
	entryID := uuid.New()
	if req.Id != nil {
		entryID = *req.Id
		if entryID == uuid.Nil {
			return invalidRequest("entry id is required")
		}
	}

	return s.database.Do(ctx, func(ctx context.Context) error {
		entry, err := s.database.Entries.Create(ctx, domainmodels.Entry{
			ID:                entryID,
			CreatedBy:         createdBy,
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
				entityID := entityRequest.Id
				if _, exists := createdEntityIDs[entityID]; exists {
					return invalidRequest("duplicate entity id: %s", entityID)
				}
				entityID, err := s.createEntity(ctx, *entry, nil, entityRequest, now)
				if err != nil {
					return err
				}
				createdEntityIDs[entityID] = struct{}{}
				entryEntityIDs[entityID] = struct{}{}
			}
		}

		if req.Models != nil {
			for _, modelRequest := range *req.Models {
				if err := s.createModelGraph(ctx, *entry, entryEntityIDs, createdEntityIDs, modelRequest, now, createdBy); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func (s *Server) createModelGraph(
	ctx context.Context,
	entry domainmodels.Entry,
	entryEntityIDs map[uuid.UUID]struct{},
	createdEntityIDs map[uuid.UUID]struct{},
	req CreateModelRequest,
	now time.Time,
	createdBy uuid.UUID,
) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return invalidRequest("model name is required")
	}

	modelID := uuid.New()
	if req.Id != nil {
		modelID = *req.Id
		if modelID == uuid.Nil {
			return invalidRequest("model id is required")
		}
	}

	model, err := s.database.Models.Create(ctx, domainmodels.Model{
		ID:                modelID,
		EntryID:           entry.ID,
		CreatedBy:         createdBy,
		Name:              name,
		Description:       req.Description,
		ThumbnailImageURL: req.ThumbnailImageUrl,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		return fmt.Errorf("create model: %w", err)
	}
	if err := s.database.EntrySearch.IndexModel(ctx, *model); err != nil {
		return fmt.Errorf("index model search: %w", err)
	}

	modelEntityIDs := make(map[uuid.UUID]struct{})
	if req.Entities != nil {
		for _, entityRequest := range *req.Entities {
			entityID := entityRequest.Id
			if _, exists := createdEntityIDs[entityID]; exists {
				return invalidRequest("duplicate entity id: %s", entityID)
			}
			entityID, err := s.createEntity(ctx, entry, &model.ID, entityRequest, now)
			if err != nil {
				return err
			}
			createdEntityIDs[entityID] = struct{}{}
			modelEntityIDs[entityID] = struct{}{}
		}
	}

	if req.Relations != nil {
		for _, relationRequest := range *req.Relations {
			if err := s.createEntityRelation(ctx, entryEntityIDs, modelEntityIDs, relationRequest, now); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Server) createEntity(
	ctx context.Context,
	entry domainmodels.Entry,
	modelID *uuid.UUID,
	req CreateEntityRequest,
	now time.Time,
) (uuid.UUID, error) {
	entityID := req.Id
	if entityID == uuid.Nil {
		return uuid.Nil, invalidRequest("entity id is required")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return uuid.Nil, invalidRequest("entity name is required")
	}

	entityType := domainmodels.EntityType(req.Type)
	payload, err := entityPayloadFromCreateRequest(req)
	if err != nil {
		return uuid.Nil, err
	}

	entity, err := s.database.Entities.Create(ctx, domainmodels.Entity{
		ID:        entityID,
		EntryID:   entry.ID,
		ModelID:   modelID,
		Type:      entityType,
		Level:     entityLevelFromRequest(req.Level),
		Name:      name,
		Payload:   payload,
		CreatedAt: now,
		UpdatedAt: now,
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
	modelEntityIDs map[uuid.UUID]struct{},
	req CreateEntityRelationRequest,
	now time.Time,
) error {
	sourceEntityID := req.SourceEntityId
	targetEntityID := req.TargetEntityId
	if sourceEntityID == uuid.Nil {
		return invalidRequest("relation source_entity_id is required")
	}
	if targetEntityID == uuid.Nil {
		return invalidRequest("relation target_entity_id is required")
	}
	if sourceEntityID == targetEntityID {
		return invalidRequest("relation source_entity_id and target_entity_id must be different")
	}
	if !entityIDBelongsToModelGraph(sourceEntityID, entryEntityIDs, modelEntityIDs) {
		return invalidRequest("relation source entity is not part of this entry: %s", sourceEntityID)
	}
	if !entityIDBelongsToModelGraph(targetEntityID, entryEntityIDs, modelEntityIDs) {
		return invalidRequest("relation target entity is not part of this entry: %s", targetEntityID)
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
			return nil, invalidPayloadRequest("decode data payload", err)
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
			return nil, invalidPayloadRequest("decode model payload", err)
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
			return nil, invalidPayloadRequest("decode metrics payload", err)
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
			return nil, invalidPayloadRequest("decode program payload", err)
		}
		return &domainmodels.ProgramPayload{
			Name:        payload.Name,
			Version:     payload.Version,
			Description: payload.Description,
		}, nil
	default:
		return nil, invalidRequest("unexpected entity type: %s", req.Type)
	}
}

func entityLevelFromRequest(level *EntityLevel) *domainmodels.EntityLevel {
	if level == nil {
		return nil
	}
	domainLevel := domainmodels.EntityLevel(*level)
	return &domainLevel
}

func entityIDBelongsToModelGraph(
	entityID uuid.UUID,
	entryEntityIDs map[uuid.UUID]struct{},
	modelEntityIDs map[uuid.UUID]struct{},
) bool {
	if _, exists := entryEntityIDs[entityID]; exists {
		return true
	}
	if _, exists := modelEntityIDs[entityID]; exists {
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

func invalidRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidRequest, fmt.Sprintf(format, args...))
}

func invalidPayloadRequest(description string, err error) error {
	return fmt.Errorf("%s: %w", description, invalidRequest("%v", err))
}

func (s *Server) ListModels(w http.ResponseWriter, r *http.Request, entryID uuid.UUID, params ListModelsParams) {
	filters, err := modelFiltersFromParams(entryID, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid model filters")
		return
	}

	models, err := s.database.Models.List(r.Context(), filters)
	if err != nil {
		slog.Error("list models failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list models")
		return
	}

	items := make([]Model, 0, len(models))
	for _, model := range models {
		items = append(items, modelResponseFromModel(model))
	}

	writeJSON(w, http.StatusOK, ModelListResponse{Items: items})
}

func (s *Server) CreateModel(w http.ResponseWriter, r *http.Request, entryID uuid.UUID) {
	var req CreateModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
		return
	}

	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	err := s.createModelForEntry(r.Context(), entryID, req, user.ID)
	if errors.Is(err, db.ErrEntryNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "entry not found")
		return
	}
	if errors.Is(err, errInvalidRequest) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err != nil {
		slog.Error("create model failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create model")
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (s *Server) createModelForEntry(
	ctx context.Context,
	entryID uuid.UUID,
	req CreateModelRequest,
	createdBy uuid.UUID,
) error {
	now := time.Now().UTC()

	return s.database.Do(ctx, func(ctx context.Context) error {
		entry, err := s.database.Entries.Get(ctx, entryID)
		if err != nil {
			return err
		}

		entities, err := s.database.Entities.List(ctx, db.EntityFilters{
			EntryID:        &entryID,
			BelongsToEntry: true,
		})
		if err != nil {
			return fmt.Errorf("list entry entities: %w", err)
		}

		entryEntityIDs := make(map[uuid.UUID]struct{}, len(entities))
		for _, entity := range entities {
			entryEntityIDs[entity.ID] = struct{}{}
		}
		createdEntityIDs := maps.Clone(entryEntityIDs)

		return s.createModelGraph(
			ctx,
			*entry,
			entryEntityIDs,
			createdEntityIDs,
			req,
			now,
			createdBy,
		)
	})
}

func (s *Server) GetModel(w http.ResponseWriter, r *http.Request, entryID, modelID uuid.UUID) {
	model, err := s.database.Models.Get(r.Context(), entryID, modelID)
	if errors.Is(err, db.ErrModelNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
		return
	}
	if err != nil {
		slog.Error("get model failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get model")
		return
	}

	writeJSON(w, http.StatusOK, modelResponseFromModel(*model))
}

func (s *Server) DeleteModel(w http.ResponseWriter, r *http.Request, entryID, modelID uuid.UUID) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authenticated user is required")
		return
	}

	if err := s.database.Models.Delete(r.Context(), entryID, modelID, user.ID); errors.Is(err, db.ErrModelNotFound) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "model not found")
		return
	} else if errors.Is(err, db.ErrModelOwnershipMismatch) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "only the model creator can delete it")
		return
	} else if err != nil {
		slog.Error("delete model failed", "err", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to delete model")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func modelFiltersFromParams(entryID uuid.UUID, params ListModelsParams) (db.ModelFilters, error) {
	if params.Limit != nil && *params.Limit < 0 {
		return db.ModelFilters{}, errors.New("limit must be non-negative")
	}
	if params.Offset != nil && *params.Offset < 0 {
		return db.ModelFilters{}, errors.New("offset must be non-negative")
	}

	return db.ModelFilters{
		EntryID: &entryID,
		Limit:   params.Limit,
		Offset:  params.Offset,
	}, nil
}

func modelResponseFromModel(model domainmodels.Model) Model {
	return Model{
		Id:                model.ID,
		EntryId:           model.EntryID,
		CreatedBy:         model.CreatedBy,
		Name:              model.Name,
		Description:       model.Description,
		ThumbnailImageUrl: model.ThumbnailImageURL,
		CreatedAt:         model.CreatedAt,
		UpdatedAt:         model.UpdatedAt,
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
	if params.ModelId != nil {
		modelID := *params.ModelId
		filters.ModelID = &modelID
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
		Id:        entity.ID,
		EntryId:   entity.EntryID,
		ModelId:   entity.ModelID,
		Type:      EntityType(entity.Type),
		Level:     entityLevelResponseFromModel(entity.Level),
		Name:      entity.Name,
		Payload:   payload,
		CreatedAt: entity.CreatedAt,
		UpdatedAt: entity.UpdatedAt,
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
	responseMetadata := metadata
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
