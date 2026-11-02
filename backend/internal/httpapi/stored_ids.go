package httpapi

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
)

// A model revision that sends artifacts, runs or metrics replaces that whole set. To keep an
// item, a client sends it back with its stored ID and the values it read. Such an item is
// linked to the new revision as stored, not inserted again: artifacts, runs and metrics never
// change once stored, so a request that sends a stored ID with other values is refused.

// storedModelContents holds the items of the base revision a change may send back, keyed by
// ID. A nil map means the change does not replace that set, or there is no base revision.
type storedModelContents struct {
	artifacts map[uuid.UUID]domainmodels.Artifact
	runs      map[uuid.UUID]validation.ModelRun
	metrics   map[uuid.UUID]domainmodels.Metric
}

// loadStoredModelContents reads the base revision's artifacts, runs and metrics, but only the
// sets the change replaces.
func (s *Server) loadStoredModelContents(
	ctx context.Context,
	baseID uuid.UUID,
	change ModelRevisionChange,
) (storedModelContents, error) {
	var stored storedModelContents
	if change.Artifacts != nil {
		artifacts, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{ModelRevisionID: &baseID})
		if err != nil {
			return storedModelContents{}, fmt.Errorf("list artifacts of the active model revision: %w", err)
		}
		stored.artifacts = make(map[uuid.UUID]domainmodels.Artifact, len(artifacts))
		for _, artifact := range artifacts {
			stored.artifacts[artifact.ID] = artifact
		}
	}
	if change.Runs != nil {
		runs, err := s.storedModelRuns(ctx, baseID)
		if err != nil {
			return storedModelContents{}, err
		}
		stored.runs = make(map[uuid.UUID]validation.ModelRun, len(runs))
		for _, run := range runs {
			stored.runs[run.Run.ID] = run
		}
	}
	if change.Metrics != nil {
		metrics, err := s.database.Metrics.List(ctx, db.MetricFilters{ModelRevisionID: &baseID})
		if err != nil {
			return storedModelContents{}, fmt.Errorf("list metrics of the active model revision: %w", err)
		}
		stored.metrics = make(map[uuid.UUID]domainmodels.Metric, len(metrics))
		for _, metric := range metrics {
			stored.metrics[metric.ID] = metric
		}
	}
	return stored, nil
}

// checkStoredArtifactUnchanged refuses a request that sends a stored artifact's ID with
// values other than the stored ones.
func checkStoredArtifactUnchanged(stored domainmodels.Artifact, request CreateArtifactRequest) error {
	requested := artifactFromRequest(request)
	requestedType, err := artifactTypeFromRequest(request.Type)
	if err != nil {
		return fmt.Errorf("validate artifact %s: %w", request.Id, err)
	}
	sameMetadata, err := sameJSONObject(stored.Metadata, requested.Metadata)
	if err != nil {
		return fmt.Errorf("compare metadata of artifact %s: %w", request.Id, err)
	}
	same := stored.Name == requested.Name &&
		stored.Level == requested.Level &&
		stored.Type == requestedType &&
		reflect.DeepEqual(stored.URI, requested.URI) &&
		reflect.DeepEqual(stored.SHA256, requested.SHA256) &&
		reflect.DeepEqual(stored.Format, requested.Format) &&
		reflect.DeepEqual(stored.SizeBytes, requested.SizeBytes) &&
		sameMetadata
	if !same {
		return idInUse("artifact %s is stored with other values; stored artifacts cannot change, so send a new id", request.Id)
	}
	return nil
}

// checkStoredRunUnchanged refuses a request that sends a stored run's ID with values or
// artifact links other than the stored ones.
func checkStoredRunUnchanged(stored validation.ModelRun, request CreateRunRequest) error {
	requested := modelRunFromRequest(request)
	sameParameters, err := sameJSONObject(stored.Run.Parameters, requested.Run.Parameters)
	if err != nil {
		return fmt.Errorf("compare parameters of run %s: %w", request.Id, err)
	}
	sameMetadata, err := sameJSONObject(stored.Run.Metadata, requested.Run.Metadata)
	if err != nil {
		return fmt.Errorf("compare metadata of run %s: %w", request.Id, err)
	}
	same := stored.Run.Name == requested.Run.Name &&
		reflect.DeepEqual(stored.Run.SoftwareName, requested.Run.SoftwareName) &&
		reflect.DeepEqual(stored.Run.SoftwareVersion, requested.Run.SoftwareVersion) &&
		reflect.DeepEqual(stored.Run.Command, requested.Run.Command) &&
		sameParameters &&
		sameMetadata &&
		sameInstant(stored.Run.StartedAt, requested.Run.StartedAt) &&
		sameInstant(stored.Run.FinishedAt, requested.Run.FinishedAt) &&
		slices.Equal(sortedRunArtifactLinks(stored.Artifacts), sortedRunArtifactLinks(requested.Artifacts))
	if !same {
		return idInUse("run %s is stored with other values; stored runs cannot change, so send a new id", request.Id)
	}
	return nil
}

// checkStoredMetricUnchanged refuses a request that sends a stored metric's ID with values
// other than the stored ones.
func checkStoredMetricUnchanged(stored domainmodels.Metric, request CreateMetricRequest) error {
	if request.Value == nil {
		return invalidRequest("metric value is required")
	}
	requested := metricFromRequest(request)
	same := stored.Key == requested.Key &&
		stored.Value == requested.Value &&
		reflect.DeepEqual(stored.SoftwareName, requested.SoftwareName) &&
		reflect.DeepEqual(stored.SoftwareVersion, requested.SoftwareVersion) &&
		reflect.DeepEqual(stored.RunID, requested.RunID)
	if !same {
		return idInUse("metric %s is stored with other values; stored metrics cannot change, so send a new id", request.Id)
	}
	return nil
}

// sameJSONObject compares two JSON objects by their encoding, so map key order and the
// difference between a missing and an empty object do not count.
func sameJSONObject(left, right any) (bool, error) {
	leftJSON, err := jsonObjectEncoding(left)
	if err != nil {
		return false, err
	}
	rightJSON, err := jsonObjectEncoding(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftJSON, rightJSON), nil
}

func jsonObjectEncoding(value any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode JSON object: %w", err)
	}
	if string(encoded) == "null" {
		return []byte("{}"), nil
	}
	return encoded, nil
}

// sameInstant compares times at the database's microsecond precision.
func sameInstant(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Truncate(time.Microsecond).Equal(right.Truncate(time.Microsecond))
}

func sortedRunArtifactLinks(links []validation.RunArtifactLink) []validation.RunArtifactLink {
	sorted := slices.Clone(links)
	slices.SortFunc(sorted, func(left, right validation.RunArtifactLink) int {
		return cmp.Or(
			cmp.Compare(left.ArtifactID.String(), right.ArtifactID.String()),
			cmp.Compare(left.Direction, right.Direction),
		)
	})
	return sorted
}
