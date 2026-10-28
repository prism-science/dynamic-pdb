package jobs

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"dynamic-pdb/backend/internal/models"
	"dynamic-pdb/lib/cif"
	"dynamic-pdb/lib/rcsb"
)

func Test_should_set_uniprot_mutations_and_keep_original_text_when_mmcif_maps_entity(t *testing.T) {
	// given
	desired := []models.PolymerEntity{polymerEntityWithMutations("1", []string{"C4T"}, "C4T", models.MutationsStatusParsed)}
	mutations := archiveMutations{complete: true, sets: []cif.MutationSet{{
		EntityID: "1", Accession: "P11111", Mutations: []string{"C54T"}, Original: "C4T",
	}}}

	// when
	result := applyArchiveMutations(desired, nil, mutations)

	// then
	require.Len(t, result, 1)
	assert.Equal(t, []string{"C54T"}, result[0].Metadata.Mutations)
	assert.Equal(t, optionalString("C4T"), result[0].Metadata.MutationsRaw)
	assert.Equal(t, new(models.MutationsStatusParsed), result[0].Metadata.MutationsStatus)
	assert.Equal(t, []string{"C4T"}, desired[0].Metadata.Mutations)
}

func Test_should_mark_mutations_needs_review_when_mmcif_rows_cannot_be_mapped(t *testing.T) {
	// given
	desired := []models.PolymerEntity{polymerEntityWithMutations("1", []string{"C4T"}, "C4T, X5Y", models.MutationsStatusNeedsReview)}
	mutations := archiveMutations{complete: true, sets: []cif.MutationSet{{
		EntityID:     "1",
		Accession:    "P11111",
		Mutations:    []string{"C54T"},
		Original:     "C4T, X5Y",
		Unmapped:     []string{"UNP P11111 XXX -> TYR (55): reference residue \"XXX\" is not a standard amino acid"},
		NeedsReview:  true,
		ReviewReason: "engineered mutation rows could not be mapped to UniProt numbering",
	}}}

	// when
	result := applyArchiveMutations(desired, nil, mutations)

	// then
	assert.Equal(t, []string{"C54T"}, result[0].Metadata.Mutations)
	assert.Equal(t, optionalString("C4T, X5Y"), result[0].Metadata.MutationsRaw)
	assert.Equal(t, new(models.MutationsStatusNeedsReview), result[0].Metadata.MutationsStatus)
}

func Test_should_mark_mutations_needs_review_when_entity_has_mutations_against_several_accessions(t *testing.T) {
	// given
	desired := []models.PolymerEntity{polymerEntityWithMutations("1", nil, "", "")}
	mutations := archiveMutations{complete: true, sets: []cif.MutationSet{
		{EntityID: "1", Accession: "P11111", Mutations: []string{"C54T"}, Original: "C54T, A7G"},
		{EntityID: "1", Accession: "Q22222", Mutations: []string{"A7G"}, Original: "C54T, A7G"},
	}}

	// when
	result := applyArchiveMutations(desired, nil, mutations)

	// then
	assert.Equal(t, []string{"C54T", "A7G"}, result[0].Metadata.Mutations)
	assert.Equal(t, optionalString("C54T, A7G"), result[0].Metadata.MutationsRaw)
	assert.Equal(t, new(models.MutationsStatusNeedsReview), result[0].Metadata.MutationsStatus)
}

func Test_should_set_mutations_status_none_when_mmcif_set_names_no_mutation(t *testing.T) {
	// given
	desired := []models.PolymerEntity{polymerEntityWithMutations("1", []string{"C4T"}, "C4T", models.MutationsStatusParsed)}
	mutations := archiveMutations{complete: true, sets: []cif.MutationSet{{EntityID: "1", Mutations: []string{}}}}

	// when
	result := applyArchiveMutations(desired, nil, mutations)

	// then
	assert.Nil(t, result[0].Metadata.Mutations)
	assert.Nil(t, result[0].Metadata.MutationsRaw)
	assert.Equal(t, new(models.MutationsStatusNone), result[0].Metadata.MutationsStatus)
}

func Test_should_keep_rcsb_json_mutations_when_mmcif_has_no_set_for_entity(t *testing.T) {
	// given
	desired := []models.PolymerEntity{
		polymerEntityWithMutations("1", []string{"C4T"}, "C4T", models.MutationsStatusParsed),
		polymerEntityWithMutations("2", nil, "", ""),
	}
	mutations := archiveMutations{complete: true, sets: []cif.MutationSet{
		{EntityID: "2", Accession: "P11111", Mutations: []string{"A7G"}, Original: "A7G"},
		{Mutations: []string{}, Unmapped: []string{"row without entity"}, NeedsReview: true},
	}}

	// when
	result := applyArchiveMutations(desired, nil, mutations)

	// then
	assert.Equal(t, desired[0], result[0])
	assert.Equal(t, []string{"A7G"}, result[1].Metadata.Mutations)
}

func Test_should_keep_stored_mutations_when_mmcif_could_not_be_read_completely(t *testing.T) {
	// given
	desired := []models.PolymerEntity{
		polymerEntityWithMutations("1", []string{"C4T"}, "C4T", models.MutationsStatusParsed),
		polymerEntityWithMutations("2", []string{"A7G"}, "A7G", models.MutationsStatusParsed),
	}
	stored := []models.PolymerEntity{
		polymerEntityWithMutations("1", []string{"C54T"}, "C4T", models.MutationsStatusParsed),
	}

	// when
	result := applyArchiveMutations(desired, stored, archiveMutations{complete: false})

	// then
	assert.Equal(t, []string{"C54T"}, result[0].Metadata.Mutations)
	assert.Equal(t, optionalString("C4T"), result[0].Metadata.MutationsRaw)
	assert.Equal(t, desired[1], result[1])
}

func Test_should_keep_depositor_mutations_when_mmcif_maps_entity(t *testing.T) {
	// given
	desired := []models.PolymerEntity{polymerEntityWithMutations("1", []string{"C4T"}, "C4T", models.MutationsStatusParsed)}
	mutations := archiveMutations{complete: true, sets: []cif.MutationSet{{
		EntityID: "1", Accession: "P11111", Mutations: []string{"C54T"}, Original: "C4T",
	}}}
	depositor := []models.DepositorPolymerEntity{{LabelEntityID: "1", Mutations: []string{"C97A"}}}

	// when
	result := applyDepositorPolymerEntities(applyArchiveMutations(desired, nil, mutations), depositor)

	// then
	assert.Equal(t, []string{"C97A"}, result[0].Metadata.Mutations)
}

func Test_should_read_mutation_sets_when_rcsb_mmcif_is_complete(t *testing.T) {
	// given
	strategy, logs := entryStrategyServing(t, mmCIFWithMutation+"loop_\n_atom_site.group_PDB\nATOM\n#\n")

	// when
	mutations, err := strategy.readArchiveMutations(context.Background(), "1ABC")

	// then
	require.NoError(t, err)
	assert.True(t, mutations.complete)
	require.Len(t, mutations.sets, 1)
	assert.Equal(t, []string{"C54T"}, mutations.sets[0].Mutations)
	assert.Empty(t, logs.String())
}

func Test_should_warn_and_keep_stored_mutations_when_rcsb_mmcif_ends_before_coordinates(t *testing.T) {
	// given
	strategy, logs := entryStrategyServing(t, mmCIFWithMutation+"#"+string(bytes.Repeat([]byte(" "), int(cif.HeadReadLimit))))

	// when
	mutations, err := strategy.readArchiveMutations(context.Background(), "1ABC")

	// then
	require.NoError(t, err)
	assert.False(t, mutations.complete)
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), "pdb_id=1ABC")
}

func Test_should_return_error_when_rcsb_serves_no_mmcif(t *testing.T) {
	// given
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	strategy := &entrySyncStrategy{
		rcsbClient: rcsb.NewClient(rcsb.WithBaseURLs(server.URL, server.URL, server.URL, server.URL)),
		logger:     slog.New(slog.DiscardHandler),
	}

	// when
	_, err := strategy.readArchiveMutations(context.Background(), "1ABC")

	// then
	require.ErrorIs(t, err, rcsb.ErrNotFound)
}

const mmCIFWithMutation = "data_1ABC\n" +
	"loop_\n_entity.id\n_entity.type\n_entity.pdbx_mutation\n1 polymer C4T\n" +
	"_struct_ref.id 1\n_struct_ref.entity_id 1\n" +
	"_struct_ref_seq.align_id 1\n_struct_ref_seq.ref_id 1\n" +
	"loop_\n_struct_ref_seq_dif.align_id\n_struct_ref_seq_dif.mon_id\n_struct_ref_seq_dif.pdbx_seq_db_name\n" +
	"_struct_ref_seq_dif.pdbx_seq_db_accession_code\n_struct_ref_seq_dif.db_mon_id\n" +
	"_struct_ref_seq_dif.pdbx_seq_db_seq_num\n_struct_ref_seq_dif.details\n" +
	"1 THR UNP P11111 CYS 54 'engineered mutation'\n" +
	"#\n"

// entryStrategyServing returns an entry sync strategy whose RCSB serves contents as 1ABC's mmCIF,
// and the buffer its logs go to.
func entryStrategyServing(t *testing.T, contents string) (*entrySyncStrategy, *bytes.Buffer) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download/1ABC.cif" {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "1ABC.cif", time.Time{}, bytes.NewReader([]byte(contents)))
	}))
	t.Cleanup(server.Close)
	logs := &bytes.Buffer{}
	return &entrySyncStrategy{
		rcsbClient: rcsb.NewClient(rcsb.WithBaseURLs(server.URL, server.URL, server.URL, server.URL)),
		logger:     slog.New(slog.NewTextHandler(logs, nil)),
	}, logs
}

func polymerEntityWithMutations(
	labelEntityID string,
	mutations []string,
	raw string,
	status models.MutationsStatus,
) models.PolymerEntity {
	metadata := models.PolymerEntityMetadata{LabelEntityID: optionalString(labelEntityID), Mutations: mutations}
	if raw != "" {
		metadata.MutationsRaw = optionalString(raw)
	}
	if status != "" {
		metadata.MutationsStatus = new(status)
	}
	return models.PolymerEntity{Metadata: metadata}
}
