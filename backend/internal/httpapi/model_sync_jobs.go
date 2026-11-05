package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/rcsb"
)

// RCSBFiles names the URLs of RCSB files.
type RCSBFiles interface {
	GetFile(ctx context.Context, pdbID string, file string) (rcsb.Artifact, error)
}

// ensureArchiveModelSyncJob makes sure an archive deposition has a sync job once a
// revision of it is active. An existing job, stopped or not, keeps its schedule and recorded
// version. The first run is 7 to 14 days out, as for entries.
func (s *Server) ensureArchiveModelSyncJob(ctx context.Context, revision domainmodels.ModelRevision, now time.Time) error {
	pdbID := strings.TrimSpace(revision.Metadata.ExternalRefs[domainmodels.ModelSourcePDB])
	if pdbID == "" || !revision.Metadata.IsArchiveModel() {
		return nil
	}
	coordinateFile, err := s.rcsbFiles.GetFile(ctx, pdbID, rcsb.CoordinateFile(pdbID))
	if err != nil {
		return fmt.Errorf("get RCSB coordinate file URL of %s: %w", pdbID, err)
	}
	linked, err := s.database.Artifacts.List(ctx, db.ArtifactFilters{
		ModelRevisionID: &revision.ID,
		Types:           []domainmodels.ArtifactType{domainmodels.ArtifactTypeModel},
	})
	if err != nil {
		return fmt.Errorf("list model files: %w", err)
	}
	if !needsArchiveModelSync(revision, linked, coordinateFile.URI) {
		return nil
	}
	created, err := s.database.DataSyncJobs.CreateIfMissing(ctx, domainmodels.DataSyncJob{
		EntryID:     revision.EntryID,
		ModelID:     &revision.ModelID,
		ScheduledAt: nextEntryDataSyncScheduledAt(now),
	})
	if err != nil {
		return fmt.Errorf("create model data sync job: %w", err)
	}
	if created {
		slog.InfoContext(ctx, "model data sync job created", "entry_id", revision.EntryID, "model_id", revision.ModelID)
	}
	return nil
}

// needsArchiveModelSync reports whether an active model revision is an archive deposition: Deposited
// or with the archive as its origin, naming a PDB ID, and linking RCSB's coordinate file for it. A
// depositor's Deposited model links its own file, and the archive sync leaves it alone.
func needsArchiveModelSync(
	revision domainmodels.ModelRevision,
	linked []domainmodels.Artifact,
	coordinateURI string,
) bool {
	if !revision.Metadata.IsArchiveModel() || strings.TrimSpace(revision.Metadata.ExternalRefs[domainmodels.ModelSourcePDB]) == "" {
		return false
	}
	return linksCoordinateFile(linked, coordinateURI)
}

func linksCoordinateFile(linked []domainmodels.Artifact, coordinateURI string) bool {
	for _, artifact := range linked {
		if artifact.Type == domainmodels.ArtifactTypeModel && artifact.URI != nil && *artifact.URI == coordinateURI {
			return true
		}
	}
	return false
}
