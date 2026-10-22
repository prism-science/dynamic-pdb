package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5/middleware"

	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

// checkSchema applies the configured validation mode to the violations found in a request.
func (s *Server) checkSchema(r *http.Request, fieldErrors []validation.FieldError) error {
	return s.validationMode.Apply(
		r.Context(),
		slog.Default(),
		fieldErrors,
		requestIDLogKey, middleware.GetReqID(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
	)
}

func writeValidationErrors(w http.ResponseWriter, fieldErrors []validation.FieldError) {
	status := strconv.Itoa(http.StatusUnprocessableEntity)
	errorObjects := make([]ErrorObject, 0, len(fieldErrors))
	for _, fieldError := range fieldErrors {
		errorObjects = append(errorObjects, ErrorObject{
			Status: status,
			Code:   ptr(string(fieldError.Code)),
			Title:  ptr("Schema validation failed"),
			Detail: ptr(fieldError.Detail),
			Source: &ErrorSource{Pointer: ptr(fieldError.Pointer)},
		})
	}
	writeJSON(w, http.StatusUnprocessableEntity, Error{Errors: errorObjects})
}

func createEntryValidationInput(request CreateEntryRequest) (validation.CreateEntry, error) {
	input := validation.CreateEntry{
		Entry: validation.Entry{
			Title:      request.Entry.Title,
			Method:     request.Entry.Method,
			Resolution: request.Entry.Resolution,
			Artifacts:  artifactsValidationInput(request.Entry.Artifacts),
		},
	}
	if request.ModelOperations == nil {
		return input, nil
	}
	for index, operation := range *request.ModelOperations {
		model, err := modelValidationInput(createModelDataFromAddOperation(operation.Data))
		if err != nil {
			return validation.CreateEntry{}, fmt.Errorf("model operation %d: %w", index, err)
		}
		input.Models = append(input.Models, model)
	}
	return input, nil
}

func modelValidationInput(data CreateModelData) (validation.Model, error) {
	metadata, err := modelMetadataFromRequest(data.Metadata)
	if err != nil {
		return validation.Model{}, fmt.Errorf("decode model metadata for validation: %w", err)
	}
	return validation.Model{
		Title:     data.Title,
		Metadata:  metadata,
		Artifacts: artifactsValidationInput(data.Artifacts),
		Runs:      runsValidationInput(data.Runs),
		Metrics:   metricsValidationInput(data.Metrics),
	}, nil
}

func entryChangeValidationInput(payload createEntryRevisionPayload) validation.EntryChange {
	change := payload.Request.Entry
	var input validation.EntryChange
	if _, present := payload.EntryFields["title"]; present {
		input.Title = validation.Present(change.Title)
	}
	if _, present := payload.EntryFields["method"]; present {
		input.Method = validation.Present(change.Method)
	}
	if _, present := payload.EntryFields["resolution"]; present {
		input.Resolution = validation.Present(change.Resolution)
	}
	return input
}

func modelChangeValidationInput(payload createModelRevisionPayload) (validation.ModelChange, error) {
	change := payload.Request.Model
	var input validation.ModelChange
	if _, present := payload.ModelFields["title"]; present {
		input.Title = validation.Present(change.Title)
	}
	if _, present := payload.ModelFields["metadata"]; present {
		input.Metadata = validation.Present[domainmodels.ModelMetadata](nil)
		if change.Metadata != nil {
			metadata, err := modelMetadataFromRequest(change.Metadata)
			if err != nil {
				return validation.ModelChange{}, fmt.Errorf("decode model metadata for validation: %w", err)
			}
			input.Metadata = validation.Present(&metadata)
		}
	}
	if _, present := payload.ModelFields["artifacts"]; present {
		input.Artifacts = presentSlice(change.Artifacts, artifactsValidationInput)
	}
	if _, present := payload.ModelFields["runs"]; present {
		input.Runs = presentSlice(change.Runs, runsValidationInput)
	}
	if _, present := payload.ModelFields["metrics"]; present {
		input.Metrics = presentSlice(change.Metrics, metricsValidationInput)
	}
	return input, nil
}

func presentSlice[Request any, Input any](requests *[]Request, convert func(*[]Request) []Input) validation.Field[[]Input] {
	if requests == nil {
		return validation.Present[[]Input](nil)
	}
	inputs := convert(requests)
	return validation.Present(&inputs)
}

func artifactsValidationInput(requests *[]CreateArtifactRequest) []validation.Artifact {
	if requests == nil {
		return nil
	}
	artifacts := make([]validation.Artifact, 0, len(*requests))
	for _, request := range *requests {
		var metadata map[string]any
		if request.Metadata != nil {
			metadata = *request.Metadata
		}
		artifacts = append(artifacts, validation.Artifact{
			ID:        request.Id,
			Name:      request.Name,
			Level:     domainmodels.ArtifactLevel(request.Level),
			Type:      domainmodels.ArtifactType(request.Type),
			Format:    request.Format,
			SizeBytes: request.SizeBytes,
			SHA256:    request.Sha256,
			Metadata:  metadata,
		})
	}
	return artifacts
}

func metricsValidationInput(requests *[]CreateMetricRequest) []validation.Metric {
	if requests == nil {
		return nil
	}
	metrics := make([]validation.Metric, 0, len(*requests))
	for _, request := range *requests {
		metrics = append(metrics, validation.Metric{
			ID:    request.Id,
			Key:   domainmodels.MetricKey(request.Key),
			Value: request.Value,
		})
	}
	return metrics
}

func runsValidationInput(requests *[]CreateRunRequest) []validation.Run {
	if requests == nil {
		return nil
	}
	runs := make([]validation.Run, 0, len(*requests))
	for _, request := range *requests {
		run := validation.Run{ID: request.Id, Name: request.Name}
		if request.Artifacts != nil {
			for _, artifact := range *request.Artifacts {
				run.Artifacts = append(run.Artifacts, validation.RunArtifact{
					ArtifactID: artifact.ArtifactId,
					Direction:  domainmodels.RunArtifactDirection(artifact.Direction),
				})
			}
		}
		runs = append(runs, run)
	}
	return runs
}
