package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
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

	items := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, entryResponseFromModel(entry))
	}

	writeJSON(w, http.StatusOK, EntryListResponse{Items: items})
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

	return db.EntryFilters{
		Limit:  params.Limit,
		Offset: params.Offset,
	}, nil
}

func entryResponseFromModel(entry domainmodels.Entry) Entry {
	return Entry{
		Id:        entry.ID,
		Name:      entry.Name,
		CreatedAt: entry.CreatedAt,
		UpdatedAt: entry.UpdatedAt,
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

	writeJSON(w, http.StatusOK, EntityListResponse{Items: items})
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
	case domainmodels.EntityTypeModel:
		modelPayload, err := entity.Model()
		if err != nil {
			return Entity_Payload{}, fmt.Errorf("get model payload: %w", err)
		}
		if err := payload.FromModelPayload(ModelPayload{
			FileUrl: modelPayload.FileURL,
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
	default:
		return Entity_Payload{}, fmt.Errorf("%w: %s", domainmodels.ErrUnexpectedEntityType, entity.Type)
	}

	return payload, nil
}

func entityLevelResponseFromModel(level *domainmodels.EntityLevel) *EntityLevel {
	if level == nil {
		return nil
	}
	responseLevel := EntityLevel(*level)
	return &responseLevel
}
