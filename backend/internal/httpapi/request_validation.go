package httpapi

import (
	"log/slog"
	"maps"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

// checkSchema applies the server's validation mode to the schema errors of a create
// request. Handlers call it before any database write. In log-only mode the request
// continues, and the handler's inline checks return 400 for the fields they cover.
func (s *Server) checkSchema(r *http.Request, fieldErrors []validation.FieldError) error {
	return s.validationMode.Check(r.Context(), slog.Default(), requestSubject(r), fieldErrors)
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
	fieldErrors = append(fieldErrors, entryMetadataErrors(entry, request.Entry)...)
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
	fieldErrors = append(fieldErrors, modelMetadataErrors(at.Field("metadata"), data.Metadata)...)
	fieldErrors = append(fieldErrors, artifactRequestErrors(at.Field("artifacts"), data.Artifacts)...)
	fieldErrors = append(fieldErrors, metricRequestErrors(at.Field("metrics"), data.Metrics)...)
	fieldErrors = append(fieldErrors, runRequestErrors(at.Field("runs"), data.Runs)...)
	return fieldErrors
}

// entryRevisionChangeErrors checks only the fields the change sets; the rest are
// copied from the active revision.
func entryRevisionChangeErrors(payload createEntryRevisionPayload) []validation.FieldError {
	entry := validation.Root.Field("entry")
	var fieldErrors []validation.FieldError
	if _, present := payload.EntryFields["title"]; present {
		fieldErrors = append(fieldErrors, validation.RequiredText(entry.Field("title"), payload.Request.Entry.Title)...)
	}
	return append(fieldErrors, entryMetadataErrors(entry, payload.Request.Entry)...)
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
		{"metadata", func(at validation.Pointer) []validation.FieldError { return modelMetadataErrors(at, change.Metadata) }},
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
func entryMetadataErrors(at validation.Pointer, fields any) []validation.FieldError {
	metadata, err := entryMetadataFromAPIFields(fields)
	if err != nil {
		return []validation.FieldError{{
			Pointer: at,
			Code:    validation.CodeInvalidValue,
			Detail:  "does not match the entry fields: " + err.Error(),
		}}
	}
	return validation.EntryMetadata(at, metadata)
}

func modelMetadataErrors(at validation.Pointer, metadata *map[string]interface{}) []validation.FieldError {
	if metadata == nil {
		return nil
	}
	decoded, fieldErrors := decodeModelMetadataFields(at, *metadata)
	return append(fieldErrors, validation.ModelMetadata(at, decoded)...)
}

// decodeModelMetadataFields decodes each metadata field on its own. A field of the
// wrong JSON type is reported at its own pointer and left out of the result, so the
// schema rules still check every other field.
func decodeModelMetadataFields(
	at validation.Pointer,
	metadata map[string]interface{},
) (domainmodels.ModelMetadata, []validation.FieldError) {
	var decoded domainmodels.ModelMetadata
	var fieldErrors []validation.FieldError
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
		metrics = append(metrics, domainmodels.Metric{ID: request.Id, Key: metricKeyFromRequest(request.Key)})
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
