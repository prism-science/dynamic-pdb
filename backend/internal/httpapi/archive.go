package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"dynamic-pdb/backend/internal/archivemodels"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/backend/internal/validation"
	"dynamic-pdb/lib/archive"
	"dynamic-pdb/lib/rcsb"
)

const archiveLookupTimeout = 10 * time.Second

const (
	archivePdbDataType           = ArchivePdbDataType("archive_pdb_lookups")
	archiveWarningFromArchive    = ArchiveWarningSource("archive")
	archiveWarningFromValidation = ArchiveWarningSource("validation")
)

var pdbIDPattern = regexp.MustCompile(`^[0-9][A-Za-z0-9]{3}$`)

// ArchiveLookup reads what the PDB archive says about one entry.
type ArchiveLookup interface {
	Get(ctx context.Context, pdbID string) (archive.Result, error)
}

// WithArchiveLookup enables GET /v1/archive/pdb/{pdb_id}.
func (s *Server) WithArchiveLookup(lookup ArchiveLookup) *Server {
	s.archive = lookup
	return s
}

func (s *Server) GetArchivePdb(w http.ResponseWriter, r *http.Request, pdbID string) {
	if !pdbIDPattern.MatchString(pdbID) {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "pdb_id must be a four-character PDB ID")
		return
	}
	if s.archive == nil {
		slog.ErrorContext(r.Context(), "archive lookup is not configured")
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "archive lookup is not configured")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), archiveLookupTimeout)
	defer cancel()
	result, err := s.archive.Get(ctx, strings.ToUpper(pdbID))
	switch {
	case errors.Is(err, rcsb.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "RCSB has no entry with this PDB ID")
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "archive lookup failed", "err", err, "pdb_id", pdbID)
		writeError(w, http.StatusBadGateway, "BAD_GATEWAY", "failed to read the entry from RCSB")
		return
	}

	attributes, err := archiveAttributes(result)
	if err != nil {
		slog.ErrorContext(r.Context(), "build archive lookup response failed", "err", err, "pdb_id", pdbID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to build the lookup response")
		return
	}
	writeJSON(w, http.StatusOK, ArchivePdbDocument{
		Data: ArchivePdbData{
			Type:       archivePdbDataType,
			Id:         result.PDBID,
			Attributes: attributes,
		},
	})
}

func archiveAttributes(result archive.Result) (ArchivePdbAttributes, error) {
	entry, err := archiveEntryProposal(result.PDBID, result.Entry)
	if err != nil {
		return ArchivePdbAttributes{}, fmt.Errorf("build entry proposal: %w", err)
	}
	polymerEntities := make([]ArchivePolymerEntityProposal, 0, len(result.PolymerEntities))
	for _, polymerEntity := range result.PolymerEntities {
		polymerEntities = append(polymerEntities, archivePolymerEntityProposal(polymerEntity))
	}
	return ArchivePdbAttributes{
		Entry:           entry,
		Model:           archiveModelProposal(result.Model),
		PolymerEntities: polymerEntities,
		Warnings:        archiveWarnings(result.Warnings, nil),
	}, nil
}

// Validation pointers are relative to the proposal they sit in, not to the document.
func archiveEntryProposal(pdbID string, mapping archive.EntryMapping) (ArchiveEntryProposal, error) {
	metadata := archivemodels.EntryMetadata(mapping)
	metadata.ExternalRefs = map[models.EntrySource]string{models.EntrySourcePDB: pdbID}
	fieldErrors := validation.EntryMetadata(validation.Root, metadata)
	properties, err := entryPropertiesFromModel(metadata)
	if err != nil {
		return ArchiveEntryProposal{}, fmt.Errorf("convert entry metadata: %w", err)
	}

	revisionGroups := make([]ArchiveRevisionGroup, 0, len(mapping.RevisionGroups))
	for _, group := range mapping.RevisionGroups {
		revisionGroups = append(revisionGroups, ArchiveRevisionGroup{
			Group:           group.Group,
			RevisionOrdinal: group.RevisionOrdinal,
		})
	}
	proposal := ArchiveEntryProposal{
		Title:              mapping.Title,
		Details:            mapping.Details,
		Resolution:         mapping.Resolution,
		Method:             mapping.Method,
		SpaceGroup:         mapping.SpaceGroup,
		StatusCode:         mapping.StatusCode,
		InitialReleaseDate: mapping.InitialReleaseDate,
		LatestRevisionDate: mapping.LatestRevisionDate,
		LatestVersion:      mapping.LatestVersion,
		RevisionGroups:     revisionGroups,
		Supersedes:         nonNilStrings(mapping.Supersedes),
		SupersededBy:       nonNilStrings(mapping.SupersededBy),
		Provenance:         nonNilProvenance(mapping.Provenance),
		Conflicts:          archiveConflicts(mapping.Conflicts),
		Warnings:           archiveWarnings(mapping.Warnings, fieldErrors),
		Crystallography:    properties.Crystallography,
	}
	return proposal, nil
}

func archiveModelProposal(mapping archive.ModelMapping) ArchiveModelProposal {
	metadata := archivemodels.ModelMetadata(mapping)
	metrics := archivemodels.Metrics(mapping)
	fieldErrors := validation.ModelMetadata(validation.Root, metadata)
	fieldErrors = append(fieldErrors, validation.Metrics(validation.Root.Field("metrics"), metrics)...)

	proposedMetrics := make([]ArchiveMetric, 0, len(mapping.Metrics))
	for _, metric := range mapping.Metrics {
		proposedMetrics = append(proposedMetrics, ArchiveMetric{Key: metric.Key, Value: metric.Value})
	}
	return ArchiveModelProposal{
		Details:             mapping.Details,
		Authors:             nonNilStrings(mapping.Authors),
		Affiliation:         mapping.Affiliation,
		AtomCount:           mapping.AtomCount,
		ModeledResidues:     mapping.ModeledResidues,
		UniqueProteinChains: mapping.UniqueProteinChains,
		UnmodeledFraction:   mapping.UnmodeledFraction,
		Ligands:             nonNilStrings(mapping.Ligands),
		Metrics:             proposedMetrics,
		Provenance:          nonNilProvenance(mapping.Provenance),
		Conflicts:           archiveConflicts(mapping.Conflicts),
		Warnings:            archiveWarnings(mapping.Warnings, fieldErrors),
	}
}

// archivePolymerEntityProposal lists alignment-only UniProt mappings next to the
// reference identifiers, so a client sees every mapping RCSB supports.
func archivePolymerEntityProposal(mapping archive.PolymerEntityMapping) ArchivePolymerEntityProposal {
	metadata := archivemodels.PolymerEntityMetadata(mapping, true)
	fieldErrors := validation.PolymerEntity(validation.Root, metadata)

	organisms := make([]PolymerEntityOrganism, 0, len(metadata.SourceOrganisms))
	for _, organism := range metadata.SourceOrganisms {
		organisms = append(organisms, PolymerEntityOrganism{
			ScientificName: organism.ScientificName,
			NcbiTaxonomyId: organism.NCBITaxonomyID,
		})
	}
	uniProtMappings := make([]ArchiveUniProtMapping, 0, len(metadata.UniProtMappings))
	for _, uniProtMapping := range metadata.UniProtMappings {
		uniProtMappings = append(uniProtMappings, ArchiveUniProtMapping{
			Accession:  uniProtMapping.Accession,
			Source:     string(uniProtMapping.Source),
			UnpRelease: uniProtMapping.UniProtRelease,
		})
	}
	return ArchivePolymerEntityProposal{
		LabelEntityId:   metadata.LabelEntityID,
		Description:     metadata.Description,
		SourceOrganisms: organisms,
		Construct:       metadata.Construct,
		Mutations:       metadata.Mutations,
		UniprotMappings: uniProtMappings,
		Provenance:      nonNilProvenance(mapping.Provenance),
		Conflicts:       archiveConflicts(mapping.Conflicts),
		Warnings:        archiveWarnings(mapping.Warnings, fieldErrors),
	}
}

func archiveConflicts(conflicts []archive.Conflict) []ArchiveConflict {
	response := make([]ArchiveConflict, 0, len(conflicts))
	for _, conflict := range conflicts {
		response = append(response, ArchiveConflict{
			Field:  conflict.Field,
			Chosen: conflict.Chosen,
			Others: nonNilStrings(conflict.Others),
		})
	}
	return response
}

func archiveWarnings(warnings []archive.Warning, fieldErrors []validation.FieldError) []ArchiveWarning {
	response := make([]ArchiveWarning, 0, len(warnings)+len(fieldErrors))
	for _, warning := range warnings {
		response = append(response, ArchiveWarning{
			Source: archiveWarningFromArchive,
			Field:  ptr(warning.Field),
			Detail: warning.Detail,
		})
	}
	for _, fieldError := range fieldErrors {
		response = append(response, ArchiveWarning{
			Source:  archiveWarningFromValidation,
			Pointer: ptr(string(fieldError.Pointer)),
			Code:    ptr(string(fieldError.Code)),
			Detail:  fieldError.Detail,
		})
	}
	return response
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilProvenance(provenance map[string]string) map[string]string {
	if provenance == nil {
		return map[string]string{}
	}
	return provenance
}
