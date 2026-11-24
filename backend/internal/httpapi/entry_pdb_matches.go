package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

// entryListItems builds the entries of one GET /v1/entries page. A request with pdb_id or query
// also gets each entry's matched_models: its active models whose own PDB ID is one the request
// searched for, so a PDB ID search selects the model (researcher question 4). Nothing is merged
// or re-anchored; each model stays a model of its entry.
func (s *Server) entryListItems(
	ctx context.Context,
	revisions []domainmodels.EntryRevision,
	filters db.EntryRevisionFilters,
	params ListEntriesParams,
) ([]EntryInfoData, error) {
	reportsMatches := params.PdbId != nil || params.Query != nil
	var matched map[string][]MatchedModel
	if reportsMatches {
		entryIDs := make([]string, 0, len(revisions))
		for _, revision := range revisions {
			entryIDs = append(entryIDs, revision.EntryID)
		}
		var err error
		matched, err = s.matchedModelsByEntry(ctx, entryIDs, searchedModelPDBIDs(filters))
		if err != nil {
			return nil, err
		}
	}

	items := make([]EntryInfoData, 0, len(revisions))
	for _, revision := range revisions {
		info, err := entryInfoAttributesFromRevision(revision)
		if err != nil {
			return nil, fmt.Errorf("build entry info of %s: %w", revision.EntryID, err)
		}
		attributes, err := entryListAttributesFromInfo(info)
		if err != nil {
			return nil, fmt.Errorf("build entry list item of %s: %w", revision.EntryID, err)
		}
		if reportsMatches {
			models := matched[revision.EntryID]
			if models == nil {
				models = []MatchedModel{}
			}
			attributes.MatchedModels = &models
		}
		items = append(items, EntryInfoData{Type: jsonAPITypeEntries, Id: attributes.Id, Attributes: attributes})
	}
	return items, nil
}

// searchedModelPDBIDs are the PDB IDs a GET /v1/entries request searched for: its pdb_id values,
// and its whole text query when that is one word (a PDB ID is never two).
func searchedModelPDBIDs(filters db.EntryRevisionFilters) []string {
	pdbIDs := slices.Clone(filters.EntryOrModelPDBIDs)
	if filters.Query != "" && !strings.ContainsFunc(filters.Query, unicode.IsSpace) {
		pdbIDs = append(pdbIDs, filters.Query)
	}
	return pdbIDs
}

// matchedModelsByEntry finds, for each entry, its active models whose own PDB ID is one of
// pdbIDs (ignoring case), archive models first, then in model list order. The one query is served
// by the partial index model_revisions_active_pdb_ref_idx.
func (s *Server) matchedModelsByEntry(
	ctx context.Context,
	entryIDs []string,
	pdbIDs []string,
) (map[string][]MatchedModel, error) {
	matched := make(map[string][]MatchedModel, len(entryIDs))
	if len(entryIDs) == 0 || len(pdbIDs) == 0 {
		return matched, nil
	}
	activeState := domainmodels.RevisionStateActive
	activeModelState := domainmodels.ModelStateActive
	revisions, err := s.database.Models.List(ctx, db.ModelRevisionFilters{
		EntryIDs:           entryIDs,
		PDBIDs:             pdbIDs,
		State:              &activeState,
		ModelState:         &activeModelState,
		ExcludeResidueData: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list models matching the searched PDB IDs: %w", err)
	}
	slices.SortStableFunc(revisions, func(a, b domainmodels.ModelRevision) int {
		return archiveModelRank(a) - archiveModelRank(b)
	})
	for _, revision := range revisions {
		matched[revision.EntryID] = append(matched[revision.EntryID], MatchedModel{
			ModelId: revision.ModelID,
			PdbId:   revision.Metadata.ExternalRefs[domainmodels.ModelSourcePDB],
		})
	}
	return matched, nil
}

// archiveModelRank orders archive models (model type Deposited, or origin archive) before the
// others: the deposition a PDB ID names is the model it selects first.
func archiveModelRank(revision domainmodels.ModelRevision) int {
	if revision.Metadata.IsArchiveModel() {
		return 0
	}
	return 1
}

// entryListAttributesFromInfo copies every entry info field into an entry list item. It goes
// through JSON, as EntryListAttributes is EntryInfoAttributes plus matched_models, so a field added
// to EntryInfoAttributes reaches the list without a change here.
func entryListAttributesFromInfo(info EntryInfoAttributes) (EntryListAttributes, error) {
	data, err := json.Marshal(info)
	if err != nil {
		return EntryListAttributes{}, fmt.Errorf("marshal entry info: %w", err)
	}
	var attributes EntryListAttributes
	if err := json.Unmarshal(data, &attributes); err != nil {
		return EntryListAttributes{}, fmt.Errorf("unmarshal entry info: %w", err)
	}
	return attributes, nil
}
