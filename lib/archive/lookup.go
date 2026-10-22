package archive

import (
	"context"
	"fmt"
	"strings"

	"dynamic-pdb/lib/rcsb"
)

type EntryDetailsSource interface {
	GetEntryDetails(ctx context.Context, pdbID string) (rcsb.EntryDetails, error)
	GetPolymerEntityDetails(ctx context.Context, pdbID string, entityID string) (rcsb.PolymerEntityDetails, error)
}

type UniProtReleaseSource interface {
	GetUniProtRelease(ctx context.Context, pdbID string) (*string, error)
}

// Result is everything the archive proposes for one PDB entry. Warnings covers
// problems of the lookup itself; each mapping carries its own warnings.
type Result struct {
	PDBID           string
	Entry           EntryMapping
	Model           ModelMapping
	PolymerEntities []PolymerEntityMapping
	Warnings        []Warning
}

type Lookup struct {
	details  EntryDetailsSource
	releases UniProtReleaseSource
}

func NewLookup(details EntryDetailsSource, releases UniProtReleaseSource) *Lookup {
	return &Lookup{details: details, releases: releases}
}

// Get reads a PDB entry from RCSB and maps it. A missing UniProt release is
// reported as a warning, because the rest of the mapping is still useful.
func (lookup *Lookup) Get(ctx context.Context, pdbID string) (Result, error) {
	pdbID = strings.ToUpper(strings.TrimSpace(pdbID))
	details, err := lookup.details.GetEntryDetails(ctx, pdbID)
	if err != nil {
		return Result{}, fmt.Errorf("get RCSB entry %s: %w", pdbID, err)
	}

	result := Result{
		PDBID: pdbID,
		Entry: MapEntry(details),
		Model: MapModel(details),
	}
	var uniProtRelease *string
	uniProtReleaseLoaded := false
	for _, entityID := range normalizedUniqueIDs(details.Identifiers.PolymerEntityIDs) {
		entity, err := lookup.details.GetPolymerEntityDetails(ctx, pdbID, entityID)
		if err != nil {
			return Result{}, fmt.Errorf("get RCSB polymer entity %s_%s: %w", pdbID, entityID, err)
		}
		if !uniProtReleaseLoaded && HasSIFTSMapping(entity) {
			uniProtReleaseLoaded = true
			release, err := lookup.releases.GetUniProtRelease(ctx, pdbID)
			if err != nil {
				result.Warnings = append(result.Warnings, Warning{
					Field:  "uniprot_release",
					Detail: fmt.Sprintf("get UniProt release for %s: %s", pdbID, err),
				})
			}
			uniProtRelease = NormalizeUniProtRelease(release)
		}
		result.PolymerEntities = append(result.PolymerEntities, MapPolymerEntity(entity, entityID, uniProtRelease))
	}
	return result, nil
}

func normalizedUniqueIDs(values []string) []string {
	var result []string
	for _, value := range values {
		result = appendUnique(result, strings.TrimSpace(value))
	}
	return result
}
