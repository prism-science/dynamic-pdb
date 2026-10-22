package archive

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"dynamic-pdb/lib/rcsb"
)

// ErrEntryNotFound reports that RCSB has no entry with the requested PDB ID. A
// polymer entity that RCSB lists but cannot serve is a different, upstream failure.
var ErrEntryNotFound = errors.New("RCSB has no entry with this PDB ID")

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
	if errors.Is(err, rcsb.ErrNotFound) {
		return Result{}, fmt.Errorf("get RCSB entry %s: %w", pdbID, ErrEntryNotFound)
	}
	if err != nil {
		return Result{}, fmt.Errorf("get RCSB entry %s: %w", pdbID, err)
	}

	result := Result{
		PDBID: pdbID,
		Entry: MapEntry(details),
		Model: MapModel(details),
	}
	entityIDs := normalizedUniqueIDs(details.Identifiers.PolymerEntityIDs)
	entities := make([]rcsb.PolymerEntityDetails, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entity, err := lookup.details.GetPolymerEntityDetails(ctx, pdbID, entityID)
		if err != nil {
			return Result{}, fmt.Errorf("get RCSB polymer entity %s_%s: %w", pdbID, entityID, err)
		}
		entities = append(entities, entity)
	}

	uniProtRelease, releaseWarnings := lookup.uniProtRelease(ctx, pdbID, entities)
	result.Warnings = append(result.Warnings, releaseWarnings...)
	for index, entity := range entities {
		result.PolymerEntities = append(result.PolymerEntities, MapPolymerEntity(entity, entityIDs[index], uniProtRelease))
	}
	return result, nil
}

// uniProtRelease asks SIFTS for the entry's UniProt release once, before any entity
// is mapped, so every entity gets the same release whatever order RCSB lists them in.
// SIFTS is asked only when some entity's UniProt mapping depends on the release.
func (lookup *Lookup) uniProtRelease(
	ctx context.Context,
	pdbID string,
	entities []rcsb.PolymerEntityDetails,
) (*string, []Warning) {
	if !slices.ContainsFunc(entities, dependsOnUniProtRelease) {
		return nil, nil
	}
	release, err := lookup.releases.GetUniProtRelease(ctx, pdbID)
	if err != nil {
		return nil, []Warning{{
			Field:  "uniprot_release",
			Detail: fmt.Sprintf("get UniProt release for %s: %s", pdbID, err),
		}}
	}
	normalizedRelease := NormalizeUniProtRelease(release)
	if normalizedRelease == nil {
		return nil, []Warning{{
			Field:  "uniprot_release",
			Detail: fmt.Sprintf("SIFTS reports no UniProt release for %s", pdbID),
		}}
	}
	return normalizedRelease, nil
}

func normalizedUniqueIDs(values []string) []string {
	var result []string
	for _, value := range values {
		result = appendUnique(result, strings.TrimSpace(value))
	}
	return result
}
