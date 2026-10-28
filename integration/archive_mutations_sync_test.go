package integration

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_should_store_uniprot_mutations_when_rcsb_mmcif_has_engineered_mutation_rows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv(integrationRunEnv) != "1" {
		t.Skip("set " + integrationRunEnv + "=1 to run integration tests")
	}

	testCases := []struct {
		name              string
		differenceRows    string
		expectedMutations []string
		expectedRaw       string
		expectedStatus    string
	}{
		{
			name:              "mapped",
			differenceRows:    "1 THR UNP P11111 CYS 54 'engineered mutation'\n",
			expectedMutations: []string{"C54T"},
			expectedRaw:       "C4T",
			expectedStatus:    "parsed",
		},
		{
			name: "unmapped",
			differenceRows: "1 THR UNP P11111 CYS 54 'engineered mutation'\n" +
				"1 THR UNP P11111 CYS ? 'engineered mutation'\n",
			expectedMutations: []string{"C54T"},
			expectedRaw:       "C4T",
			expectedStatus:    "needs_review",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// given: an entry at version 1.0, and RCSB version 1.1 whose mmCIF lists the mutations
			root := repoRoot(t)
			backendBinaryPath := buildBackend(t, root)
			database := setupDB(t)
			auth := seedUserAndIssueToken(t, database)
			pdbID := integrationPDBID()
			fixture := seedScheduledDataSyncEntry(t, database, auth.UserID, pdbID)
			rcsb := newRCSBStub(t, pdbID)
			rcsb.coordinateFile = mmCIFWithMutationRows(pdbID, testCase.differenceRows)
			defer rcsb.Close()
			setRCSBURLs(t, rcsb.URL())
			s3 := newS3Stub(t)
			defer s3.Close()

			// when
			startBackend(t, root, backendBinaryPath, s3.URL())
			var activeRevisionID uuid.UUID
			require.Eventually(t, func() bool {
				err := database.Conn.QueryRowContext(t.Context(),
					`select id from entry_revisions where entry_id = $1 and state = 'active'`, fixture.EntryID,
				).Scan(&activeRevisionID)
				return err == nil && activeRevisionID != fixture.RevisionID
			}, 20*time.Second, 100*time.Millisecond)

			// then
			var rawMetadata []byte
			require.NoError(t, database.Conn.QueryRowContext(t.Context(),
				`select polymer_entities.metadata
				 from entry_revision_polymer_entities
				 join polymer_entities on polymer_entities.id = entry_revision_polymer_entities.polymer_entity_id
				 where entry_revision_polymer_entities.entry_revision_id = $1`, activeRevisionID,
			).Scan(&rawMetadata))
			var metadata struct {
				LabelEntityID   string   `json:"label_entity_id"`
				Mutations       []string `json:"mutations"`
				MutationsRaw    string   `json:"mutations_raw"`
				MutationsStatus string   `json:"mutations_status"`
			}
			require.NoError(t, json.Unmarshal(rawMetadata, &metadata))
			assert.Equal(t, "1", metadata.LabelEntityID)
			assert.Equal(t, testCase.expectedMutations, metadata.Mutations)
			assert.Equal(t, testCase.expectedRaw, metadata.MutationsRaw)
			assert.Equal(t, testCase.expectedStatus, metadata.MutationsStatus)
		})
	}
}

// mmCIFWithMutationRows is a coordinate file whose entity 1 (pdbx_mutation C4T) has the given
// _struct_ref_seq_dif rows.
func mmCIFWithMutationRows(pdbID string, differenceRows string) string {
	return "data_" + pdbID + "\n" +
		"loop_\n_entity.id\n_entity.type\n_entity.pdbx_mutation\n1 polymer C4T\n#\n" +
		"_struct_ref.id 1\n_struct_ref.entity_id 1\n#\n" +
		"_struct_ref_seq.align_id 1\n_struct_ref_seq.ref_id 1\n#\n" +
		"loop_\n_struct_ref_seq_dif.align_id\n_struct_ref_seq_dif.mon_id\n_struct_ref_seq_dif.pdbx_seq_db_name\n" +
		"_struct_ref_seq_dif.pdbx_seq_db_accession_code\n_struct_ref_seq_dif.db_mon_id\n" +
		"_struct_ref_seq_dif.pdbx_seq_db_seq_num\n_struct_ref_seq_dif.details\n" +
		differenceRows +
		"#\n" +
		"loop_\n_atom_site.group_PDB\n_atom_site.type_symbol\n_atom_site.label_comp_id\n" +
		"_atom_site.auth_asym_id\n_atom_site.auth_seq_id\n" +
		"ATOM N CYS A 4\n#\n"
}
