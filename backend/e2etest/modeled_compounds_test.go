package e2etest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
)

// createDepositedModelWithFileForTest adds an active Deposited model (no run needed) to a new
// active entry. Its model file has the given URI, and its metadata carries what a released CLI
// sends: typed ligands and cofactors, and the parser's review flags in extensions.
func createDepositedModelWithFileForTest(t *testing.T, token string, artifactID uuid.UUID, uri string) httpapi.CreateModelRevisionAttributes {
	t.Helper()
	entry := createAndActivateEntryForTest(t, token, map[string]any{
		"entry": map[string]any{"title": "modeled compounds entry"},
	})
	return createAndActivateModelForTest(t, token, entry.EntryId, map[string]any{
		"model": map[string]any{
			"title":               "modeled compounds model",
			"primary_artifact_id": artifactID,
			"metadata": map[string]any{
				"model_type": "Deposited",
				"ligands":    []string{"ATP"},
				"cofactors":  []string{"HEM"},
				"extensions": map[string]any{
					"compound_review": []map[string]any{{"list": "ligands", "id": "ATP", "reason": "often from the buffer"}},
				},
			},
			"artifacts": []map[string]any{{
				"id": artifactID, "name": "model.cif", "level": "L2", "type": "model", "format": "mmcif", "uri": uri,
			}},
		},
	})
}

func textPtr(value string) *string {
	return &value
}

func storedModelURI(artifactID uuid.UUID) string {
	return fmt.Sprintf("s3://dynamic-pdb/entry/models/model/artifacts/%s/model.cif", artifactID)
}

func Test_should_report_the_compounds_of_the_file_when_the_model_file_is_ours(t *testing.T) {
	// given: a model created the way a released CLI does, in enforce mode.
	token := issueEntryTokenForGitHubIDForTest(t, "modeled-compounds-owner", 8601)
	artifactID := uuid.New()
	created := createDepositedModelWithFileForTest(t, token, artifactID, storedModelURI(artifactID))

	// when: the job has not read the file yet.
	pending := getModelForTest(t, created.EntryId, created.ModelId)

	// then
	assert.Equal(t, httpapi.Pending, pending.ModeledCompounds.Status)
	assert.Empty(t, pending.ModeledCompounds.Copies)
	assert.Equal(t, []any{"ATP"}, pending.Metadata["ligands"], "the typed list is kept")

	// when: the job has read it.
	_, err := testDatabase.SaveModeledCompounds(context.Background(), db.SaveModeledCompoundsParams{
		ArtifactID: artifactID,
		CreatedBy:  uuid.MustParse(models.SystemUserID),
		Result: cif.ModeledCompounds{
			Copies: []cif.CompoundCopy{
				{CCDID: "GOL", LabelAsymID: "C", AuthAsymID: "A", AuthSeqID: "301", EntityID: "3", Source: cif.SourceNonpolyScheme},
				{CCDID: "MSE", AuthAsymID: "A", AuthSeqID: "2", Source: cif.SourcePDBHetatm, NeedsReview: true, ReviewReason: "could be a chain residue"},
			},
			Complete: true,
			Warnings: []string{},
		},
		ReadAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	done := getModelForTest(t, created.EntryId, created.ModelId)
	// The reviewer's revision path, which the review preview reads.
	revision := getModelRevisionForTest(t, fmt.Sprintf("/v1/entries/%s/models/%s/revisions/%s",
		created.EntryId, created.ModelId, created.RevisionId), adminToken)

	// then
	compounds := done.ModeledCompounds
	assert.Equal(t, httpapi.Done, compounds.Status)
	assert.Equal(t, cif.ParserVersion, stringValue(compounds.ParserVersion))
	require.NotNil(t, compounds.Complete)
	assert.True(t, *compounds.Complete)
	assert.NotNil(t, compounds.ReadAt)
	assert.Equal(t, []httpapi.ModeledCompoundCopy{
		{CcdId: "GOL", LabelAsymId: textPtr("C"), AuthAsymId: textPtr("A"), AuthSeqId: textPtr("301"), EntityId: textPtr("3"), Source: httpapi.NonpolyScheme},
		{CcdId: "MSE", AuthAsymId: textPtr("A"), AuthSeqId: textPtr("2"), Source: httpapi.PdbHetatm, NeedsReview: true, ReviewReason: textPtr("could be a chain residue")},
	}, compounds.Copies)
	assert.Equal(t, compounds, revision.ModeledCompounds, "the review preview reads the same record")
}

func Test_should_report_not_stored_when_the_model_file_is_not_in_our_bucket(t *testing.T) {
	// given: an archive-style model file on files.rcsb.org.
	token := issueEntryTokenForGitHubIDForTest(t, "modeled-compounds-archive", 8602)
	created := createDepositedModelWithFileForTest(t, token, uuid.New(), "https://files.rcsb.org/download/1ABC.cif")

	// when
	model := getModelForTest(t, created.EntryId, created.ModelId)

	// then
	assert.Equal(t, httpapi.NotStored, model.ModeledCompounds.Status)
	assert.Empty(t, model.ModeledCompounds.Copies)
	assert.Empty(t, model.ModeledCompounds.Warnings)
}

func Test_should_report_the_reason_when_the_model_file_could_not_be_read(t *testing.T) {
	// given
	token := issueEntryTokenForGitHubIDForTest(t, "modeled-compounds-failed", 8603)
	artifactID := uuid.New()
	created := createDepositedModelWithFileForTest(t, token, artifactID, storedModelURI(artifactID))
	err := testDatabase.ModeledCompounds.RecordFailure(context.Background(), artifactID, `unknown model file format "ccp4"`, nil)
	require.NoError(t, err)

	// when
	model := getModelForTest(t, created.EntryId, created.ModelId)

	// then
	assert.Equal(t, httpapi.Failed, model.ModeledCompounds.Status)
	assert.Equal(t, `unknown model file format "ccp4"`, stringValue(model.ModeledCompounds.LastError))
	assert.Empty(t, model.ModeledCompounds.Copies)
}
