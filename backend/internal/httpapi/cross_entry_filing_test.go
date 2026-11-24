package httpapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainmodels "dynamic-pdb/backend/internal/models"
)

func modelRevisionWithPDBID(pdbID string) domainmodels.ModelRevision {
	revision := domainmodels.ModelRevision{}
	if pdbID != "" {
		revision.Metadata.ExternalRefs = map[domainmodels.ModelSource]string{domainmodels.ModelSourcePDB: pdbID}
	}
	return revision
}

func entryRevisionWithPDBID(pdbID string) domainmodels.EntryRevision {
	revision := domainmodels.EntryRevision{}
	if pdbID != "" {
		revision.Metadata.ExternalRefs = map[domainmodels.EntrySource]string{domainmodels.EntrySourcePDB: pdbID}
	}
	return revision
}

// Researcher answer of 17 Nov, Q11: approving a PDB deposition filed under another entry waits for
// the grouping rules.
func Test_should_refuse_only_a_first_filing_under_another_entry_when_a_model_revision_is_approved(t *testing.T) {
	sameActive := modelRevisionWithPDBID("44PS")
	otherActive := modelRevisionWithPDBID("9ZZZ")
	computedActive := modelRevisionWithPDBID("")
	cases := []struct {
		name       string
		model      string
		active     *domainmodels.ModelRevision
		entry      string
		wantRefuse bool
		wantEntry  string
	}{
		{name: "the entry's own PDB ID, ignoring case and spaces", model: " 7kr0 ", entry: "7KR0"},
		{name: "a model without a PDB ID", model: "", entry: "7KR0"},
		{name: "a first filing under another entry", model: "44PS", entry: "7KR0", wantRefuse: true, wantEntry: "7KR0"},
		{name: "a first filing under an entry without a PDB ID", model: "44PS", entry: "", wantRefuse: true},
		{name: "a later revision of a model filed before, same PDB ID", model: "44ps", active: &sameActive, entry: "7KR0"},
		{name: "a revision that changes the model's PDB ID", model: "44PS", active: &otherActive, entry: "7KR0", wantRefuse: true, wantEntry: "7KR0"},
		{name: "a revision that gives a computed model a PDB ID", model: "44PS", active: &computedActive, entry: "7KR0", wantRefuse: true, wantEntry: "7KR0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// when
			filing := crossEntryFiling(modelRevisionWithPDBID(tc.model), tc.active, entryRevisionWithPDBID(tc.entry))

			// then
			if !tc.wantRefuse {
				assert.Nil(t, filing)
				return
			}
			require.NotNil(t, filing)
			assert.Equal(t, "44PS", filing.modelPDBID)
			assert.Equal(t, tc.wantEntry, filing.entryPDBID)
		})
	}
}

func Test_should_name_both_pdb_ids_and_what_to_do_when_a_filing_is_refused(t *testing.T) {
	// when
	underEntry := (&crossEntryFilingError{modelPDBID: "44PS", entryPDBID: "7KR0"}).Error()
	underNone := (&crossEntryFilingError{modelPDBID: "44PS"}).Error()

	// then
	assert.Contains(t, underEntry, "the model's PDB ID is 44PS and the entry's PDB ID is 7KR0")
	assert.Contains(t, underEntry, "grouping rules")
	assert.Contains(t, underEntry, "reject it or leave it in review")
	assert.Contains(t, underNone, "the entry has no PDB ID")
}

func Test_should_describe_a_refused_filing_on_the_model_revision_when_one_is_found(t *testing.T) {
	// when
	none := groupingNotApprovedFromFiling(nil)
	underEntry := groupingNotApprovedFromFiling(&crossEntryFilingError{modelPDBID: "44PS", entryPDBID: "7KR0"})
	underNone := groupingNotApprovedFromFiling(&crossEntryFilingError{modelPDBID: "44PS"})

	// then
	assert.Nil(t, none)
	require.NotNil(t, underEntry)
	assert.Equal(t, "44PS", underEntry.ModelPdbId)
	assert.Equal(t, ptr("7KR0"), underEntry.EntryPdbId)
	assert.Contains(t, underEntry.Detail, "grouping rules")
	require.NotNil(t, underNone)
	assert.Nil(t, underNone.EntryPdbId)
}
