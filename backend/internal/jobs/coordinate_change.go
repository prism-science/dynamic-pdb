package jobs

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
)

// atomicModelGroup is the RCSB revision group of a version that changed the coordinates.
const atomicModelGroup = "Atomic model"

// coordinateFileKeptNote ends the change_summary of a sync revision that applies a version with
// a coordinate change but still links the previous coordinate file.
const coordinateFileKeptNote = " (coordinate file not replaced yet)"

// archiveVersionMetadataKey is the artifact metadata key naming the RCSB version a coordinate
// file stands for.
const archiveVersionMetadataKey = "archive_version"

// coordinateChange is the new coordinate file a sync revision links instead of the previous one
// (researcher answer Q4a).
type coordinateChange struct {
	previous    models.Artifact
	replacement models.Artifact
}

// hasAtomicModelChange reports whether RCSB changed the coordinates in a version after applied,
// up to latest.
func hasAtomicModelChange(applied models.ArchiveVersion, latest models.ArchiveVersion, groups []archive.RevisionGroup) bool {
	for _, group := range groups {
		if group.RevisionOrdinal > applied.Ordinal &&
			group.RevisionOrdinal <= latest.Ordinal &&
			group.Group == atomicModelGroup {
			return true
		}
	}
	return false
}

// planCoordinateChange returns the coordinate change a sync revision of the model makes for
// latest, or the reason it must wait.
func (s *modelSyncStrategy) planCoordinateChange(
	ctx context.Context,
	revision models.ModelRevision,
	pdbID string,
	latest models.ArchiveVersion,
) (*coordinateChange, string, error) {
	coordinateFile, err := s.rcsbClient.GetFile(ctx, pdbID, rcsb.CoordinateFile(pdbID))
	if err != nil {
		return nil, "", fmt.Errorf("get RCSB coordinate file URL of %s: %w", pdbID, err)
	}
	var linked []models.Artifact
	if s.coordinateRevisions {
		linked, err = s.database.Artifacts.List(ctx, db.ArtifactFilters{
			ModelRevisionID: &revision.ID,
			Types:           []models.ArtifactType{models.ArtifactTypeModel},
		})
		if err != nil {
			return nil, "", fmt.Errorf("list model files: %w", err)
		}
	}
	previous, reason, err := coordinateChangeBlock(s.coordinateRevisions, linked, coordinateFile.URI)
	if err != nil || reason != "" {
		return nil, reason, err
	}
	replacement, err := newCoordinateArtifact(previous, latest, s.now())
	if err != nil {
		return nil, "", fmt.Errorf("prepare new coordinate file: %w", err)
	}
	return &coordinateChange{previous: previous, replacement: replacement}, "", nil
}

// coordinateChangeBlock returns the model's current coordinate file when a sync revision may
// replace it, or the reason it must wait. Replacing it is safe only once our copy keeps its
// bytes, because RCSB serves only the newest file at its URL.
func coordinateChangeBlock(
	enabled bool,
	linked []models.Artifact,
	coordinateURI string,
) (models.Artifact, string, error) {
	if !enabled {
		return models.Artifact{}, "coordinate revisions are switched off", nil
	}
	previous, reason := previousCoordinateArtifact(linked, coordinateURI)
	if reason != "" {
		return models.Artifact{}, reason, nil
	}
	switch previous.Checksum.Status {
	case models.ChecksumStatusVerified:
		if strings.TrimSpace(stringValue(previous.Checksum.StoredURI)) == "" {
			return models.Artifact{}, "the checksum job verified the previous coordinate file but kept no copy of it", nil
		}
		return previous, "", nil
	case models.ChecksumStatusPending:
		return models.Artifact{}, "the checksum job has not copied the previous coordinate file yet (checksum pending)", nil
	case models.ChecksumStatusFailed:
		return models.Artifact{}, "the checksum job could not read the previous coordinate file (checksum failed)", nil
	case models.ChecksumStatusMismatch:
		return models.Artifact{},
			"RCSB replaced the previous coordinate file before the checksum job copied it (checksum mismatch)", nil
	default:
		return models.Artifact{}, "", fmt.Errorf("unknown checksum status: %s", previous.Checksum.Status)
	}
}

// previousCoordinateArtifact returns the one linked artifact that is RCSB's coordinate file, or
// the reason there is not exactly one.
func previousCoordinateArtifact(linked []models.Artifact, coordinateURI string) (models.Artifact, string) {
	var matches []models.Artifact
	for _, artifact := range linked {
		if artifact.Type == models.ArtifactTypeModel && stringValue(artifact.URI) == coordinateURI {
			matches = append(matches, artifact)
		}
	}
	switch len(matches) {
	case 0:
		return models.Artifact{}, "the model links no RCSB coordinate file " + coordinateURI
	case 1:
		return matches[0], ""
	default:
		return models.Artifact{}, fmt.Sprintf("the model links %d artifacts for RCSB coordinate file %s", len(matches), coordinateURI)
	}
}

// newCoordinateArtifact describes the coordinate file of version: the previous file's name,
// level, type, format and URL, with the version in its metadata. Its sha256 and size are left for
// the checksum job, which reads the bytes RCSB serves.
func newCoordinateArtifact(
	previous models.Artifact,
	version models.ArchiveVersion,
	createdAt time.Time,
) (models.Artifact, error) {
	metadata := map[string]any{}
	switch previousMetadata := previous.Metadata.(type) {
	case nil:
	case map[string]any:
		metadata = maps.Clone(previousMetadata)
	default:
		return models.Artifact{}, fmt.Errorf("metadata of previous coordinate file has type %T, not an object", previous.Metadata)
	}
	metadata[archiveVersionMetadataKey] = version.Version
	return models.Artifact{
		ID:        uuid.New(),
		Name:      previous.Name,
		Level:     previous.Level,
		Type:      previous.Type,
		URI:       previous.URI,
		Format:    previous.Format,
		Metadata:  metadata,
		CreatedBy: dataSyncUserID,
		CreatedAt: createdAt,
	}, nil
}

// coordinateArtifactLinks returns the artifact links of a sync revision that replaces the
// previous coordinate file: the parent's links, with the replacement in the previous file's place.
func coordinateArtifactLinks(parentLinks []uuid.UUID, previousID uuid.UUID, replacementID uuid.UUID) []uuid.UUID {
	links := make([]uuid.UUID, 0, len(parentLinks))
	for _, artifactID := range parentLinks {
		if artifactID == previousID {
			links = append(links, replacementID)
			continue
		}
		links = append(links, artifactID)
	}
	return links
}

// coordinatePrimaryArtifact moves the primary artifact to the replacement when it was the previous
// coordinate file.
func coordinatePrimaryArtifact(primary *uuid.UUID, previousID uuid.UUID, replacementID uuid.UUID) *uuid.UUID {
	if primary != nil && *primary == previousID {
		return &replacementID
	}
	return primary
}
