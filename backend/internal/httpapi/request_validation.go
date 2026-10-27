package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

// checkSchema applies the server's validation mode to the schema errors of a create
// request. Handlers call it before any database write. In log-only mode the request
// continues, and the handler's inline checks return 400 for the fields they cover.
func (s *Server) checkSchema(r *http.Request, fieldErrors []validation.FieldError) error {
	return s.validationMode.Check(r.Context(), slog.Default(), requestSubject(r), fieldErrors)
}

// schemaCheck names the request in logs and points at the model in its body, for
// schema rules a write checks inside its transaction.
type schemaCheck struct {
	subject string
	at      validation.Pointer
}

// checkRunArtifactLinks checks the artifacts runs link; inputs may be entry files.
func (s *Server) checkRunArtifactLinks(
	ctx context.Context,
	check schemaCheck,
	entryID string,
	runs []validation.ModelRun,
	modelArtifactIDs []uuid.UUID,
) error {
	fieldErrors, err := s.runArtifactLinkErrors(ctx, check.at.Field("runs"), entryID, runs, modelArtifactIDs)
	if err != nil {
		return err
	}
	return s.validationMode.Check(ctx, slog.Default(), check.subject, fieldErrors)
}

func (s *Server) runArtifactLinkErrors(
	ctx context.Context,
	at validation.Pointer,
	entryID string,
	runs []validation.ModelRun,
	modelArtifactIDs []uuid.UUID,
) ([]validation.FieldError, error) {
	var outsideModel []uuid.UUID
	for _, run := range runs {
		for _, link := range run.Artifacts {
			if link.ArtifactID != uuid.Nil && !slices.Contains(modelArtifactIDs, link.ArtifactID) {
				outsideModel = append(outsideModel, link.ArtifactID)
			}
		}
	}
	entryArtifactIDs, err := s.database.Artifacts.EntryArtifactIDs(ctx, entryID, outsideModel)
	if err != nil {
		return nil, fmt.Errorf("read entry artifacts linked by runs: %w", err)
	}
	return validation.RunArtifactLinks(at, runs, validation.RunArtifactScope{
		ModelArtifactIDs: modelArtifactIDs,
		EntryArtifactIDs: entryArtifactIDs,
	}), nil
}

// checkModelRevisionChangeProvenance checks the metrics and runs of the revision a
// change produces. Fields the change leaves out come from base and are checked only
// where the change affects them.
func (s *Server) checkModelRevisionChangeProvenance(
	ctx context.Context,
	check schemaCheck,
	entryID string,
	payload createModelRevisionPayload,
	base domainmodels.ModelRevision,
) error {
	_, setsMetadata := payload.ModelFields["metadata"]
	_, setsMetrics := payload.ModelFields["metrics"]
	_, setsRuns := payload.ModelFields["runs"]
	change := payload.Request.Model
	before := validation.ModelRevision{ModelType: base.Metadata.ModelType}
	revision := validation.ModelRevision{ModelType: base.Metadata.ModelType}
	if setsMetadata && change.Metadata != nil {
		// Metadata field errors are reported by modelRevisionChangeErrors.
		metadata, _ := decodeModelMetadataFields(check.at.Field("metadata"), *change.Metadata)
		revision.ModelType = metadata.ModelType
	}
	becomesComputed := !before.IsComputed() && revision.IsComputed()
	if !setsMetrics && !setsRuns && !becomesComputed {
		return nil
	}
	if setsRuns {
		revision.Runs = modelRunsFromRequests(change.Runs)
	} else {
		storedRuns, err := s.storedModelRuns(ctx, base.ID)
		if err != nil {
			return err
		}
		revision.Runs = storedRuns
	}

	var fieldErrors []validation.FieldError
	if setsMetrics {
		fieldErrors = append(fieldErrors,
			validation.MetricProvenance(check.at.Field("metrics"), metricsFromRequests(change.Metrics), revision)...)
	} else if setsRuns {
		keptMetrics, err := s.database.Metrics.List(ctx, db.MetricFilters{ModelRevisionID: &base.ID})
		if err != nil {
			return fmt.Errorf("list metrics of the active model revision: %w", err)
		}
		fieldErrors = append(fieldErrors, validation.KeptMetricRuns(check.at.Field("runs"), keptMetrics, revision)...)
	}
	if setsRuns || becomesComputed {
		fieldErrors = append(fieldErrors, validation.ModelRuns(check.at.Field("runs"), revision)...)
	}
	if setsRuns {
		modelArtifactIDs := artifactIDsFromRequests(change.Artifacts)
		if _, setsArtifacts := payload.ModelFields["artifacts"]; !setsArtifacts {
			storedIDs, err := s.database.Artifacts.ModelRevisionArtifactIDs(ctx, base.ID)
			if err != nil {
				return fmt.Errorf("list artifacts of the active model revision: %w", err)
			}
			modelArtifactIDs = storedIDs
		}
		linkErrors, err := s.runArtifactLinkErrors(ctx, check.at.Field("runs"), entryID, revision.Runs, modelArtifactIDs)
		if err != nil {
			return err
		}
		fieldErrors = append(fieldErrors, linkErrors...)
	}
	return s.validationMode.Check(ctx, slog.Default(), check.subject, fieldErrors)
}

func (s *Server) storedModelRuns(ctx context.Context, modelRevisionID uuid.UUID) ([]validation.ModelRun, error) {
	runs, err := s.database.Runs.List(ctx, db.RunFilters{ModelRevisionID: &modelRevisionID})
	if err != nil {
		return nil, fmt.Errorf("list runs of the active model revision: %w", err)
	}
	links, err := s.database.Runs.ListArtifactLinks(ctx, modelRevisionID)
	if err != nil {
		return nil, fmt.Errorf("list run artifacts of the active model revision: %w", err)
	}
	modelRuns := make([]validation.ModelRun, 0, len(runs))
	for _, run := range runs {
		modelRun := validation.ModelRun{Run: run}
		for _, link := range links {
			if link.RunID == run.ID {
				modelRun.Artifacts = append(modelRun.Artifacts,
					validation.RunArtifactLink{ArtifactID: link.ArtifactID, Direction: link.Direction})
			}
		}
		modelRuns = append(modelRuns, modelRun)
	}
	return modelRuns, nil
}

// requestSubject names a request by its route, for example "POST /v1/entries/{entry_id}/models".
func requestSubject(r *http.Request) string {
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil && routeContext.RoutePattern() != "" {
		return r.Method + " " + routeContext.RoutePattern()
	}
	return r.Method + " " + r.URL.Path
}

func createEntryRequestErrors(request CreateEntryRequest) []validation.FieldError {
	entry := validation.Root.Field("entry")
	fieldErrors := validation.RequiredText(entry.Field("title"), &request.Entry.Title)
	fieldErrors = append(fieldErrors, entryMetadataErrors(entry, request.Entry, true)...)
	fieldErrors = append(fieldErrors, depositorPolymerEntityErrors(entry.Field("polymer_entities"), request.Entry.PolymerEntities)...)
	fieldErrors = append(fieldErrors, artifactRequestErrors(entry.Field("artifacts"), request.Entry.Artifacts)...)
	if request.ModelOperations != nil {
		for index, operation := range *request.ModelOperations {
			data := validation.Root.Field("model_operations").Index(index).Field("data")
			fieldErrors = append(fieldErrors, createModelRequestErrors(data, createModelDataFromAddOperation(operation.Data))...)
		}
	}
	return fieldErrors
}

func createModelRequestErrors(at validation.Pointer, data CreateModelData) []validation.FieldError {
	fieldErrors := validation.RequiredText(at.Field("title"), &data.Title)
	metadata, metadataErrors := modelMetadataRequestErrors(at.Field("metadata"), data.Metadata)
	fieldErrors = append(fieldErrors, metadataErrors...)
	fieldErrors = append(fieldErrors, artifactRequestErrors(at.Field("artifacts"), data.Artifacts)...)
	// Which artifacts a run may link needs stored data, so checkRunArtifactLinks
	// checks that inside the write transaction.
	revision := validation.ModelRevision{ModelType: metadata.ModelType, Runs: modelRunsFromRequests(data.Runs)}
	fieldErrors = append(fieldErrors, metricRequestErrors(at.Field("metrics"), data.Metrics)...)
	fieldErrors = append(fieldErrors,
		validation.MetricProvenance(at.Field("metrics"), metricsFromRequests(data.Metrics), revision)...)
	fieldErrors = append(fieldErrors, runRequestErrors(at.Field("runs"), data.Runs)...)
	fieldErrors = append(fieldErrors, validation.ModelRuns(at.Field("runs"), revision)...)
	return fieldErrors
}

func artifactIDsFromRequests(requests *[]CreateArtifactRequest) []uuid.UUID {
	if requests == nil {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(*requests))
	for _, request := range *requests {
		ids = append(ids, request.Id)
	}
	return ids
}

func metricsFromRequests(requests *[]CreateMetricRequest) []domainmodels.Metric {
	if requests == nil {
		return nil
	}
	metrics := make([]domainmodels.Metric, 0, len(*requests))
	for _, request := range *requests {
		metrics = append(metrics, metricFromRequest(request))
	}
	return metrics
}

func modelRunsFromRequests(requests *[]CreateRunRequest) []validation.ModelRun {
	if requests == nil {
		return nil
	}
	runs := make([]validation.ModelRun, 0, len(*requests))
	for _, request := range *requests {
		run := validation.ModelRun{Run: runFromRequest(request)}
		if request.Artifacts != nil {
			for _, link := range *request.Artifacts {
				run.Artifacts = append(run.Artifacts, validation.RunArtifactLink{
					ArtifactID: link.ArtifactId,
					Direction:  domainmodels.RunArtifactDirection(link.Direction),
				})
			}
		}
		runs = append(runs, run)
	}
	return runs
}

// entryRevisionChangeErrors checks only the fields the change sets; the rest are
// copied from the active revision.
func entryRevisionChangeErrors(payload createEntryRevisionPayload) []validation.FieldError {
	entry := validation.Root.Field("entry")
	var fieldErrors []validation.FieldError
	if _, present := payload.EntryFields["title"]; present {
		fieldErrors = append(fieldErrors, validation.RequiredText(entry.Field("title"), payload.Request.Entry.Title)...)
	}
	fieldErrors = append(fieldErrors, entryMetadataErrors(entry, payload.Request.Entry, false)...)
	if raw, present := payload.EntryFields["polymer_entities"]; present {
		if isJSONNull(raw) {
			return append(fieldErrors, validation.MustNotBeNull(entry.Field("polymer_entities"))...)
		}
		fieldErrors = append(fieldErrors,
			depositorPolymerEntityErrors(entry.Field("polymer_entities"), payload.Request.Entry.PolymerEntities)...)
	}
	return fieldErrors
}

func depositorPolymerEntityErrors(at validation.Pointer, entities *DepositorPolymerEntities) []validation.FieldError {
	return validation.DepositorPolymerEntities(at, depositorPolymerEntitiesFromRequest(entities))
}

func depositorPolymerEntitiesFromRequest(entities *DepositorPolymerEntities) []domainmodels.DepositorPolymerEntity {
	if entities == nil {
		return nil
	}
	result := make([]domainmodels.DepositorPolymerEntity, 0, len(*entities))
	for _, entity := range *entities {
		depositorEntity := domainmodels.DepositorPolymerEntity{
			LabelEntityID: strings.TrimSpace(entity.LabelEntityId),
			Construct:     trimmedStringPtr(entity.Construct),
		}
		if entity.Mutations != nil {
			for _, mutation := range *entity.Mutations {
				depositorEntity.Mutations = append(depositorEntity.Mutations, strings.TrimSpace(mutation))
			}
		}
		if entity.SourceOrganisms != nil {
			for _, organism := range *entity.SourceOrganisms {
				depositorEntity.SourceOrganisms = append(depositorEntity.SourceOrganisms, domainmodels.PolymerEntityOrganism{
					ScientificName: strings.TrimSpace(organism.ScientificName),
					NCBITaxonomyID: organism.NcbiTaxonomyId,
				})
			}
		}
		result = append(result, depositorEntity)
	}
	return result
}

// modelRevisionChangeErrors checks only the fields the change sets; the rest are
// copied from the active revision.
func modelRevisionChangeErrors(payload createModelRevisionPayload) []validation.FieldError {
	model := validation.Root.Field("model")
	change := payload.Request.Model
	var fieldErrors []validation.FieldError
	if _, present := payload.ModelFields["title"]; present {
		fieldErrors = append(fieldErrors, validation.RequiredText(model.Field("title"), change.Title)...)
	}
	nonNullFields := []struct {
		name   string
		errors func(at validation.Pointer) []validation.FieldError
	}{
		{"metadata", func(at validation.Pointer) []validation.FieldError {
			_, metadataErrors := modelMetadataRequestErrors(at, change.Metadata)
			return metadataErrors
		}},
		{"artifacts", func(at validation.Pointer) []validation.FieldError {
			return artifactRequestErrors(at, change.Artifacts)
		}},
		{"metrics", func(at validation.Pointer) []validation.FieldError { return metricRequestErrors(at, change.Metrics) }},
		{"runs", func(at validation.Pointer) []validation.FieldError { return runRequestErrors(at, change.Runs) }},
	}
	for _, field := range nonNullFields {
		raw, present := payload.ModelFields[field.name]
		if !present {
			continue
		}
		if isJSONNull(raw) {
			fieldErrors = append(fieldErrors, validation.MustNotBeNull(model.Field(field.name))...)
			continue
		}
		fieldErrors = append(fieldErrors, field.errors(model.Field(field.name))...)
	}
	return fieldErrors
}

// entryMetadataErrors checks the entry metadata fields of a create or change request,
// which sit directly on the request's entry object.
func entryMetadataErrors(at validation.Pointer, fields any, requireMethod bool) []validation.FieldError {
	metadata, err := entryMetadataFromAPIFields(fields)
	if err != nil {
		return []validation.FieldError{{
			Pointer: at,
			Code:    validation.CodeInvalidValue,
			Detail:  "does not match the entry fields: " + err.Error(),
		}}
	}
	fieldErrors := validation.EntryMetadata(at, metadata)
	if requireMethod {
		fieldErrors = append(fieldErrors, validation.NewEntryMethod(at, metadata)...)
	}
	return fieldErrors
}

// modelMetadataRequestErrors checks request model metadata and returns the fields that
// decoded, for rules that depend on them such as the model type.
func modelMetadataRequestErrors(
	at validation.Pointer,
	metadata *map[string]interface{},
) (domainmodels.ModelMetadata, []validation.FieldError) {
	if metadata == nil {
		return domainmodels.ModelMetadata{}, nil
	}
	decoded, fieldErrors := decodeModelMetadataFields(at, *metadata)
	return decoded, append(fieldErrors, validation.ModelMetadata(at, decoded)...)
}

// decodeModelMetadataFields decodes each metadata field on its own. A field of the
// wrong JSON type is reported at its own pointer and left out of the result, so the
// schema rules still check every other field.
func decodeModelMetadataFields(
	at validation.Pointer,
	metadata map[string]interface{},
) (domainmodels.ModelMetadata, []validation.FieldError) {
	var decoded domainmodels.ModelMetadata
	fieldErrors := validation.ObjectKeys(at, metadata, domainmodels.ModelMetadata{})
	for _, key := range slices.Sorted(maps.Keys(metadata)) {
		field := map[string]interface{}{key: metadata[key]}
		// Decoding into a scratch value first keeps a failing field out of decoded.
		var scratch domainmodels.ModelMetadata
		err := decodeMetadataRequest(field, &scratch)
		if err == nil {
			err = decodeMetadataRequest(field, &decoded)
		}
		if err != nil {
			fieldErrors = append(fieldErrors, validation.FieldError{
				Pointer: at.Field(key),
				Code:    validation.CodeInvalidValue,
				Detail:  "does not match the model metadata field: " + err.Error(),
			})
		}
	}
	return decoded, fieldErrors
}

func artifactRequestErrors(at validation.Pointer, requests *[]CreateArtifactRequest) []validation.FieldError {
	if requests == nil {
		return nil
	}
	var fieldErrors []validation.FieldError
	for index, request := range *requests {
		fieldErrors = append(fieldErrors, validation.RequiredID(at.Index(index).Field("id"), request.Id)...)
		fieldErrors = append(fieldErrors, validation.Artifact(at.Index(index), artifactFromRequest(request))...)
		if request.Metadata != nil {
			fieldErrors = append(fieldErrors,
				validation.ObjectKeys(at.Index(index).Field("metadata"), *request.Metadata, domainmodels.ArtifactMetadata{})...)
		}
	}
	return fieldErrors
}

func metricRequestErrors(at validation.Pointer, requests *[]CreateMetricRequest) []validation.FieldError {
	if requests == nil {
		return nil
	}
	var fieldErrors []validation.FieldError
	metrics := make([]domainmodels.Metric, 0, len(*requests))
	for index, request := range *requests {
		fieldErrors = append(fieldErrors, validation.RequiredID(at.Index(index).Field("id"), request.Id)...)
		fieldErrors = append(fieldErrors, validation.RequiredNumber(at.Index(index).Field("value"), request.Value)...)
		metrics = append(metrics, metricFromRequest(request))
	}
	return append(fieldErrors, validation.Metrics(at, metrics)...)
}

func runRequestErrors(at validation.Pointer, requests *[]CreateRunRequest) []validation.FieldError {
	if requests == nil {
		return nil
	}
	var fieldErrors []validation.FieldError
	for index, request := range *requests {
		runAt := at.Index(index)
		fieldErrors = append(fieldErrors, validation.RequiredID(runAt.Field("id"), request.Id)...)
		fieldErrors = append(fieldErrors, validation.Run(runAt, runFromRequest(request))...)
		if request.Metadata != nil {
			fieldErrors = append(fieldErrors,
				validation.ObjectKeys(runAt.Field("metadata"), *request.Metadata, domainmodels.RunMetadata{})...)
		}
		if request.Artifacts == nil {
			continue
		}
		for artifactIndex, link := range *request.Artifacts {
			linkAt := runAt.Field("artifacts").Index(artifactIndex)
			fieldErrors = append(fieldErrors, validation.RequiredID(linkAt.Field("artifact_id"), link.ArtifactId)...)
			fieldErrors = append(fieldErrors,
				validation.RunArtifact(linkAt, domainmodels.RunArtifactDirection(link.Direction))...)
		}
	}
	return fieldErrors
}
