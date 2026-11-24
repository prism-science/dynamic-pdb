package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dynamic-pdb/backend/internal/db"
	domainmodels "dynamic-pdb/backend/internal/models"
)

// Filing a PDB deposition under another entry (researcher answer of 17 Nov, Q11): the "add a
// model from a PDB ID" form may file a deposition such as 44PS under the entry of 7KR0, but
// reviewers don't approve it until the grouping rules (same experimental data) pass. Those rules
// come next sprint, so for now such an approval is always refused; rejecting it is not.

// groupingNotApprovedCode is the error code of a refused cross-entry filing.
const groupingNotApprovedCode = "GROUPING_NOT_APPROVED"

// crossEntryFilingError is an in-review model revision whose approval would file a PDB
// deposition under an entry with another PDB ID, or with none.
type crossEntryFilingError struct {
	modelPDBID string
	// entryPDBID is "" when the entry has no PDB ID.
	entryPDBID string
}

func (e *crossEntryFilingError) Error() string {
	entry := "the entry has no PDB ID"
	if e.entryPDBID != "" {
		entry = "the entry's PDB ID is " + e.entryPDBID
	}
	return fmt.Sprintf("the model's PDB ID is %s and %s: a PDB deposition is filed under another "+
		"entry only once the grouping rules (same experimental data) pass, so it cannot be "+
		"approved yet; reject it or leave it in review", e.modelPDBID, entry)
}

// crossEntryFiling returns the error when approving target would file it under another entry:
// its own PDB ID differs from the entry's (or the entry has none), and the model is not filed
// there with that PDB ID already (it has no active revision, or the active one names another
// PDB ID). A model without a PDB ID is never a filing; later revisions of a model filed before
// this rule keep being approvable while its PDB ID stays the same. active is nil for a model
// without an active revision.
func crossEntryFiling(
	target domainmodels.ModelRevision,
	active *domainmodels.ModelRevision,
	entry domainmodels.EntryRevision,
) *crossEntryFilingError {
	modelPDBID := normalizedPDBID(target.Metadata.ExternalRefs[domainmodels.ModelSourcePDB])
	if modelPDBID == "" {
		return nil
	}
	entryPDBID := normalizedPDBID(entry.Metadata.ExternalRefs[domainmodels.EntrySourcePDB])
	if modelPDBID == entryPDBID {
		return nil
	}
	if active != nil && normalizedPDBID(active.Metadata.ExternalRefs[domainmodels.ModelSourcePDB]) == modelPDBID {
		return nil
	}
	return &crossEntryFilingError{modelPDBID: modelPDBID, entryPDBID: entryPDBID}
}

func normalizedPDBID(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

// crossEntryFilingOf reads what crossEntryFiling needs for target: the model's active revision
// (if any) and the entry's active revision. It returns nil when approving target files nothing
// under another entry, or when the entry has no active revision.
func (s *Server) crossEntryFilingOf(ctx context.Context, target domainmodels.ModelRevision) (*crossEntryFilingError, error) {
	activeState := domainmodels.RevisionStateActive
	entry, err := s.database.Entries.Get(ctx, db.EntryRevisionFilters{EntryID: &target.EntryID, State: &activeState})
	if errors.Is(err, db.ErrEntryRevisionNotFound) {
		// An entry with no published revision has nothing to file under yet; approving a model
		// of it is refused on its own grounds.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active revision of entry %s: %w", target.EntryID, err)
	}
	active, err := s.database.Models.Get(ctx, db.ModelRevisionFilters{
		EntryID: &target.EntryID, ModelID: &target.ModelID, State: &activeState, ExcludeResidueData: true,
	})
	switch {
	case errors.Is(err, db.ErrModelRevisionNotFound):
		active = nil
	case err != nil:
		return nil, fmt.Errorf("get active revision of model %s: %w", target.ModelID, err)
	}
	return crossEntryFiling(target, active, *entry), nil
}

// groupingNotApprovedFromFiling is what a model revision response says about a refused filing.
func groupingNotApprovedFromFiling(filing *crossEntryFilingError) *GroupingNotApproved {
	if filing == nil {
		return nil
	}
	grouping := GroupingNotApproved{ModelPdbId: filing.modelPDBID, Detail: filing.Error()}
	if filing.entryPDBID != "" {
		grouping.EntryPdbId = &filing.entryPDBID
	}
	return &grouping
}
